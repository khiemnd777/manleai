package conversation

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

type providerFailureTurnInterpreter struct {
	calls   int
	outcome string
}

func (i *providerFailureTurnInterpreter) InterpretTurn(ctx context.Context, req TurnInterpretationRequest) (TurnUnderstanding, error) {
	i.calls++
	outcome := i.outcome
	if outcome == "" {
		outcome = TurnInterpreterOutcomeProviderError
	}
	return TurnUnderstanding{}, NewTurnInterpreterErrorWithDiagnostics(outcome, errors.New("provider unavailable"), map[string]string{
		"provider": "openai", "failure_stage": "turn_interpretation_response", "http_status": "503",
		"http_status_class": "5xx", "request_id": "req_safe_123",
	})
}

type staticGuidanceTurnInterpreter struct {
	turn    TurnUnderstanding
	request TurnInterpretationRequest
}

func (i *staticGuidanceTurnInterpreter) InterpretTurn(ctx context.Context, req TurnInterpretationRequest) (TurnUnderstanding, error) {
	i.request = req
	return i.turn, nil
}

func TestGuidanceRecoveryReducerSeparatesProviderFailureFromCallerNoProgress(t *testing.T) {
	current := &GuidanceRecoveryState{
		Stage: GuidanceRecoveryStageService, OfferedActions: []string{guidanceRecoveryActionCatalog},
		NoProgressCount: 2, ProviderFailureCount: 0,
	}
	provider := reduceGuidanceRecoveryState(current, GuidanceRecoveryStageService, current.OfferedActions, guidanceRecoveryEvidence{
		Kind: guidanceEvidenceProviderFailure, ProviderOutcome: TurnInterpreterOutcomeTimeout,
	})
	if provider.State.NoProgressCount != 2 || provider.State.ProviderFailureCount != 1 || provider.HandoffReason != "" {
		t.Fatalf("provider transition = %#v", provider)
	}
	if current.ProviderFailureCount != 0 {
		t.Fatalf("reducer mutated input = %#v", current)
	}

	progress := reduceGuidanceRecoveryState(provider.State, GuidanceRecoveryStageService, current.OfferedActions, guidanceRecoveryEvidence{
		Kind: guidanceEvidenceCatalogMenu, ProviderOutcome: "skipped_answer_lane",
	})
	if progress.State.NoProgressCount != 0 || progress.State.ProviderFailureCount != 0 || progress.State.Stage != GuidanceRecoveryStageService {
		t.Fatalf("progress transition = %#v", progress)
	}

	caller := reduceGuidanceRecoveryState(current, GuidanceRecoveryStageService, current.OfferedActions, guidanceRecoveryEvidence{
		Kind: guidanceEvidenceCallerNoProgress, ProviderOutcome: TurnInterpreterOutcomeLowConfidence,
	})
	if caller.State.NoProgressCount != 3 || caller.State.ProviderFailureCount != 0 || caller.HandoffReason != HandoffReasonServiceClarification {
		t.Fatalf("caller transition = %#v", caller)
	}

	providerAtLimit := *current
	providerAtLimit.ProviderFailureCount = 1
	providerHandoff := reduceGuidanceRecoveryState(&providerAtLimit, GuidanceRecoveryStageService, current.OfferedActions, guidanceRecoveryEvidence{
		Kind: guidanceEvidenceProviderFailure, ProviderOutcome: TurnInterpreterOutcomeProviderError,
	})
	if providerHandoff.State.NoProgressCount != 2 || providerHandoff.State.ProviderFailureCount != 2 || providerHandoff.HandoffReason != HandoffReasonGuidanceProviderUnavailable {
		t.Fatalf("provider handoff transition = %#v", providerHandoff)
	}
}

func TestGuidanceRecoveryOfferedActionsComeFromRuntimeCatalogAndReadyProfiles(t *testing.T) {
	services := []ServiceOption{{
		ID: "classic", Name: "Classic Manicure",
		ConsultationProfile: readyComparisonProfile(ConsultationOutcomeMaintain, ConsultationSystemNatural),
	}}
	disabled := guidanceRecoveryOfferedActions(GuidanceRecoveryStageService, services, &RuntimeConfig{ConsultationEnabled: false})
	if !containsString(disabled, guidanceRecoveryActionCatalog) || !containsString(disabled, guidanceRecoveryActionNameService) || !containsString(disabled, guidanceRecoveryActionHumanHandoff) || containsString(disabled, guidanceRecoveryActionConsultation) {
		t.Fatalf("disabled consultation actions = %#v", disabled)
	}
	enabled := guidanceRecoveryOfferedActions(GuidanceRecoveryStageService, services, &RuntimeConfig{ConsultationEnabled: true})
	if !containsString(enabled, guidanceRecoveryActionConsultation) {
		t.Fatalf("ready consultation action missing = %#v", enabled)
	}
	draftProfile := append([]ServiceOption(nil), services...)
	draftProfile[0].ConsultationProfile = &ServiceConsultationProfile{Status: "draft"}
	if actions := guidanceRecoveryOfferedActions(GuidanceRecoveryStageService, draftProfile, &RuntimeConfig{ConsultationEnabled: true}); containsString(actions, guidanceRecoveryActionConsultation) {
		t.Fatalf("draft profile exposed consultation action = %#v", actions)
	}
}

