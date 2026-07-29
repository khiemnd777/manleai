package conversation

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/database"
	"github.com/manleai/ai-receptionist/modules/booking"
)

func TestPostgresAnswerContextFenceTracksConsumedCollectionsAndTenantIsolation(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	ownerA, salonA := insertConversationHoursTenant(t, ctx, db, "answer-fence-a")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonA)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerA)
	})
	ownerB, salonB := insertConversationHoursTenant(t, ctx, db, "answer-fence-b")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonB)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerB)
	})

	repository := NewRepository(db)
	initialA := mustAnswerContextFence(t, ctx, repository, salonA)
	initialB := mustAnswerContextFence(t, ctx, repository, salonB)
	if initialA.ServiceCatalogVersion < 1 || initialA.ServiceAliasesVersion < 1 || initialA.ServiceCategoriesVersion < 1 ||
		initialA.ConsultationProfilesVersion < 1 || initialA.StaffCatalogVersion < 1 || initialA.KnowledgeBaseVersion < 1 {
		t.Fatalf("new salon did not receive all common collection fences: %#v", initialA)
	}

	var serviceID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO services (
			salon_id, pos_provider, pos_service_id, name, description,
			duration_minutes, price_from, ai_bookable, active, source, sync_status
		) VALUES ($1, 'square', $2, 'Structured Overlay', 'Natural nail reinforcement', 70, 68, true, true, 'local', 'local_only')
		RETURNING id::text
	`, salonA, "answer-fence-service-"+salonA).Scan(&serviceID); err != nil {
		t.Fatalf("insert tenant A service: %v", err)
	}
	afterService := mustAnswerContextFence(t, ctx, repository, salonA)
	if afterService.ServiceCatalogVersion <= initialA.ServiceCatalogVersion {
		t.Fatalf("service collection version did not advance: before=%d after=%d", initialA.ServiceCatalogVersion, afterService.ServiceCatalogVersion)
	}

	if _, err := db.ExecContext(ctx, `UPDATE services SET name = 'Structured Builder Overlay' WHERE id = $1 AND salon_id = $2`, serviceID, salonA); err != nil {
		t.Fatalf("update tenant A service answer field: %v", err)
	}
	afterServiceUpdate := mustAnswerContextFence(t, ctx, repository, salonA)
	if afterServiceUpdate.ServiceCatalogVersion <= afterService.ServiceCatalogVersion {
		t.Fatalf("service collection version did not advance after answer-field update: before=%d after=%d", afterService.ServiceCatalogVersion, afterServiceUpdate.ServiceCatalogVersion)
	}
	if _, err := db.ExecContext(ctx, `UPDATE services SET sync_error = 'diagnostic-only' WHERE id = $1 AND salon_id = $2`, serviceID, salonA); err != nil {
		t.Fatalf("update tenant A service diagnostic-only field: %v", err)
	}
	afterServiceDiagnostic := mustAnswerContextFence(t, ctx, repository, salonA)
	if afterServiceDiagnostic.ServiceCatalogVersion != afterServiceUpdate.ServiceCatalogVersion {
		t.Fatalf("diagnostic-only service update changed answer-context fence: before=%d after=%d", afterServiceUpdate.ServiceCatalogVersion, afterServiceDiagnostic.ServiceCatalogVersion)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO service_aliases (salon_id, service_id, alias, normalized_alias, source, status)
		VALUES ($1, $2, 'Builder refill', $3, 'owner', 'active')
	`, salonA, serviceID, "builder-refill-"+salonA); err != nil {
		t.Fatalf("insert tenant A service alias: %v", err)
	}
	afterAlias := mustAnswerContextFence(t, ctx, repository, salonA)
	if afterAlias.ServiceAliasesVersion <= afterServiceUpdate.ServiceAliasesVersion {
		t.Fatalf("service-alias collection version did not advance: before=%d after=%d", afterServiceUpdate.ServiceAliasesVersion, afterAlias.ServiceAliasesVersion)
	}

	var categoryID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO service_categories (salon_id, name, slug, source)
		VALUES ($1, 'Nail Enhancements', $2, 'manual')
		RETURNING id::text
	`, salonA, "nail-enhancements-"+salonA).Scan(&categoryID); err != nil {
		t.Fatalf("insert tenant A service category: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO service_category_aliases (salon_id, category_id, alias, normalized_alias, source, status)
		VALUES ($1, $2, 'Overlay services', $3, 'owner', 'active')
	`, salonA, categoryID, "overlay-services-"+salonA); err != nil {
		t.Fatalf("insert tenant A service-category alias: %v", err)
	}
	afterCategory := mustAnswerContextFence(t, ctx, repository, salonA)
	if afterCategory.ServiceCategoriesVersion <= afterAlias.ServiceCategoriesVersion {
		t.Fatalf("service-category collection version did not advance: before=%d after=%d", afterAlias.ServiceCategoriesVersion, afterCategory.ServiceCategoriesVersion)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO service_consultation_profiles (
			salon_id, service_id, status, recommended_outcomes,
			compatible_current_systems, owner_approved_summary
		) VALUES (
			$1, $2, 'ready', '["natural_nail_support"]'::jsonb,
			'["natural_nails"]'::jsonb, 'Adds strength while preserving a natural look.'
		)
	`, salonA, serviceID); err != nil {
		t.Fatalf("insert tenant A consultation profile: %v", err)
	}
	afterConsultation := mustAnswerContextFence(t, ctx, repository, salonA)
	if afterConsultation.ConsultationProfilesVersion <= afterCategory.ConsultationProfilesVersion {
		t.Fatalf("consultation-profile collection version did not advance: before=%d after=%d", afterCategory.ConsultationProfilesVersion, afterConsultation.ConsultationProfilesVersion)
	}

	var staffID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO staff (
			salon_id, pos_provider, pos_staff_id, name, phone, email,
			ai_bookable, active, source, sync_status
		) VALUES ($1, 'square', $2, 'Anh Le', '+13125550123', 'anh@example.test', true, true, 'local', 'local_only')
		RETURNING id::text
	`, salonA, "answer-fence-staff-"+salonA).Scan(&staffID); err != nil {
		t.Fatalf("insert tenant A staff: %v", err)
	}
	afterStaff := mustAnswerContextFence(t, ctx, repository, salonA)
	if afterStaff.StaffCatalogVersion <= afterConsultation.StaffCatalogVersion {
		t.Fatalf("staff collection version did not advance: before=%d after=%d", afterConsultation.StaffCatalogVersion, afterStaff.StaffCatalogVersion)
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE staff
		SET phone = '+13125550999', email = 'updated-anh@example.test'
		WHERE id = $1 AND salon_id = $2
	`, staffID, salonA); err != nil {
		t.Fatalf("update tenant A staff contact-only fields: %v", err)
	}
	afterStaffContact := mustAnswerContextFence(t, ctx, repository, salonA)
	if afterStaffContact.StaffCatalogVersion != afterStaff.StaffCatalogVersion {
		t.Fatalf("contact-only staff update changed answer-context fence: before=%d after=%d", afterStaff.StaffCatalogVersion, afterStaffContact.StaffCatalogVersion)
	}
	if _, err := db.ExecContext(ctx, `UPDATE staff SET name = 'Anh L.' WHERE id = $1 AND salon_id = $2`, staffID, salonA); err != nil {
		t.Fatalf("update tenant A staff answer field: %v", err)
	}
	afterStaffName := mustAnswerContextFence(t, ctx, repository, salonA)
	if afterStaffName.StaffCatalogVersion <= afterStaffContact.StaffCatalogVersion {
		t.Fatalf("staff collection version did not advance after name update: before=%d after=%d", afterStaffContact.StaffCatalogVersion, afterStaffName.StaffCatalogVersion)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO knowledge_items (salon_id, title, category, body, status, source)
		VALUES ($1, 'Gift cards', 'faq', 'Digital gift cards are available.', 'active', 'owner')
	`, salonA); err != nil {
		t.Fatalf("insert tenant A knowledge: %v", err)
	}
	afterKnowledge := mustAnswerContextFence(t, ctx, repository, salonA)
	if afterKnowledge.KnowledgeBaseVersion <= afterStaffName.KnowledgeBaseVersion {
		t.Fatalf("knowledge collection version did not advance: before=%d after=%d", afterStaffName.KnowledgeBaseVersion, afterKnowledge.KnowledgeBaseVersion)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM staff WHERE id = $1 AND salon_id = $2`, staffID, salonA); err != nil {
		t.Fatalf("delete tenant A staff: %v", err)
	}
	afterStaffDelete := mustAnswerContextFence(t, ctx, repository, salonA)
	if afterStaffDelete.StaffCatalogVersion <= afterKnowledge.StaffCatalogVersion {
		t.Fatalf("staff collection version did not advance after delete: before=%d after=%d", afterKnowledge.StaffCatalogVersion, afterStaffDelete.StaffCatalogVersion)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM services WHERE id = $1 AND salon_id = $2`, serviceID, salonA); err != nil {
		t.Fatalf("delete tenant A service: %v", err)
	}
	afterServiceDelete := mustAnswerContextFence(t, ctx, repository, salonA)
	if afterServiceDelete.ServiceCatalogVersion <= afterStaffDelete.ServiceCatalogVersion {
		t.Fatalf("service collection version did not advance after delete: before=%d after=%d", afterStaffDelete.ServiceCatalogVersion, afterServiceDelete.ServiceCatalogVersion)
	}

	finalB := mustAnswerContextFence(t, ctx, repository, salonB)
	if finalB != initialB {
		t.Fatalf("tenant A mutations changed tenant B answer-context fence: before=%#v after=%#v", initialB, finalB)
	}
}

