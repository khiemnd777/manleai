package pos_square

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/config"
)

const (
	webhookTestSalonID = "00000000-0000-0000-0000-000000000101"
	webhookTestOwnerID = "00000000-0000-0000-0000-000000000102"
	webhookTestEventID = "00000000-0000-0000-0000-000000000103"
)

type fakeWebhookOperationsStore struct {
	events      []WebhookEventRecord
	metrics     WebhookMetrics
	repair      CalendarRepairHealth
	event       *WebhookEventRecord
	replayed    bool
	err         error
	actionKey   string
	fingerprint string
}

type fakeWebhookConfigurationStatusResolver struct {
	configured bool
	salonID    string
	err        error
}

func (f *fakeWebhookConfigurationStatusResolver) SquareWebhookConfigured(_ context.Context, salonID string) (bool, error) {
	f.salonID = salonID
	return f.configured, f.err
}

func (f *fakeWebhookOperationsStore) ListBookingWebhooksForOwner(context.Context, string, string, string, int, int) ([]WebhookEventRecord, WebhookMetrics, CalendarRepairHealth, error) {
	return f.events, f.metrics, f.repair, f.err
}

func (f *fakeWebhookOperationsStore) GetBookingWebhookForOwner(context.Context, string, string, string) (*WebhookEventRecord, error) {
	return f.event, f.err
}

func (f *fakeWebhookOperationsStore) RequeueBookingWebhookForOwner(_ context.Context, _, _, _ string, actionKey, fingerprint string) (*WebhookEventRecord, bool, error) {
	f.actionKey, f.fingerprint = actionKey, fingerprint
	return f.event, f.replayed, f.err
}

func (f *fakeWebhookOperationsStore) ListBookingWebhooksForSalon(_ context.Context, _, _ string, _, _ int) ([]WebhookEventRecord, WebhookMetrics, CalendarRepairHealth, error) {
	return f.events, f.metrics, f.repair, f.err
}

func (f *fakeWebhookOperationsStore) GetBookingWebhookForSalon(context.Context, string, string) (*WebhookEventRecord, error) {
	return f.event, f.err
}

func (f *fakeWebhookOperationsStore) RequeueBookingWebhookForSalon(_ context.Context, _, _, _ string, actionKey, fingerprint string) (*WebhookEventRecord, bool, error) {
	f.actionKey, f.fingerprint = actionKey, fingerprint
	return f.event, f.replayed, f.err
}

func TestWebhookOperationsListExposesOnlySafeBoundedEvidence(t *testing.T) {
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	store := &fakeWebhookOperationsStore{
		events: []WebhookEventRecord{{
			ID: webhookTestEventID, EventType: "booking.updated",
			ProcessingStatus: WebhookStatusDeadLetter, ProcessingAttempts: 10,
			LastErrorClass: "dependency", LastErrorCode: "SQUARE_WEBHOOK_ATTEMPTS_EXHAUSTED",
			DeadLetteredAt: &now, CreatedAt: now, UpdatedAt: now,
		}},
		metrics: WebhookMetrics{DeadLetter: 1, RecentWindowHours: 168},
		repair:  CalendarRepairHealth{Relevant: true, Status: "degraded", LastErrorCode: "SQUARE_CALENDAR_REPAIR_FAILED"},
	}
	service := &Service{webhookOperationsRepo: store}
	result, err := service.ListWebhookEvents(context.Background(), webhookTestSalonID, webhookTestOwnerID, WebhookStatusDeadLetter, 25, 0)
	if err != nil {
		t.Fatalf("ListWebhookEvents: %v", err)
	}
	if len(result.Events) != 1 || !result.Events[0].CanRequeue || result.Metrics.DeadLetter != 1 || result.CalendarRepair.Status != "degraded" {
		t.Fatalf("safe operations response = %#v", result)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	text := string(raw)
	for _, forbidden := range []string{
		"merchant_id", "location_id", "pos_booking_id", "event_id", "processing_token",
		"raw_payload", "signature", "customer_name", "provider response contained sensitive detail",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("response contains forbidden detail %q: %s", forbidden, text)
		}
	}
}

