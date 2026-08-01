package booking

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manleai/ai-receptionist/modules/pos"
)

type externalConcurrencyProvider struct {
	slots               []pos.TimeSlot
	createCalls         atomic.Int64
	customerCreateCalls atomic.Int64
	cancelCalls         atomic.Int64
	mode                atomic.Int32
	cancelMode          atomic.Int32
	sequence            atomic.Int64
}

func (p *externalConcurrencyProvider) Name() string { return pos.ProviderSquare }
func (p *externalConcurrencyProvider) Capabilities() pos.ProviderCapabilities {
	return pos.ProviderCapabilities{AtomicCreateNoOverlap: true, ConcreteStaffAssignment: true}
}
func (p *externalConcurrencyProvider) SchedulingCapabilities(context.Context, string, pos.ProviderFence) (pos.ProviderCapabilities, error) {
	return p.Capabilities(), nil
}
func (p *externalConcurrencyProvider) Connect(context.Context, pos.ConnectInput) (*pos.Connection, error) {
	return nil, nil
}
func (p *externalConcurrencyProvider) HealthCheck(context.Context, string) error { return nil }
func (p *externalConcurrencyProvider) ListLocations(context.Context, string) ([]pos.Location, error) {
	return nil, nil
}
func (p *externalConcurrencyProvider) ListServices(context.Context, string) ([]pos.Service, error) {
	return nil, nil
}
func (p *externalConcurrencyProvider) ListStaff(context.Context, string) ([]pos.StaffMember, error) {
	return nil, nil
}
func (p *externalConcurrencyProvider) SearchCustomerByPhone(_ context.Context, _ string, phone string) (*pos.Customer, error) {
	return &pos.Customer{POSCustomerID: "customer-" + strings.TrimPrefix(phone, "+"), Name: "External concurrency caller", Phone: phone}, nil
}
func (p *externalConcurrencyProvider) CreateCustomer(_ context.Context, _ string, input pos.CreateCustomerInput) (*pos.Customer, error) {
	p.customerCreateCalls.Add(1)
	return &pos.Customer{POSCustomerID: "customer-external-concurrency", Name: input.Name, Phone: input.Phone, Email: input.Email}, nil
}
func (p *externalConcurrencyProvider) CheckAvailability(context.Context, string, pos.AvailabilityInput) ([]pos.TimeSlot, error) {
	return append([]pos.TimeSlot(nil), p.slots...), nil
}
func (p *externalConcurrencyProvider) CreateAppointment(_ context.Context, _ string, input pos.CreateAppointmentInput) (*pos.Appointment, error) {
	p.createCalls.Add(1)
	switch p.mode.Swap(0) {
	case 1:
		return nil, pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhaseResponse, errors.New("synthetic definitive rejection"))
	case 2:
		return nil, pos.NewWriteError(pos.WriteOutcomeUnknown, pos.WritePhaseResponse, errors.New("synthetic response loss"))
	}
	sequence := p.sequence.Add(1)
	return &pos.Appointment{
		POSAppointmentID: fmt.Sprintf("external-concurrency-%d", sequence), POSAppointmentVersion: int(sequence),
		StartTime: input.StartTime, EndTime: input.StartTime.Add(time.Duration(input.DurationMinutes) * time.Minute), Status: StatusConfirmed,
	}, nil
}
func (p *externalConcurrencyProvider) RescheduleAppointment(context.Context, string, string, pos.RescheduleInput) (*pos.Appointment, error) {
	return nil, errors.New("reschedule intentionally unsupported")
}
func (p *externalConcurrencyProvider) CancelAppointment(_ context.Context, _ string, appointmentID string, input pos.CancelInput) (*pos.Appointment, error) {
	p.cancelCalls.Add(1)
	if p.cancelMode.Swap(0) == 1 {
		return nil, pos.NewWriteError(pos.WriteOutcomeUnknown, pos.WritePhaseResponse, errors.New("synthetic cancellation response loss"))
	}
	return &pos.Appointment{POSAppointmentID: appointmentID, POSAppointmentVersion: input.BookingVersion + 1, Status: StatusCancelled}, nil
}
func (p *externalConcurrencyProvider) Sync(context.Context, string) error { return nil }

type externalConcurrencyFixture struct {
	OwnerID   string
	SalonID   string
	ServiceID string
	StaffID   string
	Start     time.Time
}

