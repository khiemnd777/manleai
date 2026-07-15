package pos_square

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/modules/pos"
)

func TestSquareWriteErrorClassifiesOnlyDefinitiveClientRejectionsAsRetrySafe(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		outcome pos.WriteOutcome
		phase   string
	}{
		{name: "validation rejection", err: &squareHTTPError{StatusCode: http.StatusBadRequest, Code: "BAD_REQUEST"}, outcome: pos.WriteOutcomeDefinitiveFailure, phase: pos.WritePhaseResponse},
		{name: "permission rejection", err: &squareHTTPError{StatusCode: http.StatusForbidden, Code: "FORBIDDEN"}, outcome: pos.WriteOutcomeDefinitiveFailure, phase: pos.WritePhaseResponse},
		{name: "request timeout", err: &squareHTTPError{StatusCode: http.StatusRequestTimeout}, outcome: pos.WriteOutcomeUnknown, phase: pos.WritePhaseResponse},
		{name: "unclassified client response", err: &squareHTTPError{StatusCode: http.StatusTeapot}, outcome: pos.WriteOutcomeUnknown, phase: pos.WritePhaseResponse},
		{name: "server error", err: &squareHTTPError{StatusCode: http.StatusInternalServerError}, outcome: pos.WriteOutcomeUnknown, phase: pos.WritePhaseResponse},
		{name: "truncated response", err: &squareResponseError{Err: io.ErrUnexpectedEOF}, outcome: pos.WriteOutcomeUnknown, phase: pos.WritePhaseResponse},
		{name: "connection reset", err: io.ErrClosedPipe, outcome: pos.WriteOutcomeUnknown, phase: pos.WritePhaseDispatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := squareWriteError(tt.err, pos.WritePhaseDispatch)
			if got := pos.WriteOutcomeForError(err); got != tt.outcome {
				t.Fatalf("outcome = %s, want %s", got, tt.outcome)
			}
			var writeErr *pos.WriteError
			if !errors.As(err, &writeErr) || writeErr.Phase != tt.phase {
				t.Fatalf("write error = %#v, want %s phase", err, tt.phase)
			}
		})
	}
}

func TestDoJSONSendsSquareVersionHeader(t *testing.T) {
	transport := &capturingTransport{}
	adapter := &SquareAdapter{
		cfg: config.SquareConfig{
			APIVersion: "2026-05-20",
		},
		httpClient: &http.Client{Transport: transport},
	}
	var out map[string]bool
	if err := adapter.doJSON(context.Background(), adapter.cfg, http.MethodGet, "https://square.test/v2/locations", "", nil, &out); err != nil {
		t.Fatalf("doJSON failed: %v", err)
	}
	if got := transport.squareVersion; got != "2026-05-20" {
		t.Fatalf("Square-Version = %q, want 2026-05-20", got)
	}
	if !out["ok"] {
		t.Fatalf("unexpected response: %#v", out)
	}
}

func TestSquareScopesIncludeProductionDemoSetupWritePermissions(t *testing.T) {
	scopes := map[string]bool{}
	for _, scope := range squareScopes(config.SquareConfig{Environment: "production"}) {
		scopes[scope] = true
	}
	for _, required := range []string{"APPOINTMENTS_READ", "APPOINTMENTS_ALL_READ", "APPOINTMENTS_WRITE", "APPOINTMENTS_ALL_WRITE", "ITEMS_WRITE", "EMPLOYEES_WRITE"} {
		if !scopes[required] {
			t.Fatalf("squareScopes missing %s", required)
		}
	}
}

func TestSquareScopesUseBuyerBookingWritesInSandbox(t *testing.T) {
	scopes := map[string]bool{}
	for _, scope := range squareScopes(config.SquareConfig{Environment: "sandbox"}) {
		scopes[scope] = true
	}
	for _, required := range []string{"APPOINTMENTS_READ", "APPOINTMENTS_ALL_READ", "APPOINTMENTS_WRITE", "ITEMS_WRITE", "EMPLOYEES_WRITE"} {
		if !scopes[required] {
			t.Fatalf("squareScopes missing sandbox scope %s", required)
		}
	}
	if scopes["APPOINTMENTS_ALL_WRITE"] {
		t.Fatalf("sandbox scope list should not request APPOINTMENTS_ALL_WRITE")
	}
}

func TestConnectionScopesPreservesSquareTokenResponseScopes(t *testing.T) {
	scopes := connectionScopes("APPOINTMENTS_WRITE CUSTOMERS_READ", config.SquareConfig{Environment: "sandbox"})
	if len(scopes) != 2 || scopes[0] != "APPOINTMENTS_WRITE" || scopes[1] != "CUSTOMERS_READ" {
		t.Fatalf("connection scopes = %v, want Square token response scopes", scopes)
	}
}

func TestConnectionScopesFallbackUsesSandboxBuyerBookingWrites(t *testing.T) {
	scopes := map[string]bool{}
	for _, scope := range connectionScopes("", config.SquareConfig{Environment: "sandbox"}) {
		scopes[scope] = true
	}
	if !scopes["APPOINTMENTS_WRITE"] {
		t.Fatalf("fallback sandbox scopes should include APPOINTMENTS_WRITE")
	}
	if scopes["APPOINTMENTS_ALL_WRITE"] {
		t.Fatalf("fallback sandbox scopes should not include APPOINTMENTS_ALL_WRITE")
	}
}

