package scheduling_authority_switch

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/scheduling"
)

func TestPreviewExactReplayPrecedesMutableReadinessAndChangedReuseConflicts(t *testing.T) {
	req := validPreviewRequestFixture(TargetOwnerManual, TargetManleAICalendar)
	fingerprint, err := previewFingerprint(req)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	existing := switchRunFixture(req)
	existing.payloadFingerprint = fingerprint
	store := &fakeStore{existing: existing, currentErr: errors.New("current authority must not be loaded on replay")}
	service := NewService(store, &fakeTargetReadiness{err: errors.New("calendar readiness must not run on replay")}, nil, true)

	response, err := service.Preview(context.Background(), "salon-1", "owner-1", req)
	if err != nil || response == nil || !response.Replayed || response.SwitchRun.ID != existing.ID {
		t.Fatalf("exact replay response=%#v err=%v", response, err)
	}
	changed := req
	changed.TargetSchedulingAuthority = TargetExternalProvider
	if _, err := service.Preview(context.Background(), "salon-1", "owner-1", changed); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("changed operation reuse error=%v, want ErrOperationConflict", err)
	}
}

func TestPreviewRejectsStaleSourceFenceBeforeReadiness(t *testing.T) {
	req := validPreviewRequestFixture(TargetOwnerManual, TargetManleAICalendar)
	store := &fakeStore{current: authorityState{Authority: TargetOwnerManual, Version: 9}}
	calendar := &fakeTargetReadiness{err: errors.New("readiness must not run for stale fence")}
	service := NewService(store, calendar, nil, true)

	if _, err := service.Preview(context.Background(), "salon-1", "owner-1", req); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale fence error=%v, want ErrVersionConflict", err)
	}
	if calendar.calls != 0 || store.createCalls != 0 {
		t.Fatalf("stale preview evaluated readiness or persisted: calendar=%d create=%d", calendar.calls, store.createCalls)
	}
}

func TestPreviewRejectsSourceThatDoesNotMatchCurrentAuthority(t *testing.T) {
	req := validPreviewRequestFixture(TargetExternalProvider, TargetManleAICalendar)
	store := &fakeStore{current: authorityState{Authority: TargetOwnerManual, Version: req.ExpectedSourceAuthorityVersion}}
	calendar := &fakeTargetReadiness{err: errors.New("readiness must not run for mismatched source")}
	service := NewService(store, calendar, nil, true)
	if _, err := service.Preview(context.Background(), "salon-1", "owner-1", req); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("source mismatch error=%v, want ErrVersionConflict", err)
	}
	if calendar.calls != 0 || store.createCalls != 0 {
		t.Fatalf("source mismatch evaluated readiness or persisted: calendar=%d create=%d", calendar.calls, store.createCalls)
	}
}

func TestPreviewOwnerManualUsesRegisteredExecutorAndCanonicalEligibleServices(t *testing.T) {
	req := validPreviewRequestFixture(TargetExternalProvider, TargetOwnerManual)
	store := &fakeStore{current: authorityState{Authority: TargetExternalProvider, Version: 4}, eligibleServices: 2}
	service := NewService(store, nil, nil, true)

	response, err := service.Preview(context.Background(), "salon-1", "owner-1", req)
	if err != nil {
		t.Fatalf("preview owner_manual: %v", err)
	}
	if response.SwitchRun.Status != StatusPreviewReady || !response.SwitchRun.ReadinessSnapshot.Ready || response.SwitchRun.ReadinessSnapshot.EligibleServiceCount != 2 {
		t.Fatalf("owner_manual preview=%#v", response.SwitchRun)
	}

	blockedStore := &fakeStore{current: authorityState{Authority: TargetExternalProvider, Version: 4}}
	blockedService := NewService(blockedStore, nil, nil, false)
	blocked, err := blockedService.Preview(context.Background(), "salon-1", "owner-1", req)
	if err != nil {
		t.Fatalf("blocked owner_manual preview: %v", err)
	}
	if blocked.SwitchRun.Status != StatusPreviewBlocked || len(blocked.SwitchRun.Blockers) != 2 {
		t.Fatalf("blocked owner_manual preview=%#v", blocked.SwitchRun)
	}
}

