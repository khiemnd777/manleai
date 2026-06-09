package pos_square

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/modules/pos"
)

func TestDoJSONSendsSquareVersionHeader(t *testing.T) {
	transport := &capturingTransport{}
	adapter := &SquareAdapter{
		cfg: config.SquareConfig{
			APIVersion: "2026-05-20",
		},
		httpClient: &http.Client{Transport: transport},
	}
	var out map[string]bool
	if err := adapter.doJSON(context.Background(), http.MethodGet, "https://square.test/v2/locations", "", nil, &out); err != nil {
		t.Fatalf("doJSON failed: %v", err)
	}
	if got := transport.squareVersion; got != "2026-05-20" {
		t.Fatalf("Square-Version = %q, want 2026-05-20", got)
	}
	if !out["ok"] {
		t.Fatalf("unexpected response: %#v", out)
	}
}

func TestMapCatalogServicesKeepsVariationVersion(t *testing.T) {
	services := mapCatalogServices(squareCatalogResponse{
		Objects: []squareCatalogObject{
			{
				ID:      "ITEM_1",
				Type:    "ITEM",
				Version: 100,
				ItemData: struct {
					Name        string                `json:"name"`
					Description string                `json:"description"`
					Variations  []squareCatalogObject `json:"variations"`
				}{
					Name:        "Classic Manicure",
					Description: "Manicure service",
					Variations: []squareCatalogObject{
						{
							ID:      "VAR_1",
							Type:    "ITEM_VARIATION",
							Version: 222,
							ItemVariationData: struct {
								Name            string `json:"name"`
								ServiceDuration int64  `json:"service_duration"`
								PriceMoney      struct {
									Amount   int64  `json:"amount"`
									Currency string `json:"currency"`
								} `json:"price_money"`
							}{
								Name:            "Regular",
								ServiceDuration: 1800000,
								PriceMoney: struct {
									Amount   int64  `json:"amount"`
									Currency string `json:"currency"`
								}{Amount: 3000, Currency: "USD"},
							},
						},
					},
				},
			},
		},
	})

	if len(services) != 1 {
		t.Fatalf("expected one service, got %d", len(services))
	}
	if services[0].POSServiceID != "VAR_1" {
		t.Fatalf("unexpected service id: %s", services[0].POSServiceID)
	}
	if services[0].POSServiceVersion != 222 {
		t.Fatalf("unexpected service version: %d", services[0].POSServiceVersion)
	}
	if services[0].DurationMinutes != 30 {
		t.Fatalf("unexpected duration: %d", services[0].DurationMinutes)
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
		DurationMinutes: 45,
	})
	if err != nil {
		t.Fatalf("build availability request failed: %v", err)
	}
	filter := request.Query.Filter
	if filter.LocationID != "loc_1" {
		t.Fatalf("location id = %s, want loc_1", filter.LocationID)
	}
	if filter.StartAtRange.StartAt != "2026-06-10T00:00:00Z" || filter.StartAtRange.EndAt != "2026-06-11T00:00:00Z" {
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

func TestBuildSquareCreateBookingRequestIncludesRequiredSegmentFields(t *testing.T) {
	start := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	request, err := buildSquareCreateBookingRequest("loc_1", pos.CreateAppointmentInput{
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
	if request.IdempotencyKey == "" {
		t.Fatalf("expected idempotency key")
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

func TestBuildSquareCreateBookingRequestRequiresServiceVersion(t *testing.T) {
	_, err := buildSquareCreateBookingRequest("loc_1", pos.CreateAppointmentInput{
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
	if request.IdempotencyKey == "" {
		t.Fatalf("expected idempotency key")
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

func TestBuildSquareCancelBookingRequestIncludesVersion(t *testing.T) {
	request, err := buildSquareCancelBookingRequest(pos.CancelInput{
		BookingVersion: 7,
		Reason:         "Customer requested cancellation",
	})
	if err != nil {
		t.Fatalf("build cancel booking request failed: %v", err)
	}
	if request.IdempotencyKey == "" {
		t.Fatalf("expected idempotency key")
	}
	if request.BookingVersion != 7 {
		t.Fatalf("booking version = %d, want 7", request.BookingVersion)
	}
}

func TestBuildSquareCancelBookingRequestRequiresVersion(t *testing.T) {
	_, err := buildSquareCancelBookingRequest(pos.CancelInput{})
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
