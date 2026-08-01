package pos_square

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
)

func TestVerifySquareWebhookSignatureUsesURLAndRawBody(t *testing.T) {
	const notificationURL = "https://api.example.com/api/integrations/square/webhook"
	const signatureKey = "webhook-secret"
	body := []byte(`{"event_id":"event_1"}`)
	signature := squareWebhookTestSignature(notificationURL, signatureKey, body)

	if !verifySquareWebhookSignature(notificationURL, signatureKey, body, signature) {
		t.Fatal("expected signature to validate")
	}
	if verifySquareWebhookSignature(notificationURL+"/changed", signatureKey, body, signature) {
		t.Fatal("changed notification URL must invalidate signature")
	}
	if verifySquareWebhookSignature(notificationURL, signatureKey, append(body, ' '), signature) {
		t.Fatal("changed raw body must invalidate signature")
	}
}

func TestReceiveBookingWebhookVerifiesAndEnqueuesMinimalEvent(t *testing.T) {
	const notificationURL = "https://api.example.com/api/integrations/square/webhook"
	const signatureKey = "webhook-secret"
	body := []byte(`{
		"merchant_id":"MERCHANT_1",
		"location_id":"LOC_1",
		"type":"booking.updated",
		"event_id":"EVENT_1",
		"created_at":"2026-07-15T10:00:00Z",
		"data":{"type":"booking","id":"BOOKING_1","object":{"booking":{
			"id":"BOOKING_1","version":7,"status":"ACCEPTED","start_at":"2026-07-20T15:00:00Z",
			"location_id":"LOC_1","customer_note":"must not be persisted"
		}}}
	}`)
	store := &fakeSquareWebhookStore{target: &SquareWebhookTarget{SalonID: "salon_1", OwnerUserID: "owner_1"}, inserted: true}
	service := &Service{
		adapter: &SquareAdapter{configResolver: staticSquareConfigResolver{cfg: config.SquareConfig{
			WebhookNotificationURL: notificationURL,
			WebhookSignatureKey:    signatureKey,
		}}},
		webhookRepo: store,
	}

	receipt, err := service.ReceiveBookingWebhook(
		context.Background(),
		body,
		squareWebhookTestSignature(notificationURL, signatureKey, body),
	)
	if err != nil {
		t.Fatalf("ReceiveBookingWebhook failed: %v", err)
	}
	if receipt == nil || !receipt.Accepted || receipt.Duplicate {
		t.Fatalf("receipt = %#v, want accepted new event", receipt)
	}
	if store.event.SalonID != "salon_1" || store.event.EventID != "EVENT_1" || store.event.POSBookingID != "BOOKING_1" {
		t.Fatalf("event identity = %#v", store.event)
	}
	if store.event.POSBookingVersion != 7 || store.event.BookingStatus != "ACCEPTED" || store.event.BookingStartAt == nil {
		t.Fatalf("event booking metadata = %#v", store.event)
	}
	if store.enqueueAccess.Scope != databasecontext.ScopeProvider || store.enqueueAccess.SystemSalonID != "salon_1" || store.enqueueAccess.ActorUserID != "" {
		t.Fatalf("webhook enqueue access context = %#v", store.enqueueAccess)
	}
}

func TestReceiveBookingWebhookRejectsInvalidSignatureBeforeEnqueue(t *testing.T) {
	body := []byte(`{"merchant_id":"MERCHANT_1","location_id":"LOC_1","type":"booking.created","event_id":"EVENT_1","data":{"type":"booking","id":"BOOKING_1","object":{"booking":{"id":"BOOKING_1","location_id":"LOC_1"}}}}`)
	store := &fakeSquareWebhookStore{target: &SquareWebhookTarget{SalonID: "salon_1"}, inserted: true}
	service := &Service{
		adapter: &SquareAdapter{configResolver: staticSquareConfigResolver{cfg: config.SquareConfig{
			WebhookNotificationURL: "https://api.example.com/api/integrations/square/webhook",
			WebhookSignatureKey:    "webhook-secret",
		}}},
		webhookRepo: store,
	}

	if _, err := service.ReceiveBookingWebhook(context.Background(), body, "invalid"); err != ErrWebhookSignatureInvalid {
		t.Fatalf("error = %v, want ErrWebhookSignatureInvalid", err)
	}
	if store.enqueueCalls != 0 {
		t.Fatalf("enqueue calls = %d, want 0", store.enqueueCalls)
	}
}

func TestReceiveBookingWebhookRejectsSignedMismatchedLocationsBeforeTenantRouting(t *testing.T) {
	const notificationURL = "https://api.example.com/api/integrations/square/webhook"
	const signatureKey = "webhook-secret"
	body := []byte(`{"merchant_id":"MERCHANT_1","location_id":"LOC_ROOT","type":"booking.updated","event_id":"EVENT_1","data":{"type":"booking","id":"BOOKING_1","object":{"booking":{"id":"BOOKING_1","location_id":"LOC_BOOKING"}}}}`)
	store := &fakeSquareWebhookStore{target: &SquareWebhookTarget{SalonID: "salon_1"}, inserted: true}
	service := &Service{
		adapter: &SquareAdapter{configResolver: staticSquareConfigResolver{cfg: config.SquareConfig{
			WebhookNotificationURL: notificationURL,
			WebhookSignatureKey:    signatureKey,
		}}},
		webhookRepo: store,
	}

	_, err := service.ReceiveBookingWebhook(
		context.Background(),
		body,
		squareWebhookTestSignature(notificationURL, signatureKey, body),
	)
	if !errors.Is(err, ErrWebhookPayloadInvalid) {
		t.Fatalf("error = %v, want ErrWebhookPayloadInvalid", err)
	}
	if store.findCalls != 0 || store.enqueueCalls != 0 {
		t.Fatalf("tenant lookup/enqueue calls = %d/%d, want 0/0", store.findCalls, store.enqueueCalls)
	}
}

