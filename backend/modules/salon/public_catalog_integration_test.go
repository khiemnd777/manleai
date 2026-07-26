package salon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/public_catalog"
	"github.com/manleai/ai-receptionist/modules/scheduling/fence"
)

func TestPostgresOwnerManualPublicCatalogAuthorityFenceTenantAndFailClosedRead(t *testing.T) {
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
	ownerID := insertSalonTestOwner(t, ctx, db, "public-owner")
	otherOwnerID := insertSalonTestOwner(t, ctx, db, "public-other-owner")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE owner_user_id IN ($1,$2)`, ownerID, otherOwnerID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1,$2)`, ownerID, otherOwnerID)
	})

	repository := NewRepository(db)
	service := NewService(repository)
	salon, err := service.Create(ctx, ownerID, CreateSalonRequest{
		OperationKey: "public-owner-" + uuid.NewString(),
		Name:         "Owner Catalog Salon",
		Phone:        "+13125551010",
	})
	if err != nil {
		t.Fatalf("create owner-manual salon: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO services (
			salon_id,pos_provider,pos_service_id,name,duration_minutes,
			ai_bookable,active,source,sync_status
		) VALUES ($1,'square',NULL,'Owner Signature Service',45,true,true,'local','local_only')
	`, salon.ID); err != nil {
		t.Fatalf("insert canonical local service: %v", err)
	}

	slug := "owner-catalog-" + strings.ToLower(uuid.NewString()[:8])
	settings, err := service.UpdatePublicCatalogSettings(ctx, salon.ID, ownerID, UpdatePublicCatalogRequest{
		PublicSlug:                         slug,
		PublicCatalogEnabled:               true,
		ExpectedSchedulingAuthorityVersion: 1,
	})
	if err != nil {
		t.Fatalf("publish owner-manual catalog without POS/staff: %v", err)
	}
	if !settings.CanPublish || settings.SchedulingAuthority != booking.SchedulingAuthorityOwnerManual ||
		settings.SchedulingAuthorityVersion != 1 || settings.EligibleServiceCount != 1 || settings.EligibleStaffCount != 0 {
		t.Fatalf("owner-manual readiness = %#v", settings)
	}

	publicRepository := public_catalog.NewRepository(db)
	catalog, err := publicRepository.GetBySlug(ctx, slug)
	if err != nil {
		t.Fatalf("read published owner-manual catalog: %v", err)
	}
	if catalog.SchedulingAuthority != booking.SchedulingAuthorityOwnerManual || catalog.SchedulingAuthorityVersion != 1 ||
		len(catalog.Services) != 1 || len(catalog.Staff) != 0 {
		t.Fatalf("public owner-manual catalog = %#v", catalog)
	}
	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal public catalog: %v", err)
	}
	for _, forbidden := range []string{"active_pos_provider", "provider_entity_id", "pos_service_id", "owner_user_id"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public response exposed %q: %s", forbidden, encoded)
		}
	}

	if _, err := service.GetPublicCatalogSettings(ctx, salon.ID, otherOwnerID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant read error=%v, want ErrNotFound", err)
	}
	if _, err := service.UpdatePublicCatalogSettings(ctx, salon.ID, otherOwnerID, UpdatePublicCatalogRequest{
		PublicSlug: slug, PublicCatalogEnabled: false,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant update error=%v, want ErrNotFound", err)
	}

	lockTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin authority race transaction: %v", err)
	}
	defer lockTx.Rollback()
	if _, err := lockTx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, fence.AdvisoryKey(salon.ID)); err != nil {
		t.Fatalf("hold scheduling fence: %v", err)
	}
	raced := make(chan error, 1)
	go func() {
		_, updateErr := service.UpdatePublicCatalogSettings(context.Background(), salon.ID, ownerID, UpdatePublicCatalogRequest{
			PublicSlug:                         slug,
			PublicCatalogEnabled:               true,
			ExpectedSchedulingAuthorityVersion: 1,
		})
		raced <- updateErr
	}()
	select {
	case err := <-raced:
		t.Fatalf("publish bypassed held scheduling fence: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := lockTx.ExecContext(ctx, `
		UPDATE salon_settings SET scheduling_authority='external_provider',updated_at=now() WHERE salon_id=$1
	`, salon.ID); err != nil {
		t.Fatalf("switch authority while holding fence: %v", err)
	}
	if err := lockTx.Commit(); err != nil {
		t.Fatalf("commit authority switch: %v", err)
	}
	select {
	case err := <-raced:
		if !errors.Is(err, ErrSchedulingAuthorityChanged) {
			t.Fatalf("raced publish error=%v, want ErrSchedulingAuthorityChanged", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("raced publish remained blocked after scheduling fence release")
	}

	if _, err := publicRepository.GetBySlug(ctx, slug); !errors.Is(err, public_catalog.ErrNotFound) {
		t.Fatalf("stale published page after unready switch error=%v, want public ErrNotFound", err)
	}
	var enabled bool
	var authority string
	var version int64
	if err := db.QueryRowContext(ctx, `
		SELECT salon.public_catalog_enabled,settings.scheduling_authority,settings.scheduling_authority_version
		FROM salons salon JOIN salon_settings settings ON settings.salon_id=salon.id
		WHERE salon.id=$1
	`, salon.ID).Scan(&enabled, &authority, &version); err != nil {
		t.Fatalf("read stale publication state: %v", err)
	}
	if !enabled || authority != booking.SchedulingAuthorityExternalProvider || version != 2 {
		t.Fatalf("final publish/authority state=%v/%q/%d", enabled, authority, version)
	}

	internalSalon, err := service.Create(ctx, ownerID, CreateSalonRequest{
		OperationKey:        "public-internal-" + uuid.NewString(),
		SchedulingAuthority: booking.SchedulingAuthorityManleAICalendar,
		Name:                "Internal Catalog Salon",
		Phone:               "+13125551011",
	})
	if err != nil {
		t.Fatalf("create internal salon: %v", err)
	}
	var internalServiceID, internalStaffID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO services (salon_id,pos_provider,pos_service_id,name,duration_minutes,ai_bookable,active,source,sync_status)
		VALUES ($1,'square',NULL,'Internal Signature Service',60,true,true,'local','local_only') RETURNING id::text
	`, internalSalon.ID).Scan(&internalServiceID); err != nil {
		t.Fatalf("insert internal service: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO staff (salon_id,pos_provider,pos_staff_id,name,ai_bookable,active,source,sync_status)
		VALUES ($1,'square',NULL,'Internal Technician',true,true,'local','local_only') RETURNING id::text
	`, internalSalon.ID).Scan(&internalStaffID); err != nil {
		t.Fatalf("insert internal staff: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO manleai_calendar_configs (
			salon_id,slot_step_minutes,minimum_booking_notice_minutes,booking_horizon_days,
			max_party_size,default_buffer_before_minutes,default_buffer_after_minutes
		) VALUES ($1,15,0,30,4,0,0)
	`, internalSalon.ID); err != nil {
		t.Fatalf("insert internal config: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO manleai_calendar_service_policies (salon_id,service_id,enabled,capacity_mode)
		VALUES ($1,$2,true,'staff_only')
	`, internalSalon.ID, internalServiceID); err != nil {
		t.Fatalf("insert internal service policy: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO manleai_calendar_service_staff (salon_id,service_id,staff_id) VALUES ($1,$2,$3)
	`, internalSalon.ID, internalServiceID, internalStaffID); err != nil {
		t.Fatalf("insert internal service staff: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO manleai_calendar_staff_weekly_periods (salon_id,staff_id,day_of_week,start_minute,end_minute)
		VALUES ($1,$2,1,540,1020)
	`, internalSalon.ID, internalStaffID); err != nil {
		t.Fatalf("insert internal weekly period: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_business_hour_periods (
			salon_id,day_of_week,start_local_time,end_local_time,source,provider,provider_location_id,provider_period_index
		) VALUES ($1,1,'09:00','17:00','local_override','','',1)
	`, internalSalon.ID); err != nil {
		t.Fatalf("insert internal local hour: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE manleai_calendar_configs SET activated_at=now(),activated_by_user_id=$2,activated_version=version WHERE salon_id=$1
	`, internalSalon.ID, ownerID); err != nil {
		t.Fatalf("activate current internal config: %v", err)
	}
	internalSlug := "internal-catalog-" + strings.ToLower(uuid.NewString()[:8])
	internalSettings, err := service.UpdatePublicCatalogSettings(ctx, internalSalon.ID, ownerID, UpdatePublicCatalogRequest{
		PublicSlug: internalSlug, PublicCatalogEnabled: true, ExpectedSchedulingAuthorityVersion: 1,
	})
	if err != nil {
		t.Fatalf("publish internal catalog: %v", err)
	}
	if !internalSettings.CanPublish || internalSettings.PublishedHoursCount != 1 || internalSettings.EligibleStaffCount != 1 {
		t.Fatalf("internal readiness=%#v", internalSettings)
	}
	internalCatalog, err := publicRepository.GetBySlug(ctx, internalSlug)
	if err != nil || len(internalCatalog.Hours) != 1 || internalCatalog.Hours[0].Source != "local_override" {
		t.Fatalf("internal public catalog=%#v err=%v", internalCatalog, err)
	}

	externalSalon, err := service.Create(ctx, ownerID, CreateSalonRequest{
		OperationKey:        "public-external-" + uuid.NewString(),
		SchedulingAuthority: booking.SchedulingAuthorityExternalProvider,
		Name:                "Connected Catalog Salon",
		Phone:               "+13125551012",
	})
	if err != nil {
		t.Fatalf("create external salon: %v", err)
	}
	var externalServiceID, externalStaffID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO services (
			salon_id,pos_provider,pos_service_id,pos_service_version,name,duration_minutes,
			ai_bookable,active,source,sync_status,last_synced_at
		) VALUES ($1,'square','provider-service',7,'Connected Signature Service',50,true,true,'imported','synced',now())
		RETURNING id::text
	`, externalSalon.ID).Scan(&externalServiceID); err != nil {
		t.Fatalf("insert external service: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO staff (
			salon_id,pos_provider,pos_staff_id,name,ai_bookable,active,source,sync_status,last_synced_at
		) VALUES ($1,'square','provider-staff','Connected Technician',true,true,'imported','synced',now())
		RETURNING id::text
	`, externalSalon.ID).Scan(&externalStaffID); err != nil {
		t.Fatalf("insert external staff: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections (salon_id,provider,status,location_id,last_sync_at,snapshot_generation)
		VALUES ($1,'square','active','provider-location',now(),3)
	`, externalSalon.ID); err != nil {
		t.Fatalf("insert external connection: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_entity_links (
			salon_id,entity_type,entity_id,provider,provider_entity_id,provider_version,sync_status,last_synced_at
		) VALUES
		($1,'service',$2,'square','provider-service',7,'synced',now()),
		($1,'staff',$3,'square','provider-staff',5,'synced',now())
	`, externalSalon.ID, externalServiceID, externalStaffID); err != nil {
		t.Fatalf("insert external entity links: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_business_hour_periods (
			salon_id,day_of_week,start_local_time,end_local_time,source,provider,provider_location_id,provider_period_index,last_synced_at
		) VALUES ($1,2,'10:00','18:00','imported','square','provider-location',0,now())
	`, externalSalon.ID); err != nil {
		t.Fatalf("insert external hour: %v", err)
	}
	externalSlug := "external-catalog-" + strings.ToLower(uuid.NewString()[:8])
	externalSettings, err := service.UpdatePublicCatalogSettings(ctx, externalSalon.ID, ownerID, UpdatePublicCatalogRequest{
		PublicSlug: externalSlug, PublicCatalogEnabled: true, ExpectedSchedulingAuthorityVersion: 1,
	})
	if err != nil {
		t.Fatalf("publish external catalog: %v", err)
	}
	if !externalSettings.CanPublish || externalSettings.EligibleServiceCount != 1 || externalSettings.EligibleStaffCount != 1 {
		t.Fatalf("external readiness=%#v", externalSettings)
	}
	externalCatalog, err := publicRepository.GetBySlug(ctx, externalSlug)
	if err != nil || externalCatalog.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider || len(externalCatalog.Services) != 1 {
		t.Fatalf("external public catalog=%#v err=%v", externalCatalog, err)
	}
	firstSafe, err := publicRepository.GetFirstPublished(ctx)
	if err != nil {
		t.Fatalf("get first safe published catalog: %v", err)
	}
	if firstSafe.Salon.Slug == slug || len(firstSafe.Services) == 0 {
		t.Fatalf("first published returned stale/empty catalog %#v", firstSafe)
	}
}