func TestGuidanceProviderFailurePromptOnlyOffersStateOwnedActions(t *testing.T) {
	tests := []struct {
		name      string
		state     GuidanceRecoveryState
		want      []string
		doNotWant []string
	}{
		{
			name: "catalog and consultation stay actionable",
			state: GuidanceRecoveryState{Stage: GuidanceRecoveryStageService, OfferedActions: []string{
				guidanceRecoveryActionCatalog, guidanceRecoveryActionNameService, guidanceRecoveryActionConsultation, guidanceRecoveryActionHumanHandoff,
			}},
			want: []string{"bookable service menu", "help choosing a service", "owner help"},
		},
		{
			name: "catalog without ready profiles does not invent consultation",
			state: GuidanceRecoveryState{Stage: GuidanceRecoveryStageService, OfferedActions: []string{
				guidanceRecoveryActionCatalog, guidanceRecoveryActionNameService, guidanceRecoveryActionHumanHandoff,
			}},
			want: []string{"bookable service menu", "name a service", "owner help"}, doNotWant: []string{"help choosing"},
		},
		{
			name:  "missing catalog never offers a menu",
			state: GuidanceRecoveryState{Stage: GuidanceRecoveryStageService, OfferedActions: []string{guidanceRecoveryActionHumanHandoff}},
			want:  []string{"owner help"}, doNotWant: []string{"service menu"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prompt := guidanceProviderFailurePrompt(test.state)
			if strings.Contains(prompt, "trouble understanding") || !strings.Contains(prompt, "trouble checking the salon's service guidance") {
				t.Fatalf("provider failure copy blamed caller understanding: %q", prompt)
			}
			for _, phrase := range test.want {
				if !strings.Contains(prompt, phrase) {
					t.Fatalf("prompt %q does not contain %q", prompt, phrase)
				}
			}
			for _, phrase := range test.doNotWant {
				if strings.Contains(prompt, phrase) {
					t.Fatalf("prompt %q unexpectedly contains %q", prompt, phrase)
				}
			}
		})
	}
}

func TestNormalizedDialogStatePromotesLegacyGuidanceWithoutSQLMigration(t *testing.T) {
	state := normalizedDialogState(DialogState{
		Version: DialogStateVersion - 1, Phase: DialogPhaseClarifying,
		LastPromptKey: legacyPromptServiceGuidanceRecovery, NoProgressCount: 2,
		ProviderFailureCount: 1, ProgressFingerprint: "legacy-fingerprint",
	})
	if state.Version != DialogStateVersion || state.Guidance == nil || state.Guidance.Stage != GuidanceRecoveryStageService {
		t.Fatalf("normalized legacy state = %#v", state)
	}
	if state.Guidance.NoProgressCount != 2 || state.Guidance.ProviderFailureCount != 1 || state.Guidance.ProgressFingerprint != "legacy-fingerprint" {
		t.Fatalf("promoted guidance counters = %#v", state.Guidance)
	}
	if state.LastPromptKey != "" || state.NoProgressCount != 0 || state.ProviderFailureCount != 0 || state.ProgressFingerprint != "" {
		t.Fatalf("legacy guidance owner was not cleared = %#v", state)
	}
}

func TestPhoneGuidanceRecoveryDoesNotExposeDisabledConsultation(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	store.cfg.ConsultationEnabled = false
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(&staticGuidanceTurnInterpreter{turn: TurnUnderstanding{
		Goal: "consultation", GuidanceAction: GuidanceActionConsultation, Confidence: 0.94, Reason: "caller_requests_service_guidance",
	}})

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Could you walk me through what might suit me?",
	})
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if session.Intent != IntentUnknown || consultationStateActive(session.DialogState.Consultation) || session.DialogState.Guidance == nil {
		t.Fatalf("disabled consultation state = intent=%q dialog=%#v", session.Intent, session.DialogState)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Personalized service guidance isn't available") || !strings.Contains(store.lastTurn.AIMessage, "bookable service menu") {
		t.Fatalf("disabled consultation reply = %q", store.lastTurn.AIMessage)
	}
	if store.lastTurn.CustomerMetadata["turn_guidance_action"] != GuidanceActionConsultation ||
		store.lastTurn.CustomerMetadata["service_guidance_capability"] != string(ServiceGuidanceCapabilityDisabled) {
		t.Fatalf("disabled consultation metadata = %#v", store.lastTurn.CustomerMetadata)
	}
	assertGuidanceDidNotCallBookingTools(t, bookingTool)
}

