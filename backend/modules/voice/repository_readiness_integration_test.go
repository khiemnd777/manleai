package voice

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/pos"
)

func TestPhoneReadinessSeparatesGuidanceFromBookingSnapshotFence(t *testing.T) {
	databaseURL := os.Getenv("VOICE_READINESS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("VOICE_READINESS_TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	suffix := uuid.NewString()
	var ownerID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, full_name)
		VALUES ($1, 'integration-test', 'Voice Readiness Test')
		RETURNING id::text
	`, "voice-readiness-"+suffix+"@example.test").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons (name, phone, owner_user_id, active_pos_provider, ai_enabled)
		VALUES ('Voice Readiness Test Salon', '+13125550188', $1, 'square', true)
		RETURNING id::text
	`, ownerID).Scan(&salonID); err != nil {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID)
		t.Fatalf("insert salon: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID)
	})
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_settings (salon_id, consultation_enabled)
		VALUES ($1, true)
	`, salonID); err != nil {
		t.Fatalf("insert salon settings: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections (salon_id, provider, status, location_id)
		VALUES ($1, 'square', 'connected', 'voice-readiness-location')
	`, salonID); err != nil {
		t.Fatalf("insert POS connection: %v", err)
	}

	posRepository := pos.NewRepository(db)
	generation, err := posRepository.BeginProviderSnapshot(ctx, salonID, pos.ProviderSquare, "voice-readiness-location")
	if err != nil {
		t.Fatalf("begin provider snapshot: %v", err)
	}
	providerServiceID := "voice-readiness-service-" + suffix
	providerStaffID := "voice-readiness-staff-" + suffix
	if _, err := posRepository.ApplyProviderSnapshot(ctx, salonID, pos.ProviderSnapshot{
		Provider: pos.ProviderSquare, LocationID: "voice-readiness-location", Generation: generation,
		Services: []pos.Service{{
			POSProvider: pos.ProviderSquare, POSServiceID: providerServiceID, POSServiceVersion: 4,
			Name: "Voice Readiness Manicure", DurationMinutes: 45, AIBookable: true, Active: true,
		}},
		Staff: []pos.StaffMember{{
			POSProvider: pos.ProviderSquare, POSStaffID: providerStaffID,
			Name: "Voice Readiness Artist", AIBookable: true, Active: true,
		}},
	}); err != nil {
		t.Fatalf("apply provider snapshot: %v", err)
	}
	if err := posRepository.MarkSyncCompleteForGeneration(ctx, salonID, pos.ProviderSquare, generation, pos.StatusActive, ""); err != nil {
		t.Fatalf("complete provider snapshot: %v", err)
	}
	var serviceID string
	if err := db.QueryRowContext(ctx, `
		SELECT entity_id::text FROM pos_entity_links
		WHERE salon_id = $1 AND entity_type = 'service' AND provider = 'square' AND provider_entity_id = $2
	`, salonID, providerServiceID).Scan(&serviceID); err != nil {
		t.Fatalf("load service ID: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO service_consultation_profiles (
			salon_id, service_id, status, recommended_outcomes, compatible_current_systems,
			owner_approved_summary, updated_by
		)
		VALUES ($1, $2, 'ready', '["maintain"]'::jsonb, '["natural"]'::jsonb, 'Owner-approved guidance.', $3)
	`, salonID, serviceID, ownerID); err != nil {
		t.Fatalf("insert consultation profile: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_business_hour_periods (
			salon_id, day_of_week, start_local_time, end_local_time, source, provider,
			provider_location_id, provider_period_index, last_synced_at
		)
		VALUES ($1, 1, '09:00', '17:00', 'imported', 'square', 'voice-readiness-location', 0, now())
	`, salonID); err != nil {
		t.Fatalf("insert business hours: %v", err)
	}

	repository := NewRepository(db)
	voiceStatus, err := repository.GetSalonVoiceStatus(ctx, salonID, ownerID)
	if err != nil {
		t.Fatalf("voice scheduling status: %v", err)
	}
	if !voiceStatus.AIEnabled || voiceStatus.SchedulingAuthority != "external_provider" || voiceStatus.SchedulingAuthorityVersion != 1 || voiceStatus.BookingMode != "pending_approval" {
		t.Fatalf("voice scheduling status = %#v", voiceStatus)
	}
	if _, err := repository.GetSalonVoiceStatus(ctx, salonID, uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner voice status error = %v, want ErrNotFound", err)
	}
	ready, err := repository.GetPhoneBookingReadiness(ctx, salonID, ownerID)
	if err != nil {
		t.Fatalf("ready status: %v", err)
	}
	if ready.GuidanceServiceCount != 1 || ready.ServiceCount != 1 || ready.StaffCount != 1 || ready.ConsultationReadyServices != 1 ||
		ready.ServiceGuidance.Status != conversation.ServiceGuidanceCapabilityRecommendationReady {
		t.Fatalf("ready status = %#v", ready)
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE pos_connections SET snapshot_generation = 0 WHERE salon_id = $1 AND provider = 'square'
	`, salonID); err != nil {
		t.Fatalf("invalidate snapshot fence: %v", err)
	}
	blocked, err := repository.GetPhoneBookingReadiness(ctx, salonID, ownerID)
	if err != nil {
		t.Fatalf("blocked status: %v", err)
	}
	if blocked.ProviderSynced || blocked.GuidanceServiceCount != 1 || blocked.ServiceCount != 0 || blocked.StaffCount != 0 ||
		blocked.ConsultationReadyServices != 1 || blocked.ServiceGuidance.Status != conversation.ServiceGuidanceCapabilityRecommendationReady ||
		!blocked.ServiceGuidance.CatalogAvailable || !blocked.ServiceGuidance.RecommendationReady {
		t.Fatalf("blocked status = %#v", blocked)
	}
}