func TestExternalAtomicSlotCommitAcrossIndependentServices(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("OWNER_FIRST_RELEASE_GATE_DATABASE_REQUIRED") == "1" {
			t.Fatal("TEST_DATABASE_URL is required in release-gate mode")
		}
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	dbOne, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer dbOne.Close()
	dbTwo, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer dbTwo.Close()
	dbOne.SetMaxOpenConns(4)
	dbTwo.SetMaxOpenConns(4)
	ctx := context.Background()
	fixture := seedExternalConcurrencyFixture(t, ctx, dbOne)
	defer cleanupExternalConcurrencyFixture(ctx, dbOne, fixture)

	provider := &externalConcurrencyProvider{}
	for offset := 0; offset < 7; offset++ {
		start := fixture.Start.Add(time.Duration(offset) * time.Hour)
		provider.slots = append(provider.slots, pos.TimeSlot{
			StartTime: start, EndTime: start.Add(time.Hour), StaffID: "provider-staff-concurrency",
			Segments: []pos.TimeSlotSegment{{ServiceID: "provider-service-concurrency", StaffID: "provider-staff-concurrency", DurationMinutes: 60}},
		})
	}
	repositoryOne := NewRepository(dbOne)
	repositoryTwo := NewRepository(dbTwo)
	if err := repositoryOne.EnsureSalonOwner(ctx, fixture.SalonID, fixture.OwnerID); err != nil {
		t.Fatalf("seed owner boundary: %v", err)
	}
	providerName, fence, err := repositoryOne.GetActiveProviderFence(ctx, fixture.SalonID, fixture.OwnerID)
	if err != nil {
		t.Fatalf("seed provider fence: %v", err)
	}
	if _, err := repositoryOne.GetBookableService(ctx, fixture.SalonID, providerName, fixture.ServiceID); err != nil {
		t.Fatalf("seed bookable service at %#v: %v", fence, err)
	}
	if _, err := repositoryOne.GetBookableStaff(ctx, fixture.SalonID, providerName, fixture.StaffID); err != nil {
		t.Fatalf("seed bookable staff at %#v: %v", fence, err)
	}
	serviceOne := NewService(repositoryOne, []pos.POSProvider{provider})
	serviceTwo := NewService(repositoryTwo, []pos.POSProvider{provider})

	quoteA := quoteExternalSlot(t, ctx, serviceOne, fixture, 0)
	quoteB := quoteExternalSlot(t, ctx, serviceTwo, fixture, 0)
	staleOverlap := quoteExternalSlot(t, ctx, serviceOne, fixture, 0)
	adjacent := quoteExternalSlot(t, ctx, serviceTwo, fixture, 1)
	adjacentRebook := quoteExternalSlot(t, ctx, serviceOne, fixture, 1)

	requests := []CreateBookingRequest{
		externalCreateRequest(fixture, quoteA, "race-a-"+uuid.NewString()),
		externalCreateRequest(fixture, quoteB, "race-b-"+uuid.NewString()),
	}
	type result struct {
		attempt *BookingAttempt
		err     error
		index   int
	}
	results := make(chan result, 2)
	startGate := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(2)
	for index, service := range []*Service{serviceOne, serviceTwo} {
		go func(index int, service *Service) {
			ready.Done()
			<-startGate
			attempt, callErr := service.Create(ctx, fixture.SalonID, fixture.OwnerID, requests[index])
			results <- result{attempt: attempt, err: callErr, index: index}
		}(index, service)
	}
	ready.Wait()
	close(startGate)
	winners := 0
	conflicts := 0
	var winningRequest CreateBookingRequest
	for range 2 {
		result := <-results
		if result.err == nil && result.attempt != nil && result.attempt.Status == StatusConfirmed {
			winners++
			winningRequest = requests[result.index]
			continue
		}
		if errors.Is(result.err, ErrSlotCommitConflict) {
			conflicts++
			continue
		}
		t.Fatalf("unexpected race result: attempt=%#v err=%v", result.attempt, result.err)
	}
	if winners != 1 || conflicts != 1 || provider.createCalls.Load() != 1 {
		t.Fatalf("winner/conflict/provider=%d/%d/%d, want 1/1/1", winners, conflicts, provider.createCalls.Load())
	}

	replayed, err := serviceOne.Create(ctx, fixture.SalonID, fixture.OwnerID, winningRequest)
	if err != nil || replayed == nil || replayed.Status != StatusConfirmed || provider.createCalls.Load() != 1 {
		t.Fatalf("exact replay attempt/error/calls=%#v/%v/%d", replayed, err, provider.createCalls.Load())
	}
	changed := winningRequest
	changed.Notes = "different logical payload"
	if _, err := serviceOne.Create(ctx, fixture.SalonID, fixture.OwnerID, changed); !errors.Is(err, ErrOperationConflict) || provider.createCalls.Load() != 1 {
		t.Fatalf("changed operation error/calls=%v/%d", err, provider.createCalls.Load())
	}

	overlapRequest := externalCreateRequest(fixture, staleOverlap, "overlap-"+uuid.NewString())
	if _, err := serviceOne.Create(ctx, fixture.SalonID, fixture.OwnerID, overlapRequest); !errors.Is(err, ErrSlotCommitConflict) || provider.createCalls.Load() != 1 {
		t.Fatalf("true overlap error/calls=%v/%d", err, provider.createCalls.Load())
	}
	adjacentRequest := externalCreateRequest(fixture, adjacent, "adjacent-"+uuid.NewString())
	adjacentAttempt, err := serviceTwo.Create(ctx, fixture.SalonID, fixture.OwnerID, adjacentRequest)
	if err != nil || adjacentAttempt == nil || adjacentAttempt.Status != StatusConfirmed || provider.createCalls.Load() != 2 {
		t.Fatalf("half-open adjacent attempt/error/calls=%#v/%v/%d", adjacentAttempt, err, provider.createCalls.Load())
	}

	definiteQuote := quoteExternalSlot(t, ctx, serviceOne, fixture, 2)
	definiteRetryQuote := quoteExternalSlot(t, ctx, serviceTwo, fixture, 2)
	provider.mode.Store(1)
	if attempt, err := serviceOne.Create(ctx, fixture.SalonID, fixture.OwnerID, externalCreateRequest(fixture, definiteQuote, "definite-"+uuid.NewString())); err != nil || attempt == nil || attempt.ProviderOutcome != ProviderOutcomeFailed {
		t.Fatalf("definite failure attempt/error=%#v/%v", attempt, err)
	}
	definiteReplacement, err := serviceTwo.Create(ctx, fixture.SalonID, fixture.OwnerID, externalCreateRequest(fixture, definiteRetryQuote, "after-definite-"+uuid.NewString()))
	if err != nil || definiteReplacement == nil || definiteReplacement.Appointment == nil {
		t.Fatalf("released definite claim did not permit a new operation: %v", err)
	}

	unknownQuote := quoteExternalSlot(t, ctx, serviceOne, fixture, 3)
	unknownContender := quoteExternalSlot(t, ctx, serviceTwo, fixture, 3)
	provider.mode.Store(2)
	unknownAttempt, err := serviceOne.Create(ctx, fixture.SalonID, fixture.OwnerID, externalCreateRequest(fixture, unknownQuote, "unknown-"+uuid.NewString()))
	if err != nil || unknownAttempt == nil || unknownAttempt.ProviderOutcome != ProviderOutcomeUnknown || unknownAttempt.Reconciliation != ReconciliationRequired {
		t.Fatalf("unknown attempt/error=%#v/%v", unknownAttempt, err)
	}
	callsAfterUnknown := provider.createCalls.Load()
	if _, err := serviceTwo.Create(ctx, fixture.SalonID, fixture.OwnerID, externalCreateRequest(fixture, unknownContender, "unknown-contender-"+uuid.NewString())); !errors.Is(err, ErrSlotCommitConflict) || provider.createCalls.Load() != callsAfterUnknown {
		t.Fatalf("unknown claim contender error/calls=%v/%d", err, provider.createCalls.Load())
	}

	provider.cancelMode.Store(1)
	unknownCancelRequest := CancelRequest{
		OperationKey: "unknown-cancel-" + uuid.NewString(), Reason: "provider response was lost", Source: SourceOwnerDashboard,
	}
	_, unknownCancelAttempt, err := serviceOne.Cancel(ctx, fixture.SalonID, fixture.OwnerID, winningRequestAppointmentID(t, ctx, dbOne, fixture.SalonID, winningRequest.OperationKey), unknownCancelRequest)
	if err != nil || unknownCancelAttempt == nil || unknownCancelAttempt.ProviderOutcome != ProviderOutcomeUnknown || unknownCancelAttempt.Reconciliation != ReconciliationRequired {
		t.Fatalf("unknown cancellation attempt/error=%#v/%v", unknownCancelAttempt, err)
	}
	unknownCancelCalls := provider.cancelCalls.Load()
	if _, replayedCancel, replayErr := serviceTwo.Cancel(ctx, fixture.SalonID, fixture.OwnerID, winningRequestAppointmentID(t, ctx, dbOne, fixture.SalonID, winningRequest.OperationKey), unknownCancelRequest); replayErr != nil || replayedCancel == nil || provider.cancelCalls.Load() != unknownCancelCalls {
		t.Fatalf("unknown cancellation replay/error/calls=%#v/%v/%d", replayedCancel, replayErr, provider.cancelCalls.Load())
	}
	var originalClaimActive bool
	if err := dbOne.QueryRowContext(ctx, `SELECT released_at IS NULL FROM external_slot_claims WHERE salon_id=$1 AND booking_attempt_id=(SELECT booking_attempt_id FROM appointments WHERE id=$2)`, fixture.SalonID, winningRequestAppointmentID(t, ctx, dbOne, fixture.SalonID, winningRequest.OperationKey)).Scan(&originalClaimActive); err != nil || !originalClaimActive {
		t.Fatalf("unknown cancellation released original claim: active=%t err=%v", originalClaimActive, err)
	}

	cancelled, cancelFallback, err := serviceOne.Cancel(ctx, fixture.SalonID, fixture.OwnerID, adjacentAttempt.Appointment.ID, CancelRequest{
		OperationKey: "verified-cancel-" + uuid.NewString(), Reason: "verified provider cancellation", Source: SourceOwnerDashboard,
	})
	if err != nil || cancelFallback != nil || cancelled == nil || cancelled.Status != StatusCancelled {
		t.Fatalf("verified cancellation appointment/fallback/error=%#v/%#v/%v", cancelled, cancelFallback, err)
	}
	adjacentReplacement, err := serviceTwo.Create(ctx, fixture.SalonID, fixture.OwnerID, externalCreateRequest(fixture, adjacentRebook, "after-verified-cancel-"+uuid.NewString()))
	if err != nil || adjacentReplacement == nil || adjacentReplacement.Status != StatusConfirmed {
		t.Fatalf("verified cancel did not release claim for rebook: %#v/%v", adjacentReplacement, err)
	}

	crashQuote := quoteExternalSlot(t, ctx, serviceOne, fixture, 4)
	crashContender := quoteExternalSlot(t, ctx, serviceTwo, fixture, 4)
	crashClaim := claimExternalSlotWithoutDispatch(t, ctx, repositoryOne, fixture, crashQuote)
	if err := repositoryTwo.ExpireBookingOperationLeases(ctx, fixture.SalonID); err != nil {
		t.Fatalf("recover expired pre-dispatch claim: %v", err)
	}
	var crashState string
	var crashReleased bool
	if err := dbOne.QueryRowContext(ctx, `SELECT state,released_at IS NOT NULL FROM external_slot_claims WHERE salon_id=$1 AND booking_attempt_id=$2`, fixture.SalonID, crashClaim.Attempt.ID).Scan(&crashState, &crashReleased); err != nil || crashState != ExternalSlotClaimReleased || !crashReleased {
		t.Fatalf("pre-dispatch recovery state/released/error=%s/%t/%v", crashState, crashReleased, err)
	}
	crashReplacement, err := serviceTwo.Create(ctx, fixture.SalonID, fixture.OwnerID, externalCreateRequest(fixture, crashContender, "after-pre-dispatch-crash-"+uuid.NewString()))
	if err != nil || crashReplacement == nil || crashReplacement.Status != StatusConfirmed {
		t.Fatalf("pre-dispatch recovery did not reopen interval: %#v/%v", crashReplacement, err)
	}

	reconciliationQuote := quoteExternalSlot(t, ctx, serviceOne, fixture, 5)
	provider.mode.Store(2)
	reconciliationAttempt, err := serviceOne.Create(ctx, fixture.SalonID, fixture.OwnerID, externalCreateRequest(fixture, reconciliationQuote, "response-loss-reconcile-"+uuid.NewString()))
	if err != nil || reconciliationAttempt == nil || reconciliationAttempt.ProviderOutcome != ProviderOutcomeUnknown || reconciliationAttempt.Reconciliation != ReconciliationRequired {
		t.Fatalf("response-loss reconciliation attempt/error=%#v/%v", reconciliationAttempt, err)
	}
	dispatchesBeforeReconciliation := provider.createCalls.Load()
	attachAuthoritativeMirrorForUnknownCreate(t, ctx, dbOne, serviceOne, fixture, reconciliationAttempt)
	if provider.createCalls.Load() != dispatchesBeforeReconciliation {
		t.Fatalf("authoritative reconciliation dispatched provider again: %d -> %d", dispatchesBeforeReconciliation, provider.createCalls.Load())
	}

	otherFixture := seedExternalConcurrencyFixture(t, ctx, dbOne)
	defer cleanupExternalConcurrencyFixture(ctx, dbOne, otherFixture)
	otherService := NewService(NewRepository(dbTwo), []pos.POSProvider{provider})
	otherQuote := quoteExternalSlot(t, ctx, otherService, otherFixture, 4)
	otherAttempt, err := otherService.Create(ctx, otherFixture.SalonID, otherFixture.OwnerID, externalCreateRequest(otherFixture, otherQuote, "cross-tenant-same-resource-"+uuid.NewString()))
	if err != nil || otherAttempt == nil || otherAttempt.Status != StatusConfirmed {
		t.Fatalf("cross-tenant same resource should not conflict: %#v/%v", otherAttempt, err)
	}

	var confirmedAppointments, activeClaims, unknownClaims int
	if err := dbOne.QueryRowContext(ctx, `SELECT count(*) FROM appointments WHERE salon_id=$1 AND status='confirmed'`, fixture.SalonID).Scan(&confirmedAppointments); err != nil {
		t.Fatal(err)
	}
	if err := dbOne.QueryRowContext(ctx, `SELECT count(*) FROM external_slot_claims WHERE salon_id=$1 AND released_at IS NULL`, fixture.SalonID).Scan(&activeClaims); err != nil {
		t.Fatal(err)
	}
	if err := dbOne.QueryRowContext(ctx, `SELECT count(*) FROM external_slot_claims WHERE salon_id=$1 AND state IN ('dispatched_unknown','reconciliation_required') AND released_at IS NULL`, fixture.SalonID).Scan(&unknownClaims); err != nil {
		t.Fatal(err)
	}
	if confirmedAppointments != 5 || activeClaims != 6 || unknownClaims != 1 {
		t.Fatalf("appointments/active/unknown=%d/%d/%d, want 5/6/1", confirmedAppointments, activeClaims, unknownClaims)
	}
}

