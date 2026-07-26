package salon

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling/fence"
)

func TestPostgresOwnerFirstSalonCreationReplayConcurrencyAndSettingsFence(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	ownerID := insertSalonTestOwner(t, ctx, db, "onboarding-owner")
	otherOwnerID := insertSalonTestOwner(t, ctx, db, "onboarding-other-owner")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE owner_user_id IN ($1, $2)`, ownerID, otherOwnerID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1, $2)`, ownerID, otherOwnerID)
	})

	repository := NewRepository(db)
	service := NewService(repository)
	defaultRequest := CreateSalonRequest{
		OperationKey: " default-create-" + uuid.NewString() + " ",
		Name:         " Owner First Salon ",
		Phone:        " +13125555401 ",
		City:         " Austin ",
	}
	created, err := service.Create(ctx, ownerID, defaultRequest)
	if err != nil {
		t.Fatalf("create default owner-first salon: %v", err)
	}
	if created.SchedulingAuthority != booking.SchedulingAuthorityOwnerManual || created.SchedulingAuthorityVersion != 1 {
		t.Fatalf("default create authority/version=%q/%d, want owner_manual/1", created.SchedulingAuthority, created.SchedulingAuthorityVersion)
	}
	settings, err := service.GetSettings(ctx, created.ID, ownerID)
	if err != nil {
		t.Fatalf("load default settings: %v", err)
	}
	if settings.SchedulingAuthority != booking.SchedulingAuthorityOwnerManual || settings.BookingMode != "pending_approval" {
		t.Fatalf("default settings authority/mode=%q/%q", settings.SchedulingAuthority, settings.BookingMode)
	}

	loaded, err := service.Get(ctx, created.ID, ownerID)
	if err != nil || loaded.SchedulingAuthority != booking.SchedulingAuthorityOwnerManual || loaded.SchedulingAuthorityVersion != 1 {
		t.Fatalf("get salon authority=%#v err=%v", loaded, err)
	}
	listed, err := service.List(ctx, ownerID)
	if err != nil {
		t.Fatalf("list salons: %v", err)
	}
	if !salonListContainsAuthority(listed, created.ID, booking.SchedulingAuthorityOwnerManual, 1) {
		t.Fatalf("list response did not join authority/version for salon %s: %#v", created.ID, listed)
	}

	replayed, err := service.Create(ctx, ownerID, defaultRequest)
	if err != nil || replayed.ID != created.ID {
		t.Fatalf("exact replay salon=%#v err=%v, want ID %s", replayed, err, created.ID)
	}
	changed := defaultRequest
	changed.Name = "Changed Salon Name"
	if _, err := service.Create(ctx, ownerID, changed); !errors.Is(err, ErrCreateOperationConflict) {
		t.Fatalf("changed-payload replay error=%v, want ErrCreateOperationConflict", err)
	}
	assertSalonCreateConflictHTTP(t, service, ownerID, changed)

	explicitByAuthority := make(map[string]*Salon)
	for _, authority := range []string{
		booking.SchedulingAuthorityManleAICalendar,
		booking.SchedulingAuthorityExternalProvider,
	} {
		item, err := service.Create(ctx, ownerID, CreateSalonRequest{
			OperationKey:        "explicit-" + authority + "-" + uuid.NewString(),
			SchedulingAuthority: authority,
			Name:                "Explicit " + authority,
			Phone:               "+13125555402",
		})
		if err != nil {
			t.Fatalf("create explicit %s salon: %v", authority, err)
		}
		if item.SchedulingAuthority != authority || item.SchedulingAuthorityVersion != 1 {
			t.Fatalf("explicit %s response authority/version=%q/%d", authority, item.SchedulingAuthority, item.SchedulingAuthorityVersion)
		}
		explicitByAuthority[authority] = item
	}

	concurrentKey := "concurrent-create-" + uuid.NewString()
	concurrentRequest := CreateSalonRequest{
		OperationKey:        concurrentKey,
		SchedulingAuthority: booking.SchedulingAuthorityOwnerManual,
		Name:                "Concurrent Owner First Salon",
		Phone:               "+13125555403",
	}
	const workers = 8
	ids := make(chan string, workers)
	errorsFound := make(chan error, workers)
	var waitGroup sync.WaitGroup
	for i := 0; i < workers; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			item, err := service.Create(ctx, ownerID, concurrentRequest)
			if err != nil {
				errorsFound <- err
				return
			}
			ids <- item.ID
		}()
	}
	waitGroup.Wait()
	close(ids)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent create: %v", err)
	}
	var concurrentID string
	for id := range ids {
		if concurrentID == "" {
			concurrentID = id
		}
		if id != concurrentID {
			t.Fatalf("concurrent create returned multiple IDs %s and %s", concurrentID, id)
		}
	}
	assertSalonCreationAggregateCounts(t, ctx, db, ownerID, concurrentKey, 1, 1, 7)

	otherTenant, err := service.Create(ctx, otherOwnerID, CreateSalonRequest{
		OperationKey:        concurrentKey,
		SchedulingAuthority: booking.SchedulingAuthorityOwnerManual,
		Name:                concurrentRequest.Name,
		Phone:               concurrentRequest.Phone,
	})
	if err != nil {
		t.Fatalf("same operation key for another tenant: %v", err)
	}
	if otherTenant.ID == concurrentID {
		t.Fatalf("tenant-separated create reused salon ID %s", otherTenant.ID)
	}
	assertSalonCreationAggregateCounts(t, ctx, db, otherOwnerID, concurrentKey, 1, 1, 7)

	for _, invalid := range []CreateSalonRequest{
		{Name: "Missing Key", Phone: "+13125555404"},
		{OperationKey: strings.Repeat("x", 257), Name: "Long Key", Phone: "+13125555405"},
		{OperationKey: "bad-authority-" + uuid.NewString(), SchedulingAuthority: "square", Name: "Bad Authority", Phone: "+13125555406"},
	} {
		if _, err := service.Create(ctx, ownerID, invalid); !errors.Is(err, ErrValidation) {
			t.Fatalf("invalid create %#v error=%v, want ErrValidation", invalid, err)
		}
	}

	rollbackKey := "rollback-create-" + uuid.NewString()
	_, err = repository.Create(ctx, ownerID, CreateSalonRequest{
		OperationKey:        rollbackKey,
		SchedulingAuthority: "invalid_authority",
		Name:                "Rollback Salon",
		Phone:               "+13125555407",
		Timezone:            "America/Chicago",
		PrimaryLanguage:     "en",
		SecondaryLanguage:   "vi",
	}, strings.Repeat("f", 64))
	if err == nil {
		t.Fatal("repository accepted invalid authority and did not exercise rollback")
	}
	assertSalonCreationAggregateCounts(t, ctx, db, ownerID, rollbackKey, 0, 0, 0)

	confirmedSettings := validSalonSettingsUpdate("confirmed_booking")
	if _, err := service.UpdateSettings(ctx, created.ID, ownerID, confirmedSettings); !errors.Is(err, ErrValidation) {
		t.Fatalf("owner_manual confirmed update error=%v, want ErrValidation", err)
	}
	settings, err = service.GetSettings(ctx, created.ID, ownerID)
	if err != nil || settings.BookingMode != "pending_approval" {
		t.Fatalf("rejected owner_manual update changed mode=%#v err=%v", settings, err)
	}
	_, err = db.ExecContext(ctx, `UPDATE salon_settings SET booking_mode='confirmed_booking' WHERE salon_id=$1`, created.ID)
	assertSalonPostgresConstraint(t, err, "23514", "salon_settings_owner_manual_booking_mode_guard")

	externalSalon := explicitByAuthority[booking.SchedulingAuthorityExternalProvider]
	externalSettings, err := service.UpdateSettings(ctx, externalSalon.ID, ownerID, confirmedSettings)
	if err != nil || externalSettings.BookingMode != "confirmed_booking" {
		t.Fatalf("external_provider confirmed mode=%#v err=%v", externalSettings, err)
	}
	if _, err := service.UpdateSettings(ctx, externalSalon.ID, ownerID, validSalonSettingsUpdate("pending_approval")); err != nil {
		t.Fatalf("restore external salon pending mode: %v", err)
	}

	lockTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin settings race transaction: %v", err)
	}
	defer lockTx.Rollback()
	if _, err := lockTx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, fence.AdvisoryKey(externalSalon.ID)); err != nil {
		t.Fatalf("hold scheduling fence: %v", err)
	}
	raceResult := make(chan error, 1)
	go func() {
		_, err := service.UpdateSettings(context.Background(), externalSalon.ID, ownerID, confirmedSettings)
		raceResult <- err
	}()
	select {
	case err := <-raceResult:
		t.Fatalf("settings update bypassed held scheduling fence: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := lockTx.ExecContext(ctx, `
		UPDATE salon_settings SET scheduling_authority='owner_manual', updated_at=now() WHERE salon_id=$1
	`, externalSalon.ID); err != nil {
		t.Fatalf("switch authority while holding shared fence: %v", err)
	}
	if err := lockTx.Commit(); err != nil {
		t.Fatalf("commit authority side of settings race: %v", err)
	}
	select {
	case err := <-raceResult:
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("settings update after owner_manual switch error=%v, want ErrValidation", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("settings update remained blocked after scheduling fence release")
	}
	settings, err = service.GetSettings(ctx, externalSalon.ID, ownerID)
	if err != nil || settings.SchedulingAuthority != booking.SchedulingAuthorityOwnerManual || settings.BookingMode != "pending_approval" {
		t.Fatalf("settings race final state=%#v err=%v, want owner_manual/pending_approval", settings, err)
	}
}

func insertSalonTestOwner(t *testing.T, ctx context.Context, db *sql.DB, prefix string) string {
	t.Helper()
	var ownerID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email,password_hash,full_name)
		VALUES ($1,'integration-test','Salon Onboarding Owner')
		RETURNING id::text
	`, prefix+"-"+uuid.NewString()+"@example.com").Scan(&ownerID); err != nil {
		t.Fatalf("insert salon test owner: %v", err)
	}
	return ownerID
}