func TestPreviewOwnerManualRejectsConfirmedBookingMode(t *testing.T) {
	req := validPreviewRequestFixture(TargetExternalProvider, TargetOwnerManual)
	store := &fakeStore{
		current:          authorityState{Authority: TargetExternalProvider, Version: 4},
		eligibleServices: 1,
		bookingMode:      "confirmed_booking",
	}
	response, err := NewService(store, nil, nil, true).Preview(context.Background(), "salon-1", "owner-1", req)
	if err != nil {
		t.Fatalf("preview incompatible owner mode: %v", err)
	}
	if response.SwitchRun.Status != StatusPreviewBlocked || len(response.SwitchRun.Blockers) != 1 || response.SwitchRun.Blockers[0].Code != "BOOKING_MODE_COMPATIBLE" {
		t.Fatalf("incompatible owner mode preview=%#v", response.SwitchRun)
	}
}

func TestPreviewManleAICalendarUsesStaffOnlyCapabilitiesNotAggregateExecutionReady(t *testing.T) {
	req := validPreviewRequestFixture(TargetOwnerManual, TargetManleAICalendar)
	store := &fakeStore{current: authorityState{Authority: TargetOwnerManual, Version: 4}}
	calendar := &fakeTargetReadiness{readiness: scheduling.TargetReadiness{
		TargetSchedulingAuthority: TargetManleAICalendar, Ready: true, ConfigVersion: 12,
		Checks: []scheduling.TargetReadinessCheck{
			{Code: "CONFIGURATION_READY", Ready: true},
			{Code: "STAFF_ONLY_AVAILABILITY_READY", Ready: true},
			{Code: "STAFF_ONLY_CREATE_READY", Ready: true},
		},
	}}
	service := NewService(store, calendar, nil, true)

	response, err := service.Preview(context.Background(), "salon-1", "owner-1", req)
	if err != nil {
		t.Fatalf("preview manleai_calendar: %v", err)
	}
	if response.SwitchRun.Status != StatusPreviewReady || !response.SwitchRun.ReadinessSnapshot.Ready || response.SwitchRun.ReadinessSnapshot.ConfigVersion != 12 {
		t.Fatalf("manleai_calendar preview=%#v", response.SwitchRun)
	}
}

func TestPreviewExternalProviderPersistsOnlySanitizedChecks(t *testing.T) {
	req := validPreviewRequestFixture(TargetOwnerManual, TargetExternalProvider)
	store := &fakeStore{current: authorityState{Authority: TargetOwnerManual, Version: 4}}
	external := &fakeTargetReadiness{readiness: scheduling.TargetReadiness{
		TargetSchedulingAuthority: TargetExternalProvider, Ready: true, ServiceCount: 3, StaffCount: 2, BusinessHourPeriodCount: 7,
		Checks: []scheduling.TargetReadinessCheck{{Code: "EXTERNAL_PROVIDER_CONNECT_SQUARE", Ready: true}},
	}}
	service := NewService(store, nil, external, true)

	response, err := service.Preview(context.Background(), "salon-1", "owner-1", req)
	if err != nil {
		t.Fatalf("preview external_provider: %v", err)
	}
	if response.SwitchRun.Status != StatusPreviewReady || len(response.SwitchRun.ReadinessSnapshot.Checks) != 1 || response.SwitchRun.ReadinessSnapshot.Checks[0].Code != "EXTERNAL_PROVIDER_CONNECT_SQUARE" {
		t.Fatalf("external preview=%#v", response.SwitchRun)
	}
	if len(response.SwitchRun.Blockers) != 0 || response.SwitchRun.ReadinessSnapshot.ServiceCount != 3 {
		t.Fatalf("external sanitized snapshot=%#v blockers=%#v", response.SwitchRun.ReadinessSnapshot, response.SwitchRun.Blockers)
	}
}

