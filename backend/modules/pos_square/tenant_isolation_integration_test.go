package pos_square

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/database"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/internal/encryption"
	"github.com/manleai/ai-receptionist/modules/booking"
	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	"github.com/manleai/ai-receptionist/modules/pos"
)

func TestSquareSchedulingEvidenceUsesExactSystemSalonWithoutOwnerMembership(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("OWNER_FIRST_RELEASE_GATE_DATABASE_REQUIRED") == "1" {
			t.Fatal("TEST_DATABASE_URL is required in release-gate mode")
		}
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := database.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	ownerA, salonA := insertStrictSquareTenant(t, context.Background(), db, "System evidence tenant A", "+13125551141", suffix+"a")
	ownerB, salonB := insertStrictSquareTenant(t, context.Background(), db, "System evidence tenant B", "+13125551142", suffix+"b")
	defer cleanupStrictSquareTenants(t, db, []string{salonA, salonB}, []string{ownerA, ownerB})
	if _, err := db.ExecContext(context.Background(), `
		UPDATE salon_memberships
		SET status='revoked', version=version+1, updated_at=now()
		WHERE salon_id=$1 AND user_id=$2
	`, salonA, ownerA); err != nil {
		t.Fatalf("revoke owner membership: %v", err)
	}

	loadEvidence := func(ctx context.Context) (squareSchedulingTargetEvidence, error) {
		t.Helper()
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			return squareSchedulingTargetEvidence{}, err
		}
		defer tx.Rollback()
		return loadSquareSchedulingTargetEvidenceTx(ctx, tx, salonA, ownerA)
	}

	if _, err := loadEvidence(databasecontext.WithActor(context.Background(), ownerA)); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("revoked owner scheduling evidence error=%v, want POS not found", err)
	}
	if _, err := loadEvidence(databasecontext.WithScope(context.Background(), databasecontext.ScopeProvider)); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("unbound provider scheduling evidence error=%v, want POS not found", err)
	}
	if _, err := loadEvidence(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeProvider, salonB)); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("cross-tenant provider scheduling evidence error=%v, want POS not found", err)
	}
	if evidence, err := loadEvidence(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeProvider, salonA)); err != nil {
		t.Fatalf("exact-salon provider scheduling evidence: %v", err)
	} else if evidence.AuthorityVersion != 1 {
		t.Fatalf("provider scheduling evidence authority version=%d, want 1", evidence.AuthorityVersion)
	}
}

