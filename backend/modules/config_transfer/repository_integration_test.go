package configtransfer

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/salon"
	"github.com/manleai/ai-receptionist/modules/scheduling/fence"
)

func TestRepositoryPostgresImportReplayTenantAndAuthoritySwitchFence(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(8)
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	ownerID := insertConfigurationTransferTestUser(t, ctx, db, "configuration-owner")
	otherOwnerID := insertConfigurationTransferTestUser(t, ctx, db, "configuration-other")
	onboardingOwnerID := insertConfigurationTransferTestUser(t, ctx, db, "configuration-onboarding")
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons (name, phone, owner_user_id, timezone, active_pos_provider, ai_enabled)
		VALUES ('Configuration Transfer Fence', '+13125550771', $1, 'America/Chicago', 'square', false)
		RETURNING id::text
	`, ownerID).Scan(&salonID); err != nil {
		t.Fatalf("insert salon: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE owner_user_id = $1`, onboardingOwnerID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1, $2, $3)`, ownerID, otherOwnerID, onboardingOwnerID)
	})
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_settings (salon_id, ai_greeting, booking_mode, scheduling_authority)
		VALUES ($1, 'Original greeting', 'pending_approval', 'owner_manual')
	`, salonID); err != nil {
		t.Fatalf("insert settings: %v", err)
	}

	repository := NewRepository(db)
	firstPlan := configurationTransferAIPlan(salonID, "configuration-replay-"+uuid.NewString(), "same-payload", booking.SchedulingAuthorityOwnerManual, 1, "Imported greeting")
	firstRunID, replayed, err := repository.ApplyImport(ctx, salonID, ownerID, firstPlan)
	if err != nil || replayed || firstRunID == "" {
		t.Fatalf("first apply run=%q replayed=%t err=%v", firstRunID, replayed, err)
	}
	secondRunID, replayed, err := repository.ApplyImport(ctx, salonID, ownerID, firstPlan)
	if err != nil || !replayed || secondRunID != firstRunID {
		t.Fatalf("exact replay run=%q replayed=%t err=%v, want %q", secondRunID, replayed, err, firstRunID)
	}
	changed := *firstPlan
	changed.PayloadFingerprint = "changed-payload"
	if _, _, err := repository.ApplyImport(ctx, salonID, ownerID, &changed); !errors.Is(err, ErrImportConflict) {
		t.Fatalf("changed request-id reuse error=%v, want ErrImportConflict", err)
	}
	concurrentPlan := configurationTransferAIPlan(salonID, "configuration-concurrent-"+uuid.NewString(), "concurrent-payload", booking.SchedulingAuthorityOwnerManual, 1, "Concurrent imported greeting")
	type applyResult struct {
		runID    string
		replayed bool
		err      error
	}
	concurrentResults := make(chan applyResult, 2)
	for range 2 {
		go func() {
			runID, wasReplay, applyErr := repository.ApplyImport(context.Background(), salonID, ownerID, concurrentPlan)
			concurrentResults <- applyResult{runID: runID, replayed: wasReplay, err: applyErr}
		}()
	}
	left := <-concurrentResults
	right := <-concurrentResults
	if left.err != nil || right.err != nil || left.runID == "" || left.runID != right.runID || left.replayed == right.replayed {
		t.Fatalf("concurrent exact replay left=%#v right=%#v", left, right)
	}
	otherTenantPlan := configurationTransferAIPlan(salonID, "configuration-other-"+uuid.NewString(), "other-payload", booking.SchedulingAuthorityOwnerManual, 1, "Other owner greeting")
	if _, _, err := repository.ApplyImport(ctx, salonID, otherOwnerID, otherTenantPlan); !errors.Is(err, salon.ErrNotFound) {
		t.Fatalf("cross-tenant apply error=%v, want salon.ErrNotFound", err)
	}

	type onboardingResult struct {
		salonID string
		runID   string
		err     error
	}
	onboardingResults := make(chan onboardingResult, 2)
	for index := range 2 {
		plan := configurationTransferOnboardingPlan("onboarding-concurrent-"+uuid.NewString(), "onboarding-payload-"+string(rune('a'+index)))
		go func() {
			createdSalonID, runID, _, applyErr := repository.ApplyOnboardingImport(context.Background(), onboardingOwnerID, plan)
			onboardingResults <- onboardingResult{salonID: createdSalonID, runID: runID, err: applyErr}
		}()
	}
	onboardingLeft := <-onboardingResults
	onboardingRight := <-onboardingResults
	successCount := 0
	conflictCount := 0
	for _, result := range []onboardingResult{onboardingLeft, onboardingRight} {
		switch {
		case result.err == nil && result.salonID != "" && result.runID != "":
			successCount++
		case errors.Is(result.err, ErrOnboardingSalonExists):
			conflictCount++
		default:
			t.Fatalf("unexpected concurrent onboarding result: %#v", result)
		}
	}
	if successCount != 1 || conflictCount != 1 {
		t.Fatalf("concurrent onboarding results left=%#v right=%#v", onboardingLeft, onboardingRight)
	}
	var onboardingSalonCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM salons WHERE owner_user_id = $1`, onboardingOwnerID).Scan(&onboardingSalonCount); err != nil {
		t.Fatalf("count onboarding salons: %v", err)
	}
	if onboardingSalonCount != 1 {
		t.Fatalf("concurrent onboarding created %d salons, want exactly one", onboardingSalonCount)
	}

	var currentVersion int64
	if err := db.QueryRowContext(ctx, `SELECT scheduling_authority_version FROM salon_settings WHERE salon_id = $1`, salonID).Scan(&currentVersion); err != nil {
		t.Fatalf("load authority version: %v", err)
	}
	stalePlan := configurationTransferAIPlan(salonID, "configuration-fence-"+uuid.NewString(), "fence-payload", booking.SchedulingAuthorityOwnerManual, currentVersion, "Must not apply")
	switchTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin simulated authority switch: %v", err)
	}
	if _, err := switchTx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, fence.AdvisoryKey(salonID)); err != nil {
		_ = switchTx.Rollback()
		t.Fatalf("lock shared scheduling fence: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, _, applyErr := repository.ApplyImport(context.Background(), salonID, ownerID, stalePlan)
		result <- applyErr
	}()
	select {
	case err := <-result:
		_ = switchTx.Rollback()
		t.Fatalf("configuration apply bypassed held scheduling fence: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if _, err := switchTx.ExecContext(ctx, `UPDATE salon_settings SET scheduling_authority = 'manleai_calendar' WHERE salon_id = $1`, salonID); err != nil {
		_ = switchTx.Rollback()
		t.Fatalf("simulate authority switch: %v", err)
	}
	if err := switchTx.Commit(); err != nil {
		t.Fatalf("commit simulated authority switch: %v", err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrAuthorityChanged) {
			t.Fatalf("apply after concurrent switch error=%v, want ErrAuthorityChanged", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("configuration apply did not resume after authority switch released the shared fence")
	}
	var greeting string
	if err := db.QueryRowContext(ctx, `SELECT ai_greeting FROM salon_settings WHERE salon_id = $1`, salonID).Scan(&greeting); err != nil {
		t.Fatalf("load greeting: %v", err)
	}
	if greeting != "Concurrent imported greeting" {
		t.Fatalf("stale authority apply mutated settings: greeting=%q", greeting)
	}
	var staleRunCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM configuration_import_runs WHERE salon_id = $1 AND request_id = $2`, salonID, stalePlan.RequestID).Scan(&staleRunCount); err != nil {
		t.Fatalf("count stale import runs: %v", err)
	}
	if staleRunCount != 0 {
		t.Fatalf("stale authority apply persisted %d import runs", staleRunCount)
	}
}

