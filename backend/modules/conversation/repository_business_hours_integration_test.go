package conversation

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/booking"
)

func TestPostgresAuthoritySpecificBusinessHoursSourcesAndOwnerCacheFence(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	ownerA, salonA := insertConversationHoursTenant(t, ctx, db, "a")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonA)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerA)
	})
	ownerB, salonB := insertConversationHoursTenant(t, ctx, db, "b")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonB)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerB)
	})

	repository := NewRepository(db)
	initialFence, err := repository.GetAnswerContextFence(ctx, salonA)
	if err != nil {
		t.Fatalf("load initial owner answer-context fence: %v", err)
	}
	if initialFence.SchedulingAuthority != booking.SchedulingAuthorityOwnerManual || !initialFence.Ready || initialFence.LocalBusinessHoursVersion < 1 {
		t.Fatalf("initial owner answer-context fence = %#v", initialFence)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_business_hour_periods (
			salon_id, day_of_week, start_local_time, end_local_time,
			end_at_midnight, source, provider_period_index
		) VALUES
			($1, 4, TIME '09:00', TIME '12:00', false, 'local_override', 1),
			($1, 4, TIME '13:00', TIME '18:00', false, 'local_override', 2),
			($1, 6, TIME '18:00', TIME '00:00', true, 'local_override', 1)
	`, salonA); err != nil {
		t.Fatalf("insert tenant A local hours: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_business_hour_periods (
			salon_id, day_of_week, start_local_time, end_local_time,
			source, provider_period_index
		) VALUES ($1, 4, TIME '10:00', TIME '16:00', 'local_override', 1)
	`, salonB); err != nil {
		t.Fatalf("insert tenant B local hours: %v", err)
	}

	afterInsertFence, err := repository.GetAnswerContextFence(ctx, salonA)
	if err != nil {
		t.Fatalf("load owner fence after local-hours insert: %v", err)
	}
	if afterInsertFence.LocalBusinessHoursVersion <= initialFence.LocalBusinessHoursVersion {
		t.Fatalf("local-hours fence did not advance: before=%d after=%d", initialFence.LocalBusinessHoursVersion, afterInsertFence.LocalBusinessHoursVersion)
	}

	ownerHours, err := repository.ListOwnerManagedBusinessHourPeriods(ctx, salonA)
	if err != nil {
		t.Fatalf("list tenant A owner-managed hours: %v", err)
	}
	if len(ownerHours) != 3 || ownerHours[0].DayOfWeek != 4 || ownerHours[0].StartLocalTime != "09:00:00" || ownerHours[1].StartLocalTime != "13:00:00" || ownerHours[2].EndLocalTime != "24:00:00" {
		t.Fatalf("tenant A owner-managed hours = %#v", ownerHours)
	}
	for _, period := range ownerHours {
		if period.Source != "local_override" || period.Provider != "" {
			t.Fatalf("owner-managed period contains provider evidence: %#v", period)
		}
	}
	ownerHoursB, err := repository.ListOwnerManagedBusinessHourPeriods(ctx, salonB)
	if err != nil {
		t.Fatalf("list tenant B owner-managed hours: %v", err)
	}
	if len(ownerHoursB) != 1 || ownerHoursB[0].DayOfWeek != 4 || ownerHoursB[0].StartLocalTime != "10:00:00" || ownerHoursB[0].EndLocalTime != "16:00:00" {
		t.Fatalf("tenant B owner-managed hours = %#v", ownerHoursB)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections (
			salon_id, provider, status, location_id, snapshot_generation, last_sync_at
		) VALUES ($1, 'square', 'active', 'external-location-a', 7, now())
	`, salonA); err != nil {
		t.Fatalf("insert tenant A active provider connection: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_business_hour_periods (
			salon_id, day_of_week, start_local_time, end_local_time,
			source, provider, provider_location_id, provider_period_index, last_synced_at
		) VALUES ($1, 2, TIME '08:00', TIME '20:00', 'imported', 'square', 'external-location-a', 1, now())
	`, salonA); err != nil {
		t.Fatalf("insert tenant A provider-imported hours: %v", err)
	}

	externalHours, err := repository.ListExternalProviderBusinessHourPeriods(ctx, salonA)
	if err != nil {
		t.Fatalf("list tenant A external-provider hours: %v", err)
	}
	if len(externalHours) != 1 || externalHours[0].Source != "imported" || externalHours[0].Provider != "square" || externalHours[0].StartLocalTime != "08:00:00" {
		t.Fatalf("tenant A external-provider hours = %#v", externalHours)
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE salon_business_hour_periods
		SET end_local_time = TIME '18:30'
		WHERE salon_id = $1 AND source = 'local_override' AND day_of_week = 4 AND provider_period_index = 2
	`, salonA); err != nil {
		t.Fatalf("update tenant A local hours: %v", err)
	}
	afterUpdateFence, err := repository.GetAnswerContextFence(ctx, salonA)
	if err != nil {
		t.Fatalf("load owner fence after local-hours update: %v", err)
	}
	if afterUpdateFence.LocalBusinessHoursVersion <= afterInsertFence.LocalBusinessHoursVersion {
		t.Fatalf("local-hours fence did not advance after update: before=%d after=%d", afterInsertFence.LocalBusinessHoursVersion, afterUpdateFence.LocalBusinessHoursVersion)
	}
}

func insertConversationHoursTenant(t *testing.T, ctx context.Context, db *sql.DB, label string) (string, string) {
	t.Helper()
	suffix := uuid.NewString()
	var ownerID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, full_name)
		VALUES ($1, 'integration-test', $2)
		RETURNING id::text
	`, "conversation-hours-"+label+"-"+suffix+"@example.com", "Conversation Hours Owner "+label).Scan(&ownerID); err != nil {
		t.Fatalf("insert tenant %s owner: %v", label, err)
	}

	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons (name, phone, owner_user_id)
		VALUES ($1, '+13125550777', $2)
		RETURNING id::text
	`, "Conversation Hours Salon "+label+" "+suffix, ownerID).Scan(&salonID); err != nil {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID)
		t.Fatalf("insert tenant %s salon: %v", label, err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_settings (salon_id, scheduling_authority, booking_mode)
		VALUES ($1, 'owner_manual', 'pending_approval')
	`, salonID); err != nil {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID)
		t.Fatalf("insert tenant %s settings: %v", label, err)
	}
	return ownerID, salonID
}