func TestSquareTwoTenantEndToEndIsolation(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("OWNER_FIRST_RELEASE_GATE_DATABASE_REQUIRED") == "1" {
			t.Fatal("TEST_DATABASE_URL is required in release-gate mode")
		}
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	db, err := database.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	tenantA := strictSquareTenantFixture{
		Key: "aurora", ClientID: "client-aurora-" + suffix, ClientSecret: "secret-aurora-" + suffix,
		AccessToken: "token-aurora-" + suffix, RefreshToken: "refresh-aurora-" + suffix,
		MerchantID: "merchant-aurora-" + suffix, LocationID: "location-aurora-" + suffix,
		ServiceID: "service-aurora-" + suffix, ServiceVersion: 4101, ServiceName: "Aurora Foot Ritual",
		StaffID: "staff-aurora-" + suffix, StaffName: "Mina Park",
		CustomerID: "customer-aurora-" + suffix, CustomerName: "Helen Cruz", CustomerPhone: "+13125551041",
		BookingID: "booking-aurora-" + suffix, CalendarBookingID: "calendar-aurora-" + suffix,
		WebhookKey: "webhook-aurora-" + suffix,
	}
	tenantB := strictSquareTenantFixture{
		Key: "cedar", ClientID: "client-cedar-" + suffix, ClientSecret: "secret-cedar-" + suffix,
		AccessToken: "token-cedar-" + suffix, RefreshToken: "refresh-cedar-" + suffix,
		MerchantID: "merchant-cedar-" + suffix, LocationID: "location-cedar-" + suffix,
		ServiceID: "service-cedar-" + suffix, ServiceVersion: 5202, ServiceName: "Cedar Gel Renewal",
		StaffID: "staff-cedar-" + suffix, StaffName: "Owen Lee",
		CustomerID: "customer-cedar-" + suffix, CustomerName: "Rosa Diaz", CustomerPhone: "+13125551052",
		BookingID: "booking-cedar-" + suffix, CalendarBookingID: "calendar-cedar-" + suffix,
		WebhookKey: "webhook-cedar-" + suffix,
	}
	tenantA.Host = "square-aurora-" + suffix + ".invalid"
	tenantB.Host = "square-cedar-" + suffix + ".invalid"
	tenantA.RedirectURL = "https://app.example.test/square/aurora/" + suffix
	tenantB.RedirectURL = "https://app.example.test/square/cedar/" + suffix
	tenantA.WebhookURL = "https://hooks.example.test/square/aurora/" + suffix
	tenantB.WebhookURL = "https://hooks.example.test/square/cedar/" + suffix

	ownerA, salonA := insertStrictSquareTenant(t, ctx, db, "Aurora tenant", "+13125550141", suffix+"a")
	ownerB, salonB := insertStrictSquareTenant(t, ctx, db, "Cedar tenant", "+13125550152", suffix+"b")
	defer cleanupStrictSquareTenants(t, db, []string{salonA, salonB}, []string{ownerA, ownerB})

	cipher, err := encryption.NewTokenCipher("strict-square-two-tenant-" + suffix)
	if err != nil {
		t.Fatal(err)
	}
	configRepo := integrationconfig.NewRepository(db)
	configService := integrationconfig.NewService(configRepo, cipher, config.Config{})
	posRepo := pos.NewRepository(db)

	if _, err := configService.ResolveSquareConfig(ctx, salonB); !errors.Is(err, integrationconfig.ErrNotFound) {
		t.Fatalf("missing tenant configuration error=%v, want integrationconfig.ErrNotFound", err)
	}
	configVersions := map[string]int64{}
	for _, item := range []struct {
		salonID string
		ownerID string
		fixture strictSquareTenantFixture
	}{{salonA, ownerA, tenantA}, {salonB, ownerB, tenantB}} {
		response, err := configService.UpdateSquareForPlatform(ctx, item.salonID, item.ownerID, integrationconfig.UpdateSquareSettingsRequest{
			TechnicalMutationControl: integrationconfig.TechnicalMutationControl{
				ActionKey:       "strict-square-config-" + item.fixture.Key + "-" + suffix,
				ExpectedVersion: 0,
			},
			Environment:            "sandbox",
			ClientID:               item.fixture.ClientID,
			ClientSecret:           item.fixture.ClientSecret,
			RedirectURL:            item.fixture.RedirectURL,
			APIVersion:             "2026-05-20",
			APIBaseURL:             "https://" + item.fixture.Host,
			WebhookNotificationURL: stringPointer(item.fixture.WebhookURL),
			WebhookSignatureKey:    item.fixture.WebhookKey,
		})
		if err != nil {
			t.Fatalf("save %s Square config: %v", item.fixture.Key, err)
		}
		if !response.Configured || !response.ClientSecretConfigured || !response.WebhookConfigured || response.Version <= 0 {
			t.Fatalf("%s Square config response=%#v", item.fixture.Key, response)
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), item.fixture.ClientSecret) || strings.Contains(string(encoded), item.fixture.WebhookKey) {
			t.Fatalf("%s Square response exposed a write-only secret", item.fixture.Key)
		}
		configVersions[item.salonID] = response.Version
	}
	assertStrictSquareStoredConfig(t, ctx, configService, salonA, ownerA, tenantA, tenantB)
	assertStrictSquareStoredConfig(t, ctx, configService, salonB, ownerB, tenantB, tenantA)

	transport := newStrictSquareTransport(tenantA, tenantB)
	adapter, err := NewSquareAdapter(configService, posRepo, cipher)
	if err != nil {
		t.Fatal(err)
	}
	adapter.httpClient = &http.Client{Transport: transport, Timeout: 5 * time.Second}

	requestCount := transport.requestCount()
	if _, err := adapter.OAuthURL(ctx, " ", "blank-tenant-state"); !errors.Is(err, ErrTenantContextRequired) {
		t.Fatalf("blank tenant context error=%v, want ErrTenantContextRequired", err)
	}
	if transport.requestCount() != requestCount {
		t.Fatal("blank tenant context reached the Square transport")
	}
	if _, err := posRepo.GetActiveProvider(ctx, salonA, ownerA); !errors.Is(err, pos.ErrActiveProviderNotConfigured) {
		t.Fatalf("blank active provider error=%v, want pos.ErrActiveProviderNotConfigured", err)
	}

	squareService := NewService(posRepo, adapter, "strict-square-state-"+suffix, nil)
	connectA, err := squareService.ConnectURLForPlatform(ctx, salonA)
	if err != nil {
		t.Fatal(err)
	}
	connectB, err := squareService.ConnectURLForPlatform(ctx, salonB)
	if err != nil {
		t.Fatal(err)
	}
	assertStrictSquareOAuthURL(t, connectA.URL, tenantA, tenantB)
	assertStrictSquareOAuthURL(t, connectB.URL, tenantB, tenantA)

	tamperedState := tamperStrictSquareStateTenant(t, connectB.State, salonA)
	requestCount = transport.requestCount()
	if _, err := squareService.HandleCallback(ctx, "code-cedar", tamperedState); err == nil {
		t.Fatal("cross-tenant OAuth state tampering unexpectedly succeeded")
	}
	if transport.requestCount() != requestCount {
		t.Fatal("tampered OAuth state reached the Square transport")
	}

	connectionA, err := squareService.HandleCallback(ctx, "code-aurora", connectA.State)
	if err != nil {
		t.Fatal(err)
	}
	connectionB, err := squareService.HandleCallback(ctx, "code-cedar", connectB.State)
	if err != nil {
		t.Fatal(err)
	}
	assertStrictSquareConnection(t, cipher, connectionA, salonA, tenantA, tenantB)
	assertStrictSquareConnection(t, cipher, connectionB, salonB, tenantB, tenantA)

	for _, item := range []struct {
		salonID string
		fixture strictSquareTenantFixture
	}{{salonA, tenantA}, {salonB, tenantB}} {
		locations, err := squareService.LocationsForPlatform(ctx, item.salonID)
		if err != nil {
			t.Fatalf("list %s locations: %v", item.fixture.Key, err)
		}
		if len(locations) != 1 || locations[0].ID != item.fixture.LocationID {
			t.Fatalf("%s locations=%#v", item.fixture.Key, locations)
		}
		if _, err := squareService.SelectLocationForPlatform(ctx, item.salonID, item.fixture.LocationID); err != nil {
			t.Fatalf("select %s location: %v", item.fixture.Key, err)
		}
		summary, err := squareService.SyncForPlatform(ctx, item.salonID)
		if err != nil {
			t.Fatalf("sync %s: %v", item.fixture.Key, err)
		}
		if summary.ServicesSynced != 1 || summary.StaffSynced != 1 || summary.BusinessHourPeriodsSynced != 1 || summary.CustomersSynced != 1 {
			t.Fatalf("%s sync summary=%#v", item.fixture.Key, summary)
		}
		assertStrictSquareImportedData(t, ctx, db, item.salonID, item.fixture)
	}

	connections := map[string]*pos.Connection{}
	for _, item := range []struct {
		salonID string
		ownerID string
		fixture strictSquareTenantFixture
	}{{salonA, ownerA, tenantA}, {salonB, ownerB, tenantB}} {
		connection, err := posRepo.GetConnection(ctx, item.salonID, pos.ProviderSquare)
		if err != nil {
			t.Fatal(err)
		}
		connections[item.salonID] = connection
		if connection.Status != pos.StatusActive || connection.LastSyncAt == nil || connection.SnapshotGeneration <= 0 {
			t.Fatalf("%s connection is not sync-active: %#v", item.fixture.Key, connection)
		}
		activation, replayed, err := squareService.ActivateInitialProviderForPlatform(ctx, item.salonID, item.ownerID, InitialProviderActivationRequest{
			ActionKey:                           "strict-square-activate-" + item.fixture.Key + "-" + suffix,
			ExpectedVersion:                     0,
			ExpectedIntegrationConfigVersion:    configVersions[item.salonID],
			ExpectedConnectionCapabilityVersion: connection.BookingWriteCapabilityVersion,
		})
		if err != nil {
			t.Fatalf("activate %s Square: %v", item.fixture.Key, err)
		}
		if replayed || activation.Provider != pos.ProviderSquare || activation.Version != 1 {
			t.Fatalf("%s activation=%#v replayed=%t", item.fixture.Key, activation, replayed)
		}
		var authority string
		if err := db.QueryRowContext(ctx, `SELECT scheduling_authority FROM salon_settings WHERE salon_id=$1`, item.salonID).Scan(&authority); err != nil {
			t.Fatal(err)
		}
		if authority != booking.SchedulingAuthorityOwnerManual {
			t.Fatalf("%s POS activation changed scheduling authority to %q", item.fixture.Key, authority)
		}
		if _, err := db.ExecContext(ctx, `UPDATE salon_settings SET scheduling_authority='external_provider' WHERE salon_id=$1`, item.salonID); err != nil {
			t.Fatal(err)
		}
		capability, replayed, err := squareService.ReevaluateSchedulingCapabilityForPlatform(ctx, item.salonID, item.ownerID, ReevaluateSchedulingCapabilityRequest{
			ActionKey:                           "strict-square-capability-" + item.fixture.Key + "-" + suffix,
			ExpectedConnectionCapabilityVersion: connection.BookingWriteCapabilityVersion,
			ExpectedIntegrationConfigVersion:    configVersions[item.salonID],
		})
		if err != nil {
			t.Fatalf("evaluate %s capability: %v", item.fixture.Key, err)
		}
		if replayed || !capability.AutomaticSingleCreate || !capability.EvidenceCurrent || capability.WritePermissionMode != pos.SchedulingWriteModeBuyer {
			t.Fatalf("%s capability=%#v replayed=%t", item.fixture.Key, capability, replayed)
		}
	}

	requestCount = transport.requestCount()
	staleFence := pos.ProviderFence{LocationID: tenantB.LocationID, SnapshotGeneration: connections[salonB].SnapshotGeneration}
	if _, err := adapter.CheckAvailability(ctx, salonA, pos.AvailabilityInput{
		ServiceID: tenantA.ServiceID, StaffID: tenantA.StaffID, PreferredDate: "2026-08-03", Timezone: "UTC", DurationMinutes: 45,
		ProviderFence: staleFence,
	}); !errors.Is(err, pos.ErrStaleProviderFence) {
		t.Fatalf("cross-tenant provider fence error=%v, want pos.ErrStaleProviderFence", err)
	}
	if transport.requestCount() != requestCount {
		t.Fatal("cross-tenant provider fence reached the Square transport")
	}

	bookingService := booking.NewService(booking.NewRepository(db), []pos.POSProvider{adapter})
	attemptB, appointmentB := createStrictSquareBooking(t, ctx, db, bookingService, salonB, ownerB, tenantB)
	if attemptB.Status != booking.StatusConfirmed || appointmentB.Status != booking.StatusConfirmed || attemptB.POSBookingID != tenantB.BookingID {
		t.Fatalf("tenant B booking attempt=%#v appointment=%#v", attemptB, appointmentB)
	}
	beforeTenantB := strictSquareTenantSnapshot(t, ctx, db, salonB)

	attemptA, appointmentA := createStrictSquareBooking(t, ctx, db, bookingService, salonA, ownerA, tenantA)
	if attemptA.Status != booking.StatusConfirmed || appointmentA.Status != booking.StatusConfirmed || attemptA.POSBookingID != tenantA.BookingID {
		t.Fatalf("tenant A booking attempt=%#v appointment=%#v", attemptA, appointmentA)
	}

	webhookRepo := NewWebhookRepository(db)
	squareService.SetWebhookRepository(webhookRepo)
	webhookBody := []byte(fmt.Sprintf(`{"merchant_id":%q,"location_id":%q,"type":"booking.updated","event_id":%q,"created_at":"2026-08-03T12:00:00Z","data":{"type":"booking","id":%q,"object":{"booking":{"id":%q,"version":7,"status":"ACCEPTED","start_at":"2026-08-03T15:00:00Z","location_id":%q}}}}`,
		tenantA.MerchantID, tenantA.LocationID, "event-aurora-"+suffix, tenantA.CalendarBookingID, tenantA.CalendarBookingID, tenantA.LocationID))
	providerCtx := databasecontext.WithScope(ctx, databasecontext.ScopeProvider)
	receipt, err := squareService.ReceiveBookingWebhook(providerCtx, webhookBody, signStrictSquareWebhook(tenantA.WebhookURL, tenantA.WebhookKey, webhookBody))
	if err != nil {
		t.Fatal(err)
	}
	if receipt == nil || !receipt.Accepted || receipt.Duplicate {
		t.Fatalf("tenant A webhook receipt=%#v", receipt)
	}
	processor := NewWebhookProcessor(webhookRepo, bookingService)
	processed, err := processor.ProcessWebhookEvents(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("processed webhook count=%d, want 1", processed)
	}
	var importedCalendarAppointmentSalon string
	if err := db.QueryRowContext(ctx, `SELECT salon_id::text FROM appointments WHERE pos_provider='square' AND pos_appointment_id=$1`, tenantA.CalendarBookingID).Scan(&importedCalendarAppointmentSalon); err != nil {
		t.Fatal(err)
	}
	if importedCalendarAppointmentSalon != salonA {
		t.Fatalf("calendar appointment routed to salon=%s, want tenant A", importedCalendarAppointmentSalon)
	}

	afterTenantB := strictSquareTenantSnapshot(t, ctx, db, salonB)
	if !reflect.DeepEqual(beforeTenantB, afterTenantB) {
		t.Fatalf("tenant A flow mutated tenant B\nbefore=%#v\nafter=%#v", beforeTenantB, afterTenantB)
	}

	reconnectB, err := squareService.ConnectURLForPlatform(ctx, salonB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := squareService.HandleCallback(ctx, "code-cedar-reconnect-aurora", reconnectB.State); err != nil {
		t.Fatal(err)
	}
	if _, err := squareService.SelectLocationForPlatform(ctx, salonB, tenantA.LocationID); !errors.Is(err, pos.ErrProviderLocationTenantConflict) {
		t.Fatalf("duplicate merchant/location error=%v, want pos.ErrProviderLocationTenantConflict", err)
	}
	reconnectedB, err := posRepo.GetConnection(ctx, salonB, pos.ProviderSquare)
	if err != nil {
		t.Fatal(err)
	}
	if reconnectedB.MerchantID != tenantA.MerchantID || reconnectedB.LocationID != "" || reconnectedB.LastSyncAt != nil {
		t.Fatalf("tenant B conflicting reconnect=%#v, want merchant A with no selected location or sync evidence", reconnectedB)
	}
	owner, err := NewWebhookRepository(db).FindWebhookTarget(providerCtx, tenantA.MerchantID, tenantA.LocationID)
	if err != nil {
		t.Fatal(err)
	}
	if owner.SalonID != salonA {
		t.Fatalf("duplicate reconnect changed Square webhook owner to %s", owner.SalonID)
	}

	if violations := transport.violations(); len(violations) != 0 {
		t.Fatalf("fake Square transport observed tenant-boundary violations: %v", violations)
	}
	if transport.unknownHostCalls() != 0 {
		t.Fatal("a Square request escaped the two configured fake tenant hosts")
	}
}