func configurationTransferAIPlan(salonID string, requestID string, fingerprint string, authority string, authorityVersion int64, greeting string) *importPlan {
	return &importPlan{
		Bundle: ConfigurationBundle{
			SchemaVersion:    SchemaVersion,
			IncludedSections: []string{SectionAI},
			AIReceptionist: AIReceptionistExport{
				AIGreeting:              greeting,
				AIVoice:                 "professional_female",
				AITone:                  "natural_human",
				BookingMode:             "pending_approval",
				RecordingEnabled:        true,
				RecordingConsentMessage: "This call may be recorded.",
				SMSConfirmationEnabled:  true,
				SMSReminderEnabled:      true,
				ReminderHoursBefore:     24,
				HandoffEnabled:          true,
			},
		},
		PayloadFingerprint:     fingerprint,
		SchemaVersion:          SchemaVersion,
		SalonID:                salonID,
		RequestID:              requestID,
		Summary:                newSummaryMap([]string{SectionAI}),
		CanApply:               true,
		BookingMode:            "pending_approval",
		IncludedSections:       map[string]bool{SectionAI: true},
		TargetAuthority:        authority,
		TargetAuthorityVersion: authorityVersion,
	}
}

func configurationTransferOnboardingPlan(requestID string, fingerprint string) *importPlan {
	sections := []string{SectionSalon, SectionAI, SectionPublic, SectionIntegrations, SectionCategories, SectionServiceAliases, SectionConsultation, SectionKnowledge}
	return &importPlan{
		Bundle: ConfigurationBundle{
			SchemaVersion:    SchemaVersion,
			IncludedSections: sections,
			SalonProfile: SalonProfileExport{
				Name:              "Concurrent Onboarding Salon",
				Phone:             "+13125550772",
				Timezone:          "America/Chicago",
				PrimaryLanguage:   "en",
				SecondaryLanguage: "vi",
				ActivePOSProvider: "square",
			},
			AIReceptionist: AIReceptionistExport{
				AIGreeting:              "Thanks for calling.",
				AIVoice:                 "professional_female",
				AITone:                  "natural_human",
				BookingMode:             "pending_approval",
				RecordingEnabled:        true,
				RecordingConsentMessage: "This call may be recorded.",
				SMSConfirmationEnabled:  true,
				SMSReminderEnabled:      true,
				ReminderHoursBefore:     24,
				HandoffEnabled:          true,
			},
		},
		PayloadFingerprint:     fingerprint,
		SchemaVersion:          SchemaVersion,
		RequestID:              requestID,
		Summary:                newSummaryMap(sections),
		CanApply:               true,
		AIEnabled:              true,
		BookingMode:            "pending_approval",
		IncludedSections:       sectionSet(sections),
		TargetAuthority:        booking.SchedulingAuthorityOwnerManual,
		TargetAuthorityVersion: 1,
		Onboarding:             true,
	}
}

func insertConfigurationTransferTestUser(t *testing.T, ctx context.Context, db *sql.DB, prefix string) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, full_name)
		VALUES ($1, 'integration-test', 'Configuration Transfer Test Owner')
		RETURNING id::text
	`, prefix+"-"+uuid.NewString()+"@example.com").Scan(&id); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	return id
}
