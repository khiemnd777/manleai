package tenant_registration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
)

type registrationStoreStub struct {
	submits           int
	last              PublicSubmissionRequest
	submitFingerprint string
	mutation          MutationRequest
	mutationFP        string
	note              AddNoteRequest
	noteFP            string
	redactionLimit    int
	detailStatus      Status
}

func (s *registrationStoreStub) Submit(_ context.Context, _, reference, fingerprint string, req PublicSubmissionRequest) (*PublicSubmissionResponse, error) {
	s.submits++
	s.last = req
	s.submitFingerprint = fingerprint
	return &PublicSubmissionResponse{Status: "received", RequestReference: reference}, nil
}
func (*registrationStoreStub) List(context.Context, ListFilter) ([]ListItem, map[Status]int64, bool, error) {
	return nil, nil, false, nil
}
func (s *registrationStoreStub) Get(context.Context, string) (*Detail, error) {
	status := s.detailStatus
	if status == "" {
		status = StatusNew
	}
	return &Detail{ListItem: ListItem{Status: status}}, nil
}
func (s *registrationStoreStub) Mutate(_ context.Context, _, _, fingerprint string, req MutationRequest) (*MutationResult, error) {
	s.mutation = req
	s.mutationFP = fingerprint
	return &MutationResult{}, nil
}
func (s *registrationStoreStub) AddNote(_ context.Context, _, _, fingerprint string, req AddNoteRequest) (*AddNoteResult, error) {
	s.note = req
	s.noteFP = fingerprint
	return &AddNoteResult{}, nil
}
func (s *registrationStoreStub) RedactDue(_ context.Context, limit int) (int, error) {
	s.redactionLimit = limit
	return limit, nil
}

type registrationAuthorizer struct{ allowed access.Capability }

func (a registrationAuthorizer) Authorize(_ context.Context, _ middleware.ActorContext, check access.AccessCheck) error {
	if check.Capability != a.allowed {
		return errors.New("denied")
	}
	return nil
}

func validSubmission() PublicSubmissionRequest {
	return PublicSubmissionRequest{SubmissionKey: "249705ef-25f0-4b5c-9247-883f10e504ca", ContactFullName: "Linh Nguyen", ContactEmail: "OWNER@EXAMPLE.COM", ContactPhone: "(312) 555-0148", SalonName: "Lakeview Nails", SalonPhone: "773-555-0180", City: "Chicago", State: "il", ZipCode: "60614", SalonWebsite: "https://lakeview.example", LocationCount: 1, PreferredContactLanguage: "vi", CurrentBookingSystem: "Manual calendar", EstimatedWeeklyCallVolume: "80", RequestedHelp: "Help answer calls and capture appointment requests.", Notes: "Please contact after 3 PM.", Locale: "vi", SourcePage: "pricing", MarketingPlanInterest: "growth", ConsentVersion: ConsentVersion, ContactConsent: true}
}

func TestSubmitNormalizesDynamicIntakeWithoutProviderInterpretation(t *testing.T) {
	store := &registrationStoreStub{}
	result, err := NewService(store, nil).Submit(context.Background(), validSubmission())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "received" || store.submits != 1 {
		t.Fatalf("unexpected result %#v submits=%d", result, store.submits)
	}
	if store.last.ContactEmail != "owner@example.com" || store.last.ContactPhoneNormalized != "+13125550148" || store.last.SalonPhoneNormalized != "+17735550180" || store.last.State != "IL" {
		t.Fatalf("normalization failed: %#v", store.last)
	}
	if store.last.CurrentBookingSystem != "Manual calendar" {
		t.Fatal("intake evidence should be preserved as text")
	}
}

