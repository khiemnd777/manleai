package pos_square

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

func TestWebhookProcessorRepairsBookingWindowAndCompletesEvent(t *testing.T) {
	bookingStart := time.Date(2026, 7, 20, 15, 0, 0, 0, time.UTC)
	store := &fakeSquareWebhookProcessorStore{events: []SquareBookingWebhookEvent{{
		ID: "event_row_1", SalonID: "salon_1", OwnerUserID: "owner_1", BookingStartAt: &bookingStart,
		ProcessingToken: "claim_1", ProcessingAttempts: 3,
	}}}
	syncer := &fakeSquareCalendarSyncer{}
	processor := NewWebhookProcessor(store, syncer)

	processed, err := processor.ProcessWebhookEvents(context.Background(), 10)
	if err != nil {
		t.Fatalf("ProcessWebhookEvents failed: %v", err)
	}
	if processed != 1 || syncer.calls != 1 {
		t.Fatalf("processed/calls = %d/%d, want 1/1", processed, syncer.calls)
	}
	if syncer.salonID != "salon_1" || syncer.ownerUserID != "owner_1" {
		t.Fatalf("sync scope = %s/%s", syncer.salonID, syncer.ownerUserID)
	}
	if got := syncer.request.StartTime; !got.Equal(bookingStart.Add(-24 * time.Hour)) {
		t.Fatalf("start = %s, want booking window", got)
	}
	if got := syncer.request.EndTime; !got.Equal(bookingStart.Add(24 * time.Hour)) {
		t.Fatalf("end = %s, want booking window", got)
	}
	if store.completedEventID != "event_row_1" || store.completedEventErr != nil {
		t.Fatalf("completion = %s/%v", store.completedEventID, store.completedEventErr)
	}
	if store.completedEventToken != "claim_1" || store.completedEventAttempts != 3 {
		t.Fatalf("completion claim = %q/%d, want claim_1/3", store.completedEventToken, store.completedEventAttempts)
	}
	if len(store.claimLimits) != 2 || store.claimLimits[0] != 1 || store.claimLimits[1] != 1 {
		t.Fatalf("claim limits = %#v, want one item per claim plus empty poll", store.claimLimits)
	}
}

func TestWebhookProcessorDoesNotLetStaleWorkerCompleteNewerClaim(t *testing.T) {
	store := &fakeSquareWebhookProcessorStore{
		events: []SquareBookingWebhookEvent{{
			ID: "event_row_1", SalonID: "salon_1", OwnerUserID: "owner_1",
			ProcessingToken: "stale_claim", ProcessingAttempts: 1,
		}},
		currentEventToken: "newer_claim",
	}
	processor := NewWebhookProcessor(store, &fakeSquareCalendarSyncer{})

	processed, err := processor.ProcessWebhookEvents(context.Background(), 1)
	if processed != 1 || !errors.Is(err, ErrWebhookClaimLost) {
		t.Fatalf("processed/error = %d/%v, want one lost-claim error", processed, err)
	}
	if store.eventCompletionApplied {
		t.Fatal("stale worker must not overwrite the newer claim state")
	}
}

func TestWebhookProcessorPersistsFailureForRetry(t *testing.T) {
	providerErr := errors.New("provider unavailable")
	store := &fakeSquareWebhookProcessorStore{events: []SquareBookingWebhookEvent{{
		ID: "event_row_1", SalonID: "salon_1", OwnerUserID: "owner_1",
		ProcessingToken: "claim_1", ProcessingAttempts: 1,
	}}}
	processor := NewWebhookProcessor(store, &fakeSquareCalendarSyncer{err: providerErr})

	processed, err := processor.ProcessWebhookEvents(context.Background(), 10)
	if processed != 1 || !errors.Is(err, providerErr) {
		t.Fatalf("processed/error = %d/%v, want one provider error", processed, err)
	}
	if !errors.Is(store.completedEventErr, providerErr) {
		t.Fatalf("stored completion error = %v, want provider error", store.completedEventErr)
	}
}