func TestPreviewPropagatesSanitizedCalendarAndExternalBlockers(t *testing.T) {
	tests := []struct {
		name   string
		target string
		code   string
	}{
		{name: "calendar", target: TargetManleAICalendar, code: "STAFF_ONLY_CREATE_READY"},
		{name: "external", target: TargetExternalProvider, code: "EXTERNAL_PROVIDER_SELECT_LOCATION"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validPreviewRequestFixture(TargetOwnerManual, test.target)
			store := &fakeStore{current: authorityState{Authority: TargetOwnerManual, Version: 4}}
			evaluator := &fakeTargetReadiness{readiness: scheduling.TargetReadiness{
				TargetSchedulingAuthority: test.target,
				Checks:                    []scheduling.TargetReadinessCheck{{Code: test.code, Ready: false}},
				Blockers:                  []scheduling.TargetReadinessBlocker{{Code: test.code, Message: "Sanitized readiness blocker."}},
			}}
			var calendar, external targetReadiness
			if test.target == TargetManleAICalendar {
				calendar = evaluator
			} else {
				external = evaluator
			}
			response, err := NewService(store, calendar, external, true).Preview(context.Background(), "salon-1", "owner-1", request)
			if err != nil {
				t.Fatalf("blocked preview: %v", err)
			}
			if response.SwitchRun.Status != StatusPreviewBlocked || response.SwitchRun.ReadinessSnapshot.Ready || len(response.SwitchRun.Blockers) != 1 || response.SwitchRun.Blockers[0].Code != test.code {
				t.Fatalf("blocked preview=%#v", response.SwitchRun)
			}
		})
	}
}

func TestPreviewValidatesKnownDifferentAuthoritiesAndStableFence(t *testing.T) {
	service := NewService(&fakeStore{}, nil, nil, true)
	tests := []PreviewRequest{
		{},
		validPreviewRequestFixture(TargetOwnerManual, TargetOwnerManual),
		validPreviewRequestFixture("future_authority", TargetOwnerManual),
		validPreviewRequestFixture(TargetOwnerManual, "future_authority"),
	}
	invalidVersion := validPreviewRequestFixture(TargetOwnerManual, TargetExternalProvider)
	invalidVersion.ExpectedSourceAuthorityVersion = 0
	tests = append(tests, invalidVersion)
	for _, req := range tests {
		if _, err := service.Preview(context.Background(), "salon-1", "owner-1", req); !errors.Is(err, ErrValidation) {
			t.Fatalf("request %#v error=%v, want ErrValidation", req, err)
		}
	}
	boundary := validPreviewRequestFixture(TargetOwnerManual, TargetExternalProvider)
	boundary.OperationKey = strings.Repeat("k", maxOperationKeyBytes)
	if !validPreviewRequest("salon-1", "owner-1", boundary) {
		t.Fatal("256-byte operation key must be accepted")
	}
	boundary.OperationKey += "x"
	if validPreviewRequest("salon-1", "owner-1", boundary) {
		t.Fatal("257-byte operation key must be rejected")
	}
}

func TestCommitExactReplayPrecedesReadinessDrift(t *testing.T) {
	run := switchRunFixture(validPreviewRequestFixture(TargetExternalProvider, TargetOwnerManual))
	run.Status = "committed"
	store := &fakeStore{commitReplay: run, commitReplayFound: true}
	response, err := NewService(store, nil, nil, true).Commit(context.Background(), "salon-1", "owner-1", run.ID, CommitRequest{ActionKey: "commit-1"})
	if err != nil || response == nil || !response.Replayed || response.SwitchRun.ID != run.ID {
		t.Fatalf("commit replay=%#v err=%v", response, err)
	}
}