func TestReceiveBookingWebhookFailsClosedForAmbiguousTenantMapping(t *testing.T) {
	body := []byte(`{"merchant_id":"MERCHANT_1","location_id":"LOC_1","type":"booking.created","event_id":"EVENT_1","data":{"type":"booking","id":"BOOKING_1","object":{"booking":{"id":"BOOKING_1","location_id":"LOC_1"}}}}`)
	store := &fakeSquareWebhookStore{findErr: ErrWebhookTargetAmbiguous}
	service := &Service{
		adapter:     &SquareAdapter{},
		webhookRepo: store,
	}

	if _, err := service.ReceiveBookingWebhook(context.Background(), body, "unused"); err != ErrWebhookSignatureInvalid {
		t.Fatalf("error = %v, want fail-closed ErrWebhookSignatureInvalid", err)
	}
	if store.enqueueCalls != 0 {
		t.Fatalf("enqueue calls = %d, want 0", store.enqueueCalls)
	}
}

func TestUniqueWebhookTargetRejectsMissingAndDuplicateTenantMappings(t *testing.T) {
	if _, err := uniqueWebhookTarget(nil); !errors.Is(err, ErrWebhookTargetNotFound) {
		t.Fatalf("empty mapping error = %v, want ErrWebhookTargetNotFound", err)
	}
	if _, err := uniqueWebhookTarget([]SquareWebhookTarget{{SalonID: "salon_1"}, {SalonID: "salon_2"}}); !errors.Is(err, ErrWebhookTargetAmbiguous) {
		t.Fatalf("duplicate mapping error = %v, want ErrWebhookTargetAmbiguous", err)
	}
	target, err := uniqueWebhookTarget([]SquareWebhookTarget{{SalonID: "salon_1", OwnerUserID: "owner_1"}})
	if err != nil || target == nil || target.SalonID != "salon_1" {
		t.Fatalf("unique mapping = %#v/%v, want salon_1", target, err)
	}
}

func TestSquareWebhookTargetConnectionStatusesCoverOperationalRecoveryStates(t *testing.T) {
	got := make(map[string]bool)
	for _, status := range squareWebhookTargetConnectionStatuses() {
		got[status] = true
	}
	for _, status := range []string{
		"connected",
		"syncing",
		"active",
		"error",
		"expired_token",
	} {
		if !got[status] {
			t.Fatalf("operational webhook status %q is not eligible", status)
		}
	}
	for _, status := range []string{"not_connected", "disabled"} {
		if got[status] {
			t.Fatalf("terminal webhook status %q must remain ineligible", status)
		}
	}
}

func TestWebhookClaimUpdateResultRejectsStaleWorker(t *testing.T) {
	if err := webhookClaimUpdateResult(driver.RowsAffected(0), nil); !errors.Is(err, ErrWebhookClaimLost) {
		t.Fatalf("zero-row completion error = %v, want ErrWebhookClaimLost", err)
	}
	if err := webhookClaimUpdateResult(driver.RowsAffected(1), nil); err != nil {
		t.Fatalf("owned completion error = %v, want nil", err)
	}
}

func TestCalendarRepairClaimAndCompletionAreLeaseTokenFenced(t *testing.T) {
	repositoryBytes, err := os.ReadFile("webhook_repository.go")
	if err != nil {
		t.Fatalf("read webhook repository: %v", err)
	}
	repositorySource := string(repositoryBytes)
	for _, fragment := range []string{
		"app_worker_claim_square_calendar_repairs",
		"AND lease_token = $2",
		"return webhookClaimUpdateResult(result, err)",
	} {
		if !strings.Contains(repositorySource, fragment) {
			t.Fatalf("calendar repair lease fencing is missing %q", fragment)
		}
	}
	migrationBytes, err := os.ReadFile("../../migrations/V41__square_booking_webhooks_and_calendar_repair.sql")
	if err != nil {
		t.Fatalf("read V41 migration: %v", err)
	}
	if !strings.Contains(string(migrationBytes), "lease_token TEXT") {
		t.Fatal("V41 migration must persist the calendar repair lease token")
	}
	contractBytes, err := os.ReadFile("../../migrations/V79__system_tenant_contract_preparation.sql")
	if err != nil {
		t.Fatalf("read V79 migration: %v", err)
	}
	for _, fragment := range []string{
		"lease_token = gen_random_uuid()::TEXT",
		"RETURNING state.salon_id, state.lease_token",
		"app_worker_discovery_allowed()",
	} {
		if !strings.Contains(string(contractBytes), fragment) {
			t.Fatalf("V79 calendar repair claim is missing %q", fragment)
		}
	}
}

type fakeSquareWebhookStore struct {
	target        *SquareWebhookTarget
	findErr       error
	findCalls     int
	inserted      bool
	enqueueErr    error
	enqueueCalls  int
	event         SquareBookingWebhookEvent
	enqueueAccess databasecontext.AccessContext
}

func (f *fakeSquareWebhookStore) FindWebhookTarget(context.Context, string, string) (*SquareWebhookTarget, error) {
	f.findCalls++
	return f.target, f.findErr
}

func (f *fakeSquareWebhookStore) EnqueueBookingWebhook(ctx context.Context, event SquareBookingWebhookEvent) (bool, error) {
	f.enqueueCalls++
	f.event = event
	f.enqueueAccess = databasecontext.FromContext(ctx)
	return f.inserted, f.enqueueErr
}

func squareWebhookTestSignature(notificationURL string, signatureKey string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(signatureKey))
	_, _ = mac.Write([]byte(notificationURL))
	_, _ = mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
