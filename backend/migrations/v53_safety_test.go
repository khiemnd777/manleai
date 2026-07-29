package migrations

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/pos"
)

func TestV53ExternalQuoteAuthorityFenceContract(t *testing.T) {
	payload, err := os.ReadFile("V53__external_availability_quote_authority_fence.sql")
	if err != nil {
		t.Fatalf("read V53: %v", err)
	}
	sql := string(payload)
	for _, required := range []string{
		"SET authority_fence_provenance = 'legacy_unknown'",
		"ADD COLUMN retry_of_attempt_id UUID",
		"REFERENCES booking_attempts(salon_id, id)",
		"DROP CONSTRAINT availability_quotes_external_provider_shape_check",
		"DROP CONSTRAINT availability_quotes_target_version_check",
		"target_authority_appointment_version >= 0",
		"scheduling_authority_version IS NOT NULL",
		"scheduling_authority_version >= 1",
		"authority_fence_provenance = 'retry_origin'",
		"retry_of_attempt_id IS NOT NULL",
		"legacy_unknown quote provenance is migration-owned",
		"availability quote authority provenance is immutable",
		"UPDATE OF authority_fence_provenance, retry_of_attempt_id",
		"authority_config_version IS NULL",
		"provider_snapshot_generation IS NOT NULL",
		"VALIDATE CONSTRAINT availability_quotes_external_provider_shape_check",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("V53 missing %q", required)
		}
	}
}

