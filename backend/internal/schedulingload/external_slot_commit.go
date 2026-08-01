package schedulingload

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/pos"
)

const (
	externalProviderServiceID = "synthetic-external-service"
	externalProviderStaffID   = "synthetic-external-staff"
)

type externalLoadProvider struct {
	slots                []pos.TimeSlot
	dispatches           atomic.Int64
	createDispatches     atomic.Int64
	rescheduleDispatches atomic.Int64
	cancelDispatches     atomic.Int64
	sequence             atomic.Int64
}

func (provider *externalLoadProvider) Name() string { return pos.ProviderSquare }
func (provider *externalLoadProvider) Capabilities() pos.ProviderCapabilities {
	return pos.ProviderCapabilities{AtomicCreateNoOverlap: true, ConcreteStaffAssignment: true}
}
func (provider *externalLoadProvider) SchedulingCapabilities(context.Context, string, pos.ProviderFence) (pos.ProviderCapabilities, error) {
	return provider.Capabilities(), nil
}
func (provider *externalLoadProvider) Connect(context.Context, pos.ConnectInput) (*pos.Connection, error) {
	return nil, nil
}
func (provider *externalLoadProvider) HealthCheck(context.Context, string) error { return nil }
func (provider *externalLoadProvider) ListLocations(context.Context, string) ([]pos.Location, error) {
	return nil, nil
}
func (provider *externalLoadProvider) ListServices(context.Context, string) ([]pos.Service, error) {
	return nil, nil
}
func (provider *externalLoadProvider) ListStaff(context.Context, string) ([]pos.StaffMember, error) {
	return nil, nil
}
func (provider *externalLoadProvider) SearchCustomerByPhone(_ context.Context, _ string, phone string) (*pos.Customer, error) {
	return &pos.Customer{POSCustomerID: "synthetic-customer-" + strings.TrimPrefix(phone, "+"), Name: "Synthetic caller", Phone: phone}, nil
}
func (provider *externalLoadProvider) CreateCustomer(context.Context, string, pos.CreateCustomerInput) (*pos.Customer, error) {
	return nil, errors.New("fake provider customer creation is intentionally disabled")
}
func (provider *externalLoadProvider) CheckAvailability(context.Context, string, pos.AvailabilityInput) ([]pos.TimeSlot, error) {
	return append([]pos.TimeSlot(nil), provider.slots...), nil
}
func (provider *externalLoadProvider) CreateAppointment(_ context.Context, _ string, input pos.CreateAppointmentInput) (*pos.Appointment, error) {
	provider.dispatches.Add(1)
	provider.createDispatches.Add(1)
	switch {
	case strings.Contains(input.Notes, "synthetic-definitive-failure"):
		return nil, pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhaseResponse, errors.New("bounded fake provider definitive failure"))
	case strings.Contains(input.Notes, "synthetic-response-loss"):
		return nil, pos.NewWriteError(pos.WriteOutcomeUnknown, pos.WritePhaseResponse, errors.New("bounded fake provider response loss"))
	}
	sequence := provider.sequence.Add(1)
	return &pos.Appointment{
		POSAppointmentID:      fmt.Sprintf("synthetic-external-booking-%d", sequence),
		POSAppointmentVersion: 1,
		StartTime:             input.StartTime,
		EndTime:               input.StartTime.Add(time.Duration(input.DurationMinutes) * time.Minute),
		Status:                "confirmed",
	}, nil
}
func (provider *externalLoadProvider) RescheduleAppointment(context.Context, string, string, pos.RescheduleInput) (*pos.Appointment, error) {
	provider.dispatches.Add(1)
	provider.rescheduleDispatches.Add(1)
	return nil, errors.New("fake provider reschedule must remain unreachable")
}
func (provider *externalLoadProvider) CancelAppointment(_ context.Context, _ string, appointmentID string, input pos.CancelInput) (*pos.Appointment, error) {
	provider.dispatches.Add(1)
	provider.cancelDispatches.Add(1)
	if strings.Contains(input.Reason, "synthetic-cancel-response-loss") {
		return nil, pos.NewWriteError(pos.WriteOutcomeUnknown, pos.WritePhaseResponse, errors.New("bounded fake cancellation response loss"))
	}
	return &pos.Appointment{
		POSAppointmentID: appointmentID, POSAppointmentVersion: input.BookingVersion + 1,
		Status: "cancelled",
	}, nil
}
func (provider *externalLoadProvider) Sync(context.Context, string) error { return nil }

