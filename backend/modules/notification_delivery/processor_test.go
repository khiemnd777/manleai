package notificationdelivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/internal/databasecontext"
)

type fakeProcessorRepository struct {
	items              []ClaimedNotification
	disabled           int
	safeFailures       int
	unknownFailures    int
	definitiveFailures int
	dispatches         int
	providerResults    int
	providerAccess     databasecontext.AccessContext
}

func (f *fakeProcessorRepository) RecoverExpiredLeases(context.Context, int) (int, error) {
	return 0, nil
}
func (f *fakeProcessorRepository) ClaimBatch(context.Context, int, time.Duration) ([]ClaimedNotification, error) {
	return f.items, nil
}
func (f *fakeProcessorRepository) RecordDisabled(context.Context, ClaimedNotification) error {
	f.disabled++
	return nil
}
func (f *fakeProcessorRepository) RecordSafeFailure(context.Context, ClaimedNotification, string, time.Time) error {
	f.safeFailures++
	return nil
}
func (f *fakeProcessorRepository) RecordOutcomeUnknown(context.Context, ClaimedNotification, string) error {
	f.unknownFailures++
	return nil
}
func (f *fakeProcessorRepository) RecordDefinitiveFailure(context.Context, ClaimedNotification, string) error {
	f.definitiveFailures++
	return nil
}
func (f *fakeProcessorRepository) MarkDispatchStarted(context.Context, ClaimedNotification, string) error {
	f.dispatches++
	return nil
}
func (f *fakeProcessorRepository) RecordProviderResult(ctx context.Context, _ ClaimedNotification, _ SendResult) error {
	f.providerResults++
	f.providerAccess = databasecontext.FromContext(ctx)
	return nil
}

type fakeResolver struct {
	channel DeliveryChannel
	err     error
}

func (f fakeResolver) ResolveDeliveryChannel(context.Context, string) (DeliveryChannel, error) {
	return f.channel, f.err
}

type fakeSender struct {
	result SendResult
	err    error
	calls  int
}

func (f *fakeSender) Send(context.Context, OutboundMessage) (SendResult, error) {
	f.calls++
	return f.result, f.err
}

func TestProcessorDoesNotDispatchWhenOwnerSMSIsDisabled(t *testing.T) {
	repo := &fakeProcessorRepository{items: []ClaimedNotification{{ID: "notification-1", SalonID: "salon-1", ClaimToken: "claim-1", AttemptNumber: 1}}}
	processor := NewProcessor(repo, fakeResolver{err: ErrConfigDisabled})
	processed, err := processor.ProcessOnce(context.Background(), 20)
	if err != nil || processed != 1 || repo.disabled != 1 || repo.dispatches != 0 {
		t.Fatalf("processed=%d err=%v disabled=%d dispatches=%d", processed, err, repo.disabled, repo.dispatches)
	}
}

func TestProcessorRetriesOnlySafePredispatchConfigFailure(t *testing.T) {
	repo := &fakeProcessorRepository{items: []ClaimedNotification{{ID: "notification-1", SalonID: "salon-1", ClaimToken: "claim-1", AttemptNumber: 2}}}
	processor := NewProcessor(repo, fakeResolver{err: ErrConfigNotReady})
	processed, err := processor.ProcessOnce(context.Background(), 20)
	if err != nil || processed != 1 || repo.safeFailures != 1 || repo.dispatches != 0 {
		t.Fatalf("processed=%d err=%v safe=%d dispatches=%d", processed, err, repo.safeFailures, repo.dispatches)
	}
}

func TestProcessorDeadLettersAmbiguousPostDispatchOutcome(t *testing.T) {
	sender := &fakeSender{err: &SendError{Code: "DELIVERY_OUTCOME_UNKNOWN", Ambiguous: true, Err: errors.New("timeout")}}
	repo := &fakeProcessorRepository{items: []ClaimedNotification{{ID: "notification-1", SalonID: "salon-1", ClaimToken: "claim-1", AttemptNumber: 1, Message: "Owner message"}}}
	processor := NewProcessor(repo, fakeResolver{channel: DeliveryChannel{Enabled: true, Destination: "+15555550100", DestinationMasked: "••••0100", Sender: sender}})
	processed, err := processor.ProcessOnce(context.Background(), 20)
	if err != nil || processed != 1 || repo.dispatches != 1 || repo.unknownFailures != 1 || repo.safeFailures != 0 {
		t.Fatalf("processed=%d err=%v dispatches=%d unknown=%d safe=%d", processed, err, repo.dispatches, repo.unknownFailures, repo.safeFailures)
	}
}

func TestProcessorPersistsProviderEvidenceWithoutCallingItDelivered(t *testing.T) {
	sender := &fakeSender{result: SendResult{ProviderMessageID: "SM123", ProviderStatus: "queued", DeliveryStatus: StatusProviderAccepted, StatusRank: 10}}
	repo := &fakeProcessorRepository{items: []ClaimedNotification{{ID: "notification-1", SalonID: "salon-1", ClaimToken: "claim-1", AttemptNumber: 1, Message: "Owner message"}}}
	processor := NewProcessor(repo, fakeResolver{channel: DeliveryChannel{Enabled: true, Destination: "+15555550100", DestinationMasked: "••••0100", Sender: sender}})
	processed, err := processor.ProcessOnce(context.Background(), 20)
	if err != nil || processed != 1 || repo.providerResults != 1 || repo.unknownFailures != 0 {
		t.Fatalf("processed=%d err=%v results=%d unknown=%d", processed, err, repo.providerResults, repo.unknownFailures)
	}
	if repo.providerAccess.Scope != databasecontext.ScopeWorker || repo.providerAccess.SystemSalonID != "salon-1" || repo.providerAccess.ActorUserID != "" {
		t.Fatalf("provider result access context = %#v", repo.providerAccess)
	}
}

func TestSafeRetryDelayIsBoundedByTechnicalOwner(t *testing.T) {
	if got := SafeRetryDelay(1); got != 30*time.Second {
		t.Fatalf("attempt 1 delay=%s", got)
	}
	if got := SafeRetryDelay(99); got != 8*time.Minute {
		t.Fatalf("bounded delay=%s", got)
	}
}

func TestManualRequeueStartsANewBoundedRetryCycle(t *testing.T) {
	item := ClaimedNotification{AttemptNumber: MaxSafeDeliveryAttempts + 1, RequeueCount: 1}
	if got := DeliveryAttemptInCycle(item); got != 1 {
		t.Fatalf("delivery attempt in cycle=%d, want 1", got)
	}
	if got := SafeRetryDelay(DeliveryAttemptInCycle(item)); got != SafeRetryBaseDelay {
		t.Fatalf("first retry after requeue delay=%s", got)
	}
}