func TestGuidanceIntentSurvivesUnavailableCatalogAcrossPhoneAndSimulator(t *testing.T) {
	tests := []struct {
		name    string
		channel string
		message string
	}{
		{name: "phone uncertain choice", channel: ChannelPhone, message: "I don't know whether I should choose a service"},
		{name: "simulator uncertain choice", channel: ChannelSimulator, message: "I don't know whether I should choose a service"},
		{name: "phone broad nail need", channel: ChannelPhone, message: "I want a service for my nails"},
		{name: "simulator broad nail need", channel: ChannelSimulator, message: "I want a service for my nails"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeConversationStore()
			store.session.Channel = test.channel
			store.services = nil
			bookingTool := &fakeBookingTool{}
			interpreter := &staticGuidanceTurnInterpreter{turn: TurnUnderstanding{
				Goal: "consultation", GuidanceAction: GuidanceActionConsultation, Confidence: 0.94, Reason: "caller_requests_service_guidance",
			}}
			service := NewService(store, bookingTool)
			service.SetTurnInterpreter(interpreter)

			session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
				Message: test.message, EventKey: "capability_unavailable_" + test.channel,
			})
			if err != nil {
				t.Fatalf("Message: %v", err)
			}
			if session.Status != StatusActive || session.Intent != IntentUnknown || consultationStateActive(session.DialogState.Consultation) || session.DialogState.Guidance == nil {
				t.Fatalf("capability fallback state = status=%q intent=%q dialog=%#v", session.Status, session.Intent, session.DialogState)
			}
			if !containsString(interpreter.request.RecognizableGuidanceActions, GuidanceActionConsultation) ||
				len(interpreter.request.RecognizableGuidanceActions) != len(GuidanceActionValues()) {
				t.Fatalf("recognizable actions were capability-filtered: %#v", interpreter.request.RecognizableGuidanceActions)
			}
			if !strings.Contains(store.lastTurn.AIMessage, "I understand you'd like help choosing") ||
				!strings.Contains(store.lastTurn.AIMessage, "can't access the salon's service guide") ||
				strings.Contains(store.lastTurn.AIMessage, "trouble understanding") {
				t.Fatalf("capability fallback reply = %q", store.lastTurn.AIMessage)
			}
			if store.lastTurn.CustomerMetadata["turn_interpreter_outcome"] != TurnInterpreterOutcomeAccepted ||
				store.lastTurn.CustomerMetadata["service_guidance_capability"] != string(ServiceGuidanceCapabilityCatalogUnavailable) {
				t.Fatalf("capability fallback metadata = %#v", store.lastTurn.CustomerMetadata)
			}
			assertGuidanceDidNotCallBookingTools(t, bookingTool)
		})
	}
}

func TestGuidanceUsesCanonicalCatalogWhileBookingSnapshotIsNotReady(t *testing.T) {
	tests := []struct {
		name    string
		channel string
		message string
	}{
		{name: "reported simulator wording", channel: ChannelSimulator, message: "I don't know what service should I book for my nails"},
		{name: "different phone wording", channel: ChannelPhone, message: "Could you help me figure out which nail appointment fits me?"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeConversationStore()
			store.session.Channel = test.channel
			store.services = nil
			store.guidanceServices = []ServiceOption{
				{ID: "svc_natural", Name: "Natural Nail Care", CategoryID: "cat_hands", CategoryName: "Hand Care", ConsultationProfile: readyComparisonProfile(ConsultationOutcomeMaintain, ConsultationSystemNatural)},
				{ID: "svc_overlay", Name: "Structured Overlay", CategoryID: "cat_hands", CategoryName: "Hand Care", ConsultationProfile: readyComparisonProfile(ConsultationOutcomeAddStrength, ConsultationSystemGel)},
			}
			store.answerContextFence = AnswerContextFence{
				ActiveProvider: "square", ConnectionStatus: "connected", LocationID: "location_1",
				SnapshotGeneration: 1, Ready: false,
			}
			bookingTool := &fakeBookingTool{}
			service := NewService(store, bookingTool)
			service.SetTurnInterpreter(&staticGuidanceTurnInterpreter{turn: TurnUnderstanding{
				Goal: "consultation", GuidanceAction: GuidanceActionConsultation, Confidence: 0.95,
				Reason: "caller_requests_help_choosing_from_catalog",
			}})
			service.SetReplyGenerator(&fakeReplyGenerator{message: "What do you currently have on your nails?"})

			session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
				Message: test.message, EventKey: "stale_booking_snapshot_" + test.channel,
			})
			if err != nil {
				t.Fatalf("Message: %v", err)
			}
			if session.Intent != IntentConsultation || !consultationStateActive(session.DialogState.Consultation) {
				t.Fatalf("consultation state = intent=%q dialog=%#v", session.Intent, session.DialogState)
			}
			if store.lastTurn.AIMessage != "What do you currently have on your nails?" {
				t.Fatalf("guidance reply=%q metadata=%#v", store.lastTurn.AIMessage, store.lastTurn.CustomerMetadata)
			}
			assertGuidanceDidNotCallBookingTools(t, bookingTool)
		})
	}
}