func TestSubmitHoneypotReturnsGenericReceiptWithoutPersistence(t *testing.T) {
	store := &registrationStoreStub{}
	req := validSubmission()
	req.WebsiteConfirmation = "https://bot.invalid"
	result, err := NewService(store, nil).Submit(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "received" || result.RequestReference == "" || store.submits != 0 {
		t.Fatalf("honeypot leaked behavior or persisted: %#v submits=%d", result, store.submits)
	}
}

func TestSubmitRejectsChangedDataShapesAndMissingConsent(t *testing.T) {
	service := NewService(&registrationStoreStub{}, nil)
	cases := []PublicSubmissionRequest{validSubmission(), validSubmission(), validSubmission()}
	cases[0].ContactConsent = false
	cases[1].State = "ZZ"
	cases[2].ContactPhone = "555"
	for index, req := range cases {
		if _, err := service.Submit(context.Background(), req); !errors.Is(err, ErrValidation) {
			t.Fatalf("case %d expected validation, got %v", index, err)
		}
	}
}

func TestSubmitRejectsUnknownOrUnboundedPublicFields(t *testing.T) {
	service := NewService(&registrationStoreStub{}, nil)
	tests := []struct {
		name   string
		mutate func(*PublicSubmissionRequest)
	}{
		{"submission key", func(req *PublicSubmissionRequest) { req.SubmissionKey = "not-a-uuid" }},
		{"consent version", func(req *PublicSubmissionRequest) { req.ConsentVersion = "future-copy" }},
		{"locale", func(req *PublicSubmissionRequest) { req.Locale = "fr" }},
		{"source page", func(req *PublicSubmissionRequest) { req.SourcePage = "campaign" }},
		{"plan", func(req *PublicSubmissionRequest) { req.MarketingPlanInterest = "enterprise" }},
		{"website credentials", func(req *PublicSubmissionRequest) { req.SalonWebsite = "https://user:secret@example.test" }},
		{"zip", func(req *PublicSubmissionRequest) { req.ZipCode = "6061" }},
		{"locations", func(req *PublicSubmissionRequest) { req.LocationCount = 101 }},
		{"notes", func(req *PublicSubmissionRequest) { req.Notes = strings.Repeat("x", 4001) }},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			req := validSubmission()
			item.mutate(&req)
			if _, err := service.Submit(context.Background(), req); !errors.Is(err, ErrValidation) {
				t.Fatalf("expected validation, got %v", err)
			}
		})
	}
}