func TestMapSquareBusinessHourPeriodsPreservesSplitDayPeriods(t *testing.T) {
	periods := mapSquareBusinessHourPeriods([]squareBusinessHourPeriod{
		{DayOfWeek: "MON", StartLocalTime: "09:30", EndLocalTime: "12:00"},
		{DayOfWeek: "MON", StartLocalTime: "13:00", EndLocalTime: "19:00"},
		{DayOfWeek: "SUNDAY", StartLocalTime: "10:00", EndLocalTime: "15:00"},
		{DayOfWeek: "BAD", StartLocalTime: "09:00", EndLocalTime: "10:00"},
	})

	if len(periods) != 3 {
		t.Fatalf("periods = %#v, want three mapped periods", periods)
	}
	if periods[0].DayOfWeek != 1 || periods[0].ProviderPeriodIndex != 0 {
		t.Fatalf("first monday period = %#v, want day 1 index 0", periods[0])
	}
	if periods[1].DayOfWeek != 1 || periods[1].ProviderPeriodIndex != 1 {
		t.Fatalf("second monday period = %#v, want day 1 index 1", periods[1])
	}
	if periods[2].DayOfWeek != 0 || periods[2].ProviderPeriodIndex != 0 {
		t.Fatalf("sunday period = %#v, want day 0 index 0", periods[2])
	}
}

func TestOAuthURLDoesNotSendSessionFalseInSandbox(t *testing.T) {
	adapter := &SquareAdapter{
		cfg: config.SquareConfig{
			Environment: "sandbox",
			ClientID:    "sandbox-client-id",
			RedirectURL: "https://demo.test/api/integrations/square/callback",
		},
	}
	oauthURL, err := adapter.OAuthURL(context.Background(), "salon_1", "state_1")
	if err != nil {
		t.Fatalf("OAuthURL failed: %v", err)
	}
	parsed, err := url.Parse(oauthURL)
	if err != nil {
		t.Fatalf("parse OAuth URL failed: %v", err)
	}
	if parsed.Host != "connect.squareupsandbox.com" {
		t.Fatalf("OAuth host = %q, want sandbox host", parsed.Host)
	}
	if got := parsed.Query().Get("session"); got != "" {
		t.Fatalf("sandbox session parameter = %q, want omitted", got)
	}
	scopeValues := strings.Fields(parsed.Query().Get("scope"))
	scopes := map[string]bool{}
	for _, scope := range scopeValues {
		scopes[scope] = true
	}
	if !scopes["APPOINTMENTS_WRITE"] {
		t.Fatalf("sandbox OAuth scope should include APPOINTMENTS_WRITE: %v", scopeValues)
	}
	if scopes["APPOINTMENTS_ALL_WRITE"] {
		t.Fatalf("sandbox OAuth scope should not include APPOINTMENTS_ALL_WRITE: %v", scopeValues)
	}
}

func TestOAuthURLSendsSessionFalseInProduction(t *testing.T) {
	adapter := &SquareAdapter{
		cfg: config.SquareConfig{
			Environment: "production",
			ClientID:    "production-client-id",
			RedirectURL: "https://demo.test/api/integrations/square/callback",
		},
	}
	oauthURL, err := adapter.OAuthURL(context.Background(), "salon_1", "state_1")
	if err != nil {
		t.Fatalf("OAuthURL failed: %v", err)
	}
	parsed, err := url.Parse(oauthURL)
	if err != nil {
		t.Fatalf("parse OAuth URL failed: %v", err)
	}
	if parsed.Host != "connect.squareup.com" {
		t.Fatalf("OAuth host = %q, want production host", parsed.Host)
	}
	if got := parsed.Query().Get("session"); got != "false" {
		t.Fatalf("production session parameter = %q, want false", got)
	}
	scopeValues := strings.Fields(parsed.Query().Get("scope"))
	scopes := map[string]bool{}
	for _, scope := range scopeValues {
		scopes[scope] = true
	}
	if !scopes["APPOINTMENTS_ALL_WRITE"] {
		t.Fatalf("production OAuth scope should include APPOINTMENTS_ALL_WRITE: %v", scopeValues)
	}
}

func TestMapCatalogServicesKeepsVariationVersion(t *testing.T) {
	var response squareCatalogResponse
	if err := json.Unmarshal([]byte(`{"objects":[{"id":"ITEM_1","type":"ITEM","version":100,"item_data":{"name":"Classic Manicure","description":"Manicure service","variations":[{"id":"VAR_1","type":"ITEM_VARIATION","version":1781282541083,"item_variation_data":{"name":"Regular","service_duration":1800000,"available_for_booking":true,"price_money":{"amount":3000,"currency":"USD"}}}]}}]}`), &response); err != nil {
		t.Fatalf("decode catalog response: %v", err)
	}
	services := mapCatalogServices(response)

	if len(services) != 1 {
		t.Fatalf("expected one service, got %d", len(services))
	}
	if services[0].POSServiceID != "VAR_1" {
		t.Fatalf("unexpected service id: %s", services[0].POSServiceID)
	}
	if services[0].POSServiceVersion != 1781282541083 {
		t.Fatalf("unexpected service version: %d", services[0].POSServiceVersion)
	}
	if services[0].DurationMinutes != 30 {
		t.Fatalf("unexpected duration: %d", services[0].DurationMinutes)
	}
	if !services[0].AIBookable {
		t.Fatal("expected positively eligible variation to be AI-bookable")
	}
}

func TestMapCatalogServicesRequiresPositiveBookingEligibilityAndDuration(t *testing.T) {
	var response squareCatalogResponse
	if err := json.Unmarshal([]byte(`{"objects":[
		{"id":"ITEM_NO_VARIATIONS","type":"ITEM","item_data":{"name":"No variation"}},
		{"id":"ITEM_SERVICES","type":"ITEM","item_data":{"name":"Service","variations":[
			{"id":"VAR_BOOKABLE","type":"ITEM_VARIATION","item_variation_data":{"name":"Bookable","service_duration":1800000,"available_for_booking":true}},
			{"id":"VAR_PROVIDER_DISABLED","type":"ITEM_VARIATION","item_variation_data":{"name":"Provider disabled","service_duration":1800000,"available_for_booking":false}},
			{"id":"VAR_ELIGIBILITY_MISSING","type":"ITEM_VARIATION","item_variation_data":{"name":"Eligibility missing","service_duration":1800000}},
			{"id":"VAR_DURATION_MISSING","type":"ITEM_VARIATION","item_variation_data":{"name":"Duration missing","available_for_booking":true}},
			{"id":"VAR_SUBMINUTE_DURATION","type":"ITEM_VARIATION","item_variation_data":{"name":"Subminute duration","service_duration":30000,"available_for_booking":true}}
		]}}
	]}`), &response); err != nil {
		t.Fatalf("decode catalog response: %v", err)
	}

	services := mapCatalogServices(response)
	if len(services) != 6 {
		t.Fatalf("services = %#v, want six imported rows", services)
	}
	bookableByID := make(map[string]bool, len(services))
	for _, service := range services {
		bookableByID[service.POSServiceID] = service.AIBookable
	}
	if !bookableByID["VAR_BOOKABLE"] {
		t.Fatal("VAR_BOOKABLE should be AI-bookable")
	}
	for _, providerID := range []string{"ITEM_NO_VARIATIONS", "VAR_PROVIDER_DISABLED", "VAR_ELIGIBILITY_MISSING", "VAR_DURATION_MISSING", "VAR_SUBMINUTE_DURATION"} {
		if bookableByID[providerID] {
			t.Fatalf("%s should not be AI-bookable", providerID)
		}
	}
}