func TestServiceMenuUsesCanonicalCatalogWhileBookingSnapshotIsNotReady(t *testing.T) {
	store := newFakeConversationStore()
	store.services = nil
	store.guidanceServices = []ServiceOption{
		{ID: "svc_hands", Name: "Signature Hand Care", CategoryID: "cat_hands", CategoryName: "Hand Care"},
		{ID: "svc_feet", Name: "Cloud Foot Reset", CategoryID: "cat_feet", CategoryName: "Foot Care"},
	}
	store.answerContextFence = AnswerContextFence{
		ActiveProvider: "square", ConnectionStatus: "connected", LocationID: "location_1",
		SnapshotGeneration: 1, Ready: false,
	}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(&staticGuidanceTurnInterpreter{turn: TurnUnderstanding{
		Goal: "information", GuidanceAction: GuidanceActionServiceCatalog, Confidence: 0.96,
		Reason: "caller_requested_service_catalog",
	}})

	if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Show me your services", EventKey: "stale_snapshot_service_menu",
	}); err != nil {
		t.Fatalf("Message: %v", err)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Signature Hand Care") ||
		!strings.Contains(store.lastTurn.AIMessage, "Cloud Foot Reset") ||
		store.lastTurn.AIMetadata["answer_source"] != answerSourceServiceCatalog {
		t.Fatalf("stale-snapshot service menu reply=%q metadata=%#v", store.lastTurn.AIMessage, store.lastTurn.AIMetadata)
	}
	assertGuidanceDidNotCallBookingTools(t, bookingTool)
}

func TestAvailabilityRejectsGuidanceOnlyServiceBeforeProviderCall(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	session := store.session
	session.ServiceID = "svc_guidance_only"
	session.ServiceName = "Guidance Only Service"
	session.BookingSegments = bookingSegmentsFromServices([]ServiceOption{{ID: session.ServiceID, Name: session.ServiceName}}, session)
	turn := newTurnRecord("salon_1", "owner_1", store.session, session, "Next Friday", "booking_fence_test", nil, nil, &store.cfg)

	err := service.offerAvailableSlots(context.Background(), "owner_1", &turn, &session, []ServiceOption{{
		ID: session.ServiceID, Name: session.ServiceName, BookingReady: false,
	}}, store.staff, "2026-08-21", false, &store.cfg)
	if !errors.Is(err, errBookingCatalogNotReady) {
		t.Fatalf("offerAvailableSlots error = %v, want booking catalog fence", err)
	}
	if bookingTool.availabilityCalls != 0 || bookingTool.calls != 0 {
		t.Fatalf("booking fence invoked provider tools: availability=%d booking=%d", bookingTool.availabilityCalls, bookingTool.calls)
	}
}

func TestGuidanceCatalogActionUsesStructuredCatalogAcrossPhoneAndSimulator(t *testing.T) {
	for _, channel := range []string{ChannelPhone, ChannelSimulator} {
		t.Run(channel, func(t *testing.T) {
			store := newFakeConversationStore()
			store.session.Channel = channel
			store.services = []ServiceOption{
				{ID: "svc_hands", Name: "Signature Hand Care"},
				{ID: "svc_feet", Name: "Cloud Foot Reset"},
			}
			bookingTool := &fakeBookingTool{}
			service := NewService(store, bookingTool)
			service.SetTurnInterpreter(&staticGuidanceTurnInterpreter{turn: TurnUnderstanding{
				Goal: "information", GuidanceAction: GuidanceActionServiceCatalog, Confidence: 0.96,
				Reason: "caller_requested_service_catalog",
			}})

			if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
				Message: "Show me your services", EventKey: "catalog_channel_parity_" + channel,
			}); err != nil {
				t.Fatalf("Message: %v", err)
			}
			if !strings.Contains(store.lastTurn.AIMessage, "Signature Hand Care") ||
				!strings.Contains(store.lastTurn.AIMessage, "Cloud Foot Reset") ||
				store.lastTurn.AIMetadata["answer_source"] != answerSourceServiceCatalog {
				t.Fatalf("structured catalog reply = %q metadata=%#v", store.lastTurn.AIMessage, store.lastTurn.AIMetadata)
			}
			assertGuidanceDidNotCallBookingTools(t, bookingTool)
		})
	}
}