func TestScheduledRepairUsesBoundedNinetyOneDayRange(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	store := &fakeSquareWebhookProcessorStore{targets: []SquareCalendarRepairTarget{{SalonID: "salon_1", OwnerUserID: "owner_1", LeaseToken: "repair-claim-1"}}}
	syncer := &fakeSquareCalendarSyncer{}
	processor := NewWebhookProcessor(store, syncer)
	processor.now = func() time.Time { return now }

	processed, err := processor.ProcessScheduledRepairs(context.Background(), 2)
	if err != nil {
		t.Fatalf("ProcessScheduledRepairs failed: %v", err)
	}
	if processed != 1 || syncer.request.EndTime.Sub(syncer.request.StartTime) != 91*24*time.Hour {
		t.Fatalf("processed/range = %d/%s, want one 91-day range", processed, syncer.request.EndTime.Sub(syncer.request.StartTime))
	}
	if store.completedSalonID != "salon_1" || store.completedRepairErr != nil {
		t.Fatalf("repair completion = %s/%v", store.completedSalonID, store.completedRepairErr)
	}
	if store.completedRepairToken != "repair-claim-1" || !store.repairCompletionApplied {
		t.Fatalf("repair completion token/applied = %q/%t", store.completedRepairToken, store.repairCompletionApplied)
	}
}

func TestScheduledRepairDoesNotLetStaleWorkerCompleteNewerLease(t *testing.T) {
	store := &fakeSquareWebhookProcessorStore{
		targets:            []SquareCalendarRepairTarget{{SalonID: "salon_1", OwnerUserID: "owner_1", LeaseToken: "stale-repair-claim"}},
		currentRepairToken: "newer-repair-claim",
	}
	processor := NewWebhookProcessor(store, &fakeSquareCalendarSyncer{})

	processed, err := processor.ProcessScheduledRepairs(context.Background(), 1)
	if processed != 1 || !errors.Is(err, ErrWebhookClaimLost) {
		t.Fatalf("processed/error = %d/%v, want one lost repair claim", processed, err)
	}
	if store.repairCompletionApplied {
		t.Fatal("stale repair worker must not overwrite the newer lease")
	}
}

type fakeSquareWebhookProcessorStore struct {
	events                  []SquareBookingWebhookEvent
	targets                 []SquareCalendarRepairTarget
	claimErr                error
	claimLimits             []int
	completedEventID        string
	completedEventToken     string
	completedEventAttempts  int
	completedEventErr       error
	currentEventToken       string
	eventCompletionApplied  bool
	completedSalonID        string
	completedRepairToken    string
	completedRepairErr      error
	currentRepairToken      string
	repairCompletionApplied bool
}

func (f *fakeSquareWebhookProcessorStore) ClaimBookingWebhooks(_ context.Context, limit int) ([]SquareBookingWebhookEvent, error) {
	f.claimLimits = append(f.claimLimits, limit)
	if f.claimErr != nil || len(f.events) == 0 {
		return nil, f.claimErr
	}
	event := f.events[0]
	f.events = f.events[1:]
	return []SquareBookingWebhookEvent{event}, nil
}

func (f *fakeSquareWebhookProcessorStore) CompleteBookingWebhook(_ context.Context, id string, token string, attempts int, err error) error {
	f.completedEventID = id
	f.completedEventToken = token
	f.completedEventAttempts = attempts
	f.completedEventErr = err
	if f.currentEventToken != "" && f.currentEventToken != token {
		return ErrWebhookClaimLost
	}
	f.eventCompletionApplied = true
	return nil
}

func (f *fakeSquareWebhookProcessorStore) ClaimCalendarRepairTargets(context.Context, int) ([]SquareCalendarRepairTarget, error) {
	return f.targets, f.claimErr
}

func (f *fakeSquareWebhookProcessorStore) CompleteCalendarRepair(_ context.Context, salonID string, leaseToken string, err error) error {
	f.completedSalonID = salonID
	f.completedRepairToken = leaseToken
	f.completedRepairErr = err
	if f.currentRepairToken != "" && f.currentRepairToken != leaseToken {
		return ErrWebhookClaimLost
	}
	f.repairCompletionApplied = true
	return nil
}

type fakeSquareCalendarSyncer struct {
	calls       int
	salonID     string
	ownerUserID string
	request     booking.CalendarSyncRequest
	err         error
}

func (f *fakeSquareCalendarSyncer) SyncCalendar(_ context.Context, salonID string, ownerUserID string, req booking.CalendarSyncRequest) (*booking.CalendarSyncResponse, error) {
	f.calls++
	f.salonID = salonID
	f.ownerUserID = ownerUserID
	f.request = req
	if f.err != nil {
		return nil, f.err
	}
	return &booking.CalendarSyncResponse{}, nil
}