func TestPostgresAnswerContextFenceNormalizesAuthoritySpecificEvidence(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	ownerID, salonID := insertConversationHoursTenant(t, ctx, db, "authority-normalization")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID)
	})
	repository := NewRepository(db)

	ownerBeforeProvider := mustAnswerContextFence(t, ctx, repository, salonID)
	if ownerBeforeProvider.SchedulingAuthority != booking.SchedulingAuthorityOwnerManual || !ownerBeforeProvider.Ready {
		t.Fatalf("initial owner-manual fence = %#v", ownerBeforeProvider)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections (
			salon_id, provider, status, location_id, snapshot_generation, last_sync_at
		) VALUES ($1, 'square', 'active', 'normalization-location', 8, now())
	`, salonID); err != nil {
		t.Fatalf("insert active provider connection: %v", err)
	}
	ownerAfterProvider := mustAnswerContextFence(t, ctx, repository, salonID)
	if ownerAfterProvider != ownerBeforeProvider {
		t.Fatalf("provider connection changed owner-manual fence: before=%#v after=%#v", ownerBeforeProvider, ownerAfterProvider)
	}
	assertNoProviderAnswerContextEvidence(t, ownerAfterProvider)

	if _, err := db.ExecContext(ctx, `
		UPDATE salon_settings
		SET scheduling_authority = 'external_provider', booking_mode = 'confirmed_booking'
		WHERE salon_id = $1
	`, salonID); err != nil {
		t.Fatalf("switch to external-provider authority: %v", err)
	}
	externalBeforeHours := mustAnswerContextFence(t, ctx, repository, salonID)
	if externalBeforeHours.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider || !externalBeforeHours.Ready ||
		externalBeforeHours.ActiveProvider != "square" || externalBeforeHours.LocationID != "normalization-location" || externalBeforeHours.SnapshotGeneration != 8 ||
		externalBeforeHours.LocalBusinessHoursVersion != 0 {
		t.Fatalf("external-provider fence = %#v", externalBeforeHours)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_business_hour_periods (
			salon_id, day_of_week, start_local_time, end_local_time, source, provider_period_index
		) VALUES ($1, 3, TIME '11:00', TIME '19:00', 'local_override', 1)
	`, salonID); err != nil {
		t.Fatalf("insert local hours while external-provider authority is selected: %v", err)
	}
	externalAfterHours := mustAnswerContextFence(t, ctx, repository, salonID)
	if externalAfterHours != externalBeforeHours {
		t.Fatalf("local hours changed external-provider fence: before=%#v after=%#v", externalBeforeHours, externalAfterHours)
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE salon_settings
		SET scheduling_authority = 'manleai_calendar'
		WHERE salon_id = $1
	`, salonID); err != nil {
		t.Fatalf("switch to ManleAI Calendar authority: %v", err)
	}
	calendarBeforeProviderChange := mustAnswerContextFence(t, ctx, repository, salonID)
	if calendarBeforeProviderChange.SchedulingAuthority != booking.SchedulingAuthorityManleAICalendar || calendarBeforeProviderChange.LocalBusinessHoursVersion != 0 {
		t.Fatalf("ManleAI Calendar fence = %#v", calendarBeforeProviderChange)
	}
	assertNoProviderAnswerContextEvidence(t, calendarBeforeProviderChange)
	if _, err := db.ExecContext(ctx, `
		UPDATE pos_connections
		SET status = 'syncing', location_id = 'irrelevant-location', snapshot_generation = 9, last_sync_at = now()
		WHERE salon_id = $1 AND provider = 'square'
	`, salonID); err != nil {
		t.Fatalf("change irrelevant provider evidence under ManleAI Calendar authority: %v", err)
	}
	calendarAfterProviderChange := mustAnswerContextFence(t, ctx, repository, salonID)
	if calendarAfterProviderChange != calendarBeforeProviderChange {
		t.Fatalf("provider state changed ManleAI Calendar fence: before=%#v after=%#v", calendarBeforeProviderChange, calendarAfterProviderChange)
	}
}

func mustAnswerContextFence(t *testing.T, ctx context.Context, repository *Repository, salonID string) AnswerContextFence {
	t.Helper()
	fence, err := repository.GetAnswerContextFence(ctx, salonID)
	if err != nil {
		t.Fatalf("load answer-context fence for salon %s: %v", salonID, err)
	}
	return fence
}

func assertNoProviderAnswerContextEvidence(t *testing.T, fence AnswerContextFence) {
	t.Helper()
	if fence.ActiveProvider != "" || fence.ConnectionStatus != "" || fence.LocationID != "" || fence.SnapshotGeneration != 0 || fence.LastSyncAtRFC3339 != "" {
		t.Fatalf("authority-irrelevant provider evidence leaked into answer-context fence: %#v", fence)
	}
}