type externalLoadReplica struct {
	db      *sql.DB
	service *booking.Service
}

type externalRaceResult struct {
	report    WorkloadReport
	winner    booking.CreateBookingRequest
	attempt   *booking.BookingAttempt
	conflicts int
	samples   []operationSample
}

func runExternalSlotCommitWorkload(
	ctx context.Context,
	config Config,
	seed seededRun,
	violations *InvariantViolations,
) ([]WorkloadReport, ExternalSlotCommitEvidence, []PoolEvidence, error) {
	provider := &externalLoadProvider{}
	for offset := 0; offset < 9; offset++ {
		start := config.workloadTime().Add(time.Duration(offset) * time.Hour)
		provider.slots = append(provider.slots, pos.TimeSlot{
			StartTime: start, EndTime: start.Add(time.Hour), StaffID: externalProviderStaffID,
			Segments: []pos.TimeSlotSegment{{ServiceID: externalProviderServiceID, StaffID: externalProviderStaffID, DurationMinutes: 60}},
		})
	}
	overlapStart := config.workloadTime().Add(90 * time.Minute)
	provider.slots = append(provider.slots, pos.TimeSlot{
		StartTime: overlapStart, EndTime: overlapStart.Add(time.Hour), StaffID: externalProviderStaffID,
		Segments: []pos.TimeSlotSegment{{ServiceID: externalProviderServiceID, StaffID: externalProviderStaffID, DurationMinutes: 60}},
	})
	replicas, err := openExternalLoadReplicas(ctx, config, provider)
	if err != nil {
		return nil, ExternalSlotCommitEvidence{}, nil, err
	}
	defer closeExternalLoadReplicas(replicas)

	reports := make([]WorkloadReport, 0, 9)
	claimSamples := make([]operationSample, 0, config.OperationsPerWorkload*3)
	expectedDispatches := 0
	expectedConflicts := 0
	observedConflicts := 0

	firstRace, err := runExternalCreateRace(ctx, config, replicas, seed.External, seed.OwnerID, []time.Duration{0}, "one-staff-slot")
	if err != nil {
		return reports, ExternalSlotCommitEvidence{}, externalReplicaPools(replicas), err
	}
	reports = append(reports, firstRace.report)
	claimSamples = append(claimSamples, firstRace.samples...)
	expectedDispatches++
	expectedConflicts += config.OperationsPerWorkload - 1
	observedConflicts += firstRace.conflicts

	staleCancelQuotes := make(map[time.Time]*booking.AvailabilityResult, 2)
	for _, offset := range []time.Duration{time.Hour, 90 * time.Minute} {
		quote, quoteErr := externalLoadQuote(ctx, replicas[0].service, seed.External, seed.OwnerID, config.workloadTime().Add(offset), "")
		if quoteErr != nil {
			return reports, ExternalSlotCommitEvidence{}, externalReplicaPools(replicas), quoteErr
		}
		staleCancelQuotes[quote.Slots[0].StartTime] = quote
	}
	secondRace, err := runExternalCreateRace(ctx, config, replicas, seed.External, seed.OwnerID, []time.Duration{time.Hour, 90 * time.Minute}, "overlapping-interval-race")
	if err != nil {
		return reports, ExternalSlotCommitEvidence{}, externalReplicaPools(replicas), err
	}
	staleCancelQuote := staleCancelQuotes[secondRace.winner.StartTime]
	if staleCancelQuote == nil {
		return reports, ExternalSlotCommitEvidence{}, externalReplicaPools(replicas), errors.New("overlap race winner has no matching pre-race quote")
	}
	reports = append(reports, secondRace.report)
	claimSamples = append(claimSamples, secondRace.samples...)
	expectedDispatches++
	expectedConflicts += config.OperationsPerWorkload - 1
	observedConflicts += secondRace.conflicts

	replayStarted := time.Now()
	dispatchBeforeReplay := provider.dispatches.Load()
	replaySamples := runConcurrent(ctx, config.OperationsPerWorkload, config.Concurrency, func(callCtx context.Context, index int) sampleResult {
		attempt, callErr := replicas[index%len(replicas)].service.Create(callCtx, seed.External.SalonID, seed.OwnerID, firstRace.winner)
		return sampleResult{Success: callErr == nil && attempt != nil && attempt.ID == firstRace.attempt.ID, Replayed: callErr == nil, UnexpectedError: callErr != nil || attempt == nil || attempt.ID != firstRace.attempt.ID}
	})
	if provider.dispatches.Load() != dispatchBeforeReplay {
		violations.Idempotency++
	}
	reports = append(reports, summarizeWorkload("external_exact_replay", replayStarted, replaySamples))
	claimSamples = append(claimSamples, replaySamples...)

	definiteStarted := time.Now()
	definiteQuote, err := externalLoadQuote(ctx, replicas[0].service, seed.External, seed.OwnerID, config.workloadTime().Add(3*time.Hour), "")
	if err != nil {
		return reports, ExternalSlotCommitEvidence{}, externalReplicaPools(replicas), err
	}
	definiteAttempt, callErr := replicas[0].service.Create(ctx, seed.External.SalonID, seed.OwnerID, externalLoadCreateRequest(seed.External, definiteQuote, "definite-"+config.RunID, "synthetic-definitive-failure"))
	definiteExpected := callErr == nil && definiteAttempt != nil && definiteAttempt.ProviderOutcome == booking.ProviderOutcomeFailed
	releasedQuote, err := externalLoadQuote(ctx, replicas[1%len(replicas)].service, seed.External, seed.OwnerID, config.workloadTime().Add(3*time.Hour), "")
	if err != nil {
		return reports, ExternalSlotCommitEvidence{}, externalReplicaPools(replicas), err
	}
	releasedAttempt, releasedErr := replicas[1%len(replicas)].service.Create(ctx, seed.External.SalonID, seed.OwnerID, externalLoadCreateRequest(seed.External, releasedQuote, "after-definite-"+config.RunID, "released-claim-rebook"))
	definiteSamples := []operationSample{
		{success: definiteExpected, unexpectedError: !definiteExpected},
		{success: releasedErr == nil && releasedAttempt != nil && releasedAttempt.Status == booking.StatusConfirmed, unexpectedError: releasedErr != nil || releasedAttempt == nil || releasedAttempt.Status != booking.StatusConfirmed},
	}
	reports = append(reports, summarizeWorkload("external_definite_failure_release", definiteStarted, definiteSamples))
	claimSamples = append(claimSamples, definiteSamples...)
	expectedDispatches += 2

	unknownStarted := time.Now()
	unknownQuote, err := externalLoadQuote(ctx, replicas[0].service, seed.External, seed.OwnerID, config.workloadTime().Add(4*time.Hour), "")
	if err != nil {
		return reports, ExternalSlotCommitEvidence{}, externalReplicaPools(replicas), err
	}
	unknownContender, err := externalLoadQuote(ctx, replicas[1%len(replicas)].service, seed.External, seed.OwnerID, config.workloadTime().Add(4*time.Hour), "")
	if err != nil {
		return reports, ExternalSlotCommitEvidence{}, externalReplicaPools(replicas), err
	}
	unknownAttempt, unknownErr := replicas[0].service.Create(ctx, seed.External.SalonID, seed.OwnerID, externalLoadCreateRequest(seed.External, unknownQuote, "unknown-"+config.RunID, "synthetic-response-loss"))
	unknownExpected := unknownErr == nil && unknownAttempt != nil && unknownAttempt.ProviderOutcome == booking.ProviderOutcomeUnknown && unknownAttempt.Reconciliation == booking.ReconciliationRequired
	dispatchAfterUnknown := provider.dispatches.Load()
	_, contenderErr := replicas[1%len(replicas)].service.Create(ctx, seed.External.SalonID, seed.OwnerID, externalLoadCreateRequest(seed.External, unknownContender, "unknown-contender-"+config.RunID, "blocked-contender"))
	contenderExpected := errors.Is(contenderErr, booking.ErrSlotCommitConflict) && provider.dispatches.Load() == dispatchAfterUnknown
	unknownSamples := []operationSample{
		{success: unknownExpected, unexpectedError: !unknownExpected},
		{expectedConflict: contenderExpected, unexpectedError: !contenderExpected},
	}
	reports = append(reports, summarizeWorkload("external_dispatch_unknown_retention", unknownStarted, unknownSamples))
	claimSamples = append(claimSamples, unknownSamples...)
	expectedDispatches++
	expectedConflicts++
	if contenderExpected {
		observedConflicts++
	}

	rescheduleStarted := time.Now()
	rescheduleTime := config.workloadTime().Add(5 * time.Hour)
	rescheduleQuote, err := externalLoadQuote(ctx, replicas[0].service, seed.External, seed.OwnerID, rescheduleTime, firstRace.attempt.Appointment.ID)
	if err != nil {
		return reports, ExternalSlotCommitEvidence{}, externalReplicaPools(replicas), err
	}
	rescheduleContenderQuote, err := externalLoadQuote(ctx, replicas[1%len(replicas)].service, seed.External, seed.OwnerID, rescheduleTime, "")
	if err != nil {
		return reports, ExternalSlotCommitEvidence{}, externalReplicaPools(replicas), err
	}
	rescheduleCallsBefore := provider.rescheduleDispatches.Load()
	rescheduleSamples := runConcurrent(ctx, 2, 2, func(callCtx context.Context, index int) sampleResult {
		if index == 0 {
			_, _, rescheduleErr := replicas[0].service.Reschedule(callCtx, seed.External.SalonID, seed.OwnerID, firstRace.attempt.Appointment.ID, booking.RescheduleRequest{
				OperationKey: "request-only-reschedule-" + config.RunID, AvailabilityQuoteID: rescheduleQuote.QuoteID,
				SlotFingerprint: rescheduleQuote.Slots[0].Fingerprint, StartTime: rescheduleQuote.Slots[0].StartTime,
				StaffID: seed.External.StaffID, Source: booking.SourceOwnerDashboard,
			})
			blocked := errors.Is(rescheduleErr, booking.ErrSchedulingAuthorityNotReady)
			return sampleResult{Success: blocked, UnexpectedError: !blocked}
		}
		created, createErr := replicas[1%len(replicas)].service.Create(callCtx, seed.External.SalonID, seed.OwnerID,
			externalLoadCreateRequest(seed.External, rescheduleContenderQuote, "reschedule-extension-contender-"+config.RunID, "reschedule-extension-race"))
		succeeded := createErr == nil && created != nil && created.Status == booking.StatusConfirmed
		return sampleResult{Success: succeeded, UnexpectedError: !succeeded}
	})
	if provider.rescheduleDispatches.Load() != rescheduleCallsBefore {
		violations.ProviderCalls++
	}
	expectedDispatches++
	reports = append(reports, summarizeWorkload("external_reschedule_extension_race", rescheduleStarted, rescheduleSamples))
	claimSamples = append(claimSamples, rescheduleSamples...)

	cancelStarted := time.Now()
	cancelSamples := runExternalCancelRebookRace(ctx, config, replicas, provider, seed, secondRace, staleCancelQuote, &expectedDispatches, &expectedConflicts, &observedConflicts)
	reports = append(reports, summarizeWorkload("external_cancel_rebook_race", cancelStarted, cancelSamples))
	claimSamples = append(claimSamples, cancelSamples...)

	unknownCancelStarted := time.Now()
	unknownCancelSamples, unknownCancelDispatches, err := runExternalUnknownCancel(ctx, config, replicas, seed)
	if err != nil {
		return reports, ExternalSlotCommitEvidence{}, externalReplicaPools(replicas), err
	}
	expectedDispatches += unknownCancelDispatches
	reports = append(reports, summarizeWorkload("external_cancel_unknown_retention", unknownCancelStarted, unknownCancelSamples))
	claimSamples = append(claimSamples, unknownCancelSamples...)

	crossTenantStarted := time.Now()
	crossTenantSamples, err := runExternalCrossTenant(ctx, config, replicas, seed)
	if err != nil {
		return reports, ExternalSlotCommitEvidence{}, externalReplicaPools(replicas), err
	}
	expectedDispatches += 2
	reports = append(reports, summarizeWorkload("external_cross_tenant_resource_identity", crossTenantStarted, crossTenantSamples))
	claimSamples = append(claimSamples, crossTenantSamples...)

	evidence, err := inspectExternalSlotCommitInvariants(ctx, replicas[0].db, seed, provider, expectedDispatches, expectedConflicts, observedConflicts, claimSamples)
	if err != nil {
		return reports, ExternalSlotCommitEvidence{}, externalReplicaPools(replicas), err
	}
	if evidence.ConflictLoserProviderDispatches > 0 {
		violations.ProviderCalls += evidence.ConflictLoserProviderDispatches
	}
	violations.Duplicates += evidence.DuplicateConfirmations
	violations.Orphans += evidence.OrphanClaims + evidence.OrphanIntervals + evidence.OrphanEvents
	violations.Safety += evidence.UnexpectedClaimReleases
	if evidence.RealProviderRuntimeCalls > 0 {
		violations.ProviderEvidence += evidence.RealProviderRuntimeCalls
	}
	return reports, evidence, externalReplicaPools(replicas), nil
}