func TestCommitRejectsReadinessDriftBeforePersistence(t *testing.T) {
	run := switchRunFixture(validPreviewRequestFixture(TargetExternalProvider, TargetOwnerManual))
	run.ReadinessSnapshot = scheduling.TargetReadiness{TargetSchedulingAuthority: TargetOwnerManual, AuthorityVersion: 4, Ready: true, EligibleServiceCount: 2, Checks: []scheduling.TargetReadinessCheck{{Code: "OWNER_MANUAL_EXECUTOR_REGISTERED", Ready: true, Scope: "executor"}, {Code: "ELIGIBLE_SERVICE_REQUIRED", Ready: true, Scope: "service"}}}
	store := &fakeStore{existing: run, eligibleServices: 1}
	if _, err := NewService(store, nil, nil, true).Commit(context.Background(), "salon-1", "owner-1", run.ID, CommitRequest{ActionKey: "commit-drift"}); !errors.Is(err, ErrReadinessConflict) {
		t.Fatalf("readiness drift error=%v, want ErrReadinessConflict", err)
	}
	if store.commitCalls != 0 {
		t.Fatalf("commit store called %d times", store.commitCalls)
	}
}

func TestCommitUsesRegisteredExternalAdapterOpaqueTransactionalProof(t *testing.T) {
	proof := strings.Repeat("b", 64)
	readiness := scheduling.TargetReadiness{
		TargetSchedulingAuthority:    TargetExternalProvider,
		AuthorityVersion:             4,
		Ready:                        true,
		ReadinessEvidenceVersion:     7,
		ReadinessEvidenceFingerprint: proof,
		Checks:                       []scheduling.TargetReadinessCheck{{Code: "OTHER_PROVIDER_READY", Ready: true, Scope: "provider"}},
	}
	run := switchRunFixture(validPreviewRequestFixture(TargetOwnerManual, TargetExternalProvider))
	run.ReadinessSnapshot = readiness
	store := &fakeStore{existing: run}
	adapter := &fakeTargetReadiness{readiness: readiness, txReadiness: readiness}

	response, err := NewService(store, nil, adapter, true).Commit(context.Background(), "salon-1", "owner-1", run.ID, CommitRequest{ActionKey: "commit-other-provider"})
	if err != nil || response == nil || response.SwitchRun.Status != "committed" {
		t.Fatalf("opaque adapter commit = %#v/%v", response, err)
	}
	if adapter.calls != 1 || adapter.txCalls != 1 || store.commitCalls != 1 {
		t.Fatalf("adapter/store calls = preflight:%d tx:%d commit:%d", adapter.calls, adapter.txCalls, store.commitCalls)
	}
}

func TestCommitRejectsChangedExternalAdapterTransactionalProof(t *testing.T) {
	readiness := scheduling.TargetReadiness{
		TargetSchedulingAuthority:    TargetExternalProvider,
		AuthorityVersion:             4,
		Ready:                        true,
		ReadinessEvidenceVersion:     1,
		ReadinessEvidenceFingerprint: strings.Repeat("c", 64),
	}
	run := switchRunFixture(validPreviewRequestFixture(TargetOwnerManual, TargetExternalProvider))
	run.ReadinessSnapshot = readiness
	store := &fakeStore{existing: run}
	changed := readiness
	changed.ReadinessEvidenceFingerprint = strings.Repeat("d", 64)
	adapter := &fakeTargetReadiness{readiness: readiness, txReadiness: changed}

	_, err := NewService(store, nil, adapter, true).Commit(context.Background(), "salon-1", "owner-1", run.ID, CommitRequest{ActionKey: "commit-proof-drift"})
	if !errors.Is(err, ErrReadinessConflict) {
		t.Fatalf("changed transactional proof error = %v, want readiness conflict", err)
	}
	if adapter.txCalls != 1 || store.existing.Status == "committed" {
		t.Fatalf("changed proof tx calls/status = %d/%s", adapter.txCalls, store.existing.Status)
	}
}

type fakeStore struct {
	existing          *SwitchRun
	current           authorityState
	currentErr        error
	eligibleServices  int
	bookingMode       string
	createCalls       int
	commitReplay      *SwitchRun
	commitReplayFound bool
	commitCalls       int
}

func (f *fakeStore) FindByOperationKey(context.Context, string, string, string) (*SwitchRun, error) {
	if f.existing == nil {
		return nil, ErrNotFound
	}
	return f.existing, nil
}

func (f *fakeStore) CurrentAuthority(context.Context, string, string) (authorityState, error) {
	return f.current, f.currentErr
}