func TestListServicesPaginatesAndFiltersSelectedLocation(t *testing.T) {
	transport := &sequenceTransport{responses: []string{
		`{"objects":[{"id":"ITEM_1","type":"ITEM","present_at_all_locations":false,"present_at_location_ids":["LOC_1"],"item_data":{"name":"Classic Manicure","variations":[{"id":"VAR_1","type":"ITEM_VARIATION","version":101,"present_at_all_locations":false,"present_at_location_ids":["LOC_1"],"item_variation_data":{"name":"Regular","service_duration":1800000,"available_for_booking":true,"price_money":{"amount":3000,"currency":"USD"}}}]} }],"cursor":"next-page"}`,
		`{"objects":[{"id":"ITEM_2","type":"ITEM","absent_at_location_ids":["LOC_1"],"item_data":{"name":"Hidden Service","variations":[{"id":"VAR_2","type":"ITEM_VARIATION","version":102,"item_variation_data":{"name":"Regular","service_duration":1800000}}]}},{"id":"ITEM_3","type":"ITEM","item_data":{"name":"Gel Manicure","variations":[{"id":"VAR_3","type":"ITEM_VARIATION","version":103,"is_deleted":true,"item_variation_data":{"name":"Regular","service_duration":2700000}}]}}]}`,
	}}
	adapter := &SquareAdapter{
		cfg:        config.SquareConfig{APIBaseURL: "https://square.test"},
		httpClient: &http.Client{Transport: transport},
	}

	services, err := adapter.listServices(context.Background(), adapter.cfg, "token", "LOC_1")
	if err != nil {
		t.Fatalf("listServices failed: %v", err)
	}
	if len(services) != 1 || services[0].POSServiceID != "VAR_1" {
		t.Fatalf("services = %#v, want only VAR_1", services)
	}
	if !services[0].AIBookable {
		t.Fatal("VAR_1 should be AI-bookable")
	}
	if len(transport.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(transport.requests))
	}
	if got := transport.requests[1].URL.Query().Get("cursor"); got != "next-page" {
		t.Fatalf("second cursor = %q, want next-page", got)
	}
}

func TestListServicesRejectsRepeatedPaginationCursor(t *testing.T) {
	transport := &sequenceTransport{responses: []string{
		`{"objects":[],"cursor":"same"}`,
		`{"objects":[],"cursor":"same"}`,
	}}
	adapter := &SquareAdapter{
		cfg:        config.SquareConfig{APIBaseURL: "https://square.test"},
		httpClient: &http.Client{Transport: transport},
	}

	_, err := adapter.listServices(context.Background(), adapter.cfg, "token", "LOC_1")
	if err == nil || !strings.Contains(err.Error(), "repeated cursor") {
		t.Fatalf("error = %v, want repeated cursor error", err)
	}
}

func TestListStaffPaginatesAndScopesSelectedLocation(t *testing.T) {
	transport := &sequenceTransport{responses: []string{
		`{"team_members":[{"id":"TEAM_1","given_name":"Linh","family_name":"Tran","email_address":"linh@example.com","phone_number":"+13125550101","status":"ACTIVE"},{"id":"TEAM_2","given_name":"Not","family_name":"Bookable","status":"ACTIVE"}],"cursor":"next-team-page"}`,
		`{"team_members":[{"id":"TEAM_3","status":"ACTIVE"},{"id":"TEAM_INACTIVE","given_name":"Inactive","status":"INACTIVE"},{"id":"TEAM_STATUS_MISSING","given_name":"Unknown Status"}]}`,
		`{"team_member_booking_profiles":[{"team_member_id":"TEAM_1","display_name":"Profile Linh","is_bookable":true},{"team_member_id":"TEAM_2","display_name":"Not Bookable","is_bookable":false},{"team_member_id":"TEAM_INACTIVE","display_name":"Inactive","is_bookable":true},{"team_member_id":"TEAM_STATUS_MISSING","display_name":"Unknown Status","is_bookable":true}],"cursor":"next-profile-page"}`,
		`{"team_member_booking_profiles":[{"team_member_id":"TEAM_3","display_name":"Profile Fallback","is_bookable":true}]}`,
	}}
	adapter := &SquareAdapter{
		cfg:        config.SquareConfig{APIBaseURL: "https://square.test"},
		httpClient: &http.Client{Transport: transport},
	}

	staff, err := adapter.listStaff(context.Background(), adapter.cfg, "token", "LOC_1")
	if err != nil {
		t.Fatalf("listStaff failed: %v", err)
	}
	if len(staff) != 2 || staff[0].POSStaffID != "TEAM_1" || staff[1].POSStaffID != "TEAM_3" {
		t.Fatalf("staff = %#v, want only active team members with bookable profiles", staff)
	}
	if staff[0].Name != "Linh Tran" || staff[0].Email != "linh@example.com" || staff[0].Phone != "+13125550101" {
		t.Fatalf("team contact fields were not preserved: %#v", staff[0])
	}
	if staff[1].Name != "Profile Fallback" {
		t.Fatalf("profile display-name fallback = %q, want Profile Fallback", staff[1].Name)
	}
	if len(transport.requestBodies) != 4 {
		t.Fatalf("request bodies = %d, want 4", len(transport.requestBodies))
	}
	var first squareTeamMembersSearchRequest
	if err := json.Unmarshal([]byte(transport.requestBodies[0]), &first); err != nil {
		t.Fatalf("decode first request: %v", err)
	}
	if len(first.Query.Filter.LocationIDs) != 1 || first.Query.Filter.LocationIDs[0] != "LOC_1" {
		t.Fatalf("location filter = %#v, want LOC_1", first.Query.Filter.LocationIDs)
	}
	var second squareTeamMembersSearchRequest
	if err := json.Unmarshal([]byte(transport.requestBodies[1]), &second); err != nil {
		t.Fatalf("decode second request: %v", err)
	}
	if second.Cursor != "next-team-page" {
		t.Fatalf("second cursor = %q, want next-team-page", second.Cursor)
	}
	for index := 2; index < 4; index++ {
		request := transport.requests[index]
		if request.Method != http.MethodGet || request.URL.Path != "/v2/bookings/team-member-booking-profiles" {
			t.Fatalf("profile request %d = %s %s", index, request.Method, request.URL.Path)
		}
		query := request.URL.Query()
		if query.Get("bookable_only") != "true" || query.Get("location_id") != "LOC_1" || query.Get("limit") != "100" {
			t.Fatalf("profile request query = %v", query)
		}
	}
	if got := transport.requests[3].URL.Query().Get("cursor"); got != "next-profile-page" {
		t.Fatalf("profile second cursor = %q, want next-profile-page", got)
	}
}