func openExternalLoadReplicas(ctx context.Context, config Config, provider *externalLoadProvider) ([]externalLoadReplica, error) {
	replicas := make([]externalLoadReplica, 0, config.Concurrency)
	for index := 0; index < config.Concurrency; index++ {
		db, err := sql.Open("postgres", config.DatabaseURL)
		if err != nil {
			closeExternalLoadReplicas(replicas)
			return nil, err
		}
		db.SetMaxOpenConns(2)
		db.SetMaxIdleConns(2)
		db.SetConnMaxLifetime(5 * time.Minute)
		if err := db.PingContext(ctx); err != nil {
			db.Close()
			closeExternalLoadReplicas(replicas)
			return nil, err
		}
		repository := booking.NewRepository(db)
		replicas = append(replicas, externalLoadReplica{db: db, service: booking.NewService(repository, []pos.POSProvider{provider})})
	}
	return replicas, nil
}

func closeExternalLoadReplicas(replicas []externalLoadReplica) {
	for _, replica := range replicas {
		_ = replica.db.Close()
	}
}

func externalReplicaPools(replicas []externalLoadReplica) []PoolEvidence {
	result := make([]PoolEvidence, 0, len(replicas))
	for _, replica := range replicas {
		stats := replica.db.Stats()
		result = append(result, PoolEvidence{
			MaxOpenConnections: stats.MaxOpenConnections, OpenConnections: stats.OpenConnections,
			InUse: stats.InUse, Idle: stats.Idle, WaitCount: stats.WaitCount, WaitDurationMS: stats.WaitDuration.Milliseconds(),
		})
	}
	return result
}