func TestGuidanceCatalogOnlyUsesDataOwnedCategoriesWithoutInventingRecommendation(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "svc_hands", Name: "Signature Hand Care", CategoryID: "cat_hands", CategoryName: "Hand Care", ConsultationProfile: &ServiceConsultationProfile{Status: "draft"}},
		{ID: "svc_feet", Name: "Cloud Foot Reset", CategoryID: "cat_feet", CategoryName: "Foot Care"},
	}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(&staticGuidanceTurnInterpreter{turn: TurnUnderstanding{
		Goal: "consultation", GuidanceAction: GuidanceActionConsultation, Confidence: 0.93, Reason: "caller_requests_service_guidance",
	}})

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Could you point me toward the right kind of appointment?", EventKey: "catalog_only_guidance",
	})
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if consultationStateActive(session.DialogState.Consultation) || session.DialogState.Guidance == nil {
		t.Fatalf("catalog-only state = %#v", session.DialogState)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Hand Care") || !strings.Contains(store.lastTurn.AIMessage, "Foot Care") ||
		strings.Contains(store.lastTurn.AIMessage, "recommend") {
		t.Fatalf("catalog-only reply = %q", store.lastTurn.AIMessage)
	}
	if store.lastTurn.CustomerMetadata["service_guidance_capability"] != string(ServiceGuidanceCapabilityCatalogOnly) {
		t.Fatalf("catalog-only metadata = %#v", store.lastTurn.CustomerMetadata)
	}
	assertGuidanceDidNotCallBookingTools(t, bookingTool)
}

func TestPhoneGuidanceRecoveryEntersConsultationFromNaturalSemanticParaphrase(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	store.session.CustomerPhone = "+13125550199"
	store.services = []ServiceOption{
		{ID: "classic_mani", Name: "Classic Manicure", CategoryID: "mani", CategoryName: "Manicure", ConsultationProfile: readyComparisonProfile(ConsultationOutcomeMaintain, ConsultationSystemNatural)},
		{ID: "spa_pedi", Name: "Spa Pedicure", CategoryID: "pedi", CategoryName: "Pedicure", ConsultationProfile: readyComparisonProfile(ConsultationOutcomeColorRefresh, ConsultationSystemGel)},
	}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	interpreter := &staticGuidanceTurnInterpreter{turn: TurnUnderstanding{
		Goal: "consultation", GuidanceAction: GuidanceActionConsultation, Confidence: 0.92, Reason: "caller_is_unsure_which_service_fits",
	}}
	service.SetTurnInterpreter(interpreter)
	service.SetReplyGenerator(&fakeReplyGenerator{message: "What result would you like from the service?"})

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I've never had this done and would love some direction.", EventKey: "semantic_guidance_1",
	})
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if session.Intent != IntentConsultation || session.DialogState.Phase != DialogPhaseConsultation || !consultationStateActive(session.DialogState.Consultation) {
		t.Fatalf("consultation state = intent=%q dialog=%#v", session.Intent, session.DialogState)
	}
	if session.DialogState.Guidance != nil || store.lastTurn.AIMessage != "What result would you like from the service?" {
		t.Fatalf("consultation transition reply=%q dialog=%#v", store.lastTurn.AIMessage, session.DialogState)
	}
	if len(interpreter.request.CatalogServices) != 0 || !containsString(interpreter.request.RecognizableGuidanceActions, GuidanceActionConsultation) {
		t.Fatalf("guidance input was not scoped: request=%#v", interpreter.request)
	}
	assertGuidanceDidNotCallBookingTools(t, bookingTool)
}

func TestGuidanceSemanticActionRoutesUnseenMenuWordingToStructuredCatalog(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelSimulator
	store.services = []ServiceOption{
		{ID: "classic_mani", Name: "Classic Manicure"},
		{ID: "dip_mani", Name: "Dip Powder Manicure"},
	}
	store.knowledge = []KnowledgeSnippet{{ID: "knowledge_services", Title: "Services", Body: "Stale service copy;..."}}
	bookingTool := &fakeBookingTool{}
	interpreter := &staticGuidanceTurnInterpreter{turn: TurnUnderstanding{
		Goal: "information", GuidanceAction: GuidanceActionServiceCatalog, Confidence: 0.96,
		Reason: "caller_requested_service_catalog", InterpreterDiagnostics: map[string]string{"schema_fingerprint": "sha256:guidance123"},
	}}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(interpreter)

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Show me your services?", EventKey: "unseen_menu_1",
	})
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if session.Intent != IntentUnknown || session.DialogState.Guidance == nil || session.DialogState.Guidance.Stage != GuidanceRecoveryStageService {
		t.Fatalf("menu guidance state = intent=%q dialog=%#v", session.Intent, session.DialogState)
	}
	if store.lastTurn.AIMetadata["answer_source"] != answerSourceServiceCatalog || store.lastTurn.AIMetadata["answer_source_reason"] != "service_menu" {
		t.Fatalf("answer metadata = %#v", store.lastTurn.AIMetadata)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Classic Manicure") || !strings.Contains(store.lastTurn.AIMessage, "Dip Powder Manicure") || strings.Contains(store.lastTurn.AIMessage, ";...") {
		t.Fatalf("structured menu reply = %q", store.lastTurn.AIMessage)
	}
	if got := store.lastTurn.CustomerMetadata["turn_guidance_action"]; got != GuidanceActionServiceCatalog {
		t.Fatalf("guidance action metadata = %#v", got)
	}
	if store.lastTurn.CustomerMetadata["turn_semantic_contract"] != TurnSemanticContractGuidance || store.lastTurn.CustomerMetadata["turn_interpreter_schema_fingerprint"] != "sha256:guidance123" || store.lastTurn.CustomerMetadata["turn_interpreter_ms"] == nil {
		t.Fatalf("simulator diagnostics = %#v", store.lastTurn.CustomerMetadata)
	}
	if len(interpreter.request.CatalogServices) != 0 || !containsString(interpreter.request.RecognizableGuidanceActions, GuidanceActionServiceCatalog) {
		t.Fatalf("semantic request was not action-scoped = %#v", interpreter.request)
	}
	assertGuidanceDidNotCallBookingTools(t, bookingTool)
}