func winningRequestAppointmentID(t *testing.T, ctx context.Context, db *sql.DB, salonID string, operationKey string) string {
	t.Helper()
	var appointmentID string
	if err := db.QueryRowContext(ctx, `SELECT appointment.id::text FROM appointments appointment JOIN booking_attempts attempt ON attempt.id=appointment.booking_attempt_id AND attempt.salon_id=appointment.salon_id WHERE appointment.salon_id=$1 AND attempt.operation_key=$2`, salonID, operationKey).Scan(&appointmentID); err != nil {
		t.Fatal(err)
	}
	return appointmentID
}

func claimExternalSlotWithoutDispatch(t *testing.T, ctx context.Context, repository *Repository, fixture externalConcurrencyFixture, quote *AvailabilityResult) *BookingOperationClaim {
	t.Helper()
	fence := pos.ProviderFence{LocationID: "external-concurrency-location", SnapshotGeneration: 1}
	safety, err := repository.GetExternalSchedulingSafety(ctx, fixture.SalonID, pos.ProviderSquare, fence)
	if err != nil {
		t.Fatal(err)
	}
	service := ServiceRef{ID: fixture.ServiceID, POSProvider: pos.ProviderSquare, POSServiceID: "provider-service-concurrency", POSServiceVersion: 1, Name: "Dynamic concurrency service", DurationMinutes: 60, ProviderFence: fence}
	staff := StaffRef{ID: fixture.StaffID, POSProvider: pos.ProviderSquare, POSStaffID: "provider-staff-concurrency", Name: "Dynamic concurrency staff", ProviderFence: fence}
	slot := quote.Slots[0]
	claim, err := repository.ClaimPendingBookingAttempt(ctx, PendingBookingRecord{
		SalonID: fixture.SalonID, Source: "external-concurrency-crash", Provider: pos.ProviderSquare,
		POSIdempotencyKey: uuid.NewString(), OperationKey: "pre-dispatch-crash-" + uuid.NewString(), RequestFingerprint: strings.Repeat("e", 64),
		AvailabilityQuoteID: quote.QuoteID, SlotFingerprint: slot.Fingerprint, ProviderFence: fence,
		ProcessingToken: uuid.NewString(), LeaseExpiresAt: time.Now().UTC().Add(-time.Minute),
		CustomerName: "Pre-dispatch caller", CustomerPhone: "+13125550998",
		Service: service, Staff: staff, StaffSelectionMode: StaffSelectionSpecific,
		Segments:  []BookingSegmentRecord{{Service: service, Staff: staff, StaffSelectionMode: StaffSelectionSpecific, SortOrder: 1}},
		StartTime: slot.StartTime, EndTime: slot.EndTime, Notes: "simulated crash before dispatch",
		RequireExternalSlotClaim: true, SchedulingSafety: *safety,
		ClaimIntervals: []ExternalSlotClaimIntervalRecord{{ResourceKind: "staff", ResourceID: fixture.StaffID, SourceSegmentIndexes: []int{1}, OccupiedStartTime: slot.StartTime, OccupiedEndTime: slot.EndTime}},
	})
	if err != nil || claim == nil || !claim.Acquired {
		t.Fatalf("claim before simulated crash=%#v err=%v", claim, err)
	}
	return claim
}