func TestConnectionMatchesProviderFenceRequiresExactReadySnapshot(t *testing.T) {
	syncedAt := time.Now().UTC()
	ready := &pos.Connection{
		Status:             pos.StatusActive,
		LocationID:         "loc_1",
		SnapshotGeneration: 7,
		LastSyncAt:         &syncedAt,
	}
	want := pos.ProviderFence{LocationID: "loc_1", SnapshotGeneration: 7}
	if !connectionMatchesProviderFence(ready, want) {
		t.Fatal("exact active synced provider fence should match")
	}
	for _, test := range []struct {
		name       string
		connection pos.Connection
		fence      pos.ProviderFence
	}{
		{name: "location changed", connection: *ready, fence: pos.ProviderFence{LocationID: "loc_2", SnapshotGeneration: 7}},
		{name: "generation changed", connection: *ready, fence: pos.ProviderFence{LocationID: "loc_1", SnapshotGeneration: 6}},
		{name: "sync incomplete", connection: func() pos.Connection { item := *ready; item.LastSyncAt = nil; return item }(), fence: want},
		{name: "not active", connection: func() pos.Connection { item := *ready; item.Status = pos.StatusConnected; return item }(), fence: want},
		{name: "missing expected fence", connection: *ready, fence: pos.ProviderFence{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if connectionMatchesProviderFence(&test.connection, test.fence) {
				t.Fatalf("connection unexpectedly matched stale fence: connection=%#v fence=%#v", test.connection, test.fence)
			}
		})
	}
}

func TestValidateSquareBookingLocationsFailsWholePageOnNonemptyMismatch(t *testing.T) {
	bookings := []squareBooking{
		{ID: "booking_1", LocationID: "loc_1"},
		{ID: "booking_2", LocationID: ""},
	}
	if err := validateSquareBookingLocations(bookings, "loc_1"); err != nil {
		t.Fatalf("matching and omitted booking locations should pass: %v", err)
	}

	bookings = append(bookings, squareBooking{ID: "booking_3", LocationID: "loc_2"})
	if err := validateSquareBookingLocations(bookings, "loc_1"); !errors.Is(err, pos.ErrStaleProviderFence) {
		t.Fatalf("mismatched page error = %v, want pos.ErrStaleProviderFence", err)
	}
	if err := validateSquareBookingLocations(nil, " "); !errors.Is(err, pos.ErrStaleProviderFence) {
		t.Fatalf("missing expected location error = %v, want pos.ErrStaleProviderFence", err)
	}
}

func TestListStaffRejectsRepeatedTeamMemberCursor(t *testing.T) {
	transport := &sequenceTransport{responses: []string{
		`{"team_members":[],"cursor":"same-team-cursor"}`,
		`{"team_members":[],"cursor":"same-team-cursor"}`,
	}}
	adapter := &SquareAdapter{
		cfg:        config.SquareConfig{APIBaseURL: "https://square.test"},
		httpClient: &http.Client{Transport: transport},
	}

	staff, err := adapter.listStaff(context.Background(), adapter.cfg, "token", "LOC_1")
	if err == nil || !strings.Contains(err.Error(), "team member pagination repeated cursor") {
		t.Fatalf("staff/error = %#v/%v, want repeated team member cursor error", staff, err)
	}
}

func TestListStaffRejectsRepeatedBookingProfileCursor(t *testing.T) {
	transport := &sequenceTransport{responses: []string{
		`{"team_members":[{"id":"TEAM_1","status":"ACTIVE"}]}`,
		`{"team_member_booking_profiles":[{"team_member_id":"TEAM_1","display_name":"Linh","is_bookable":true}],"cursor":"same-profile-cursor"}`,
		`{"team_member_booking_profiles":[],"cursor":"same-profile-cursor"}`,
	}}
	adapter := &SquareAdapter{
		cfg:        config.SquareConfig{APIBaseURL: "https://square.test"},
		httpClient: &http.Client{Transport: transport},
	}

	staff, err := adapter.listStaff(context.Background(), adapter.cfg, "token", "LOC_1")
	if err == nil || !strings.Contains(err.Error(), "booking profile pagination repeated cursor") {
		t.Fatalf("staff/error = %#v/%v, want repeated booking profile cursor error", staff, err)
	}
}