func runExternalCreateRace(ctx context.Context, config Config, replicas []externalLoadReplica, salon seededSalon, ownerID string, startOffsets []time.Duration, label string) (externalRaceResult, error) {
	if len(startOffsets) == 0 {
		return externalRaceResult{}, errors.New("external race requires at least one start offset")
	}
	requests := make([]booking.CreateBookingRequest, config.OperationsPerWorkload)
	for index := range requests {
		wanted := config.workloadTime().Add(startOffsets[index%len(startOffsets)])
		quote, err := externalLoadQuote(ctx, replicas[index%len(replicas)].service, salon, ownerID, wanted, "")
		if err != nil {
			return externalRaceResult{}, err
		}
		requests[index] = externalLoadCreateRequest(salon, quote, label+"-"+uuid.NewString(), label)
	}
	started := time.Now()
	var lock sync.Mutex
	var winner booking.CreateBookingRequest
	var attempt *booking.BookingAttempt
	conflicts := 0
	samples := runConcurrent(ctx, len(requests), config.Concurrency, func(callCtx context.Context, index int) sampleResult {
		created, callErr := replicas[index%len(replicas)].service.Create(callCtx, salon.SalonID, ownerID, requests[index])
		if callErr == nil && created != nil && created.Status == booking.StatusConfirmed {
			lock.Lock()
			winner = requests[index]
			attempt = created
			lock.Unlock()
			return sampleResult{Success: true}
		}
		if errors.Is(callErr, booking.ErrSlotCommitConflict) {
			lock.Lock()
			conflicts++
			lock.Unlock()
			return sampleResult{ExpectedConflict: true}
		}
		return sampleResult{UnexpectedError: true}
	})
	if attempt == nil || conflicts != len(requests)-1 {
		return externalRaceResult{}, fmt.Errorf("%s race winner/conflicts were not 1/%d", label, len(requests)-1)
	}
	return externalRaceResult{
		report: summarizeWorkload("external_"+label, started, samples), winner: winner,
		attempt: attempt, conflicts: conflicts, samples: samples,
	}, nil
}