func TestV53BackfillsLegacyExternalQuotesAndPreservesABAFence(t *testing.T) {
	if os.Getenv("MIGRATION_TEST_DATABASE_URL") == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	db, ctx, tx := beginV49PostgresTest(t, "v53_quote_fence_")
	defer db.Close()
	defer tx.Rollback()
	applyV49MigrationChain(t, ctx, tx, 52)

	ownerID := uuid.New()
	salonID := uuid.New()
	quoteID := uuid.New()
	if _, err := tx.ExecContext(ctx, `INSERT INTO users (id,email,password_hash,full_name) VALUES ($1,$2,'hash','V53 Owner')`, ownerID, "v53-owner-"+uuid.NewString()+"@example.com"); err != nil {
		t.Fatalf("seed V53 owner: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO salons (id,name,phone,owner_user_id) VALUES ($1,'V53 Salon','5555300',$2)`, salonID, ownerID); err != nil {
		t.Fatalf("seed V53 salon: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO salon_settings (salon_id,scheduling_authority) VALUES ($1,'external_provider')`, salonID); err != nil {
		t.Fatalf("seed V53 settings: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO availability_quotes (
			id,salon_id,scheduling_authority,scheduling_authority_version,
			authority_provider,authority_location_id,authority_snapshot_generation,
			provider,provider_location_id,provider_snapshot_generation,
			request_fingerprint,expires_at
		) VALUES ($1,$2,'external_provider',NULL,'square','location-v53',7,'square','location-v53',7,$3,now()+interval '1 hour')
	`, quoteID, salonID, strings.Repeat("a", 64)); err != nil {
		t.Fatalf("seed legacy external quote: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
		t.Fatalf("flush pre-V53 deferred quote guards: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE salon_settings SET scheduling_authority='manleai_calendar' WHERE salon_id=$1`, salonID); err != nil {
		t.Fatalf("switch external to internal before V53: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE salon_settings SET scheduling_authority='external_provider' WHERE salon_id=$1`, salonID); err != nil {
		t.Fatalf("switch internal to external before V53: %v", err)
	}

	before := v53OperationalDigest(t, ctx, tx, salonID)
	if _, err := tx.ExecContext(ctx, readV53(t)); err != nil {
		t.Fatalf("apply V53: %v", err)
	}
	afterMigration := v53OperationalDigest(t, ctx, tx, salonID)
	if before.authority != afterMigration.authority || before.version != afterMigration.version || before.appointments != afterMigration.appointments || before.attempts != afterMigration.attempts {
		t.Fatalf("V53 rewrote non-quote operational state before=%#v after=%#v", before, afterMigration)
	}
	var quoteVersion sql.NullInt64
	var provenance string
	if err := tx.QueryRowContext(ctx, `SELECT scheduling_authority_version,authority_fence_provenance FROM availability_quotes WHERE id=$1`, quoteID).Scan(&quoteVersion, &provenance); err != nil {
		t.Fatalf("read backfilled quote fence: %v", err)
	}
	if quoteVersion.Valid || provenance != "legacy_unknown" {
		t.Fatalf("legacy quote authority version/provenance=%#v/%q, want NULL/legacy_unknown", quoteVersion, provenance)
	}
	expectV49PostgresError(t, ctx, tx, "23514", "availability_quotes_authority_fence_provenance_guard", `
		UPDATE availability_quotes SET authority_fence_provenance='known' WHERE id=$1
	`, quoteID)

	expectV49PostgresError(t, ctx, tx, "23514", "availability_quotes_external_provider_shape_check", `
		INSERT INTO availability_quotes (
			id,salon_id,scheduling_authority,scheduling_authority_version,
			authority_provider,authority_location_id,authority_snapshot_generation,
			provider,provider_location_id,provider_snapshot_generation,
			request_fingerprint,expires_at
		) VALUES ($1,$2,'external_provider',NULL,'square','location-v53',7,'square','location-v53',7,$3,now()+interval '1 hour')
	`, uuid.New(), salonID, strings.Repeat("b", 64))
	if _, err := tx.ExecContext(ctx, `SET CONSTRAINTS ALL DEFERRED`); err != nil {
		t.Fatalf("defer target quote tenant FK for shape-only version-zero fixture: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO availability_quotes (
			id,salon_id,scheduling_authority,scheduling_authority_version,
			authority_fence_provenance,
			authority_provider,authority_location_id,authority_snapshot_generation,
			provider,provider_location_id,provider_snapshot_generation,
			request_fingerprint,operation_type,target_appointment_id,
			target_authority_appointment_version,expires_at
		) VALUES ($1,$2,'external_provider',NULL,'target_origin','square','location-v53',7,'square','location-v53',7,
		          $3,'reschedule',$4,0,now()+interval '1 hour')
	`, uuid.New(), salonID, strings.Repeat("c", 64), uuid.New()); err != nil {
		t.Fatalf("external target-origin quote with provider version zero: %v", err)
	}

	var authority string
	var currentVersion int64
	if err := tx.QueryRowContext(ctx, `SELECT scheduling_authority,scheduling_authority_version FROM salon_settings WHERE salon_id=$1`, salonID).Scan(&authority, &currentVersion); err != nil {
		t.Fatalf("read ABA authority fence: %v", err)
	}
	if authority != "external_provider" || currentVersion != 3 || quoteVersion.Valid {
		t.Fatalf("ABA fence authority/version/quote=%q/%d/%#v, want external_provider/3/NULL", authority, currentVersion, quoteVersion)
	}
	if err := tx.QueryRowContext(ctx, `SELECT scheduling_authority_version,authority_fence_provenance FROM availability_quotes WHERE id=$1`, quoteID).Scan(&quoteVersion, &provenance); err != nil {
		t.Fatalf("reread quote after ABA: %v", err)
	}
	if quoteVersion.Valid || provenance != "legacy_unknown" {
		t.Fatalf("ABA legacy quote version/provenance=%#v/%q, want NULL/legacy_unknown", quoteVersion, provenance)
	}
}

func TestV53LegacyUnknownQuoteFailsClosedBeforeProviderDispatchAfterABA(t *testing.T) {
	databaseURL := os.Getenv("MIGRATION_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open V53 service database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	schemaName := "v53_service_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pq.QuoteIdentifier(schemaName)
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create V53 service schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, "SET search_path TO "+quotedSchema+", public"); err != nil {
		t.Fatalf("set V53 service search path: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "SET search_path TO public")
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+quotedSchema+" CASCADE")
	})

	preV53, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin pre-V53 chain: %v", err)
	}
	applyV49MigrationChain(t, ctx, preV53, 52)
	ownerID := uuid.New()
	salonID := uuid.New()
	serviceID := uuid.New()
	staffID := uuid.New()
	quoteID := uuid.New()
	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	end := start.Add(45 * time.Minute)
	slotFingerprint := strings.Repeat("7", 64)
	if _, err := preV53.ExecContext(ctx, `
		INSERT INTO users (id,email,password_hash,full_name)
		VALUES ($1,$2,'hash','V53 Service Owner')
	`, ownerID, "v53-service-"+uuid.NewString()+"@example.com"); err != nil {
		t.Fatalf("seed V53 service owner: %v", err)
	}
	if _, err := preV53.ExecContext(ctx, `
		INSERT INTO salons (id,name,phone,owner_user_id,timezone,active_pos_provider)
		VALUES ($1,'V53 Service Salon','5555301',$2,'America/Chicago','square')
	`, salonID, ownerID); err != nil {
		t.Fatalf("seed V53 service salon: %v", err)
	}
	if _, err := preV53.ExecContext(ctx, `INSERT INTO salon_settings (salon_id,scheduling_authority) VALUES ($1,'external_provider')`, salonID); err != nil {
		t.Fatalf("seed V53 service settings: %v", err)
	}
	if _, err := preV53.ExecContext(ctx, `
		INSERT INTO pos_connections (salon_id,provider,status,location_id,snapshot_generation,last_sync_at)
		VALUES ($1,'square','active','v53-location',7,now())
	`, salonID); err != nil {
		t.Fatalf("seed V53 service connection: %v", err)
	}
	if _, err := preV53.ExecContext(ctx, `
		INSERT INTO services (id,salon_id,pos_provider,pos_service_id,pos_service_version,name,duration_minutes,active,ai_bookable,sync_status)
		VALUES ($1,$2,'square','v53-service',11,'V53 Service',45,true,true,'synced')
	`, serviceID, salonID); err != nil {
		t.Fatalf("seed V53 service: %v", err)
	}
	if _, err := preV53.ExecContext(ctx, `
		INSERT INTO staff (id,salon_id,pos_provider,pos_staff_id,name,active,ai_bookable,sync_status)
		VALUES ($1,$2,'square','v53-staff','V53 Staff',true,true,'synced')
	`, staffID, salonID); err != nil {
		t.Fatalf("seed V53 staff: %v", err)
	}
	if _, err := preV53.ExecContext(ctx, `
		INSERT INTO pos_entity_links (salon_id,entity_type,entity_id,provider,provider_entity_id,provider_version,sync_status,last_synced_at)
		VALUES ($1,'service',$2,'square','v53-service',11,'synced',now()),
		       ($1,'staff',$3,'square','v53-staff',NULL,'synced',now())
	`, salonID, serviceID, staffID); err != nil {
		t.Fatalf("seed V53 provider links: %v", err)
	}
	if _, err := preV53.ExecContext(ctx, `
		INSERT INTO availability_quotes (
			id,salon_id,scheduling_authority,scheduling_authority_version,
			authority_provider,authority_location_id,authority_snapshot_generation,
			provider,provider_location_id,provider_snapshot_generation,request_fingerprint,expires_at
		) VALUES ($1,$2,'external_provider',NULL,'square','v53-location',7,'square','v53-location',7,$3,now()+interval '1 hour')
	`, quoteID, salonID, strings.Repeat("8", 64)); err != nil {
		t.Fatalf("seed pre-V53 quote: %v", err)
	}
	segments := `[{"service_id":"` + serviceID.String() + `","staff_id":"` + staffID.String() + `","staff_selection_mode":"specific","duration_minutes":45}]`
	if _, err := preV53.ExecContext(ctx, `
		INSERT INTO availability_quote_slots (salon_id,quote_id,slot_fingerprint,start_time,end_time,segments)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb)
	`, salonID, quoteID, slotFingerprint, start, end, segments); err != nil {
		t.Fatalf("seed pre-V53 quote slot: %v", err)
	}
	if _, err := preV53.ExecContext(ctx, `UPDATE salon_settings SET scheduling_authority='owner_manual' WHERE salon_id=$1`, salonID); err != nil {
		t.Fatalf("pre-V53 ABA switch away: %v", err)
	}
	if _, err := preV53.ExecContext(ctx, `UPDATE salon_settings SET scheduling_authority='external_provider' WHERE salon_id=$1`, salonID); err != nil {
		t.Fatalf("pre-V53 ABA switch back: %v", err)
	}
	if err := preV53.Commit(); err != nil {
		t.Fatalf("commit pre-V53 chain: %v", err)
	}

	// Current runtime authorization is membership-based in the public SaaS
	// access schema, while this test deliberately keeps the operational fixture
	// in an isolated pre-V53 schema. Mirror only the actor/salon identity into
	// the current access owner so the test continues to exercise the legacy
	// quote fence rather than failing earlier at the V67 membership boundary.
	if _, err := db.ExecContext(ctx, "SET search_path TO public"); err != nil {
		t.Fatalf("select current runtime access schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO public.users (id,email,password_hash,full_name)
		VALUES ($1,$2,'hash','V53 Runtime Access Owner')
	`, ownerID, "v53-runtime-access-"+uuid.NewString()+"@example.com"); err != nil {
		t.Fatalf("seed current runtime access owner: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO public.salons (id,name,phone,owner_user_id)
		VALUES ($1,'V53 Runtime Access Salon','5555302',$2)
	`, salonID, ownerID); err != nil {
		t.Fatalf("seed current runtime access salon: %v", err)
	}
	if _, err := db.ExecContext(ctx, "SET search_path TO "+quotedSchema+", public"); err != nil {
		t.Fatalf("restore V53 service search path: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM public.salons WHERE id=$1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM public.users WHERE id=$1`, ownerID)
	})

	if _, err := db.ExecContext(ctx, readV53(t)); err != nil {
		t.Fatalf("apply V53 service migration: %v", err)
	}
	var authority string
	var authorityVersion int64
	var quoteVersion sql.NullInt64
	var provenance string
	if err := db.QueryRowContext(ctx, `
		SELECT settings.scheduling_authority,settings.scheduling_authority_version,
		       quote.scheduling_authority_version,quote.authority_fence_provenance
		FROM salon_settings settings
		JOIN availability_quotes quote ON quote.salon_id=settings.salon_id
		WHERE settings.salon_id=$1 AND quote.id=$2
	`, salonID, quoteID).Scan(&authority, &authorityVersion, &quoteVersion, &provenance); err != nil {
		t.Fatalf("load V53 service ABA evidence: %v", err)
	}
	if authority != booking.SchedulingAuthorityExternalProvider || authorityVersion != 3 || quoteVersion.Valid || provenance != "legacy_unknown" {
		t.Fatalf("V53 service ABA evidence = %s/%d/%#v/%s", authority, authorityVersion, quoteVersion, provenance)
	}

	provider := &v53CountingProvider{}
	service := booking.NewService(booking.NewRepository(db), []pos.POSProvider{provider})
	_, err = service.Create(ctx, salonID.String(), ownerID.String(), booking.CreateBookingRequest{
		OperationKey:        "v53-legacy-quote-create",
		AvailabilityQuoteID: quoteID.String(),
		SlotFingerprint:     slotFingerprint,
		CustomerName:        "Legacy Quote Caller",
		CustomerPhone:       "+13125550123",
		ServiceID:           serviceID.String(),
		StaffID:             staffID.String(),
		StaffSelectionMode:  booking.StaffSelectionSpecific,
		StartTime:           start,
	})
	if !errors.Is(err, booking.ErrAvailabilityQuoteStale) {
		t.Fatalf("legacy quote create error = %v, want stale quote", err)
	}
	if provider.calls != 0 {
		t.Fatalf("legacy quote provider calls = %d, want zero", provider.calls)
	}
	var attempts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM booking_attempts WHERE salon_id=$1`, salonID).Scan(&attempts); err != nil {
		t.Fatalf("count legacy quote attempts: %v", err)
	}
	if attempts != 0 {
		t.Fatalf("legacy quote attempts = %d, want zero", attempts)
	}
}

type v53CountingProvider struct {
	calls int
}

func (p *v53CountingProvider) Name() string { return pos.ProviderSquare }

func (p *v53CountingProvider) Connect(context.Context, pos.ConnectInput) (*pos.Connection, error) {
	p.calls++
	return nil, nil
}
func (p *v53CountingProvider) HealthCheck(context.Context, string) error { p.calls++; return nil }
func (p *v53CountingProvider) ListLocations(context.Context, string) ([]pos.Location, error) {
	p.calls++
	return nil, nil
}
func (p *v53CountingProvider) ListServices(context.Context, string) ([]pos.Service, error) {
	p.calls++
	return nil, nil
}
func (p *v53CountingProvider) ListStaff(context.Context, string) ([]pos.StaffMember, error) {
	p.calls++
	return nil, nil
}
func (p *v53CountingProvider) SearchCustomerByPhone(context.Context, string, string) (*pos.Customer, error) {
	p.calls++
	return nil, nil
}
func (p *v53CountingProvider) CreateCustomer(context.Context, string, pos.CreateCustomerInput) (*pos.Customer, error) {
	p.calls++
	return nil, nil
}
func (p *v53CountingProvider) CheckAvailability(context.Context, string, pos.AvailabilityInput) ([]pos.TimeSlot, error) {
	p.calls++
	return nil, nil
}
func (p *v53CountingProvider) CreateAppointment(context.Context, string, pos.CreateAppointmentInput) (*pos.Appointment, error) {
	p.calls++
	return nil, nil
}
func (p *v53CountingProvider) RescheduleAppointment(context.Context, string, string, pos.RescheduleInput) (*pos.Appointment, error) {
	p.calls++
	return nil, nil
}
func (p *v53CountingProvider) CancelAppointment(context.Context, string, string, pos.CancelInput) (*pos.Appointment, error) {
	p.calls++
	return nil, nil
}
func (p *v53CountingProvider) Sync(context.Context, string) error { p.calls++; return nil }

func readV53(t *testing.T) string {
	t.Helper()
	source, err := Files.ReadFile("V53__external_availability_quote_authority_fence.sql")
	if err != nil {
		t.Fatalf("read V53 migration: %v", err)
	}
	return string(source)
}

type v53Digest struct {
	authority    string
	version      int64
	appointments int
	attempts     int
}

func v53OperationalDigest(t *testing.T, ctx context.Context, tx *sql.Tx, salonID uuid.UUID) v53Digest {
	t.Helper()
	var result v53Digest
	if err := tx.QueryRowContext(ctx, `SELECT scheduling_authority,scheduling_authority_version FROM salon_settings WHERE salon_id=$1`, salonID).Scan(&result.authority, &result.version); err != nil {
		t.Fatalf("read V53 settings digest: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM appointments WHERE salon_id=$1`, salonID).Scan(&result.appointments); err != nil {
		t.Fatalf("count V53 appointments: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM booking_attempts WHERE salon_id=$1`, salonID).Scan(&result.attempts); err != nil {
		t.Fatalf("count V53 attempts: %v", err)
	}
	return result
}