func TestSubmissionFingerprintIsCanonicalAndDoesNotInterpretBookingText(t *testing.T) {
	firstStore, secondStore := &registrationStoreStub{}, &registrationStoreStub{}
	first := validSubmission()
	second := validSubmission()
	second.ContactEmail = " owner@example.com "
	second.State = " IL "
	if _, err := NewService(firstStore, nil).Submit(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(secondStore, nil).Submit(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if firstStore.submitFingerprint != secondStore.submitFingerprint || len(firstStore.submitFingerprint) != 64 {
		t.Fatalf("canonical fingerprints differ: %q / %q", firstStore.submitFingerprint, secondStore.submitFingerprint)
	}
	if firstStore.last.CurrentBookingSystem != "Manual calendar" {
		t.Fatal("booking-system evidence was reinterpreted")
	}
}

func TestReviewAuthorizationUsesPlatformCapabilityAndTransitions(t *testing.T) {
	store := &registrationStoreStub{}
	actor := middleware.ActorContext{UserID: "platform-user", PrincipalScope: middleware.PrincipalScopePlatform}
	service := NewService(store, registrationAuthorizer{allowed: access.CapabilityRegistrationRead})
	result, err := service.List(context.Background(), actor, ListFilter{AssignedTo: "me"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Limit != 25 {
		t.Fatalf("default limit=%d", result.Limit)
	}
	if !CanTransition(StatusInReview, StatusQualified) || CanTransition(StatusConverted, StatusQualified) {
		t.Fatal("status transition contract changed")
	}
	tenantActor := actor
	tenantActor.PrincipalScope = middleware.PrincipalScopeTenant
	if _, err := service.List(context.Background(), tenantActor, ListFilter{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("tenant actor should fail, got %v", err)
	}
}

func TestStatusContractIsCompleteAndTerminalStatusesCannotReopen(t *testing.T) {
	want := map[Status][]Status{
		StatusNew:             {StatusInReview, StatusQualified, StatusDeclined, StatusSpam},
		StatusInReview:        {StatusQualified, StatusDeclined, StatusSpam},
		StatusQualified:       {StatusSetupInProgress, StatusDeclined},
		StatusSetupInProgress: {StatusQualified, StatusConverted},
		StatusConverted:       {}, StatusDeclined: {}, StatusSpam: {},
	}
	for status, transitions := range want {
		got := AllowedTransitions(status)
		if len(got) != len(transitions) {
			t.Fatalf("%s transitions=%v, want %v", status, got, transitions)
		}
		for index := range transitions {
			if got[index] != transitions[index] {
				t.Fatalf("%s transitions=%v, want %v", status, got, transitions)
			}
		}
	}
}

func TestReviewMutationAndNoteValidationProduceStableFingerprints(t *testing.T) {
	store := &registrationStoreStub{}
	service := NewService(store, registrationAuthorizer{allowed: access.CapabilityRegistrationManage})
	actor := middleware.ActorContext{UserID: "b3709746-412e-4e1d-abec-526028298eb9", PrincipalScope: middleware.PrincipalScopePlatform}
	requestID := "f4c52425-1b39-431b-9f72-abf7c32dfd88"
	status := StatusInReview
	request := MutationRequest{ActionKey: "review-1", ExpectedVersion: 1, Status: &status}
	if _, err := service.Mutate(context.Background(), actor, requestID, request); err != nil {
		t.Fatal(err)
	}
	firstFingerprint := store.mutationFP
	if _, err := service.Mutate(context.Background(), actor, requestID, request); err != nil {
		t.Fatal(err)
	}
	if firstFingerprint == "" || firstFingerprint != store.mutationFP {
		t.Fatalf("mutation fingerprint changed: %q / %q", firstFingerprint, store.mutationFP)
	}
	if _, err := service.AddNote(context.Background(), actor, requestID, AddNoteRequest{ActionKey: "note-1", ExpectedVersion: 1, Content: "  verified callback window  "}); err != nil {
		t.Fatal(err)
	}
	if store.note.Content != "verified callback window" || len(store.noteFP) != 64 {
		t.Fatalf("note not normalized/fingerprinted: %#v %q", store.note, store.noteFP)
	}
	for _, invalid := range []MutationRequest{{ActionKey: "", ExpectedVersion: 1, Status: &status}, {ActionKey: "x", ExpectedVersion: 0, Status: &status}, {ActionKey: "x", ExpectedVersion: 1}} {
		if _, err := service.Mutate(context.Background(), actor, requestID, invalid); !errors.Is(err, ErrValidation) {
			t.Fatalf("invalid mutation %#v error=%v", invalid, err)
		}
	}
}

func TestRedactionBatchIsBounded(t *testing.T) {
	store := &registrationStoreStub{}
	service := NewService(store, nil)
	for _, limit := range []int{0, 501} {
		if _, err := service.RedactDue(context.Background(), limit); !errors.Is(err, ErrValidation) {
			t.Fatalf("limit %d error=%v", limit, err)
		}
	}
	if count, err := service.RedactDue(context.Background(), 100); err != nil || count != 100 || store.redactionLimit != 100 {
		t.Fatalf("redact count=%d limit=%d err=%v", count, store.redactionLimit, err)
	}
}

func TestListMaskHelpersNeverReturnRawContactValues(t *testing.T) {
	if got := maskEmail("owner@example.com"); got == "owner@example.com" || got != "o••••@example.com" {
		t.Fatalf("masked email=%q", got)
	}
	if got := maskPhone("+1 (312) 555-0148"); got == "+1 (312) 555-0148" || got != "•••-•••-0148" {
		t.Fatalf("masked phone=%q", got)
	}
}

func TestOpsCanPrepareValidatedProvisioningDraft(t *testing.T) {
	store := &registrationStoreStub{}
	service := NewService(store, registrationAuthorizer{allowed: access.CapabilityRegistrationManage})
	actor := middleware.ActorContext{UserID: "b3709746-412e-4e1d-abec-526028298eb9", PrincipalScope: middleware.PrincipalScopePlatform}
	draft := ProvisioningDraft{OwnerEmail: "OWNER@EXAMPLE.COM", OwnerFullName: "  Mai Tran ", OwnerPhone: "312-555-0148", SalonName: " Prepared Nails ", SalonPhone: "773-555-0180", City: "Chicago", State: "il", ZipCode: "60614", Timezone: "America/Chicago", PrimaryLanguage: "vi", SecondaryLanguage: "en", HandoffPhone: "773-555-0180"}
	_, err := service.Mutate(context.Background(), actor, "f4c52425-1b39-431b-9f72-abf7c32dfd88", MutationRequest{ActionKey: "draft-1", ExpectedVersion: 2, ProvisioningDraft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	if store.mutation.ProvisioningDraft == nil || store.mutation.ProvisioningDraft.OwnerEmail != "owner@example.com" || store.mutation.ProvisioningDraft.State != "IL" || store.mutation.ProvisioningDraft.SalonName != "Prepared Nails" {
		t.Fatalf("draft not normalized: %#v", store.mutation.ProvisioningDraft)
	}
}