func externalLoadQuote(ctx context.Context, service *booking.Service, salon seededSalon, ownerID string, wanted time.Time, targetAppointmentID string) (*booking.AvailabilityResult, error) {
	availability, err := service.AvailableSlots(ctx, salon.SalonID, ownerID, booking.AvailabilityRequest{
		TargetAppointmentID: targetAppointmentID, ServiceID: salon.ServiceID, StaffID: salon.StaffID,
		StaffSelectionMode: booking.StaffSelectionSpecific, PreferredDate: wanted.Format("2006-01-02"), Limit: 20,
	})
	if err != nil {
		return nil, err
	}
	for _, slot := range availability.Slots {
		if slot.StartTime.Equal(wanted) {
			copy := *availability
			copy.Slots = []booking.AvailabilitySlot{slot}
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("synthetic external slot %s is unavailable", wanted.Format(time.RFC3339))
}

func externalLoadCreateRequest(salon seededSalon, quote *booking.AvailabilityResult, operationKey string, notes string) booking.CreateBookingRequest {
	slot := quote.Slots[0]
	return booking.CreateBookingRequest{
		OperationKey: operationKey, AvailabilityQuoteID: quote.QuoteID, SlotFingerprint: slot.Fingerprint,
		Source: booking.SourceOwnerDashboard, CustomerName: "Synthetic caller " + operationKey[:min(len(operationKey), 12)], CustomerPhone: syntheticPhone(operationKey, "external-caller"),
		ServiceID: salon.ServiceID, StaffID: salon.StaffID, StaffSelectionMode: booking.StaffSelectionSpecific,
		StartTime: slot.StartTime, Notes: notes,
	}
}

func runExternalCancelRebookRace(
	ctx context.Context,
	config Config,
	replicas []externalLoadReplica,
	provider *externalLoadProvider,
	seed seededRun,
	winner externalRaceResult,
	staleQuote *booking.AvailabilityResult,
	expectedDispatches *int,
	expectedConflicts *int,
	observedConflicts *int,
) []operationSample {
	if winner.attempt == nil || winner.attempt.Appointment == nil {
		return []operationSample{{unexpectedError: true}}
	}
	cancelRequest := booking.CancelRequest{OperationKey: "cancel-race-" + config.RunID, Reason: "verified synthetic cancellation", Source: booking.SourceOwnerDashboard}
	rebookRequest := externalLoadCreateRequest(seed.External, staleQuote, "cancel-race-rebook-"+config.RunID, "cancel-rebook-race")
	var rebookConflict atomic.Bool
	var rebookSuccess atomic.Bool
	samples := runConcurrent(ctx, 2, 2, func(callCtx context.Context, index int) sampleResult {
		if index == 0 {
			appointment, fallback, callErr := replicas[0].service.Cancel(callCtx, seed.External.SalonID, seed.OwnerID, winner.attempt.Appointment.ID, cancelRequest)
			success := callErr == nil && fallback == nil && appointment != nil && appointment.Status == booking.StatusCancelled
			return sampleResult{Success: success, UnexpectedError: !success}
		}
		created, callErr := replicas[1%len(replicas)].service.Create(callCtx, seed.External.SalonID, seed.OwnerID, rebookRequest)
		if errors.Is(callErr, booking.ErrSlotCommitConflict) {
			rebookConflict.Store(true)
			return sampleResult{ExpectedConflict: true}
		}
		success := callErr == nil && created != nil && created.Status == booking.StatusConfirmed
		rebookSuccess.Store(success)
		return sampleResult{Success: success, UnexpectedError: !success}
	})
	*expectedDispatches += 2
	if rebookConflict.Load() {
		*expectedConflicts++
		*observedConflicts++
		freshQuote, err := externalLoadQuote(ctx, replicas[0].service, seed.External, seed.OwnerID, winner.winner.StartTime, "")
		if err != nil {
			return append(samples, operationSample{unexpectedError: true})
		}
		created, err := replicas[0].service.Create(ctx, seed.External.SalonID, seed.OwnerID, externalLoadCreateRequest(seed.External, freshQuote, "cancel-race-fresh-"+config.RunID, "fresh-after-cancel"))
		samples = append(samples, operationSample{success: err == nil && created != nil && created.Status == booking.StatusConfirmed, unexpectedError: err != nil || created == nil || created.Status != booking.StatusConfirmed})
	} else if !rebookSuccess.Load() {
		return append(samples, operationSample{unexpectedError: true})
	}
	if provider.cancelDispatches.Load() == 0 {
		return append(samples, operationSample{unexpectedError: true})
	}
	return samples
}

func runExternalUnknownCancel(ctx context.Context, config Config, replicas []externalLoadReplica, seed seededRun) ([]operationSample, int, error) {
	quote, err := externalLoadQuote(ctx, replicas[0].service, seed.External, seed.OwnerID, config.workloadTime().Add(7*time.Hour), "")
	if err != nil {
		return nil, 0, err
	}
	created, err := replicas[0].service.Create(ctx, seed.External.SalonID, seed.OwnerID, externalLoadCreateRequest(seed.External, quote, "unknown-cancel-target-"+config.RunID, "unknown-cancel-target"))
	if err != nil || created == nil || created.Appointment == nil {
		return nil, 0, fmt.Errorf("create unknown-cancel target: %w", err)
	}
	appointment, fallback, cancelErr := replicas[1%len(replicas)].service.Cancel(ctx, seed.External.SalonID, seed.OwnerID, created.Appointment.ID, booking.CancelRequest{
		OperationKey: "unknown-cancel-" + config.RunID, Reason: "synthetic-cancel-response-loss", Source: booking.SourceOwnerDashboard,
	})
	expected := cancelErr == nil && appointment == nil && fallback != nil && fallback.ProviderOutcome == booking.ProviderOutcomeUnknown && fallback.Reconciliation == booking.ReconciliationRequired
	return []operationSample{{success: true}, {success: expected, unexpectedError: !expected}}, 2, nil
}

func runExternalCrossTenant(ctx context.Context, config Config, replicas []externalLoadReplica, seed seededRun) ([]operationSample, error) {
	firstQuote, err := externalLoadQuote(ctx, replicas[0].service, seed.External, seed.OwnerID, config.workloadTime().Add(6*time.Hour), "")
	if err != nil {
		return nil, err
	}
	secondQuote, err := externalLoadQuote(ctx, replicas[1%len(replicas)].service, seed.ExternalOther, seed.OwnerID, config.workloadTime().Add(6*time.Hour), "")
	if err != nil {
		return nil, err
	}
	requests := []struct {
		salon seededSalon
		quote *booking.AvailabilityResult
	}{{seed.External, firstQuote}, {seed.ExternalOther, secondQuote}}
	samples := runConcurrent(ctx, 2, 2, func(callCtx context.Context, index int) sampleResult {
		created, callErr := replicas[index%len(replicas)].service.Create(callCtx, requests[index].salon.SalonID, seed.OwnerID,
			externalLoadCreateRequest(requests[index].salon, requests[index].quote, fmt.Sprintf("cross-tenant-%s-%d", config.RunID, index), "cross-tenant-same-resource"))
		success := callErr == nil && created != nil && created.Status == booking.StatusConfirmed
		return sampleResult{Success: success, UnexpectedError: !success}
	})
	return samples, nil
}

func inspectExternalSlotCommitInvariants(
	ctx context.Context,
	db *sql.DB,
	seed seededRun,
	provider *externalLoadProvider,
	expectedDispatches int,
	expectedConflicts int,
	observedConflicts int,
	claimSamples []operationSample,
) (ExternalSlotCommitEvidence, error) {
	evidence := ExternalSlotCommitEvidence{
		ExpectedFakeProviderDispatches: expectedDispatches, FakeProviderDispatches: int(provider.dispatches.Load()),
		ExpectedConflictCount: expectedConflicts, ObservedConflictCount: observedConflicts,
		ClaimLatency: summarizeOperationLatencies(claimSamples),
	}
	salonIDs := []string{seed.External.SalonID, seed.ExternalOther.SalonID}
	queries := []struct {
		target *int
		query  string
	}{
		{&evidence.DuplicateConfirmations, `SELECT count(*) FROM appointments first_appointment JOIN appointments second_appointment ON second_appointment.salon_id=first_appointment.salon_id AND second_appointment.staff_id=first_appointment.staff_id AND second_appointment.id>first_appointment.id AND first_appointment.start_time<second_appointment.end_time AND second_appointment.start_time<first_appointment.end_time WHERE first_appointment.salon_id=ANY($1) AND first_appointment.status='confirmed' AND second_appointment.status='confirmed'`},
		{&evidence.UnexpectedClaimReleases, `SELECT count(*) FROM external_slot_claims WHERE salon_id=ANY($1) AND state IN ('dispatched_unknown','reconciliation_required') AND released_at IS NOT NULL`},
		{&evidence.OrphanClaims, `SELECT count(*) FROM external_slot_claims claim LEFT JOIN booking_attempts attempt ON attempt.salon_id=claim.salon_id AND attempt.id=claim.booking_attempt_id WHERE claim.salon_id=ANY($1) AND attempt.id IS NULL`},
		{&evidence.OrphanIntervals, `SELECT count(*) FROM external_slot_claim_intervals interval_row LEFT JOIN external_slot_claims claim ON claim.salon_id=interval_row.salon_id AND claim.id=interval_row.claim_id WHERE interval_row.salon_id=ANY($1) AND claim.id IS NULL`},
		{&evidence.OrphanEvents, `SELECT count(*) FROM external_slot_claim_events event_row LEFT JOIN external_slot_claims claim ON claim.salon_id=event_row.salon_id AND claim.id=event_row.claim_id WHERE event_row.salon_id=ANY($1) AND claim.id IS NULL`},
		{&evidence.UnknownClaims, `SELECT count(*) FROM external_slot_claims WHERE salon_id=ANY($1) AND state IN ('dispatched_unknown','reconciliation_required') AND released_at IS NULL`},
		{&evidence.ReconciliationRequired, `SELECT count(*) FROM booking_attempts WHERE salon_id=ANY($1) AND reconciliation_status='required' AND superseded_at IS NULL`},
	}
	for _, item := range queries {
		if err := db.QueryRowContext(ctx, item.query, pq.Array(salonIDs)).Scan(item.target); err != nil {
			return ExternalSlotCommitEvidence{}, err
		}
	}
	if evidence.FakeProviderDispatches > evidence.ExpectedFakeProviderDispatches {
		evidence.ConflictLoserProviderDispatches = evidence.FakeProviderDispatches - evidence.ExpectedFakeProviderDispatches
	}
	return evidence, nil
}

func summarizeOperationLatencies(samples []operationSample) LatencySummary {
	values := make([]time.Duration, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.latency)
	}
	return summarizeLatencies(values)
}