func TestGuidanceProviderFailureRequiresSemanticClassificationOnRetry(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	bookingTool := &fakeBookingTool{}
	interpreter := &providerFailureTurnInterpreter{outcome: TurnInterpreterOutcomeTimeout}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(interpreter)

	if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "My nails are long.", EventKey: "bounded_choice_1",
	}); err != nil {
		t.Fatalf("provider failure Message: %v", err)
	}
	if guidance := store.session.DialogState.Guidance; guidance == nil || guidance.ProviderFailureCount != 1 {
		t.Fatalf("provider recovery state = %#v", guidance)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "book an appointment") || !strings.Contains(store.lastTurn.AIMessage, "bookable service menu") {
		t.Fatalf("provider recovery choices = %q", store.lastTurn.AIMessage)
	}

	semanticRetry := &staticGuidanceTurnInterpreter{turn: TurnUnderstanding{
		Goal: "book_appointment", GuidanceAction: GuidanceActionBook, Confidence: 0.98,
	}}
	service.SetTurnInterpreter(semanticRetry)
	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "How I book an appointment?", EventKey: "bounded_choice_2",
	})
	if err != nil {
		t.Fatalf("semantic retry Message: %v", err)
	}
	if interpreter.calls != 1 || semanticRetry.request.CustomerMessage == "" {
		t.Fatalf("retry did not use the semantic provider: initial_calls=%d retry_request=%#v", interpreter.calls, semanticRetry.request)
	}
	if session.Status != StatusActive || session.Intent != IntentBooking || session.DialogState.Guidance == nil || session.DialogState.Guidance.Stage != GuidanceRecoveryStageService || session.DialogState.Guidance.ProviderFailureCount != 0 {
		t.Fatalf("semantic booking state = status=%q intent=%q guidance=%#v", session.Status, session.Intent, session.DialogState.Guidance)
	}
	if store.lastTurn.CustomerMetadata["turn_guidance_action"] != GuidanceActionBook || store.lastTurn.AIMessage != "Which bookable service would you like?" {
		t.Fatalf("semantic booking turn = reply=%q metadata=%#v", store.lastTurn.AIMessage, store.lastTurn.CustomerMetadata)
	}
	assertGuidanceDidNotCallBookingTools(t, bookingTool)
}

func TestPhoneGuidanceRecoveryMenuProgressPreventsProviderFailureHandoff(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	store.services = []ServiceOption{
		{ID: "classic_mani", Name: "Classic Manicure", CategoryID: "mani", CategoryName: "Manicure", ConsultationProfile: readyComparisonProfile(ConsultationOutcomeMaintain, ConsultationSystemNatural)},
		{ID: "spa_pedi", Name: "Spa Pedicure", CategoryID: "pedi", CategoryName: "Pedicure", ConsultationProfile: readyComparisonProfile(ConsultationOutcomeColorRefresh, ConsultationSystemGel)},
	}
	bookingTool := &fakeBookingTool{}
	interpreter := &providerFailureTurnInterpreter{outcome: TurnInterpreterOutcomeTimeout}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(interpreter)

	messages := []string{
		"I'm not sure where to start.",
		"What services do you offer?",
		"I only want something done for my nails.",
	}
	for index, message := range messages {
		if index == 1 {
			service.SetTurnInterpreter(&staticGuidanceTurnInterpreter{turn: testGuidanceUnderstanding(GuidanceActionServiceCatalog, ConversationQuestionModeList, ConversationQuestionCatalog)})
		}
		if index == 2 {
			service.SetTurnInterpreter(interpreter)
		}
		if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
			Message: message, EventKey: "menu_progress_" + strconv.Itoa(index+1),
		}); err != nil {
			t.Fatalf("Message %d: %v", index+1, err)
		}
		if index == 0 {
			guidance := store.session.DialogState.Guidance
			if guidance == nil || guidance.Stage != GuidanceRecoveryStageCallerGoal || guidance.ProviderFailureCount != 1 || guidance.NoProgressCount != 0 {
				t.Fatalf("initial provider failure state = %#v", guidance)
			}
		}
		if index == 1 {
			guidance := store.session.DialogState.Guidance
			if guidance == nil || guidance.Stage != GuidanceRecoveryStageService || guidance.ProviderFailureCount != 0 || guidance.NoProgressCount != 0 {
				t.Fatalf("catalog progress state = %#v metadata=%#v", guidance, store.lastTurn.CustomerMetadata)
			}
			if !strings.Contains(store.lastTurn.AIMessage, "Classic Manicure") || !strings.Contains(store.lastTurn.AIMessage, "help narrow it down") {
				t.Fatalf("catalog reply = %q", store.lastTurn.AIMessage)
			}
		}
	}

	session := store.session
	guidance := session.DialogState.Guidance
	if session.Status != StatusActive || session.Handoff != nil || guidance == nil || guidance.Stage != GuidanceRecoveryStageService {
		t.Fatalf("final recovery session = status=%q handoff=%#v guidance=%#v", session.Status, session.Handoff, guidance)
	}
	if guidance.ProviderFailureCount != 1 || guidance.NoProgressCount != 0 || session.ServiceID != "" {
		t.Fatalf("final recovery counters/service = guidance=%#v service=%q", guidance, session.ServiceID)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "trouble checking the salon's service guidance") || strings.Contains(store.lastTurn.AIMessage, "trouble understanding") {
		t.Fatalf("provider recovery reply = %q", store.lastTurn.AIMessage)
	}
	assertGuidanceDidNotCallBookingTools(t, bookingTool)
}