func (f *fakeStore) EligibleServiceCount(context.Context, string, string) (int, error) {
	return f.eligibleServices, nil
}

func (f *fakeStore) BookingMode(context.Context, string, string) (string, error) {
	if f.bookingMode == "" {
		return "pending_approval", nil
	}
	return f.bookingMode, nil
}

func (f *fakeStore) CreateOrReplayPreview(_ context.Context, input persistPreviewInput) (*SwitchRun, bool, error) {
	f.createCalls++
	now := time.Now().UTC()
	run := &SwitchRun{
		ID: "run-1", SalonID: input.SalonID,
		SourceSchedulingAuthority: input.SourceSchedulingAuthority, TargetSchedulingAuthority: input.TargetSchedulingAuthority,
		ExpectedSourceAuthorityVersion: input.ExpectedSourceAuthorityVersion,
		OperationKey:                   input.OperationKey, ActorUserID: input.OwnerUserID, ReadinessSnapshot: input.ReadinessSnapshot, Blockers: input.Blockers,
		Status: input.Status, PreviewedAt: now, CreatedAt: now, UpdatedAt: now, payloadFingerprint: input.PayloadFingerprint,
	}
	return run, false, nil
}

func (f *fakeStore) Latest(context.Context, string, string) (*SwitchRun, error) {
	if f.existing == nil {
		return nil, ErrNotFound
	}
	return f.existing, nil
}

func (f *fakeStore) Get(context.Context, string, string, string) (*SwitchRun, error) {
	if f.existing == nil {
		return nil, ErrNotFound
	}
	return f.existing, nil
}

func (f *fakeStore) ReplayCommit(context.Context, string, string, string, string, string) (*SwitchRun, bool, error) {
	return f.commitReplay, f.commitReplayFound, nil
}

func (f *fakeStore) Commit(ctx context.Context, input commitInput) (*SwitchRun, bool, error) {
	f.commitCalls++
	if f.existing == nil {
		return nil, false, ErrNotFound
	}
	if input.ValidateTargetReadiness != nil {
		if err := input.ValidateTargetReadiness(ctx, nil); err != nil {
			return nil, false, err
		}
	}
	f.existing.Status = "committed"
	now := time.Now().UTC()
	f.existing.CommittedAt = &now
	return f.existing, false, nil
}

type fakeTargetReadiness struct {
	readiness   scheduling.TargetReadiness
	txReadiness scheduling.TargetReadiness
	err         error
	txErr       error
	calls       int
	txCalls     int
}

func (f *fakeTargetReadiness) SchedulingTargetReadinessTx(context.Context, *sql.Tx, string, string) (scheduling.TargetReadiness, error) {
	f.txCalls++
	if f.txReadiness.TargetSchedulingAuthority == "" {
		return f.readiness, f.txErr
	}
	return f.txReadiness, f.txErr
}

func (f *fakeTargetReadiness) SchedulingTargetReadiness(context.Context, string, string) (scheduling.TargetReadiness, error) {
	f.calls++
	return f.readiness, f.err
}

func validPreviewRequestFixture(source string, target string) PreviewRequest {
	return PreviewRequest{
		OperationKey: "switch-preview-1", SourceSchedulingAuthority: source,
		TargetSchedulingAuthority: target, ExpectedSourceAuthorityVersion: 4,
	}
}

func switchRunFixture(req PreviewRequest) *SwitchRun {
	now := time.Now().UTC()
	return &SwitchRun{
		ID: "run-existing", SalonID: "salon-1",
		SourceSchedulingAuthority: req.SourceSchedulingAuthority, TargetSchedulingAuthority: req.TargetSchedulingAuthority,
		ExpectedSourceAuthorityVersion: req.ExpectedSourceAuthorityVersion, OperationKey: req.OperationKey,
		ReadinessSnapshot: ReadinessSnapshot{TargetSchedulingAuthority: req.TargetSchedulingAuthority, Ready: true, Checks: []ReadinessCheck{}},
		Blockers:          []ReadinessBlocker{}, Status: StatusPreviewReady, PreviewedAt: now, CreatedAt: now, UpdatedAt: now,
	}
}