type strictSquareTenantFixture struct {
	Key, Host, ClientID, ClientSecret, RedirectURL, WebhookURL, WebhookKey string
	AccessToken, RefreshToken, MerchantID, LocationID                      string
	ServiceID, ServiceName                                                 string
	ServiceVersion                                                         int64
	StaffID, StaffName                                                     string
	CustomerID, CustomerName, CustomerPhone                                string
	BookingID, CalendarBookingID                                           string
}

type strictSquareTransport struct {
	mu               sync.Mutex
	byHost           map[string]strictSquareTenantFixture
	requests         int
	unknownHosts     int
	boundaryFailures []string
}

func newStrictSquareTransport(fixtures ...strictSquareTenantFixture) *strictSquareTransport {
	byHost := make(map[string]strictSquareTenantFixture, len(fixtures))
	for _, fixture := range fixtures {
		byHost[fixture.Host] = fixture
	}
	return &strictSquareTransport{byHost: byHost}
}

func (f *strictSquareTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.requests++
	fixture, ok := f.byHost[request.URL.Host]
	if !ok {
		f.unknownHosts++
		f.mu.Unlock()
		return nil, fmt.Errorf("unrecognized fake Square host")
	}
	f.mu.Unlock()

	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	if request.URL.Path != "/oauth2/token" && request.Header.Get("Authorization") != "Bearer "+fixture.AccessToken {
		f.recordViolation(fixture.Key + ": wrong tenant access token")
		return nil, fmt.Errorf("tenant-bound Square access token mismatch")
	}

	switch {
	case request.Method == http.MethodPost && request.URL.Path == "/oauth2/token":
		var payload map[string]string
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}
		if payload["client_id"] != fixture.ClientID || payload["client_secret"] != fixture.ClientSecret {
			f.recordViolation(fixture.Key + ": wrong tenant OAuth credentials")
			return nil, fmt.Errorf("tenant-bound Square OAuth credential mismatch")
		}
		merchantID := fixture.MerchantID
		accessToken := fixture.AccessToken
		if payload["code"] == "code-cedar-reconnect-aurora" && fixture.Key == "cedar" {
			for _, candidate := range f.byHost {
				if candidate.Key == "aurora" {
					merchantID = candidate.MerchantID
					accessToken = "token-cedar-reconnected"
					break
				}
			}
		}
		return strictSquareJSONResponse(request, map[string]any{
			"access_token": accessToken, "refresh_token": fixture.RefreshToken, "merchant_id": merchantID,
			"scope": "APPOINTMENTS_READ APPOINTMENTS_ALL_READ APPOINTMENTS_WRITE ITEMS_READ EMPLOYEES_READ CUSTOMERS_READ",
		})
	case request.Method == http.MethodGet && request.URL.Path == "/v2/locations":
		return strictSquareJSONResponse(request, map[string]any{"locations": []map[string]any{{
			"id": fixture.LocationID, "name": fixture.Key + " studio", "status": "ACTIVE", "timezone": "UTC",
		}}})
	case request.Method == http.MethodGet && request.URL.Path == "/v2/catalog/list":
		available := true
		return strictSquareJSONResponse(request, squareCatalogResponse{Objects: []squareCatalogObject{{
			ID: "item-" + fixture.Key, Type: "ITEM", PresentAtAllLocations: &available,
			ItemData: struct {
				Name        string                `json:"name"`
				Description string                `json:"description"`
				Variations  []squareCatalogObject `json:"variations"`
			}{
				Name: fixture.ServiceName, Description: fixture.ServiceName + " description",
				Variations: []squareCatalogObject{{
					ID: fixture.ServiceID, Type: "ITEM_VARIATION", Version: fixture.ServiceVersion,
					ItemVariationData: struct {
						Name                string `json:"name"`
						ServiceDuration     int64  `json:"service_duration"`
						AvailableForBooking *bool  `json:"available_for_booking"`
						PriceMoney          struct {
							Amount   int64  `json:"amount"`
							Currency string `json:"currency"`
						} `json:"price_money"`
					}{Name: "Regular", ServiceDuration: 45 * 60 * 1000, AvailableForBooking: &available},
				}},
			},
		}}})
	case request.Method == http.MethodPost && request.URL.Path == "/v2/team-members/search":
		return strictSquareJSONResponse(request, map[string]any{"team_members": []map[string]any{{
			"id": fixture.StaffID, "given_name": fixture.StaffName, "status": "ACTIVE",
		}}})
	case request.Method == http.MethodGet && request.URL.Path == "/v2/bookings/team-member-booking-profiles":
		return strictSquareJSONResponse(request, map[string]any{"team_member_booking_profiles": []map[string]any{{
			"team_member_id": fixture.StaffID, "display_name": fixture.StaffName, "is_bookable": true,
		}}})
	case request.Method == http.MethodGet && request.URL.Path == "/v2/locations/"+fixture.LocationID:
		return strictSquareJSONResponse(request, map[string]any{"location": map[string]any{
			"id": fixture.LocationID, "business_hours": map[string]any{"periods": []map[string]any{{
				"day_of_week": "MON", "start_local_time": "09:00", "end_local_time": "18:00",
			}}},
		}})
	case request.Method == http.MethodPost && request.URL.Path == "/v2/customers/search":
		parts := strings.Fields(fixture.CustomerName)
		return strictSquareJSONResponse(request, map[string]any{"customers": []map[string]any{{
			"id": fixture.CustomerID, "given_name": parts[0], "family_name": strings.Join(parts[1:], " "), "phone_number": fixture.CustomerPhone,
		}}})
	case request.Method == http.MethodPost && request.URL.Path == "/v2/bookings/availability/search":
		var payload squareAvailabilityRequest
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}
		if payload.Query.Filter.LocationID != fixture.LocationID || len(payload.Query.Filter.SegmentFilters) != 1 ||
			payload.Query.Filter.SegmentFilters[0].ServiceVariationID != fixture.ServiceID {
			f.recordViolation(fixture.Key + ": cross-tenant availability payload")
			return nil, fmt.Errorf("tenant-bound Square availability payload mismatch")
		}
		return strictSquareJSONResponse(request, map[string]any{"availabilities": []map[string]any{{
			"start_at": "2026-08-03T15:00:00Z", "location_id": fixture.LocationID,
			"appointment_segments": []map[string]any{{
				"duration_minutes": 45, "team_member_id": fixture.StaffID, "service_variation_id": fixture.ServiceID,
			}},
		}}})
	case request.Method == http.MethodPost && request.URL.Path == "/v2/bookings":
		var payload squareCreateBookingRequest
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}
		if payload.Booking.LocationID != fixture.LocationID || payload.Booking.CustomerID != fixture.CustomerID ||
			len(payload.Booking.AppointmentSegments) != 1 || payload.Booking.AppointmentSegments[0].ServiceVariationID != fixture.ServiceID ||
			payload.Booking.AppointmentSegments[0].TeamMemberID != fixture.StaffID {
			f.recordViolation(fixture.Key + ": cross-tenant booking payload")
			return nil, fmt.Errorf("tenant-bound Square booking payload mismatch")
		}
		booking := payload.Booking
		booking.ID = fixture.BookingID
		booking.Version = 3
		booking.Status = "ACCEPTED"
		return strictSquareJSONResponse(request, squareBookingResponse{Booking: booking})
	case request.Method == http.MethodGet && request.URL.Path == "/v2/bookings":
		return strictSquareJSONResponse(request, squareBookingListResponse{Bookings: []squareBooking{{
			ID: fixture.CalendarBookingID, Version: 7, Status: "ACCEPTED", CustomerID: fixture.CustomerID,
			StartAt: "2026-08-03T15:00:00Z", LocationID: fixture.LocationID,
			AppointmentSegments: []squareAppointmentSegment{{
				DurationMinutes: 45, TeamMemberID: fixture.StaffID, ServiceVariationID: fixture.ServiceID, ServiceVariationVersion: fixture.ServiceVersion,
			}},
		}}})
	default:
		return nil, fmt.Errorf("unhandled fake Square request %s %s", request.Method, request.URL.Path)
	}
}