func TestPhoneGuidanceRecoveryDoesNotInferConsultationFromWordingDuringProviderFailure(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(&providerFailureTurnInterpreter{})

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "What service should I choose?",
	})
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	guidance := session.DialogState.Guidance
	if session.Intent != IntentUnknown || consultationStateActive(session.DialogState.Consultation) || guidance == nil || guidance.Stage != GuidanceRecoveryStageCallerGoal {
		t.Fatalf("provider failure should preserve semantic uncertainty: intent=%q dialog=%#v reply=%q", session.Intent, session.DialogState, store.lastTurn.AIMessage)
	}
	if guidance.ProviderFailureCount != 1 || guidance.NoProgressCount != 0 {
		t.Fatalf("provider failure counters = %#v", guidance)
	}
	assertGuidanceDidNotCallBookingTools(t, bookingTool)
}

func TestPhoneGuidanceRecoverySelectsExactCatalogServiceWithoutSemanticProvider(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	store.services = []ServiceOption{{ID: "gel_mani", Name: "Gel Manicure", CategoryID: "mani", CategoryName: "Manicure"}}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(&providerFailureTurnInterpreter{})

	if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "I'm undecided.", EventKey: "exact_1"}); err != nil {
		t.Fatalf("recovery Message: %v", err)
	}
	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Gel Manicure, please.", EventKey: "exact_2"})
	if err != nil {
		t.Fatalf("service selection Message: %v", err)
	}
	if session.ServiceID != "gel_mani" || session.Intent != IntentBooking || session.DialogState.Guidance != nil {
		t.Fatalf("selected service state = service=%q intent=%q dialog=%#v", session.ServiceID, session.Intent, session.DialogState)
	}
	if !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "what day") {
		t.Fatalf("next booking prompt = %q", store.lastTurn.AIMessage)
	}
	assertGuidanceDidNotCallBookingTools(t, bookingTool)
}

func TestPhoneGuidanceRecoveryUsesOwnerManagedCategoryAliasButNotGenericNailsGuess(t *testing.T) {
	services := []ServiceOption{
		{ID: "classic_mani", Name: "Classic Manicure", CategoryID: "mani", CategoryName: "Manicure"},
		{ID: "gel_mani", Name: "Gel Manicure", CategoryID: "mani", CategoryName: "Manicure"},
		{ID: "spa_pedi", Name: "Spa Pedicure", CategoryID: "pedi", CategoryName: "Pedicure"},
	}

	withAlias := newFakeConversationStore()
	withAlias.session.Channel = ChannelPhone
	withAlias.session.Intent = IntentBooking
	withAlias.session.DialogState.Guidance = &GuidanceRecoveryState{Stage: GuidanceRecoveryStageService}
	withAlias.services = services
	withAlias.categoryAliases = []ServiceCategoryAlias{{ID: "alias_nails", CategoryID: "mani", CategoryName: "Manicure", Alias: "nails", NormalizedAlias: "nails"}}
	service := NewService(withAlias, &fakeBookingTool{})
	service.SetTurnInterpreter(&providerFailureTurnInterpreter{})

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Just something for my nails.", EventKey: "alias_1"})
	if err != nil {
		t.Fatalf("alias Message: %v", err)
	}
	if session.ServiceID != "" || session.DialogState.Guidance == nil || session.DialogState.Guidance.ProviderFailureCount != 0 {
		t.Fatalf("category alias state = service=%q guidance=%#v", session.ServiceID, session.DialogState.Guidance)
	}
	if !strings.Contains(storeLower(withAlias.lastTurn.AIMessage), "classic manicure") || !strings.Contains(storeLower(withAlias.lastTurn.AIMessage), "gel manicure") || strings.Contains(storeLower(withAlias.lastTurn.AIMessage), "spa pedicure") {
		t.Fatalf("category alias clarification = %q", withAlias.lastTurn.AIMessage)
	}

	withoutAlias := newFakeConversationStore()
	withoutAlias.session.Channel = ChannelPhone
	withoutAlias.session.Intent = IntentBooking
	withoutAlias.session.DialogState.Guidance = &GuidanceRecoveryState{Stage: GuidanceRecoveryStageService}
	withoutAlias.services = services
	service = NewService(withoutAlias, &fakeBookingTool{})
	service.SetTurnInterpreter(&providerFailureTurnInterpreter{})

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Just something for my nails.", EventKey: "no_alias_1"})
	if err != nil {
		t.Fatalf("no-alias Message: %v", err)
	}
	if session.ServiceID != "" || session.DialogState.Guidance == nil || session.DialogState.Guidance.ProviderFailureCount != 1 {
		t.Fatalf("generic nails state = service=%q guidance=%#v", session.ServiceID, session.DialogState.Guidance)
	}
	if strings.Contains(storeLower(withoutAlias.lastTurn.AIMessage), "classic manicure") || strings.Contains(storeLower(withoutAlias.lastTurn.AIMessage), "gel manicure") {
		t.Fatalf("generic nails guessed a category = %q", withoutAlias.lastTurn.AIMessage)
	}
}