func TestListCustomersPaginatesAndRejectsRepeatedCursor(t *testing.T) {
	transport := &sequenceTransport{responses: []string{
		`{"customers":[{"id":"CUSTOMER_1","given_name":"Linh","family_name":"Tran"}],"cursor":"next-customer-page"}`,
		`{"customers":[{"id":"CUSTOMER_2","given_name":"Mai","family_name":"Nguyen"}],"cursor":"next-customer-page"}`,
	}}
	adapter := &SquareAdapter{
		cfg:        config.SquareConfig{APIBaseURL: "https://square.test"},
		httpClient: &http.Client{Transport: transport},
	}

	customers, err := adapter.listCustomers(context.Background(), adapter.cfg, "token")
	if err == nil || !strings.Contains(err.Error(), "repeated cursor") {
		t.Fatalf("customers/error = %#v/%v, want repeated cursor error", customers, err)
	}
	if len(customers) != 0 {
		t.Fatalf("customers should not be returned on incomplete pagination: %#v", customers)
	}
	if len(transport.requestBodies) != 2 {
		t.Fatalf("request bodies = %d, want 2", len(transport.requestBodies))
	}
	var second squareCustomerSearchRequest
	if err := json.Unmarshal([]byte(transport.requestBodies[1]), &second); err != nil {
		t.Fatalf("decode second request: %v", err)
	}
	if second.Cursor != "next-customer-page" {
		t.Fatalf("second cursor = %q, want next-customer-page", second.Cursor)
	}
}

func TestBuildSquareCustomerSearchRequestUsesExactPhone(t *testing.T) {
	request, err := buildSquareCustomerSearchRequest("+13125550101")
	if err != nil {
		t.Fatalf("build search request failed: %v", err)
	}
	if request.Limit != 1 {
		t.Fatalf("limit = %d, want 1", request.Limit)
	}
	if request.Query.Filter.PhoneNumber == nil || request.Query.Filter.PhoneNumber.Exact != "+13125550101" {
		t.Fatalf("unexpected phone filter: %#v", request.Query.Filter.PhoneNumber)
	}
}

func TestBuildSquareCreateCustomerRequestSplitsName(t *testing.T) {
	request, err := buildSquareCreateCustomerRequest(pos.CreateCustomerInput{
		Name:  "Linh Tran",
		Phone: "+13125550101",
		Email: "linh@example.com",
	})
	if err != nil {
		t.Fatalf("build create customer request failed: %v", err)
	}
	if request.IdempotencyKey == "" {
		t.Fatalf("expected idempotency key")
	}
	if request.GivenName != "Linh" || request.FamilyName != "Tran" {
		t.Fatalf("unexpected name split: %s %s", request.GivenName, request.FamilyName)
	}
	if request.PhoneNumber != "+13125550101" || request.EmailAddress != "linh@example.com" {
		t.Fatalf("unexpected contact fields: %#v", request)
	}
}

func TestBuildSquareAvailabilityRequest(t *testing.T) {
	request, err := buildSquareAvailabilityRequest("loc_1", pos.AvailabilityInput{
		ServiceID:       "svc_1",
		StaffID:         "staff_1",
		PreferredDate:   "2026-06-10",
		Timezone:        "America/Chicago",
		DurationMinutes: 45,
	})
	if err != nil {
		t.Fatalf("build availability request failed: %v", err)
	}
	filter := request.Query.Filter
	if filter.LocationID != "loc_1" {
		t.Fatalf("location id = %s, want loc_1", filter.LocationID)
	}
	if filter.StartAtRange.StartAt != "2026-06-10T05:00:00Z" || filter.StartAtRange.EndAt != "2026-06-11T05:00:00Z" {
		t.Fatalf("unexpected date range: %#v", filter.StartAtRange)
	}
	if len(filter.SegmentFilters) != 1 {
		t.Fatalf("expected one segment filter, got %d", len(filter.SegmentFilters))
	}
	segment := filter.SegmentFilters[0]
	if segment.ServiceVariationID != "svc_1" {
		t.Fatalf("service id = %s, want svc_1", segment.ServiceVariationID)
	}
	if segment.TeamMemberIDFilter == nil || len(segment.TeamMemberIDFilter.Any) != 1 || segment.TeamMemberIDFilter.Any[0] != "staff_1" {
		t.Fatalf("unexpected staff filter: %#v", segment.TeamMemberIDFilter)
	}
}

func TestBuildSquareAvailabilityRequestUsesSegmentInputs(t *testing.T) {
	request, err := buildSquareAvailabilityRequest("loc_1", pos.AvailabilityInput{
		PreferredDate: "2026-06-10",
		Timezone:      "America/Chicago",
		Segments: []pos.AvailabilitySegmentInput{
			{ServiceID: "svc_1", StaffID: "staff_1", DurationMinutes: 30},
			{ServiceID: "svc_2", StaffID: "staff_2", DurationMinutes: 45},
		},
	})
	if err != nil {
		t.Fatalf("build availability request failed: %v", err)
	}
	filters := request.Query.Filter.SegmentFilters
	if len(filters) != 2 {
		t.Fatalf("segment filters = %#v, want two", filters)
	}
	if filters[0].ServiceVariationID != "svc_1" || filters[0].TeamMemberIDFilter == nil || filters[0].TeamMemberIDFilter.Any[0] != "staff_1" {
		t.Fatalf("first segment filter = %#v, want svc_1/staff_1", filters[0])
	}
	if filters[1].ServiceVariationID != "svc_2" || filters[1].TeamMemberIDFilter == nil || filters[1].TeamMemberIDFilter.Any[0] != "staff_2" {
		t.Fatalf("second segment filter = %#v, want svc_2/staff_2", filters[1])
	}
}