func (f *strictSquareTransport) recordViolation(message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.boundaryFailures = append(f.boundaryFailures, message)
}

func (f *strictSquareTransport) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func (f *strictSquareTransport) violations() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.boundaryFailures...)
}

func (f *strictSquareTransport) unknownHostCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unknownHosts
}

func strictSquareJSONResponse(request *http.Request, payload any) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    request,
	}, nil
}

func insertStrictSquareTenant(t *testing.T, ctx context.Context, db *sql.DB, name, phone, suffix string) (string, string) {
	t.Helper()
	var ownerID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users(email,password_hash,full_name)
		VALUES($1,'strict-square-test',$2)
		RETURNING id::text
	`, "strict-square-"+suffix+"@example.test", name+" owner").Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons(name,phone,owner_user_id,active_pos_provider,timezone)
		VALUES($1,$2,$3,'','UTC')
		RETURNING id::text
	`, name, phone, ownerID).Scan(&salonID); err != nil {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, ownerID)
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO salon_settings(salon_id,scheduling_authority) VALUES($1,'owner_manual')`, salonID); err != nil {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id=$1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, ownerID)
		t.Fatal(err)
	}
	return ownerID, salonID
}

func cleanupStrictSquareTenants(t *testing.T, db *sql.DB, salonIDs, ownerIDs []string) {
	t.Helper()
	ctx := context.Background()
	for _, salonID := range salonIDs {
		_, _ = db.ExecContext(ctx, `DELETE FROM salons WHERE id=$1`, salonID)
	}
	for _, ownerID := range ownerIDs {
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, ownerID)
	}
}

func assertStrictSquareStoredConfig(t *testing.T, ctx context.Context, service *integrationconfig.Service, salonID, ownerID string, want, other strictSquareTenantFixture) {
	t.Helper()
	response, err := service.GetAll(ctx, salonID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if response.Square.ClientID != want.ClientID || response.Square.APIBaseURL != "https://"+want.Host || response.Square.ClientID == other.ClientID {
		t.Fatalf("%s public config crossed tenants: %#v", want.Key, response.Square)
	}
	resolved, err := service.ResolveSquareConfig(ctx, salonID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ClientID != want.ClientID || resolved.ClientSecret != want.ClientSecret || resolved.WebhookSignatureKey != want.WebhookKey ||
		resolved.ClientID == other.ClientID || resolved.ClientSecret == other.ClientSecret {
		t.Fatalf("%s runtime config resolved another tenant", want.Key)
	}
}

func assertStrictSquareOAuthURL(t *testing.T, rawURL string, want, other strictSquareTenantFixture) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("client_id") != want.ClientID || parsed.Query().Get("redirect_uri") != want.RedirectURL ||
		strings.Contains(rawURL, other.ClientID) || strings.Contains(rawURL, other.RedirectURL) {
		t.Fatalf("%s OAuth URL crossed tenant configuration: %s", want.Key, rawURL)
	}
}

func tamperStrictSquareStateTenant(t *testing.T, state, salonID string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 5 {
		t.Fatalf("unexpected Square state shape: %q", raw)
	}
	parts[1] = salonID
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, ":")))
}

func assertStrictSquareConnection(t *testing.T, cipher *encryption.TokenCipher, connection *pos.Connection, salonID string, want, other strictSquareTenantFixture) {
	t.Helper()
	if connection == nil || connection.SalonID != salonID || connection.MerchantID != want.MerchantID || connection.MerchantID == other.MerchantID {
		t.Fatalf("%s connection crossed tenants: %#v", want.Key, connection)
	}
	accessToken, err := cipher.Decrypt(connection.AccessTokenEncrypted)
	if err != nil {
		t.Fatal(err)
	}
	if accessToken != want.AccessToken || accessToken == other.AccessToken {
		t.Fatalf("%s connection resolved another tenant token", want.Key)
	}
}

func assertStrictSquareImportedData(t *testing.T, ctx context.Context, db *sql.DB, salonID string, fixture strictSquareTenantFixture) {
	t.Helper()
	checks := []struct {
		query string
		want  string
	}{
		{`SELECT name FROM services WHERE salon_id=$1 AND pos_provider='square' AND pos_service_id=$2`, fixture.ServiceName},
		{`SELECT name FROM staff WHERE salon_id=$1 AND pos_provider='square' AND pos_staff_id=$2`, fixture.StaffName},
		{`SELECT customer.name
		  FROM customers customer
		  JOIN pos_entity_links link
		    ON link.salon_id=customer.salon_id
		   AND link.entity_type='customer'
		   AND link.entity_id=customer.id
		   AND link.provider='square'
		  WHERE customer.salon_id=$1 AND link.provider_entity_id=$2`, fixture.CustomerName},
	}
	for _, check := range checks {
		var got string
		var providerID string
		switch check.want {
		case fixture.ServiceName:
			providerID = fixture.ServiceID
		case fixture.StaffName:
			providerID = fixture.StaffID
		default:
			providerID = fixture.CustomerID
		}
		if err := db.QueryRowContext(ctx, check.query, salonID, providerID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != check.want {
			t.Fatalf("%s imported value=%q, want %q", fixture.Key, got, check.want)
		}
	}
	var periodCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM salon_business_hour_periods WHERE salon_id=$1 AND provider='square' AND provider_location_id=$2`, salonID, fixture.LocationID).Scan(&periodCount); err != nil {
		t.Fatal(err)
	}
	if periodCount != 1 {
		t.Fatalf("%s imported business hour count=%d", fixture.Key, periodCount)
	}
}

