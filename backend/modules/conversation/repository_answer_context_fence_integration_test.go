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
	if ownerBeforeProvider.SchedulingAuthority != booking.SchedulingAuthorityOwnerManual {
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
	if externalBeforeHours.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider || !externalProviderAnswerContextReady(externalBeforeHours) ||
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

func TestPostgresManleAICalendarLightweightFenceTracksConfigurationDependencies(t *testing.T) {
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

	ownerA, salonA := insertConversationHoursTenant(t, ctx, db, "calendar-fence-a")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonA)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerA)
	})
	ownerB, salonB := insertConversationHoursTenant(t, ctx, db, "calendar-fence-b")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonB)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerB)
	})

	if _, err := db.ExecContext(ctx, `
		INSERT INTO manleai_calendar_configs (
			salon_id, slot_step_minutes, minimum_booking_notice_minutes,
			booking_horizon_days, max_party_size
		) VALUES ($1, 15, 30, 60, 4)
	`, salonA); err != nil {
		t.Fatalf("insert ManleAI Calendar config: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE salon_settings
		SET scheduling_authority = 'manleai_calendar'
		WHERE salon_id = $1
	`, salonA); err != nil {
		t.Fatalf("switch tenant A to ManleAI Calendar: %v", err)
	}

	repository := NewRepository(db)
	baselineA := mustAnswerContextFence(t, ctx, repository, salonA)
	baselineB := mustAnswerContextFence(t, ctx, repository, salonB)
	if baselineA.SchedulingAuthority != booking.SchedulingAuthorityManleAICalendar || baselineA.CalendarConfigVersion != 1 || baselineA.CalendarActivatedVersion != 0 {
		t.Fatalf("initial ManleAI Calendar fence = %#v", baselineA)
	}
	assertNoProviderAnswerContextEvidence(t, baselineA)

	var staffOnlyServiceID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO services (
			salon_id, pos_provider, pos_service_id, name, description,
			duration_minutes, price_from, ai_bookable, active, source, sync_status
		) VALUES ($1, 'square', $2, 'Soft Gel Structure', 'Structured gel service', 60, 72, true, true, 'local', 'local_only')
		RETURNING id::text
	`, salonA, "calendar-fence-staff-service-"+salonA).Scan(&staffOnlyServiceID); err != nil {
		t.Fatalf("insert staff-only service: %v", err)
	}
	afterStaffOnlyService := mustAnswerContextFence(t, ctx, repository, salonA)
	assertCalendarConfigFenceAdvanced(t, baselineA, afterStaffOnlyService, "staff-only service insert")

	var pooledServiceID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO services (
			salon_id, pos_provider, pos_service_id, name, description,
			duration_minutes, price_from, ai_bookable, active, source, sync_status
		) VALUES ($1, 'square', $2, 'Spa Pedicure Ritual', 'Pooled chair service', 75, 85, true, true, 'local', 'local_only')
		RETURNING id::text
	`, salonA, "calendar-fence-pooled-service-"+salonA).Scan(&pooledServiceID); err != nil {
		t.Fatalf("insert pooled service: %v", err)
	}
	afterPooledService := mustAnswerContextFence(t, ctx, repository, salonA)
	assertCalendarConfigFenceAdvanced(t, afterStaffOnlyService, afterPooledService, "pooled service insert")

	var staffID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO staff (
			salon_id, pos_provider, pos_staff_id, name, phone, email,
			ai_bookable, active, source, sync_status
		) VALUES ($1, 'square', $2, 'Ngoc Tran', '+13125550777', 'ngoc-calendar@example.test', true, true, 'local', 'local_only')
		RETURNING id::text
	`, salonA, "calendar-fence-staff-"+salonA).Scan(&staffID); err != nil {
		t.Fatalf("insert calendar staff: %v", err)
	}
	afterStaff := mustAnswerContextFence(t, ctx, repository, salonA)
	assertCalendarConfigFenceAdvanced(t, afterPooledService, afterStaff, "staff insert")

	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_business_hour_periods (
			salon_id, day_of_week, start_local_time, end_local_time, source, provider_period_index
		) VALUES ($1, 6, TIME '10:00', TIME '18:00', 'local_override', 1)
	`, salonA); err != nil {
		t.Fatalf("insert local business hours: %v", err)
	}
	afterHours := mustAnswerContextFence(t, ctx, repository, salonA)
	assertCalendarConfigFenceAdvanced(t, afterStaff, afterHours, "local hours insert")
	if afterHours.LocalBusinessHoursVersion != 0 {
		t.Fatalf("ManleAI Calendar leaked owner-manual hours fence: %#v", afterHours)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO manleai_calendar_service_policies (salon_id, service_id, enabled, capacity_mode)
		VALUES ($1, $2, true, 'staff_only'), ($1, $3, true, 'pooled')
	`, salonA, staffOnlyServiceID, pooledServiceID); err != nil {
		t.Fatalf("insert service policies: %v", err)
	}
	afterPolicies := mustAnswerContextFence(t, ctx, repository, salonA)
	assertCalendarConfigFenceAdvanced(t, afterHours, afterPolicies, "service policies insert")

	if _, err := db.ExecContext(ctx, `
		INSERT INTO manleai_calendar_service_staff (salon_id, service_id, staff_id)
		VALUES ($1, $2, $4), ($1, $3, $4)
	`, salonA, staffOnlyServiceID, pooledServiceID, staffID); err != nil {
		t.Fatalf("insert staff-service eligibility: %v", err)
	}
	afterEligibility := mustAnswerContextFence(t, ctx, repository, salonA)
	assertCalendarConfigFenceAdvanced(t, afterPolicies, afterEligibility, "staff-service eligibility insert")

	if _, err := db.ExecContext(ctx, `
		INSERT INTO manleai_calendar_staff_weekly_periods (
			salon_id, staff_id, day_of_week, start_minute, end_minute
		) VALUES ($1, $2, 6, 600, 1080)
	`, salonA, staffID); err != nil {
		t.Fatalf("insert staff weekly schedule: %v", err)
	}
	afterWeeklySchedule := mustAnswerContextFence(t, ctx, repository, salonA)
	assertCalendarConfigFenceAdvanced(t, afterEligibility, afterWeeklySchedule, "staff weekly schedule insert")

	var resourcePoolID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO manleai_calendar_resource_pools (salon_id, name, capacity)
		VALUES ($1, 'Pedicure chairs', 3)
		RETURNING id::text
	`, salonA).Scan(&resourcePoolID); err != nil {
		t.Fatalf("insert resource pool: %v", err)
	}
	afterResourcePool := mustAnswerContextFence(t, ctx, repository, salonA)
	assertCalendarConfigFenceAdvanced(t, afterWeeklySchedule, afterResourcePool, "resource pool insert")

	if _, err := db.ExecContext(ctx, `
		INSERT INTO manleai_calendar_service_resources (
			salon_id, service_id, resource_pool_id, units_required
		) VALUES ($1, $2, $3, 1)
	`, salonA, pooledServiceID, resourcePoolID); err != nil {
		t.Fatalf("insert service resource requirement: %v", err)
	}
	afterResourceRequirement := mustAnswerContextFence(t, ctx, repository, salonA)
	assertCalendarConfigFenceAdvanced(t, afterResourcePool, afterResourceRequirement, "service resource requirement insert")

	if _, err := db.ExecContext(ctx, `
		UPDATE manleai_calendar_configs
		SET activated_at = now(), activated_by_user_id = $2
		WHERE salon_id = $1
	`, salonA, ownerA); err != nil {
		t.Fatalf("activate ManleAI Calendar config: %v", err)
	}
	activated := mustAnswerContextFence(t, ctx, repository, salonA)
	if activated.CalendarConfigVersion <= afterResourceRequirement.CalendarConfigVersion || activated.CalendarActivatedVersion != activated.CalendarConfigVersion {
		t.Fatalf("activation fence is not current: before=%#v after=%#v", afterResourceRequirement, activated)
	}
	evidence, err := repository.GetManleAICalendarAnswerContextEvidence(ctx, salonA)
	if err != nil {
		t.Fatalf("load authoritative calendar answer-context evidence: %v", err)
	}
	if !evidence.Ready || !manleAICalendarEvidenceMatchesFence(evidence, activated) {
		t.Fatalf("authoritative evidence does not match activated lightweight fence: fence=%#v evidence=%#v", activated, evidence)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO manleai_calendar_exceptions (
			salon_id, scope_type, resource_pool_id, effect, starts_at, ends_at,
			capacity_override, reason, created_by_user_id
		) VALUES (
			$1, 'resource', $2, 'capacity_override', now() + interval '2 days',
			now() + interval '2 days 1 hour', 2, 'Reduced chair capacity', $3
		)
	`, salonA, resourcePoolID, ownerA); err != nil {
		t.Fatalf("insert resource exception: %v", err)
	}
	staleActivation := mustAnswerContextFence(t, ctx, repository, salonA)
	assertCalendarConfigFenceAdvanced(t, activated, staleActivation, "resource exception insert")
	if staleActivation.CalendarActivatedVersion != activated.CalendarActivatedVersion || staleActivation.CalendarActivatedVersion == staleActivation.CalendarConfigVersion {
		t.Fatalf("configuration mutation did not expose stale activation: before=%#v after=%#v", activated, staleActivation)
	}
	staleEvidence, err := repository.GetManleAICalendarAnswerContextEvidence(ctx, salonA)
	if err != nil {
		t.Fatalf("load stale authoritative calendar evidence: %v", err)
	}
	if staleEvidence.Ready || !manleAICalendarEvidenceMatchesFence(staleEvidence, staleActivation) {
		t.Fatalf("stale activation evidence = %#v for fence %#v", staleEvidence, staleActivation)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections (
			salon_id, provider, status, location_id, snapshot_generation, last_sync_at
		) VALUES ($1, 'square', 'active', 'calendar-irrelevant-location', 12, now())
	`, salonA); err != nil {
		t.Fatalf("insert irrelevant provider connection: %v", err)
	}
	afterProvider := mustAnswerContextFence(t, ctx, repository, salonA)
	if afterProvider != staleActivation {
		t.Fatalf("provider evidence changed ManleAI Calendar fence: before=%#v after=%#v", staleActivation, afterProvider)
	}

	finalB := mustAnswerContextFence(t, ctx, repository, salonB)
	if finalB != baselineB {
		t.Fatalf("tenant A calendar mutations changed tenant B fence: before=%#v after=%#v", baselineB, finalB)
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

func assertCalendarConfigFenceAdvanced(t *testing.T, before AnswerContextFence, after AnswerContextFence, mutation string) {
	t.Helper()
	if after.CalendarConfigVersion <= before.CalendarConfigVersion {
		t.Fatalf("calendar config version did not advance after %s: before=%#v after=%#v", mutation, before, after)
	}
}