func TestWebhookOperationsRequeueUsesStableFingerprintAndValidatesInput(t *testing.T) {
	event := &WebhookEventRecord{ID: webhookTestEventID, ProcessingStatus: WebhookStatusPending}
	store := &fakeWebhookOperationsStore{event: event}
	service := &Service{webhookOperationsRepo: store}
	result, replayed, err := service.RequeueWebhookEvent(context.Background(), webhookTestSalonID, webhookTestOwnerID, webhookTestEventID, WebhookRequeueRequest{ActionKey: "owner-action-1"})
	if err != nil || replayed || result == nil || store.actionKey != "owner-action-1" {
		t.Fatalf("requeue result/replayed/error = %#v/%t/%v", result, replayed, err)
	}
	if store.fingerprint != webhookRequeueFingerprint(webhookTestEventID) || len(store.fingerprint) != 64 {
		t.Fatalf("fingerprint = %q", store.fingerprint)
	}
	if _, _, err := service.RequeueWebhookEvent(context.Background(), webhookTestSalonID, webhookTestOwnerID, webhookTestEventID, WebhookRequeueRequest{}); !errors.Is(err, ErrWebhookOperationsValidation) {
		t.Fatalf("missing action key error = %v", err)
	}
	if _, err := service.ListWebhookEvents(context.Background(), webhookTestSalonID, webhookTestOwnerID, "ignored", 25, 0); !errors.Is(err, ErrWebhookOperationsValidation) {
		t.Fatalf("unsupported status error = %v", err)
	}
}

func TestPlatformWebhookOperationsReportsStoredConfigurationForExactSalon(t *testing.T) {
	store := &fakeWebhookOperationsStore{metrics: WebhookMetrics{RecentWindowHours: 168}}
	resolver := &fakeWebhookConfigurationStatusResolver{configured: true}
	service := &Service{webhookOperationsRepo: store, webhookConfigStatus: resolver}

	result, err := service.ListWebhookEventsForPlatform(
		context.Background(), webhookTestSalonID, "", 25, 0,
	)
	if err != nil {
		t.Fatalf("ListWebhookEventsForPlatform: %v", err)
	}
	if result.WebhookConfigured == nil || !*result.WebhookConfigured {
		t.Fatalf("webhook configured = %#v, want true", result.WebhookConfigured)
	}
	if resolver.salonID != webhookTestSalonID {
		t.Fatalf("resolver salon = %q, want %q", resolver.salonID, webhookTestSalonID)
	}
}

func TestPlatformSquareTechnicalRoutesRequireAuthentication(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app.Group("/api"), NewHandler(&Service{}, config.Config{}), "test-secret")
	RegisterPlatformRoutes(app.Group("/api"), NewPlatformHandler(&Service{}, nil), "test-secret")
	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/platform/tenants/" + webhookTestSalonID + "/technical/square/connect-url"},
		{method: http.MethodGet, path: "/api/platform/tenants/" + webhookTestSalonID + "/technical/square/status"},
		{method: http.MethodGet, path: "/api/platform/tenants/" + webhookTestSalonID + "/technical/square/locations"},
		{method: http.MethodPost, path: "/api/platform/tenants/" + webhookTestSalonID + "/technical/square/select-location"},
		{method: http.MethodPost, path: "/api/platform/tenants/" + webhookTestSalonID + "/technical/square/sync"},
	} {
		response, err := app.Test(httptest.NewRequest(test.method, test.path, nil))
		if err != nil {
			t.Fatalf("%s %s: %v", test.method, test.path, err)
		}
		if response.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("%s %s status=%d, want 401", test.method, test.path, response.StatusCode)
		}
	}
}

func TestWebhookOperationsHandlerSanitizesUnexpectedErrors(t *testing.T) {
	app := fiber.New()
	handler := NewHandler(&Service{}, config.Config{})
	app.Get("/failure", func(c *fiber.Ctx) error {
		return handler.webhookOperationsError(c, errors.New("provider booking BOOKING_SECRET failed with raw response"))
	})
	response, err := app.Test(httptest.NewRequest("GET", "/failure", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status=%d, want 500", response.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "BOOKING_SECRET") || strings.Contains(string(raw), "raw response") {
		t.Fatalf("unexpected error leaked: %s", raw)
	}
}
