package customernotification

import (
	"context"
	"errors"
	"testing"
	"time"

	notificationdelivery "github.com/manleai/ai-receptionist/modules/notification_delivery"
)

type fakeCustomerProcessorRepository struct {
	items          []ClaimedDelivery
	readiness      DispatchReadiness
	markErr        error
	suppressed     int
	safeFailures   int
	quiet          int
	dispatches     int
	unknown        int
	unknownCode    string
	definitive     int
	providerResult int
}

func (f *fakeCustomerProcessorRepository) RecoverExpiredLeases(context.Context, int) (int, error) {
	return 0, nil
}
func (f *fakeCustomerProcessorRepository) ClaimBatch(context.Context, int, time.Duration) ([]ClaimedDelivery, error) {
	return f.items, nil
}
func (f *fakeCustomerProcessorRepository) ResolveDispatchReadiness(context.Context, ClaimedDelivery) (DispatchReadiness, error) {
	return f.readiness, nil
}
func (f *fakeCustomerProcessorRepository) RecordSuppressed(context.Context, ClaimedDelivery, string) error {
	f.suppressed++
	return nil
}
func (f *fakeCustomerProcessorRepository) RecordSafeFailure(context.Context, ClaimedDelivery, string, time.Time) error {
	f.safeFailures++
	return nil
}
func (f *fakeCustomerProcessorRepository) RecordQuietHours(context.Context, ClaimedDelivery, time.Time) error {
	f.quiet++
	return nil
}
func (f *fakeCustomerProcessorRepository) MarkDispatchStarted(context.Context, ClaimedDelivery) error {
	f.dispatches++
	return f.markErr
}
func (f *fakeCustomerProcessorRepository) RecordOutcomeUnknown(_ context.Context, _ ClaimedDelivery, code string) error {
	f.unknown++
	f.unknownCode = code
	return nil
}
func (f *fakeCustomerProcessorRepository) RecordDefinitiveFailure(context.Context, ClaimedDelivery, string) error {
	f.definitive++
	return nil
}
func (f *fakeCustomerProcessorRepository) RecordProviderResult(context.Context, ClaimedDelivery, notificationdelivery.SendResult) error {
	f.providerResult++
	return nil
}

type fakeCustomerSenderResolver struct {
	sender notificationdelivery.Sender
	err    error
}

func (f fakeCustomerSenderResolver) ResolveCustomerSender(context.Context, string) (notificationdelivery.Sender, error) {
	return f.sender, f.err
}

type fakeCustomerSender struct {
	result notificationdelivery.SendResult
	err    error
	calls  int
}

func (f *fakeCustomerSender) Send(context.Context, notificationdelivery.OutboundMessage) (notificationdelivery.SendResult, error) {
	f.calls++
	return f.result, f.err
}

func readyCustomerDispatch() DispatchReadiness {
	return DispatchReadiness{Eligible: true, Timezone: "America/Chicago", QuietStart: "23:00", QuietEnd: "06:00", Now: time.Date(2026, 7, 24, 18, 0, 0, 0, time.UTC)}
}

func TestProcessorSuppressesWhenFinalPredispatchRevalidationLosesRace(t *testing.T) {
	sender := &fakeCustomerSender{}
	repo := &fakeCustomerProcessorRepository{items: []ClaimedDelivery{{ID: "delivery-1", SalonID: "salon-1", ClaimToken: "claim-1", AttemptNumber: 1}}, readiness: readyCustomerDispatch(), markErr: ErrDispatchBlocked}
	processed, err := NewProcessor(repo, fakeCustomerSenderResolver{sender: sender}).ProcessOnce(context.Background(), 20)
	if err != nil || processed != 1 || repo.suppressed != 1 || sender.calls != 0 {
		t.Fatalf("processed=%d err=%v suppressed=%d sends=%d", processed, err, repo.suppressed, sender.calls)
	}
}

func TestProcessorCanonicalizesAmbiguousCustomProviderCode(t *testing.T) {
	sender := &fakeCustomerSender{err: &notificationdelivery.SendError{Code: "TWILIO_SOCKET_TIMEOUT", Ambiguous: true, Err: errors.New("connection closed")}}
	repo := &fakeCustomerProcessorRepository{items: []ClaimedDelivery{{ID: "delivery-1", SalonID: "salon-1", ClaimToken: "claim-1", AttemptNumber: 1}}, readiness: readyCustomerDispatch()}
	processed, err := NewProcessor(repo, fakeCustomerSenderResolver{sender: sender}).ProcessOnce(context.Background(), 20)
	if err != nil || processed != 1 || repo.unknown != 1 || repo.unknownCode != "DELIVERY_OUTCOME_UNKNOWN" || repo.safeFailures != 0 {
		t.Fatalf("processed=%d err=%v unknown=%d code=%q safe=%d", processed, err, repo.unknown, repo.unknownCode, repo.safeFailures)
	}
}

func TestProcessorDoesNotCallProviderForStaleConsentSnapshot(t *testing.T) {
	sender := &fakeCustomerSender{}
	repo := &fakeCustomerProcessorRepository{items: []ClaimedDelivery{{ID: "delivery-1", SalonID: "salon-1", ClaimToken: "claim-1", AttemptNumber: 1}}, readiness: DispatchReadiness{Eligible: false, ReasonCode: "CUSTOMER_SMS_CONSENT_STALE"}}
	processed, err := NewProcessor(repo, fakeCustomerSenderResolver{sender: sender}).ProcessOnce(context.Background(), 20)
	if err != nil || processed != 1 || repo.suppressed != 1 || repo.dispatches != 0 || sender.calls != 0 {
		t.Fatalf("processed=%d err=%v suppressed=%d dispatches=%d sends=%d", processed, err, repo.suppressed, repo.dispatches, sender.calls)
	}
}

func TestProcessorPersistsAcceptanceWithoutCallingItDelivered(t *testing.T) {
	sender := &fakeCustomerSender{result: notificationdelivery.SendResult{ProviderMessageID: "SM123", ProviderStatus: "queued", DeliveryStatus: StatusProviderAccepted, StatusRank: 10}}
	repo := &fakeCustomerProcessorRepository{items: []ClaimedDelivery{{ID: "delivery-1", SalonID: "salon-1", ClaimToken: "claim-1", AttemptNumber: 1}}, readiness: readyCustomerDispatch()}
	processed, err := NewProcessor(repo, fakeCustomerSenderResolver{sender: sender}).ProcessOnce(context.Background(), 20)
	if err != nil || processed != 1 || repo.providerResult != 1 || repo.unknown != 0 {
		t.Fatalf("processed=%d err=%v provider=%d unknown=%d", processed, err, repo.providerResult, repo.unknown)
	}
}