func salonListContainsAuthority(items []Salon, salonID string, authority string, version int64) bool {
	for _, item := range items {
		if item.ID == salonID && item.SchedulingAuthority == authority && item.SchedulingAuthorityVersion == version {
			return true
		}
	}
	return false
}

func assertSalonCreationAggregateCounts(t *testing.T, ctx context.Context, db *sql.DB, ownerID string, operationKey string, salons int, settings int, hours int) {
	t.Helper()
	var salonCount, settingsCount, hoursCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*),
		       count(settings.id),
		       count(hours.id)
		FROM salons salon
		LEFT JOIN salon_settings settings ON settings.salon_id=salon.id
		LEFT JOIN salon_business_hours hours ON hours.salon_id=salon.id
		WHERE salon.owner_user_id=$1 AND salon.creation_operation_key=$2
	`, ownerID, operationKey).Scan(&salonCount, &settingsCount, &hoursCount); err != nil {
		t.Fatalf("count salon creation aggregate: %v", err)
	}
	// The joins repeat salon/settings once per business-hour row. Count the root
	// separately when an aggregate exists so the assertion remains exact.
	if hoursCount > 0 {
		if err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM salons WHERE owner_user_id=$1 AND creation_operation_key=$2
		`, ownerID, operationKey).Scan(&salonCount); err != nil {
			t.Fatalf("count salon roots: %v", err)
		}
		if err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM salon_settings settings
			JOIN salons salon ON salon.id=settings.salon_id
			WHERE salon.owner_user_id=$1 AND salon.creation_operation_key=$2
		`, ownerID, operationKey).Scan(&settingsCount); err != nil {
			t.Fatalf("count salon settings: %v", err)
		}
	}
	if salonCount != salons || settingsCount != settings || hoursCount != hours {
		t.Fatalf("creation aggregate counts=%d/%d/%d, want %d/%d/%d", salonCount, settingsCount, hoursCount, salons, settings, hours)
	}
}

func validSalonSettingsUpdate(bookingMode string) UpdateSettingsRequest {
	return UpdateSettingsRequest{
		AIGreeting:              "Thank you for calling. How can I help today?",
		AIVoice:                 "professional_female",
		AITone:                  DefaultAITone,
		BookingMode:             bookingMode,
		RecordingEnabled:        true,
		RecordingConsentMessage: "This call may be recorded.",
		SMSConfirmationEnabled:  true,
		SMSReminderEnabled:      true,
		ReminderHoursBefore:     24,
		HandoffEnabled:          true,
	}
}

func assertSalonPostgresConstraint(t *testing.T, err error, code string, constraint string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected PostgreSQL error %s/%s", code, constraint)
	}
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		t.Fatalf("error=%v, want PostgreSQL %s/%s", err, code, constraint)
	}
	if string(pqErr.Code) != code || pqErr.Constraint != constraint {
		t.Fatalf("PostgreSQL error=%s/%s, want %s/%s: %v", pqErr.Code, pqErr.Constraint, code, constraint, err)
	}
}

func assertSalonCreateConflictHTTP(t *testing.T, service *Service, ownerID string, request CreateSalonRequest) {
	t.Helper()
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal conflicting create request: %v", err)
	}
	app := fiber.New()
	handler := NewHandler(service)
	app.Post("/", func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalUserID, ownerID)
		return handler.Create(c)
	})
	httpRequest := httptest.NewRequest("POST", "/", bytes.NewReader(payload))
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := app.Test(httpRequest)
	if err != nil {
		t.Fatalf("execute conflicting create request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusConflict {
		t.Fatalf("conflicting create status=%d, want 409", response.StatusCode)
	}
	var errorResponse respond.ErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
		t.Fatalf("decode conflicting create response: %v", err)
	}
	if errorResponse.Error.Code != "SALON_CREATE_OPERATION_CONFLICT" {
		t.Fatalf("conflicting create code=%q, want SALON_CREATE_OPERATION_CONFLICT", errorResponse.Error.Code)
	}
}