func TestAvailabilityRangeUsesSalonLocalDayAcrossDST(t *testing.T) {
	tests := []struct {
		date      string
		wantStart string
		wantEnd   string
		wantHours time.Duration
	}{
		{date: "2026-03-08", wantStart: "2026-03-08T06:00:00Z", wantEnd: "2026-03-09T05:00:00Z", wantHours: 23 * time.Hour},
		{date: "2026-11-01", wantStart: "2026-11-01T05:00:00Z", wantEnd: "2026-11-02T06:00:00Z", wantHours: 25 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.date, func(t *testing.T) {
			start, end, err := availabilityRange(tt.date, "America/Chicago")
			if err != nil {
				t.Fatalf("availabilityRange failed: %v", err)
			}
			if start.Format(time.RFC3339) != tt.wantStart || end.Format(time.RFC3339) != tt.wantEnd {
				t.Fatalf("range = %s..%s, want %s..%s", start.Format(time.RFC3339), end.Format(time.RFC3339), tt.wantStart, tt.wantEnd)
			}
			if end.Sub(start) != tt.wantHours {
				t.Fatalf("duration = %s, want %s", end.Sub(start), tt.wantHours)
			}
		})
	}
}

func TestBuildSquareCreateBookingRequestIncludesRequiredSegmentFields(t *testing.T) {
	start := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	request, err := buildSquareCreateBookingRequest("loc_1", pos.CreateAppointmentInput{
		IdempotencyKey:  "attempt-key-1",
		CustomerID:      "cust_1",
		ServiceID:       "svc_1",
		ServiceVersion:  123,
		StaffID:         "staff_1",
		StartTime:       start,
		DurationMinutes: 45,
		Notes:           "First visit",
	})
	if err != nil {
		t.Fatalf("build booking request failed: %v", err)
	}
	if request.IdempotencyKey != "attempt-key-1" {
		t.Fatalf("idempotency key = %s, want attempt-key-1", request.IdempotencyKey)
	}
	if request.Booking.LocationID != "loc_1" || request.Booking.CustomerID != "cust_1" || request.Booking.StartAt != "2026-06-10T15:00:00Z" {
		t.Fatalf("unexpected booking fields: %#v", request.Booking)
	}
	if len(request.Booking.AppointmentSegments) != 1 {
		t.Fatalf("expected one appointment segment")
	}
	segment := request.Booking.AppointmentSegments[0]
	if segment.TeamMemberID != "staff_1" || segment.ServiceVariationID != "svc_1" || segment.ServiceVariationVersion != 123 || segment.DurationMinutes != 45 {
		t.Fatalf("unexpected segment: %#v", segment)
	}

	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal booking request failed: %v", err)
	}
	if !bytes.Contains(payload, []byte("service_variation_version")) {
		t.Fatalf("payload did not include service_variation_version: %s", string(payload))
	}
}

func TestBuildSquareCreateBookingRequestUsesMultipleSegments(t *testing.T) {
	start := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	request, err := buildSquareCreateBookingRequest("loc_1", pos.CreateAppointmentInput{
		IdempotencyKey: "attempt-key-1",
		CustomerID:     "cust_1",
		StartTime:      start,
		Notes:          "Two services",
		Segments: []pos.AppointmentSegmentInput{
			{ServiceID: "svc_1", ServiceVersion: 123, StaffID: "staff_1", DurationMinutes: 30},
			{ServiceID: "svc_2", ServiceVersion: 456, StaffID: "staff_2", DurationMinutes: 45},
		},
	})
	if err != nil {
		t.Fatalf("build booking request failed: %v", err)
	}
	segments := request.Booking.AppointmentSegments
	if len(segments) != 2 {
		t.Fatalf("segments = %#v, want two", segments)
	}
	if segments[0].ServiceVariationID != "svc_1" || segments[0].ServiceVariationVersion != 123 || segments[0].TeamMemberID != "staff_1" || segments[0].DurationMinutes != 30 {
		t.Fatalf("first segment = %#v, want svc_1/staff_1", segments[0])
	}
	if segments[1].ServiceVariationID != "svc_2" || segments[1].ServiceVariationVersion != 456 || segments[1].TeamMemberID != "staff_2" || segments[1].DurationMinutes != 45 {
		t.Fatalf("second segment = %#v, want svc_2/staff_2", segments[1])
	}
}

func TestBuildSquareCreateBookingRequestRequiresServiceVersion(t *testing.T) {
	_, err := buildSquareCreateBookingRequest("loc_1", pos.CreateAppointmentInput{
		IdempotencyKey:  "attempt-key-1",
		CustomerID:      "cust_1",
		ServiceID:       "svc_1",
		StaffID:         "staff_1",
		StartTime:       time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC),
		DurationMinutes: 45,
	})
	if err == nil {
		t.Fatalf("expected missing service version error")
	}
}

func TestBuildSquareUpdateBookingRequestIncludesVersionAndSegment(t *testing.T) {
	start := time.Date(2026, 6, 11, 16, 0, 0, 0, time.UTC)
	request, err := buildSquareUpdateBookingRequest("loc_1", pos.RescheduleInput{
		IdempotencyKey:  "attempt-key-2",
		BookingVersion:  7,
		ServiceID:       "svc_1",
		ServiceVersion:  123,
		StaffID:         "staff_1",
		StartTime:       start,
		DurationMinutes: 45,
		Notes:           "Rescheduled by owner",
	})
	if err != nil {
		t.Fatalf("build update booking request failed: %v", err)
	}
	if request.IdempotencyKey != "attempt-key-2" {
		t.Fatalf("idempotency key = %s, want attempt-key-2", request.IdempotencyKey)
	}
	if request.Booking.Version != 7 || request.Booking.LocationID != "loc_1" || request.Booking.StartAt != "2026-06-11T16:00:00Z" {
		t.Fatalf("unexpected booking fields: %#v", request.Booking)
	}
	if len(request.Booking.AppointmentSegments) != 1 {
		t.Fatalf("expected one appointment segment")
	}
	segment := request.Booking.AppointmentSegments[0]
	if segment.TeamMemberID != "staff_1" || segment.ServiceVariationID != "svc_1" || segment.ServiceVariationVersion != 123 || segment.DurationMinutes != 45 {
		t.Fatalf("unexpected segment: %#v", segment)
	}
}