func TestPhoneGuidanceRecoveryBoundsCallerNoProgressAndReplaysHandoffOnce(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(&staticGuidanceTurnInterpreter{turn: TurnUnderstanding{Goal: "unknown", Confidence: 0.95}})

	messages := []string{
		"I need a little more time to decide.",
		"That still doesn't resolve it for me.",
		"I'm not ready to choose yet.",
	}
	var session *Session
	for index, message := range messages {
		var err error
		session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: message, EventKey: "no_progress_" + strconv.Itoa(index+1)})
		if err != nil {
			t.Fatalf("Message %d: %v", index+1, err)
		}
	}
	if session.Status != StatusHandoff || session.Handoff == nil || session.Handoff.Reason != HandoffReasonServiceClarification {
		t.Fatalf("bounded recovery result = status=%q handoff=%#v", session.Status, session.Handoff)
	}
	if session.DialogState.Guidance == nil || session.DialogState.Guidance.NoProgressCount != 3 || session.DialogState.Guidance.ProviderFailureCount != 0 {
		t.Fatalf("bounded recovery counters = %#v", session.DialogState.Guidance)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "not a confirmed appointment") {
		t.Fatalf("handoff reply = %q", store.lastTurn.AIMessage)
	}
	transcriptLength := len(session.Transcript)
	replayed, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: messages[2], EventKey: "no_progress_3"})
	if err != nil {
		t.Fatalf("handoff replay: %v", err)
	}
	if len(replayed.Transcript) != transcriptLength || replayed.Handoff == nil || replayed.Handoff.Reason != HandoffReasonServiceClarification {
		t.Fatalf("handoff replay was not idempotent: transcript=%d want=%d handoff=%#v", len(replayed.Transcript), transcriptLength, replayed.Handoff)
	}
	assertGuidanceDidNotCallBookingTools(t, bookingTool)
}

func TestPhoneGuidanceRecoveryUsesDistinctProviderFailureHandoff(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.SetTurnInterpreter(&providerFailureTurnInterpreter{outcome: TurnInterpreterOutcomeTimeout})

	for index, message := range []string{"I'm uncertain.", "I still need some direction."} {
		if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
			Message: message, EventKey: "provider_failure_" + strconv.Itoa(index+1),
		}); err != nil {
			t.Fatalf("Message %d: %v", index+1, err)
		}
	}
	if store.session.Status != StatusHandoff || store.session.Handoff == nil || store.session.Handoff.Reason != HandoffReasonGuidanceProviderUnavailable {
		t.Fatalf("provider handoff = status=%q handoff=%#v", store.session.Status, store.session.Handoff)
	}
	if store.session.DialogState.Guidance == nil || store.session.DialogState.Guidance.NoProgressCount != 0 || store.session.DialogState.Guidance.ProviderFailureCount != 2 {
		t.Fatalf("provider handoff counters = %#v", store.session.DialogState.Guidance)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "salon's service guidance") || !strings.Contains(store.lastTurn.AIMessage, "won't guess") {
		t.Fatalf("provider handoff reply = %q", store.lastTurn.AIMessage)
	}
	assertGuidanceDidNotCallBookingTools(t, bookingTool)
}

func assertGuidanceDidNotCallBookingTools(t *testing.T, tool *fakeBookingTool) {
	t.Helper()
	if tool.calls != 0 || tool.availabilityCalls != 0 {
		t.Fatalf("guidance recovery called booking tools: create=%d availability=%d", tool.calls, tool.availabilityCalls)
	}
}

func storeLower(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