func attachAuthoritativeMirrorForUnknownCreate(t *testing.T, ctx context.Context, db *sql.DB, service *Service, fixture externalConcurrencyFixture, attempt *BookingAttempt) {
	t.Helper()
	providerBookingID := "response-loss-match-" + uuid.NewString()
	var mirrorAttemptID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO booking_attempts(
			salon_id,source,status,pos_provider,pos_booking_id,pos_booking_version,pos_idempotency_key,
			operation_type,provider_outcome,retry_policy,reconciliation_status,
			customer_name,customer_phone,customer_email,service_id,staff_id,staff_selection_mode,
			requested_start_time,requested_end_time,provider_location_id,provider_snapshot_generation,
			scheduling_authority,authority_provider,authority_appointment_id,authority_appointment_version,
			authority_idempotency_key,authority_location_id,authority_snapshot_generation
		) VALUES(
			$1,'pos_calendar_sync','confirmed','square',$2,3,$3,
			'book','succeeded','none','not_required',$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11,
			'external-concurrency-location',1,'external_provider','square',$2,3,$3,'external-concurrency-location',1
		) RETURNING id::text
	`, fixture.SalonID, providerBookingID, uuid.NewString(), attempt.CustomerName, attempt.CustomerPhone, attempt.CustomerEmail,
		fixture.ServiceID, fixture.StaffID, StaffSelectionSpecific, attempt.RequestedStartTime, attempt.RequestedEndTime).Scan(&mirrorAttemptID); err != nil {
		t.Fatalf("insert authoritative mirror attempt: %v", err)
	}
	var mirrorAppointmentID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO appointments(
			salon_id,booking_attempt_id,pos_provider,pos_appointment_id,pos_appointment_version,status,
			customer_name,customer_phone,customer_email,service_id,staff_id,staff_selection_mode,
			start_time,end_time,pos_sync_status,last_pos_synced_at,
			scheduling_authority,authority_provider,authority_appointment_id,authority_appointment_version,
			confirmed_at,confirmation_source
		) VALUES(
			$1,$2,'square',$3,3,'confirmed',$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11,'synced',now(),
			'external_provider','square',$3,3,now(),'external_provider'
		) RETURNING id::text
	`, fixture.SalonID, mirrorAttemptID, providerBookingID, attempt.CustomerName, attempt.CustomerPhone, attempt.CustomerEmail,
		fixture.ServiceID, fixture.StaffID, StaffSelectionSpecific, attempt.RequestedStartTime, attempt.RequestedEndTime).Scan(&mirrorAppointmentID); err != nil {
		t.Fatalf("insert authoritative mirror appointment: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO appointment_services(
			appointment_id,service_id,staff_id,staff_selection_mode,pos_service_id,pos_service_version,
			pos_staff_id,name,duration_minutes,sort_order,salon_id,scheduling_authority,
			authority_provider,authority_service_id,authority_service_version,authority_staff_id
		) VALUES($1,$2,$3,'specific','provider-service-concurrency',1,'provider-staff-concurrency',
			'Dynamic concurrency service',60,1,$4,'external_provider','square','provider-service-concurrency',1,'provider-staff-concurrency')
	`, mirrorAppointmentID, fixture.ServiceID, fixture.StaffID, fixture.SalonID); err != nil {
		t.Fatalf("insert authoritative mirror segment: %v", err)
	}
	result, err := service.ResolveReconciliation(ctx, fixture.SalonID, fixture.OwnerID, attempt.ID, ResolveReconciliationRequest{
		ActionKey: "attach-response-loss-" + uuid.NewString(), Action: ReconciliationActionProviderAttached,
		ProviderAppointmentID: providerBookingID, ProviderAppointmentVersion: 3, ProviderStatus: "ACCEPTED",
		Note: "Exact provider calendar evidence matched the response-loss operation.",
	})
	if err != nil || result == nil || result.Status != "resolved" {
		t.Fatalf("attach authoritative mirror result/error=%#v/%v", result, err)
	}
	var attemptStatus, reconciliationStatus, claimState, claimProviderBookingID string
	var appointmentCount int
	if err := db.QueryRowContext(ctx, `SELECT status,reconciliation_status FROM booking_attempts WHERE id=$1`, attempt.ID).Scan(&attemptStatus, &reconciliationStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT state,COALESCE(provider_booking_id,'') FROM external_slot_claims WHERE salon_id=$1 AND booking_attempt_id=$2`, fixture.SalonID, attempt.ID).Scan(&claimState, &claimProviderBookingID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM appointments WHERE salon_id=$1 AND booking_attempt_id=$2 AND pos_appointment_id=$3`, fixture.SalonID, attempt.ID, providerBookingID).Scan(&appointmentCount); err != nil {
		t.Fatal(err)
	}
	if attemptStatus != StatusConfirmed || reconciliationStatus != ReconciliationResolved || claimState != ExternalSlotClaimConfirmed || claimProviderBookingID != providerBookingID || appointmentCount != 1 {
		t.Fatalf("reconciled attempt/claim/appointment=%s/%s/%s/%s/%d", attemptStatus, reconciliationStatus, claimState, claimProviderBookingID, appointmentCount)
	}
}

func quoteExternalSlot(t *testing.T, ctx context.Context, service *Service, fixture externalConcurrencyFixture, slotIndex int) *AvailabilityResult {
	t.Helper()
	availability, err := service.AvailableSlots(ctx, fixture.SalonID, fixture.OwnerID, AvailabilityRequest{
		ServiceID: fixture.ServiceID, StaffID: fixture.StaffID, StaffSelectionMode: StaffSelectionSpecific,
		PreferredDate: fixture.Start.Format("2006-01-02"), Limit: 10,
	})
	if err != nil {
		t.Fatalf("availability: %v", err)
	}
	wanted := fixture.Start.Add(time.Duration(slotIndex) * time.Hour)
	for _, slot := range availability.Slots {
		if slot.StartTime.Equal(wanted) {
			copy := *availability
			copy.Slots = []AvailabilitySlot{slot}
			return &copy
		}
	}
	t.Fatalf("slot %s missing from availability %#v", wanted, availability.Slots)
	return nil
}

func externalCreateRequest(fixture externalConcurrencyFixture, availability *AvailabilityResult, operationKey string) CreateBookingRequest {
	slot := availability.Slots[0]
	identity := uuid.NewSHA1(uuid.NameSpaceOID, []byte(operationKey)).String()
	digits := make([]byte, 0, 10)
	for index := 0; index < len(identity) && len(digits) < 10; index++ {
		if identity[index] >= '0' && identity[index] <= '9' {
			digits = append(digits, identity[index])
		}
	}
	for len(digits) < 10 {
		digits = append(digits, '0')
	}
	return CreateBookingRequest{
		OperationKey: operationKey, AvailabilityQuoteID: availability.QuoteID, SlotFingerprint: slot.Fingerprint,
		Source: "external-concurrency-test", CustomerName: "Concurrency caller " + identity[:8], CustomerPhone: "+1" + string(digits),
		ServiceID: fixture.ServiceID, StaffID: fixture.StaffID, StaffSelectionMode: StaffSelectionSpecific,
		StartTime: slot.StartTime, Notes: "bounded PostgreSQL concurrency evidence",
	}
}

func seedExternalConcurrencyFixture(t *testing.T, ctx context.Context, db *sql.DB) externalConcurrencyFixture {
	t.Helper()
	fixture := externalConcurrencyFixture{OwnerID: uuid.NewString(), SalonID: uuid.NewString(), ServiceID: uuid.NewString(), StaffID: uuid.NewString()}
	fixture.Start = time.Now().UTC().Add(72 * time.Hour).Truncate(24 * time.Hour).Add(10 * time.Hour)
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,email,password_hash,full_name) VALUES($1,$2,'external-concurrency','External concurrency owner')`, fixture.OwnerID, "external-concurrency-"+uuid.NewString()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO salons(id,name,phone,owner_user_id,timezone,active_pos_provider) VALUES($1,'External concurrency',$2,$3,'UTC','square')`, fixture.SalonID, "+1312555"+strings.ReplaceAll(uuid.NewString(), "-", "")[:4], fixture.OwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO salon_settings(salon_id,scheduling_authority,booking_mode) VALUES($1,'external_provider','confirmed_booking')`, fixture.SalonID); err != nil {
		t.Fatal(err)
	}
	configID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO salon_integration_configs(id,salon_id,provider,enabled,settings) VALUES($1,$2,'square',true,'{"api_version":"2026-05-20"}')`, configID, fixture.SalonID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO technical_resource_versions(salon_id,resource_type,resource_id,version) VALUES($1,'integration_config','square',1) ON CONFLICT(salon_id,resource_type,resource_id) DO UPDATE SET version=1`, fixture.SalonID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections(salon_id,provider,status,location_id,snapshot_generation,scopes,last_sync_at)
		VALUES($1,'square','active','external-concurrency-location',1,ARRAY['APPOINTMENTS_WRITE'],now())
	`, fixture.SalonID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO services(id,salon_id,pos_provider,pos_service_id,pos_service_version,name,duration_minutes,active,ai_bookable,source,sync_status)
		VALUES($1,$2,'square','provider-service-concurrency',1,'Dynamic concurrency service',60,true,true,'imported','synced')
	`, fixture.ServiceID, fixture.SalonID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO staff(id,salon_id,pos_provider,pos_staff_id,name,active,ai_bookable,source,sync_status)
		VALUES($1,$2,'square','provider-staff-concurrency','Dynamic concurrency staff',true,true,'imported','synced')
	`, fixture.StaffID, fixture.SalonID); err != nil {
		t.Fatal(err)
	}
	for _, link := range []struct{ kind, id, providerID string }{{"service", fixture.ServiceID, "provider-service-concurrency"}, {"staff", fixture.StaffID, "provider-staff-concurrency"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO pos_entity_links(salon_id,entity_type,entity_id,provider,provider_entity_id,provider_version,sync_status,last_synced_at) VALUES($1,$2,$3,'square',$4,1,'synced',now())`, fixture.SalonID, link.kind, link.id, link.providerID); err != nil {
			t.Fatal(err)
		}
	}
	for day := 0; day < 7; day++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO salon_business_hour_periods(salon_id,day_of_week,start_local_time,end_local_time,source,provider,provider_location_id,provider_period_index) VALUES($1,$2,'00:00','23:59:59','imported','square','external-concurrency-location',0)`, fixture.SalonID, day); err != nil {
			t.Fatal(err)
		}
	}
	posRepository := pos.NewRepository(db)
	connection, err := posRepository.GetConnection(ctx, fixture.SalonID, pos.ProviderSquare)
	if err != nil {
		t.Fatal(err)
	}
	capability, _, err := posRepository.ReevaluateSquareSchedulingCapability(ctx, pos.SchedulingCapabilityEvaluationInput{
		SalonID: fixture.SalonID, ActorUserID: fixture.OwnerID, ActionKey: "external-concurrency-capability-" + uuid.NewString(),
		RequestFingerprint: strings.Repeat("d", 64), ExpectedConnectionCapabilityVersion: connection.BookingWriteCapabilityVersion,
		ExpectedIntegrationConfigVersion: 1,
	})
	if err != nil || !capability.AutomaticSingleCreate {
		t.Fatalf("seed capability=%#v err=%v", capability, err)
	}
	return fixture
}

func cleanupExternalConcurrencyFixture(ctx context.Context, db *sql.DB, fixture externalConcurrencyFixture) {
	_, _ = db.ExecContext(ctx, `DELETE FROM salons WHERE id=$1`, fixture.SalonID)
	_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, fixture.OwnerID)
}