func createStrictSquareBooking(t *testing.T, ctx context.Context, db *sql.DB, service *booking.Service, salonID, ownerID string, fixture strictSquareTenantFixture) (*booking.BookingAttempt, *booking.Appointment) {
	t.Helper()
	var serviceID, staffID string
	if err := db.QueryRowContext(ctx, `SELECT id::text FROM services WHERE salon_id=$1 AND pos_provider='square' AND pos_service_id=$2`, salonID, fixture.ServiceID).Scan(&serviceID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT id::text FROM staff WHERE salon_id=$1 AND pos_provider='square' AND pos_staff_id=$2`, salonID, fixture.StaffID).Scan(&staffID); err != nil {
		t.Fatal(err)
	}
	availability, err := service.AvailableSlots(ctx, salonID, ownerID, booking.AvailabilityRequest{
		ServiceID: serviceID, StaffID: staffID, StaffSelectionMode: booking.StaffSelectionSpecific,
		PreferredDate: "2026-08-03", Limit: 3,
	})
	if err != nil {
		t.Fatalf("%s availability: %v", fixture.Key, err)
	}
	if availability.QuoteID == "" || len(availability.Slots) != 1 || availability.Slots[0].StaffID != staffID {
		t.Fatalf("%s availability=%#v", fixture.Key, availability)
	}
	slot := availability.Slots[0]
	attempt, err := service.Create(ctx, salonID, ownerID, booking.CreateBookingRequest{
		OperationKey:        "strict-square-booking-" + fixture.Key + "-" + uuid.NewString(),
		AvailabilityQuoteID: availability.QuoteID,
		SlotFingerprint:     slot.Fingerprint,
		Source:              booking.SourceOwnerDashboard,
		CustomerName:        fixture.CustomerName,
		CustomerPhone:       fixture.CustomerPhone,
		ServiceID:           serviceID,
		StaffID:             staffID,
		StaffSelectionMode:  booking.StaffSelectionSpecific,
		StartTime:           slot.StartTime,
		Notes:               fixture.ServiceName + " tenant-isolation regression",
	})
	if err != nil {
		t.Fatalf("%s create booking: %v", fixture.Key, err)
	}
	if attempt == nil || attempt.Appointment == nil {
		t.Fatalf("%s booking returned no durable appointment: %#v", fixture.Key, attempt)
	}
	return attempt, attempt.Appointment
}

type strictSquareSnapshot map[string]string

func strictSquareTenantSnapshot(t *testing.T, ctx context.Context, db *sql.DB, salonID string) strictSquareSnapshot {
	t.Helper()
	tables := []string{
		"salon_integration_configs", "pos_connections", "pos_oauth_states", "pos_sync_logs",
		"services", "staff", "salon_business_hour_periods", "customers",
		"external_provider_scheduling_capability_evidence", "square_booking_webhook_events",
		"booking_attempts", "appointments",
	}
	snapshot := make(strictSquareSnapshot, len(tables))
	for _, table := range tables {
		query := fmt.Sprintf(`
			SELECT md5(COALESCE(jsonb_agg(to_jsonb(row_data) ORDER BY row_data.id),'[]'::jsonb)::text)
			FROM (SELECT * FROM %s WHERE salon_id=$1) row_data
		`, table)
		var digest string
		if err := db.QueryRowContext(ctx, query, salonID).Scan(&digest); err != nil {
			t.Fatalf("snapshot %s: %v", table, err)
		}
		snapshot[table] = digest
	}
	return snapshot
}

func signStrictSquareWebhook(notificationURL, signatureKey string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(signatureKey))
	_, _ = mac.Write([]byte(notificationURL))
	_, _ = mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func stringPointer(value string) *string {
	return &value
}