func TestBuildSquareUpdateBookingRequestUsesMultipleSegments(t *testing.T) {
	start := time.Date(2026, 6, 11, 16, 0, 0, 0, time.UTC)
	request, err := buildSquareUpdateBookingRequest("loc_1", pos.RescheduleInput{
		IdempotencyKey: "attempt-key-2",
		BookingVersion: 7,
		StartTime:      start,
		Notes:          "Rescheduled two services",
		Segments: []pos.AppointmentSegmentInput{
			{ServiceID: "svc_1", ServiceVersion: 123, StaffID: "staff_1", DurationMinutes: 30},
			{ServiceID: "svc_2", ServiceVersion: 456, StaffID: "staff_2", DurationMinutes: 45},
		},
	})
	if err != nil {
		t.Fatalf("build update booking request failed: %v", err)
	}
	if request.Booking.Version != 7 {
		t.Fatalf("booking version = %d, want 7", request.Booking.Version)
	}
	segments := request.Booking.AppointmentSegments
	if len(segments) != 2 {
		t.Fatalf("segments = %#v, want two", segments)
	}
	if segments[0].ServiceVariationID != "svc_1" || segments[1].ServiceVariationID != "svc_2" {
		t.Fatalf("segments = %#v, want ordered service variations", segments)
	}
}

func TestBuildSquareCancelBookingRequestIncludesVersion(t *testing.T) {
	request, err := buildSquareCancelBookingRequest(pos.CancelInput{
		IdempotencyKey: "attempt-key-3",
		BookingVersion: 7,
		Reason:         "Customer requested cancellation",
	})
	if err != nil {
		t.Fatalf("build cancel booking request failed: %v", err)
	}
	if request.IdempotencyKey != "attempt-key-3" {
		t.Fatalf("idempotency key = %s, want attempt-key-3", request.IdempotencyKey)
	}
	if request.BookingVersion != 7 {
		t.Fatalf("booking version = %d, want 7", request.BookingVersion)
	}
}

func TestBuildSquareCancelBookingRequestAllowsZeroVersion(t *testing.T) {
	request, err := buildSquareCancelBookingRequest(pos.CancelInput{
		IdempotencyKey: "attempt-key-3",
		BookingVersion: 0,
	})
	if err != nil {
		t.Fatalf("build cancel request failed: %v", err)
	}
	if request.BookingVersion != 0 {
		t.Fatalf("booking version = %d, want 0", request.BookingVersion)
	}
}

func TestBuildSquareCancelBookingRequestRequiresVersion(t *testing.T) {
	_, err := buildSquareCancelBookingRequest(pos.CancelInput{IdempotencyKey: "attempt-key-3", BookingVersion: -1})
	if err == nil {
		t.Fatalf("expected missing booking version error")
	}
}

func TestMapSquareAvailabilities(t *testing.T) {
	slots := mapSquareAvailabilities(squareAvailabilityResponse{
		Availabilities: []squareAvailability{
			{
				StartAt: "2026-06-10T15:00:00Z",
				AppointmentSegments: []squareAppointmentSegment{
					{
						DurationMinutes: 45,
						TeamMemberID:    "staff_1",
					},
				},
			},
		},
	}, 30)
	if len(slots) != 1 {
		t.Fatalf("expected one slot, got %d", len(slots))
	}
	if slots[0].StaffID != "staff_1" || slots[0].EndTime.Sub(slots[0].StartTime) != 45*time.Minute {
		t.Fatalf("unexpected slot: %#v", slots[0])
	}
}

func TestMapSquareAvailabilitiesSumsSegmentDurations(t *testing.T) {
	slots := mapSquareAvailabilities(squareAvailabilityResponse{
		Availabilities: []squareAvailability{
			{
				StartAt: "2026-06-10T15:00:00Z",
				AppointmentSegments: []squareAppointmentSegment{
					{DurationMinutes: 30, TeamMemberID: "staff_1"},
					{DurationMinutes: 45, TeamMemberID: "staff_2"},
				},
			},
		},
	}, 0)
	if len(slots) != 1 {
		t.Fatalf("expected one slot, got %d", len(slots))
	}
	if slots[0].EndTime.Sub(slots[0].StartTime) != 75*time.Minute {
		t.Fatalf("unexpected duration: %s", slots[0].EndTime.Sub(slots[0].StartTime))
	}
	if slots[0].StaffID != "staff_1" {
		t.Fatalf("slot staff id = %s, want first segment staff", slots[0].StaffID)
	}
	if len(slots[0].Segments) != 2 || slots[0].Segments[1].ServiceID != "" || slots[0].Segments[1].StaffID != "staff_2" {
		t.Fatalf("slot segments = %#v, want mapped Square segments", slots[0].Segments)
	}
}

func TestMapSquareBooking(t *testing.T) {
	appointment, err := mapSquareBooking(squareBooking{
		ID:      "booking_1",
		Version: 4,
		Status:  "ACCEPTED",
		StartAt: "2026-06-10T15:00:00Z",
		AppointmentSegments: []squareAppointmentSegment{
			{DurationMinutes: 45},
		},
	}, 30)
	if err != nil {
		t.Fatalf("map booking failed: %v", err)
	}
	if appointment.POSAppointmentID != "booking_1" || appointment.Status != "accepted" {
		t.Fatalf("unexpected appointment: %#v", appointment)
	}
	if appointment.POSAppointmentVersion != 4 {
		t.Fatalf("booking version = %d, want 4", appointment.POSAppointmentVersion)
	}
	if appointment.EndTime.Sub(appointment.StartTime) != 45*time.Minute {
		t.Fatalf("unexpected duration: %s", appointment.EndTime.Sub(appointment.StartTime))
	}
}

func TestMapSquareBookingSumsSegmentDurations(t *testing.T) {
	appointment, err := mapSquareBooking(squareBooking{
		ID:      "booking_1",
		Version: 4,
		Status:  "ACCEPTED",
		StartAt: "2026-06-10T15:00:00Z",
		AppointmentSegments: []squareAppointmentSegment{
			{DurationMinutes: 30},
			{DurationMinutes: 45},
		},
	}, 0)
	if err != nil {
		t.Fatalf("map booking failed: %v", err)
	}
	if appointment.EndTime.Sub(appointment.StartTime) != 75*time.Minute {
		t.Fatalf("unexpected duration: %s", appointment.EndTime.Sub(appointment.StartTime))
	}
}

func TestMapSquareBookingAllowsZeroVersion(t *testing.T) {
	var booking squareBooking
	if err := json.Unmarshal([]byte(`{"id":"booking_1","version":0,"status":"ACCEPTED","start_at":"2026-06-10T15:00:00Z","appointment_segments":[{"duration_minutes":45}]}`), &booking); err != nil {
		t.Fatalf("unmarshal booking failed: %v", err)
	}
	appointment, err := mapSquareBooking(booking, 30)
	if err != nil {
		t.Fatalf("map booking failed: %v", err)
	}
	if appointment.POSAppointmentVersion != 0 {
		t.Fatalf("booking version = %d, want 0", appointment.POSAppointmentVersion)
	}
}

func TestMapSquareBookingMarksMissingVersion(t *testing.T) {
	var booking squareBooking
	if err := json.Unmarshal([]byte(`{"id":"booking_1","status":"ACCEPTED","start_at":"2026-06-10T15:00:00Z","appointment_segments":[{"duration_minutes":45}]}`), &booking); err != nil {
		t.Fatalf("unmarshal booking failed: %v", err)
	}
	appointment, err := mapSquareBooking(booking, 30)
	if err != nil {
		t.Fatalf("map booking failed: %v", err)
	}
	if appointment.POSAppointmentVersion != -1 {
		t.Fatalf("booking version = %d, want -1", appointment.POSAppointmentVersion)
	}
}

func TestMapSquareCancelledBookingAllowsMissingScheduleFields(t *testing.T) {
	appointment, err := mapSquareBooking(squareBooking{
		ID:      "booking_1",
		Version: 9,
		Status:  "CANCELLED_BY_CUSTOMER",
	}, 0)
	if err != nil {
		t.Fatalf("map cancelled booking failed: %v", err)
	}
	if appointment.POSAppointmentID != "booking_1" || appointment.POSAppointmentVersion != 9 {
		t.Fatalf("unexpected appointment metadata: %#v", appointment)
	}
	if !appointment.StartTime.IsZero() || !appointment.EndTime.IsZero() {
		t.Fatalf("cancel metadata should not require schedule fields: %#v", appointment)
	}
}

func TestRetrieveBookingGetsVersion(t *testing.T) {
	transport := &sequenceTransport{
		responses: []string{`{"booking":{"id":"booking_1","version":12,"status":"ACCEPTED","start_at":"2026-06-17T17:00:00Z","appointment_segments":[{"duration_minutes":30}]}}`},
	}
	adapter := &SquareAdapter{
		cfg: config.SquareConfig{
			APIBaseURL: "https://square.test",
			APIVersion: "2026-05-20",
		},
		httpClient: &http.Client{Transport: transport},
	}

	booking, err := adapter.retrieveBooking(context.Background(), adapter.cfg, "token_1", "booking_1")
	if err != nil {
		t.Fatalf("retrieve booking failed: %v", err)
	}
	if booking.ID != "booking_1" || booking.Version != 12 {
		t.Fatalf("unexpected booking: %#v", booking)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(transport.requests))
	}
	req := transport.requests[0]
	if req.Method != http.MethodGet || req.URL.String() != "https://square.test/v2/bookings/booking_1" {
		t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
	}
	if req.Header.Get("Authorization") != "Bearer token_1" {
		t.Fatalf("missing authorization header")
	}
}

func TestMapSquareListedBookingPreservesSegmentsAndNotes(t *testing.T) {
	item, ok := mapSquareListedBooking(squareBooking{
		ID:           "booking_1",
		Version:      8,
		Status:       "ACCEPTED",
		CustomerID:   "customer_1",
		CustomerNote: "Customer note",
		StartAt:      "2026-06-17T17:00:00Z",
		AppointmentSegments: []squareAppointmentSegment{{
			DurationMinutes:         45,
			TeamMemberID:            "team_1",
			ServiceVariationID:      "service_1",
			ServiceVariationVersion: 123,
		}},
	})
	if !ok {
		t.Fatalf("mapSquareListedBooking ok = false, want true")
	}
	if item.POSAppointmentID != "booking_1" || item.POSAppointmentVersion != 8 || item.POSCustomerID != "customer_1" {
		t.Fatalf("listed appointment identity = %#v", item)
	}
	if item.EndTime.Sub(item.StartTime) != 45*time.Minute {
		t.Fatalf("duration = %s, want 45m", item.EndTime.Sub(item.StartTime))
	}
	if item.Notes != "Customer note" {
		t.Fatalf("notes = %q, want Customer note", item.Notes)
	}
	if len(item.Segments) != 1 || item.Segments[0].POSServiceID != "service_1" || item.Segments[0].POSStaffID != "team_1" {
		t.Fatalf("segments = %#v, want provider segment", item.Segments)
	}
}

type capturingTransport struct {
	squareVersion string
}

func (t *capturingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.squareVersion = req.Header.Get("Square-Version")
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(`{"ok":true}`)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

type sequenceTransport struct {
	responses     []string
	requests      []*http.Request
	requestBodies []string
}

func (t *sequenceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.requests = append(t.requests, req)
	requestBody := ""
	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		requestBody = string(body)
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	t.requestBodies = append(t.requestBodies, requestBody)
	index := len(t.requests) - 1
	body := `{}`
	if index < len(t.responses) {
		body = t.responses[index]
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}
