package voice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

func TestStatusReportsPhoneBookingReadiness(t *testing.T) {
	store := newFakeVoiceStore()
	service := newVoiceStatusService(store, testVoiceConfig(), readySchedulingTarget(booking.SchedulingAuthorityExternalProvider, 1))

	status, err := service.Status(context.Background(), "salon_1", "owner_1")
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if !status.Ready {
		t.Fatalf("voice status should be ready: %#v", status)
	}
	if !status.PhoneBookingReady {
		t.Fatalf("phone booking should be ready: %#v", status.Booking)
	}
	if !status.PhoneAnsweringReady || !status.RequestCaptureReady || !status.AutomatedBookingReady ||
		status.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider || status.AuthorityVersion != 1 || status.BookingMode != "confirmed_booking" {
		t.Fatalf("provider-neutral readiness = %#v", status)
	}
	if !status.Booking.AIEnabled || !status.Booking.SquareConnected || !status.Booking.SquareSynced {
		t.Fatalf("booking readiness flags = %#v", status.Booking)
	}
	if status.Booking.ServiceCount != 1 || status.Booking.StaffCount != 1 || status.Booking.BusinessHoursCount != 6 {
		t.Fatalf("booking readiness counts = %#v", status.Booking)
	}
}

func TestPlatformStatusPreservesBusinessReadinessAndHidesTechnicalProviderDetails(t *testing.T) {
	store := newFakeVoiceStore()
	service := newVoiceStatusService(store, testVoiceConfig(), readySchedulingTarget(booking.SchedulingAuthorityExternalProvider, 1))

	status, err := service.StatusForPlatform(context.Background(), " salon_1 ", " platform_ops_1 ")
	if err != nil {
		t.Fatalf("StatusForPlatform returned error: %v", err)
	}
	if !status.PhoneAnsweringReady || !status.RequestCaptureReady || !status.AutomatedBookingReady {
		t.Fatalf("business readiness was lost: %#v", status)
	}
	if status.Provider != "managed" || status.InboundWebhookURL != "" || status.TurnWebhookURL != "" || status.RecordingWebhookURL != "" || status.StreamWebhookURL != "" {
		t.Fatalf("technical voice details leaked: %#v", status)
	}
	for name, capability := range map[string]ProviderCapabilityStatus{
		"stt": status.AI.STT, "llm": status.AI.LLM, "tts": status.AI.TTS, "realtime": status.AI.Realtime,
	} {
		if capability.Provider != "managed" || capability.Model != "" || capability.Voice != "" {
			t.Fatalf("%s capability leaked technical config: %#v", name, capability)
		}
	}
	if store.voiceStatusOwnerUserID != "owner_1" || store.platformResolverSalonID != "salon_1" || store.platformResolverUserID != "platform_ops_1" {
		t.Fatalf("platform owner resolution = %#v", store)
	}
}

func TestVoiceConfigPropagatesSalonProviderConfigResolutionFailures(t *testing.T) {
	for _, test := range []struct {
		name      string
		twilioErr error
		openAIErr error
	}{
		{name: "Twilio repository failure", twilioErr: errors.New("Twilio config unavailable")},
		{name: "OpenAI decryption failure", openAIErr: errors.New("OpenAI config unreadable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			wantErr := test.twilioErr
			if wantErr == nil {
				wantErr = test.openAIErr
			}
			service := NewService(newFakeVoiceStore(), newFakeConversationEngine(), testVoiceConfig(), AIProviders{})
			service.SetConfigResolver(failingVoiceConfigResolver{twilioErr: test.twilioErr, openAIErr: test.openAIErr})

			cfg, err := service.voiceConfig(context.Background(), "salon_1")
			if !errors.Is(err, wantErr) || cfg.Twilio.AuthToken != "" || cfg.AI.OpenAI.APIKey != "" {
				t.Fatalf("voiceConfig = %#v, %v; want fail-closed resolver error", cfg, err)
			}
		})
	}
}

type failingVoiceConfigResolver struct {
	twilioErr error
	openAIErr error
}

type recordingVoiceConfigResolver struct {
	twilioSalonIDs []string
	openAISalonIDs []string
}

func (r *recordingVoiceConfigResolver) ResolveTwilioConfig(_ context.Context, salonID string) (config.TwilioVoiceConfig, string, error) {
	r.twilioSalonIDs = append(r.twilioSalonIDs, salonID)
	return config.TwilioVoiceConfig{
		AuthToken: "salon-scoped-token", IncomingPath: "/api/voice/twilio/incoming",
		TurnPath: "/api/voice/twilio/turn", RecordingPath: "/api/voice/twilio/recording",
	}, "https://voice.example.com", nil
}

func (r *recordingVoiceConfigResolver) ResolveStoredTwilioAuthToken(context.Context, string) (string, error) {
	return "salon-scoped-token", nil
}

func (r *recordingVoiceConfigResolver) ResolveOpenAIConfig(_ context.Context, salonID string) (config.OpenAIVoiceConfig, bool, error) {
	r.openAISalonIDs = append(r.openAISalonIDs, salonID)
	return config.OpenAIVoiceConfig{
		APIKey: "salon-scoped-key", BaseURL: "https://api.openai.com/v1",
		TranscriptionModel: "gpt-4o-mini-transcribe", ReplyModel: "gpt-4.1-mini",
		SpeechModel: "tts-1", SpeechVoice: "alloy",
	}, true, nil
}

func (r failingVoiceConfigResolver) ResolveTwilioConfig(context.Context, string) (config.TwilioVoiceConfig, string, error) {
	if r.twilioErr != nil {
		return config.TwilioVoiceConfig{}, "", r.twilioErr
	}
	return config.TwilioVoiceConfig{AuthToken: "stored-token"}, "https://api.example.com", nil
}

func (r failingVoiceConfigResolver) ResolveStoredTwilioAuthToken(context.Context, string) (string, error) {
	return "", nil
}

func (r failingVoiceConfigResolver) ResolveOpenAIConfig(context.Context, string) (config.OpenAIVoiceConfig, bool, error) {
	if r.openAIErr != nil {
		return config.OpenAIVoiceConfig{}, false, r.openAIErr
	}
	return config.OpenAIVoiceConfig{APIKey: "stored-key"}, true, nil
}

func TestServiceGuidanceReadinessSeparatesCatalogFromRecommendationCapability(t *testing.T) {
	tests := []struct {
		name                string
		serviceCount        int
		consultationEnabled bool
		readyProfiles       int
		wantStatus          conversation.ServiceGuidanceCapabilityStatus
		wantCatalog         bool
		wantRecommendation  bool
	}{
		{name: "catalog unavailable", wantStatus: conversation.ServiceGuidanceCapabilityCatalogUnavailable},
		{name: "consultation disabled", serviceCount: 3, wantStatus: conversation.ServiceGuidanceCapabilityDisabled, wantCatalog: true},
		{name: "catalog only", serviceCount: 3, consultationEnabled: true, wantStatus: conversation.ServiceGuidanceCapabilityCatalogOnly, wantCatalog: true},
		{name: "recommendation ready", serviceCount: 3, consultationEnabled: true, readyProfiles: 2, wantStatus: conversation.ServiceGuidanceCapabilityRecommendationReady, wantCatalog: true, wantRecommendation: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := serviceGuidanceReadiness(test.serviceCount, test.consultationEnabled, test.readyProfiles)
			if got.Status != test.wantStatus || got.CatalogAvailable != test.wantCatalog || got.RecommendationReady != test.wantRecommendation || got.ReadyServiceCount != test.readyProfiles {
				t.Fatalf("readiness = %#v", got)
			}
			if got.Status != conversation.ServiceGuidanceCapabilityRecommendationReady && got.Message == "" {
				t.Fatalf("degraded readiness has no operator message: %#v", got)
			}
		})
	}
}

func TestSemanticCheckVerifiesLiveContractWithoutConversationMutation(t *testing.T) {
	store := newFakeVoiceStore()
	engine := newFakeConversationEngine()
	provider := &fakeTurnContractProvider{
		configured: true,
		check: TurnContractCheck{
			Provider: ProviderOpenAI, SchemaFingerprint: "sha256:contract123", RequestID: "req_semantic_check_1",
		},
	}
	service := NewService(store, engine, testVoiceConfig(), AIProviders{TurnModel: provider})

	status, err := service.SemanticCheck(context.Background(), "salon_1", "owner_1")
	if err != nil {
		t.Fatalf("SemanticCheck: %v", err)
	}
	if !status.Configured || !status.Verified || status.Provider != ProviderOpenAI || status.SchemaFingerprint != "sha256:contract123" || status.RequestID != "req_semantic_check_1" {
		t.Fatalf("semantic check status = %#v", status)
	}
	if provider.checkCalls != 1 || provider.interpretCalls != 0 || engine.startCalls != 0 || engine.messageCalls != 0 {
		t.Fatalf("semantic check side effects: provider=%#v engine=%#v", provider, engine)
	}
	if store.voiceStatusSalonID != "salon_1" || store.voiceStatusOwnerUserID != "owner_1" {
		t.Fatalf("owner scope check = salon %q owner %q", store.voiceStatusSalonID, store.voiceStatusOwnerUserID)
	}
}

func TestSemanticCheckReturnsSafeProviderDiagnosticsAsStatus(t *testing.T) {
	store := newFakeVoiceStore()
	provider := &fakeTurnContractProvider{
		configured: true,
		check:      TurnContractCheck{Provider: ProviderOpenAI, SchemaFingerprint: "sha256:contract456"},
		err: &ProviderRequestError{
			Provider: ProviderOpenAI, Stage: "turn_interpretation_response", StatusCode: 400,
			RequestID: "req_semantic_bad_1", ErrorType: "invalid_request_error", ErrorCode: "invalid_json_schema",
			ErrorParam: "text.format.schema", SchemaFingerprint: "sha256:contract456", Err: errors.New("safe provider failure"),
		},
	}
	service := NewService(store, newFakeConversationEngine(), testVoiceConfig(), AIProviders{TurnModel: provider})

	status, err := service.SemanticCheck(context.Background(), "salon_1", "owner_1")
	if err != nil {
		t.Fatalf("SemanticCheck: %v", err)
	}
	if !status.Configured || status.Verified || status.Diagnostics["error_code"] != "invalid_json_schema" || status.Diagnostics["http_status_class"] != "4xx" || status.RequestID != "req_semantic_bad_1" {
		t.Fatalf("semantic failure status = %#v", status)
	}
}

func TestSemanticEvaluateCallsSalonScopedModelWithoutConversationOrBookingMutation(t *testing.T) {
	store := newFakeVoiceStore()
	engine := newFakeConversationEngine()
	provider := &fakeTurnContractProvider{
		configured: true,
		interpretReply: TurnModelReply{
			Goal:                    "information",
			GuidanceAction:          conversation.GuidanceActionServiceCatalog,
			GuidanceCatalogMode:     conversation.ConversationQuestionModeList,
			GuidanceQuestionSubject: conversation.ConversationQuestionCatalog,
			Confidence:              0.98,
		},
	}
	service := NewService(store, engine, testVoiceConfig(), AIProviders{TurnModel: provider})
	req := SemanticEvaluationRequest{
		ScenarioID:                  "catalog-001",
		Channel:                     conversation.ChannelSimulator,
		CustomerMessage:             "Could you walk me through the services you offer?",
		ExpectedInput:               conversation.ExpectedInputCallerGoal,
		SemanticContract:            conversation.TurnSemanticContractGuidance,
		BookingAction:               conversation.BookingActionBook,
		RecognizableGuidanceActions: conversation.GuidanceActionValues(),
		CatalogServices: []conversation.ConversationServiceRef{{
			ServiceID: "service_luna", ServiceName: "Luna Renewal", CategoryID: "category_ritual", CategoryName: "Signature Ritual",
		}},
		CatalogServiceAliases: []conversation.ConversationServiceAliasRef{{ServiceID: "service_luna", Alias: "moon refresh"}},
		CatalogCategories:     []conversation.ConversationCategoryRef{{CategoryID: "category_ritual", CategoryName: "Signature Ritual", ServiceIDs: []string{"service_luna"}}},
	}

	result, err := service.SemanticEvaluate(context.Background(), "salon_1", "owner_1", req)
	if err != nil {
		t.Fatalf("SemanticEvaluate: %v", err)
	}
	if result.ScenarioID != "catalog-001" || result.Result.GuidanceAction != conversation.GuidanceActionServiceCatalog {
		t.Fatalf("semantic evaluation result = %#v", result)
	}
	if provider.interpretCalls != 1 || provider.interpretRequest.SalonID != "salon_1" ||
		provider.interpretRequest.SessionID != "semantic-evaluation:catalog-001" ||
		provider.interpretRequest.CatalogServices[0].ServiceName != "Luna Renewal" {
		t.Fatalf("provider request = %#v", provider.interpretRequest)
	}
	if engine.startCalls != 0 || engine.messageCalls != 0 {
		t.Fatalf("semantic evaluation mutated conversation state: %#v", engine)
	}
}

func TestSemanticEvaluationRejectsCapabilityFilteredGuidanceVocabulary(t *testing.T) {
	req := SemanticEvaluationRequest{
		ScenarioID: "capability-filtered-guidance", Channel: conversation.ChannelSimulator,
		CustomerMessage: "I need help choosing.", ExpectedInput: conversation.ExpectedInputCallerGoal,
		SemanticContract: conversation.TurnSemanticContractGuidance, BookingAction: conversation.BookingActionBook,
		RecognizableGuidanceActions: []string{conversation.GuidanceActionBook, conversation.GuidanceActionHumanHandoff},
	}
	if ValidSemanticEvaluationRequest(req) {
		t.Fatalf("capability-filtered vocabulary was accepted: %#v", req.RecognizableGuidanceActions)
	}
	req.RecognizableGuidanceActions = conversation.GuidanceActionValues()
	if !ValidSemanticEvaluationRequest(req) {
		t.Fatalf("stable recognizable vocabulary was rejected: %#v", req.RecognizableGuidanceActions)
	}
}

func TestSemanticEvaluateRejectsCatalogReferencesOutsideStructuredSource(t *testing.T) {
	provider := &fakeTurnContractProvider{configured: true}
	service := NewService(newFakeVoiceStore(), newFakeConversationEngine(), testVoiceConfig(), AIProviders{TurnModel: provider})
	req := SemanticEvaluationRequest{
		ScenarioID:       "invalid-alias-target",
		Channel:          conversation.ChannelPhone,
		CustomerMessage:  "I call it the moon refresh.",
		SemanticContract: conversation.TurnSemanticContractFull,
		CatalogServices:  []conversation.ConversationServiceRef{{ServiceID: "service_luna", ServiceName: "Luna Renewal"}},
		CatalogServiceAliases: []conversation.ConversationServiceAliasRef{{
			ServiceID: "invented_service", Alias: "moon refresh",
		}},
	}

	if _, err := service.SemanticEvaluate(context.Background(), "salon_1", "owner_1", req); !errors.Is(err, ErrValidation) {
		t.Fatalf("SemanticEvaluate error = %v, want ErrValidation", err)
	}
	if provider.interpretCalls != 0 {
		t.Fatalf("invalid source data reached model provider: calls=%d", provider.interpretCalls)
	}
}

func TestSemanticEvaluateRejectsNonRuntimeStateVocabulary(t *testing.T) {
	provider := &fakeTurnContractProvider{configured: true}
	service := NewService(newFakeVoiceStore(), newFakeConversationEngine(), testVoiceConfig(), AIProviders{TurnModel: provider})
	req := SemanticEvaluationRequest{
		ScenarioID:       "invalid-state-vocabulary",
		Channel:          conversation.ChannelPhone,
		CustomerMessage:  "Please edit my service.",
		ExpectedInput:    "service_edit",
		SemanticContract: conversation.TurnSemanticContractFull,
		BookingAction:    conversation.BookingActionBook,
		CatalogServices:  []conversation.ConversationServiceRef{{ServiceID: "service_luna", ServiceName: "Luna Renewal"}},
	}

	if _, err := service.SemanticEvaluate(context.Background(), "salon_1", "owner_1", req); !errors.Is(err, ErrValidation) {
		t.Fatalf("SemanticEvaluate error = %v, want ErrValidation", err)
	}
	if provider.interpretCalls != 0 {
		t.Fatalf("invalid runtime vocabulary reached model provider: calls=%d", provider.interpretCalls)
	}
}

func TestSemanticEvaluateRejectsDraftStaffOutsideStructuredCatalog(t *testing.T) {
	provider := &fakeTurnContractProvider{configured: true}
	service := NewService(newFakeVoiceStore(), newFakeConversationEngine(), testVoiceConfig(), AIProviders{TurnModel: provider})
	req := SemanticEvaluationRequest{
		ScenarioID:       "invalid-draft-staff",
		Channel:          conversation.ChannelSimulator,
		CustomerMessage:  "Can I use another technician?",
		ExpectedInput:    conversation.ExpectedInputStaff,
		SemanticContract: conversation.TurnSemanticContractFull,
		BookingAction:    conversation.BookingActionBook,
		CatalogServices:  []conversation.ConversationServiceRef{{ServiceID: "service_luna", ServiceName: "Luna Renewal"}},
		CatalogStaff:     []conversation.ConversationStaffRef{{StaffID: "staff_1", StaffName: "Mia"}},
		CurrentDraft:     conversation.ConversationDraftRef{StaffID: "invented_staff"},
	}

	if _, err := service.SemanticEvaluate(context.Background(), "salon_1", "owner_1", req); !errors.Is(err, ErrValidation) {
		t.Fatalf("SemanticEvaluate error = %v, want ErrValidation", err)
	}
	if provider.interpretCalls != 0 {
		t.Fatalf("invalid draft staff reached model provider: calls=%d", provider.interpretCalls)
	}
}

func TestSemanticEvaluateRejectsAliasOwnershipCollision(t *testing.T) {
	provider := &fakeTurnContractProvider{configured: true}
	service := NewService(newFakeVoiceStore(), newFakeConversationEngine(), testVoiceConfig(), AIProviders{TurnModel: provider})
	req := SemanticEvaluationRequest{
		ScenarioID:       "alias-owner-collision",
		Channel:          conversation.ChannelSimulator,
		CustomerMessage:  "I call it moon refresh.",
		ExpectedInput:    conversation.ExpectedInputService,
		SemanticContract: conversation.TurnSemanticContractFull,
		BookingAction:    conversation.BookingActionBook,
		CatalogServices: []conversation.ConversationServiceRef{{
			ServiceID: "service_luna", ServiceName: "Luna Renewal", CategoryID: "category_ritual", CategoryName: "Signature Ritual",
		}},
		CatalogServiceAliases: []conversation.ConversationServiceAliasRef{{ServiceID: "service_luna", Alias: "moon refresh"}},
		CatalogCategories: []conversation.ConversationCategoryRef{{
			CategoryID: "category_ritual", CategoryName: "Signature Ritual", Aliases: []string{"moon refresh"}, ServiceIDs: []string{"service_luna"},
		}},
	}

	if _, err := service.SemanticEvaluate(context.Background(), "salon_1", "owner_1", req); !errors.Is(err, ErrValidation) {
		t.Fatalf("SemanticEvaluate error = %v, want ErrValidation", err)
	}
	if provider.interpretCalls != 0 {
		t.Fatalf("conflicting alias ownership reached model provider: calls=%d", provider.interpretCalls)
	}
}

func TestSemanticEvaluateRejectsInconsistentPartyGroups(t *testing.T) {
	provider := &fakeTurnContractProvider{configured: true}
	service := NewService(newFakeVoiceStore(), newFakeConversationEngine(), testVoiceConfig(), AIProviders{TurnModel: provider})
	req := SemanticEvaluationRequest{
		ScenarioID:       "inconsistent-party-groups",
		Channel:          conversation.ChannelPhone,
		CustomerMessage:  "Guest two wants the same service.",
		ExpectedInput:    conversation.ExpectedInputService,
		SemanticContract: conversation.TurnSemanticContractFull,
		BookingAction:    conversation.BookingActionBook,
		CatalogServices:  []conversation.ConversationServiceRef{{ServiceID: "service_luna", ServiceName: "Luna Renewal"}},
		CurrentDraft: conversation.ConversationDraftRef{
			PartySize: 3,
			PartyGroups: []conversation.ConversationPartyGroupRef{
				{GuestRef: "caller", Count: 1, ServiceIDs: []string{"service_luna"}},
				{GuestRef: "guest_2", Count: 1, ServiceIDs: []string{"service_luna"}},
			},
		},
	}

	if _, err := service.SemanticEvaluate(context.Background(), "salon_1", "owner_1", req); !errors.Is(err, ErrValidation) {
		t.Fatalf("SemanticEvaluate error = %v, want ErrValidation", err)
	}
	if provider.interpretCalls != 0 {
		t.Fatalf("inconsistent party groups reached model provider: calls=%d", provider.interpretCalls)
	}
}

func TestStatusKeepsWebhookReadyWhenPhoneBookingIsBlocked(t *testing.T) {
	store := newFakeVoiceStore()
	store.bookingReadiness = &PhoneBookingReadiness{
		Ready:           false,
		AIEnabled:       false,
		SquareConnected: false,
		SquareSynced:    false,
		Checks: []ReadinessCheck{{
			Key:      "enable_ai_booking",
			Label:    "Enable AI booking",
			Complete: false,
			Message:  "AI booking is disabled for this salon.",
		}},
		BlockedReason: "AI booking is disabled for this salon.",
	}
	store.voiceStatus.AIEnabled = false
	service := newVoiceStatusService(store, testVoiceConfig(), readySchedulingTarget(booking.SchedulingAuthorityExternalProvider, 1))

	status, err := service.Status(context.Background(), "salon_1", "owner_1")
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if !status.Ready {
		t.Fatalf("Twilio webhook should still be ready: %#v", status)
	}
	if status.PhoneBookingReady {
		t.Fatalf("phone booking should not be ready when booking prerequisites fail: %#v", status.Booking)
	}
	if status.BlockedReason != "" {
		t.Fatalf("voice blocked reason = %q, want empty because webhook is ready", status.BlockedReason)
	}
	if status.Booking.BlockedReason != "AI booking is disabled for this salon." {
		t.Fatalf("booking blocked reason = %q", status.Booking.BlockedReason)
	}
}

func TestStatusKeepsWebhookReadyWhenBookingWritesAreBlocked(t *testing.T) {
	store := newFakeVoiceStore()
	store.bookingReadiness = &PhoneBookingReadiness{
		Ready:                     false,
		AIEnabled:                 true,
		SquareConnected:           true,
		SquareSynced:              true,
		BookingWriteBlocked:       true,
		BookingWriteBlockedCode:   "POS_PERMISSION_DENIED",
		BookingWriteBlockedReason: "square INSUFFICIENT_SCOPES: missing APPOINTMENTS_ALL_WRITE.",
		Checks: []ReadinessCheck{{
			Key:      "booking_writes",
			Label:    "Square booking writes",
			Complete: false,
			Message:  "square INSUFFICIENT_SCOPES: missing APPOINTMENTS_ALL_WRITE.",
		}},
		BlockedReason: "square INSUFFICIENT_SCOPES: missing APPOINTMENTS_ALL_WRITE.",
	}
	target := readySchedulingTarget(booking.SchedulingAuthorityExternalProvider, 1)
	target.ExecutionReady = false
	target.ExecutionBlockers = []scheduling.TargetReadinessBlocker{{
		Code: "EXTERNAL_PROVIDER_BOOKING_WRITES", Scope: "provider", Message: "Resolve the external booking-write readiness blocker.",
	}}
	service := newVoiceStatusService(store, testVoiceConfig(), target)

	status, err := service.Status(context.Background(), "salon_1", "owner_1")
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if !status.Ready {
		t.Fatalf("Twilio webhook should still be ready: %#v", status)
	}
	if status.PhoneBookingReady {
		t.Fatalf("phone booking should not be ready when Square booking writes are blocked: %#v", status.Booking)
	}
	if !status.Booking.BookingWriteBlocked {
		t.Fatalf("booking write blocker was not surfaced: %#v", status.Booking)
	}
}

func TestStatusComposesAuthorityAndBookingModeCapabilities(t *testing.T) {
	tests := []struct {
		name          string
		authority     string
		mode          string
		target        scheduling.TargetReadiness
		wantCapture   bool
		wantAutomated bool
		wantBlocker   string
	}{
		{
			name:      "owner manual captures pending request without automatic confirmation",
			authority: booking.SchedulingAuthorityOwnerManual, mode: "pending_approval",
			target: func() scheduling.TargetReadiness {
				value := readySchedulingTarget(booking.SchedulingAuthorityOwnerManual, 1)
				value.ExecutionReady = false
				value.ExecutionBlockers = []scheduling.TargetReadinessBlocker{{Code: "OWNER_MANUAL_REQUEST_ONLY", Message: "Owner review is required."}}
				return value
			}(),
			wantCapture: true, wantBlocker: "OWNER_REVIEW_MODE_SELECTED",
		},
		{
			name:      "owner manual confirmed mode remains request only",
			authority: booking.SchedulingAuthorityOwnerManual, mode: "confirmed_booking",
			target: func() scheduling.TargetReadiness {
				value := readySchedulingTarget(booking.SchedulingAuthorityOwnerManual, 1)
				value.ExecutionReady = false
				value.ExecutionBlockers = []scheduling.TargetReadinessBlocker{{Code: "OWNER_MANUAL_REQUEST_ONLY", Message: "Owner review is required."}}
				return value
			}(),
			wantCapture: true, wantBlocker: "OWNER_MANUAL_REQUEST_ONLY",
		},
		{
			name:      "internal pending captures request when availability path is ready",
			authority: booking.SchedulingAuthorityManleAICalendar, mode: "pending_approval",
			target: func() scheduling.TargetReadiness {
				value := readySchedulingTarget(booking.SchedulingAuthorityManleAICalendar, 1)
				value.ExecutionReady = false
				return value
			}(),
			wantCapture: true, wantBlocker: "OWNER_REVIEW_MODE_SELECTED",
		},
		{
			name:      "internal confirmed supports both capture and atomic booking",
			authority: booking.SchedulingAuthorityManleAICalendar, mode: "confirmed_booking",
			target:      readySchedulingTarget(booking.SchedulingAuthorityManleAICalendar, 1),
			wantCapture: true, wantAutomated: true,
		},
		{
			name:      "external pending ignores booking write blocker for request capture",
			authority: booking.SchedulingAuthorityExternalProvider, mode: "pending_approval",
			target: func() scheduling.TargetReadiness {
				value := readySchedulingTarget(booking.SchedulingAuthorityExternalProvider, 1)
				value.ExecutionReady = false
				value.ExecutionBlockers = []scheduling.TargetReadinessBlocker{{Code: "EXTERNAL_PROVIDER_BOOKING_WRITES", Message: "Booking writes are blocked."}}
				return value
			}(),
			wantCapture: true, wantBlocker: "OWNER_REVIEW_MODE_SELECTED",
		},
		{
			name:      "external confirmed fails closed when execution is blocked",
			authority: booking.SchedulingAuthorityExternalProvider, mode: "confirmed_booking",
			target: func() scheduling.TargetReadiness {
				value := readySchedulingTarget(booking.SchedulingAuthorityExternalProvider, 1)
				value.ExecutionReady = false
				value.ExecutionBlockers = []scheduling.TargetReadinessBlocker{{Code: "EXTERNAL_PROVIDER_BOOKING_WRITES", Message: "Booking writes are blocked."}}
				return value
			}(),
			wantCapture: true, wantBlocker: "EXTERNAL_PROVIDER_BOOKING_WRITES",
		},
		{
			name:      "disabled mode blocks all scheduling work",
			authority: booking.SchedulingAuthorityManleAICalendar, mode: "disabled",
			target:      readySchedulingTarget(booking.SchedulingAuthorityManleAICalendar, 1),
			wantBlocker: "SCHEDULING_DISABLED",
		},
		{
			name:      "unknown booking mode fails closed",
			authority: booking.SchedulingAuthorityManleAICalendar, mode: "future_mode",
			target:      readySchedulingTarget(booking.SchedulingAuthorityManleAICalendar, 1),
			wantBlocker: "BOOKING_MODE_UNSUPPORTED",
		},
		{
			name:      "unknown scheduling authority fails closed",
			authority: "future_authority", mode: "confirmed_booking",
			target:      readySchedulingTarget("future_authority", 1),
			wantBlocker: "SCHEDULING_AUTHORITY_UNSUPPORTED",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeVoiceStore()
			store.voiceStatus.SchedulingAuthority = test.authority
			store.voiceStatus.BookingMode = test.mode
			status, err := newVoiceStatusService(store, testVoiceConfig(), test.target).Status(context.Background(), "salon_1", "owner_1")
			if err != nil {
				t.Fatalf("Status returned error: %v", err)
			}
			if status.RequestCaptureReady != test.wantCapture || status.AutomatedBookingReady != test.wantAutomated || status.PhoneBookingReady != test.wantAutomated {
				t.Fatalf("readiness capture=%t automated=%t alias=%t status=%#v", status.RequestCaptureReady, status.AutomatedBookingReady, status.PhoneBookingReady, status)
			}
			if test.wantBlocker != "" && !hasVoiceBlocker(status.AutomatedBooking.Blockers, test.wantBlocker) {
				t.Fatalf("automated blockers = %#v, want %s", status.AutomatedBooking.Blockers, test.wantBlocker)
			}
		})
	}
}

func TestStatusFailsClosedOnAuthorityFenceDrift(t *testing.T) {
	store := newFakeVoiceStore()
	target := readySchedulingTarget(booking.SchedulingAuthorityExternalProvider, 2)
	status, err := newVoiceStatusService(store, testVoiceConfig(), target).Status(context.Background(), "salon_1", "owner_1")
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if status.RequestCaptureReady || status.AutomatedBookingReady || !hasVoiceBlocker(status.RequestCapture.Blockers, "SCHEDULING_AUTHORITY_FENCE_STALE") {
		t.Fatalf("stale-fence status = %#v", status)
	}
}

func hasVoiceBlocker(items []VoiceReadinessBlocker, code string) bool {
	for _, item := range items {
		if item.Code == code {
			return true
		}
	}
	return false
}

func TestStatusReportsRealtimeInputModeWhenRealtimeConfigured(t *testing.T) {
	store := newFakeVoiceStore()
	service := newVoiceStatusService(store, config.VoiceConfig{
		Provider:      ProviderTwilio,
		PublicBaseURL: "https://voice.example.com",
		Twilio: config.TwilioVoiceConfig{
			AuthToken:      "secret",
			IncomingPath:   "/api/voice/twilio/incoming",
			RecordingPath:  "/api/voice/twilio/recording",
			StreamPath:     "/api/voice/twilio/stream",
			VoiceTransport: InputModeRealtimeStream,
		},
		AI: config.VoiceAIConfig{
			Provider: ProviderOpenAI,
			OpenAI: config.OpenAIVoiceConfig{
				APIKey:          "openai-key",
				BaseURL:         "https://api.openai.com/v1",
				RealtimeEnabled: true,
				RealtimeModel:   "gpt-realtime-2",
				RealtimeVoice:   "alloy",
			},
		},
	}, readySchedulingTarget(booking.SchedulingAuthorityExternalProvider, 1))
	service.providers = AIProviders{Realtime: fakeRealtimeProvider{configured: true}}

	status, err := service.Status(context.Background(), "salon_1", "owner_1")
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if status.InputMode != InputModeRealtimeStream {
		t.Fatalf("input mode = %q, want realtime_stream", status.InputMode)
	}
	if status.StreamWebhookURL != "wss://voice.example.com/api/voice/twilio/stream" {
		t.Fatalf("stream webhook URL = %q", status.StreamWebhookURL)
	}
	if !status.AI.Realtime.Ready {
		t.Fatalf("realtime capability should be ready: %#v", status.AI.Realtime)
	}
}

func TestIncomingCallStartsPhoneSessionAndReturnsGreeting(t *testing.T) {
	store := newFakeVoiceStore()
	engine := newFakeConversationEngine()
	service := NewService(store, engine, testVoiceConfig(), AIProviders{})

	reply, err := service.HandleIncomingCall(context.Background(), IncomingCallRequest{
		ProviderCallID: "CA123",
		FromPhone:      "(312) 555-0101",
		ToPhone:        "+1 312-555-0102",
		Payload:        map[string]string{"CallSid": "CA123"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingCall returned error: %v", err)
	}
	if !reply.Continue || reply.InputMode != InputModeGather {
		t.Fatalf("reply continue/input = %v/%s, want true/gather", reply.Continue, reply.InputMode)
	}
	if reply.Message != "Thank you for calling. How can I help you today?" {
		t.Fatalf("message = %q", reply.Message)
	}
	if reply.OpeningNotice != defaultRecordingConsentMessage {
		t.Fatalf("opening notice = %q", reply.OpeningNotice)
	}
	if engine.startCalls != 1 {
		t.Fatalf("start phone calls = %d, want 1", engine.startCalls)
	}
	if engine.startRequest.Provider != ProviderTwilio || engine.startRequest.ProviderCallID != "CA123" {
		t.Fatalf("start request provider/call = %s/%s", engine.startRequest.Provider, engine.startRequest.ProviderCallID)
	}
	if engine.startRequest.FromPhone != "3125550101" || engine.startRequest.ToPhone != "+13125550102" {
		t.Fatalf("normalized phones = %s/%s", engine.startRequest.FromPhone, engine.startRequest.ToPhone)
	}
	if len(store.events) != 1 || store.events[0].EventType != EventIncomingCall || store.events[0].CallSessionID != "session_phone" {
		t.Fatalf("events = %#v, want routed incoming call event", store.events)
	}
}

func TestIncomingCallUsesOnlyTheSalonResolvedFromTheDialedNumber(t *testing.T) {
	store := newFakeVoiceStore()
	store.salon = &InboundSalon{
		SalonID: "salon_a", OwnerUserID: "owner_a", SalonName: "Salon A",
		Phone: "+13125550111", RecordingEnabled: true,
	}
	engine := newFakeConversationEngine()
	engine.startSession.SalonID = "salon_a"
	engine.startSession.ProviderCallID = "CA-SALON-A"
	resolver := &recordingVoiceConfigResolver{}
	service := NewService(store, engine, testVoiceConfig(), AIProviders{})
	service.SetConfigResolver(resolver)

	_, err := service.HandleIncomingCall(context.Background(), IncomingCallRequest{
		Provider: ProviderTwilio, ProviderCallID: "CA-SALON-A",
		FromPhone: "+13125550999", ToPhone: "+13125550111",
	})
	if err != nil {
		t.Fatalf("HandleIncomingCall returned error: %v", err)
	}
	if engine.startSalonID != "salon_a" || engine.startOwnerUserID != "owner_a" {
		t.Fatalf("conversation scope = salon %q owner %q, want salon_a/owner_a", engine.startSalonID, engine.startOwnerUserID)
	}
	if len(resolver.twilioSalonIDs) != 1 || resolver.twilioSalonIDs[0] != "salon_a" ||
		len(resolver.openAISalonIDs) != 1 || resolver.openAISalonIDs[0] != "salon_a" {
		t.Fatalf("provider config scopes = Twilio %#v OpenAI %#v, want only salon_a", resolver.twilioSalonIDs, resolver.openAISalonIDs)
	}
	if len(store.events) != 1 || store.events[0].SalonID != "salon_a" {
		t.Fatalf("webhook events = %#v, want only salon_a", store.events)
	}
}

func TestIncomingCallPrewarmsAnswerContextInBackground(t *testing.T) {
	store := newFakeVoiceStore()
	engine := newFakeConversationEngine()
	engine.prewarmDone = make(chan struct{}, 1)
	service := NewService(store, engine, testVoiceConfig(), AIProviders{})

	_, err := service.HandleIncomingCall(context.Background(), IncomingCallRequest{
		ProviderCallID: "CA123",
		FromPhone:      "(312) 555-0101",
		ToPhone:        "+1 312-555-0102",
		Payload:        map[string]string{"CallSid": "CA123"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingCall returned error: %v", err)
	}

	select {
	case <-engine.prewarmDone:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for answer context prewarm")
	}
	if engine.prewarmCalls != 1 || engine.prewarmSalonID != "salon_1" {
		t.Fatalf("prewarm calls/salon = %d/%q", engine.prewarmCalls, engine.prewarmSalonID)
	}
}

func TestIncomingCallOmitsOpeningNoticeWhenRecordingDisabled(t *testing.T) {
	store := newFakeVoiceStore()
	store.salon.RecordingEnabled = false
	engine := newFakeConversationEngine()
	service := NewService(store, engine, testVoiceConfig(), AIProviders{})

	reply, err := service.HandleIncomingCall(context.Background(), IncomingCallRequest{
		ProviderCallID: "CA123",
		FromPhone:      "(312) 555-0101",
		ToPhone:        "+1 312-555-0102",
		Payload:        map[string]string{"CallSid": "CA123"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingCall returned error: %v", err)
	}
	if reply.OpeningNotice != "" {
		t.Fatalf("opening notice = %q, want empty", reply.OpeningNotice)
	}
}

func TestSpeechTurnRoutesLiveCallThroughAvailabilityOffer(t *testing.T) {
	store := newFakeVoiceStore()
	store.route = &CallRoute{SalonID: "salon_1", OwnerUserID: "owner_1", SessionID: "session_phone"}
	engine := newFakeConversationEngine()
	engine.messageTimings = []conversation.TurnTiming{
		{Stage: conversation.TurnTimingStageSessionLoad, Duration: 5 * time.Millisecond, Result: conversation.TurnTimingResultOK},
		{Stage: conversation.TurnTimingStageAnswerContext, Duration: 3 * time.Millisecond, Result: conversation.TurnTimingResultOK},
		{Stage: conversation.TurnTimingStageTurnRouter, Duration: 2 * time.Millisecond, Result: conversation.TurnRouteSemanticLane, Attributes: map[string]string{
			"turn_route": conversation.TurnRouteSemanticLane, "turn_expected_input": conversation.ExpectedInputCallerGoal,
			"turn_route_reason": "semantic_context_required", "turn_deterministic_coverage": conversation.TurnCoverageNone,
		}},
		{Stage: conversation.TurnTimingStageTurnInterpreter, Duration: 7 * time.Millisecond, Result: conversation.TurnTimingPathStructuredAI, Attributes: map[string]string{"turn_interpreter_outcome": conversation.TurnInterpreterOutcomeAccepted}},
		{Stage: conversation.TurnTimingStageAvailabilityPOS, Duration: 11 * time.Millisecond, Result: conversation.TurnTimingResultOK},
		{Stage: conversation.TurnTimingStageAvailabilityPOS, Duration: 4 * time.Millisecond, Result: conversation.TurnTimingResultOK},
		{Stage: conversation.TurnTimingStageSaveTurn, Duration: 6 * time.Millisecond, Result: conversation.TurnTimingResultOK},
	}
	engine.messageSession = phoneSessionWithAIReply("I found these openings: first: Wed Jun 10 at 10:00 AM with Mai Nguyen. Which works?", conversation.StatusActive, conversation.OutcomeCollecting)
	engine.messageSession.Intent = conversation.IntentBooking
	engine.messageSession.ServiceID = "service_1"
	engine.messageSession.ServiceName = "Classic Manicure"
	engine.messageSession.OfferedSlots = []conversation.OfferedSlot{{
		StartTime: time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 6, 10, 15, 45, 0, 0, time.UTC),
		StaffID:   "staff_1",
		StaffName: "Mai Nguyen",
	}}
	service := NewService(store, engine, testVoiceConfig(), AIProviders{})

	reply, err := service.HandleSpeechTurn(context.Background(), SpeechTurnRequest{
		Provider:       ProviderTwilio,
		ProviderCallID: "CA123",
		SpeechText:     "I need a classic manicure tomorrow.",
		Payload:        map[string]string{"SpeechResult": "I need a classic manicure tomorrow."},
	})
	if err != nil {
		t.Fatalf("HandleSpeechTurn returned error: %v", err)
	}
	if engine.messageCalls != 1 {
		t.Fatalf("conversation message calls = %d, want 1", engine.messageCalls)
	}
	if engine.lastMessage != "I need a classic manicure tomorrow." {
		t.Fatalf("speech text passed to conversation = %q", engine.lastMessage)
	}
	if !reply.Continue {
		t.Fatalf("reply should continue while caller chooses a slot")
	}
	if !strings.Contains(reply.Message, "I found these openings") || strings.Contains(strings.ToLower(reply.Message), "confirmed") {
		t.Fatalf("reply should offer slots without confirmation: %q", reply.Message)
	}
	if len(reply.Session.OfferedSlots) != 1 {
		t.Fatalf("offered slots = %#v, want one", reply.Session.OfferedSlots)
	}
	for key, want := range map[string]string{
		"session_load_ms":             "5",
		"answer_context_ms":           "3",
		"turn_router_ms":              "2",
		"turn_route":                  conversation.TurnRouteSemanticLane,
		"turn_expected_input":         conversation.ExpectedInputCallerGoal,
		"turn_route_reason":           "semantic_context_required",
		"turn_deterministic_coverage": conversation.TurnCoverageNone,
		"turn_interpreter_ms":         "7",
		"turn_interpreter_path":       conversation.TurnTimingPathStructuredAI,
		"turn_interpreter_outcome":    conversation.TurnInterpreterOutcomeAccepted,
		"availability_pos_ms":         "15",
		"save_turn_ms":                "6",
	} {
		if got := reply.BackendDiagnostics[key]; got != want {
			t.Fatalf("backend diagnostic %s = %q, want %q; all=%#v", key, got, want, reply.BackendDiagnostics)
		}
	}
	if _, ok := reply.BackendDiagnostics["route_config_ms"]; !ok {
		t.Fatalf("route/config timing missing: %#v", reply.BackendDiagnostics)
	}
	if len(store.events) != 1 || store.events[0].EventType != EventSpeechTurn || store.events[0].CallSessionID != "session_phone" {
		t.Fatalf("events = %#v, want speech turn event for call session", store.events)
	}
}

func TestSpeechTurnUsesExactReplayReplyWithoutReplacingCurrentSessionState(t *testing.T) {
	store := newFakeVoiceStore()
	store.route = &CallRoute{SalonID: "salon_1", OwnerUserID: "owner_1", SessionID: "session_phone"}
	engine := newFakeConversationEngine()
	engine.messageSession = phoneSessionWithAIReply("Reply for newer event E2.", conversation.StatusActive, conversation.OutcomeCollecting)
	engine.messageSession.ReplayEventKey = "event-e1"
	engine.messageSession.ReplayAIMessage = "Exact reply for retried event E1."
	service := NewService(store, engine, testVoiceConfig(), AIProviders{})

	reply, err := service.HandleSpeechTurn(context.Background(), SpeechTurnRequest{
		Provider:       ProviderTwilio,
		ProviderCallID: "CA123",
		SpeechText:     "Provider replay for E1",
		Payload:        map[string]string{"SpeechResult": "Provider replay for E1"},
	})
	if err != nil {
		t.Fatalf("HandleSpeechTurn replay: %v", err)
	}
	if reply.Message != "Exact reply for retried event E1." {
		t.Fatalf("voice replay = %q, want exact E1 reply", reply.Message)
	}
	if got := reply.Session.Transcript[len(reply.Session.Transcript)-1].Body; got != "Reply for newer event E2." {
		t.Fatalf("current session transcript was replaced: latest=%q", got)
	}
	emptyReplay := *engine.messageSession
	emptyReplay.ReplayAIMessage = ""
	if got := lastAIMessage(&emptyReplay); got != "" {
		t.Fatalf("empty exact replay fell through to newer reply: got=%q", got)
	}
}

func TestSpeechTurnPassesTranscriptionContextToSTT(t *testing.T) {
	store := newFakeVoiceStore()
	store.route = &CallRoute{SalonID: "salon_1", OwnerUserID: "owner_1", SessionID: "session_phone"}
	engine := newFakeConversationEngine()
	engine.transcriptionContext = conversation.TranscriptionContext{
		Prompt: "Active service names: Classic Manicure; Gel Manicure.",
	}
	engine.messageSession = phoneSessionWithAIReply("What day would you like?", conversation.StatusActive, conversation.OutcomeCollecting)
	stt := &fakeSpeechToTextProvider{text: "Klasos manicure"}
	service := NewService(store, engine, testVoiceConfig(), AIProviders{STT: stt})

	reply, err := service.HandleSpeechTurn(context.Background(), SpeechTurnRequest{
		Provider:         ProviderTwilio,
		ProviderCallID:   "CA123",
		Audio:            []byte("audio"),
		AudioContentType: "audio/wav",
		Payload:          map[string]string{"RecordingSid": "RE123"},
	})
	if err != nil {
		t.Fatalf("HandleSpeechTurn returned error: %v", err)
	}
	if stt.request.Prompt != "Active service names: Classic Manicure; Gel Manicure." {
		t.Fatalf("STT prompt = %q", stt.request.Prompt)
	}
	if stt.request.ContentType != "audio/wav" || string(stt.request.Audio) != "audio" {
		t.Fatalf("STT request = %#v", stt.request)
	}
	if engine.lastMessage != "Klasos manicure" {
		t.Fatalf("conversation message = %q, want transcribed text", engine.lastMessage)
	}
	if reply == nil || !reply.Continue {
		t.Fatalf("reply should continue after STT turn: %#v", reply)
	}
}

func TestSpeechTurnRealtimeFallbackOverrideKeepsRecordingMode(t *testing.T) {
	store := newFakeVoiceStore()
	store.route = &CallRoute{SalonID: "salon_1", OwnerUserID: "owner_1", SessionID: "session_phone"}
	engine := newFakeConversationEngine()
	engine.messageSession = phoneSessionWithAIReply("What service would you like to book?", conversation.StatusActive, conversation.OutcomeCollecting)
	cfg := testVoiceConfig()
	cfg.Twilio.VoiceTransport = InputModeRealtimeStream
	cfg.AI.Provider = ProviderOpenAI
	cfg.AI.OpenAI.APIKey = "openai-key"
	cfg.AI.OpenAI.RealtimeEnabled = true
	cfg.AI.OpenAI.RealtimeModel = "gpt-realtime-2"
	cfg.AI.OpenAI.RealtimeVoice = "alloy"
	service := NewService(store, engine, cfg, AIProviders{Realtime: fakeRealtimeProvider{configured: true}})

	reply, err := service.HandleSpeechTurn(context.Background(), SpeechTurnRequest{
		Provider:          ProviderTwilio,
		ProviderCallID:    "CA123",
		SpeechText:        "I need a manicure.",
		InputModeOverride: InputModeRecording,
		Payload:           map[string]string{"SpeechResult": "I need a manicure.", "voice_fallback_mode": InputModeRecording},
	})
	if err != nil {
		t.Fatalf("HandleSpeechTurn returned error: %v", err)
	}
	if reply.InputMode != InputModeRecording {
		t.Fatalf("reply input mode = %q, want recording fallback", reply.InputMode)
	}
}

func TestSpeechTurnEndsOnlyAfterConversationReturnsPOSConfirmedBooking(t *testing.T) {
	store := newFakeVoiceStore()
	store.route = &CallRoute{SalonID: "salon_1", OwnerUserID: "owner_1", SessionID: "session_phone"}
	engine := newFakeConversationEngine()
	engine.messageSession = phoneSessionWithAIReply("You are confirmed in Square Appointments for Wed Jun 10 at 10:00 AM.", conversation.StatusCompleted, conversation.OutcomeBookingConfirmed)
	engine.messageSession.Intent = conversation.IntentBooking
	engine.messageSession.BookingAttemptID = "attempt_voice"
	engine.messageSession.AppointmentID = "appointment_voice"
	start := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	engine.messageSession.RequestedStartTime = &start
	service := NewService(store, engine, testVoiceConfig(), AIProviders{})

	reply, err := service.HandleSpeechTurn(context.Background(), SpeechTurnRequest{
		Provider:       ProviderTwilio,
		ProviderCallID: "CA123",
		SpeechText:     "The first one works. My name is Linh Tran and my phone is 312-555-0101.",
		Payload:        map[string]string{"SpeechResult": "The first one works."},
	})
	if err != nil {
		t.Fatalf("HandleSpeechTurn returned error: %v", err)
	}
	if reply.Continue {
		t.Fatalf("reply should stop after confirmed booking")
	}
	if !strings.Contains(reply.Message, "confirmed in Square Appointments") {
		t.Fatalf("reply should use confirmed conversation wording: %q", reply.Message)
	}
	if reply.Session.BookingAttemptID != "attempt_voice" || reply.Session.AppointmentID != "appointment_voice" {
		t.Fatalf("booking linkage = %s/%s, want confirmed attempt and appointment", reply.Session.BookingAttemptID, reply.Session.AppointmentID)
	}
}

func TestRealtimeFallbackMessageRequiresTerminalRealtimeFailure(t *testing.T) {
	store := newFakeVoiceStore()
	store.route = &CallRoute{SalonID: "salon_1", OwnerUserID: "owner_1", SessionID: "session_phone"}
	engine := newFakeConversationEngine()
	engine.messageSession = phoneSessionWithAIReply("What time works for you?", conversation.StatusActive, conversation.OutcomeCollecting)
	service := NewService(store, engine, testVoiceConfig(), AIProviders{})

	message, err := service.RealtimeFallbackMessage(context.Background(), ProviderTwilio, "CA123")
	if err != nil {
		t.Fatalf("RealtimeFallbackMessage returned error: %v", err)
	}
	if message != "" {
		t.Fatalf("fallback message without terminal failure = %q, want empty", message)
	}

	store.events = append(store.events, WebhookEvent{
		Provider:       ProviderTwilio,
		ProviderCallID: "CA123",
		CallSessionID:  "session_phone",
		EventType:      EventRealtimeFailed,
		Payload:        map[string]string{"stage": "openai_connect", "terminal": "true"},
	})

	message, err = service.RealtimeFallbackMessage(context.Background(), ProviderTwilio, "CA123")
	if err != nil {
		t.Fatalf("RealtimeFallbackMessage returned error after terminal failure: %v", err)
	}
	if message != "I had an audio issue, but we can continue. What time works for you?" {
		t.Fatalf("fallback message = %q, want resumable approved prompt", message)
	}
	if strings.Contains(strings.ToLower(message), "owner") {
		t.Fatalf("fallback message should not imply an owner handoff: %q", message)
	}
}

func TestHandleUnintelligibleRealtimeInputUsesTypedConversationHandoff(t *testing.T) {
	store := newFakeVoiceStore()
	store.route = &CallRoute{SalonID: "salon_1", OwnerUserID: "owner_1", SessionID: "session_phone"}
	engine := newFakeConversationEngine()
	engine.messageSession = phoneSessionWithAIReply("I'm sorry, the line is too noisy. I'll ask the salon to call you back at this number. This is not a confirmed appointment.", conversation.StatusHandoff, conversation.OutcomeHandoffRequested)
	service := NewService(store, engine, testVoiceConfig(), AIProviders{})

	reply, err := service.HandleUnintelligibleRealtimeInput(context.Background(), ProviderTwilio, "CA123", "session_phone", "item_4")
	if err != nil {
		t.Fatalf("HandleUnintelligibleRealtimeInput returned error: %v", err)
	}
	if reply.Continue || reply.InputMode != InputModeRealtimeStream || !strings.Contains(reply.Message, "call you back") {
		t.Fatalf("reply = %#v", reply)
	}
	if engine.voiceInputCalls != 1 || engine.lastVoiceInputEvent != "voice-input-unintelligible:session_phone" {
		t.Fatalf("typed handoff calls/event = %d/%q", engine.voiceInputCalls, engine.lastVoiceInputEvent)
	}
	if engine.messageCalls != 0 {
		t.Fatalf("voice recovery must not synthesize a customer message, calls = %d", engine.messageCalls)
	}
}

func testVoiceConfig() config.VoiceConfig {
	return config.VoiceConfig{
		Provider:      ProviderTwilio,
		PublicBaseURL: "https://voice.example.com",
		Twilio: config.TwilioVoiceConfig{
			AuthToken:     "secret",
			IncomingPath:  "/api/voice/twilio/incoming",
			TurnPath:      "/api/voice/twilio/turn",
			RecordingPath: "/api/voice/twilio/recording",
		},
	}
}

func phoneSessionWithAIReply(reply string, status string, outcome string) *conversation.Session {
	return &conversation.Session{
		ID:             "session_phone",
		SalonID:        "salon_1",
		Channel:        conversation.ChannelPhone,
		Provider:       ProviderTwilio,
		ProviderCallID: "CA123",
		InboundPhone:   "+13125550101",
		OutboundPhone:  "+13125550102",
		Status:         status,
		Intent:         conversation.IntentUnknown,
		Outcome:        outcome,
		Transcript: []conversation.TranscriptMessage{{
			Speaker: conversation.SpeakerAI,
			Body:    reply,
		}},
	}
}

type fakeVoiceStore struct {
	salon                   *InboundSalon
	route                   *CallRoute
	bookingReadiness        *PhoneBookingReadiness
	events                  []WebhookEvent
	audio                   *AudioOutput
	voiceStatusSalonID      string
	voiceStatusOwnerUserID  string
	voiceStatus             *SalonVoiceStatus
	platformResolverSalonID string
	platformResolverUserID  string
}

func (f *fakeVoiceStore) ResolveSalonOwnerForPlatform(_ context.Context, salonID string, platformUserID string) (string, error) {
	f.platformResolverSalonID = salonID
	f.platformResolverUserID = platformUserID
	return "owner_1", nil
}

func newFakeVoiceStore() *fakeVoiceStore {
	return &fakeVoiceStore{
		salon: &InboundSalon{
			SalonID:                 "salon_1",
			OwnerUserID:             "owner_1",
			SalonName:               "Lotus Nails",
			Phone:                   "+13125550102",
			RecordingEnabled:        true,
			RecordingConsentMessage: defaultRecordingConsentMessage,
		},
		bookingReadiness: &PhoneBookingReadiness{
			Ready:                true,
			AIEnabled:            true,
			SquareConnected:      true,
			SquareSynced:         true,
			TestBookingCancelled: true,
			ServiceCount:         1,
			StaffCount:           1,
			BusinessHoursCount:   6,
			Checks: []ReadinessCheck{
				{Key: "connect_square", Label: "Connect Square Appointments", Complete: true},
				{Key: "sync_square", Label: "Sync Square calendar", Complete: true},
				{Key: "bookable_services", Label: "AI-bookable services", Complete: true},
				{Key: "bookable_staff", Label: "AI-bookable staff", Complete: true},
				{Key: "business_hours", Label: "Business hours", Complete: true},
				{Key: "enable_ai_booking", Label: "Enable AI booking", Complete: true},
			},
		},
		voiceStatus: &SalonVoiceStatus{
			SalonID: "salon_1", Phone: "+13125550102", AIEnabled: true,
			SchedulingAuthority:        booking.SchedulingAuthorityExternalProvider,
			SchedulingAuthorityVersion: 1, BookingMode: "confirmed_booking",
		},
	}
}

func (f *fakeVoiceStore) GetSalonVoiceStatus(ctx context.Context, salonID string, ownerUserID string) (*SalonVoiceStatus, error) {
	f.voiceStatusSalonID = salonID
	f.voiceStatusOwnerUserID = ownerUserID
	if f.voiceStatus == nil {
		return nil, ErrNotFound
	}
	status := *f.voiceStatus
	status.SalonID = salonID
	return &status, nil
}

type fakeSchedulingReadinessProvider struct {
	result scheduling.TargetReadiness
	err    error
}

func (f *fakeSchedulingReadinessProvider) SchedulingTargetReadiness(context.Context, string, string) (scheduling.TargetReadiness, error) {
	return f.result, f.err
}

func readySchedulingTarget(authority string, version int64) scheduling.TargetReadiness {
	return scheduling.TargetReadiness{
		TargetSchedulingAuthority: authority, AuthorityVersion: version,
		Ready: true, AvailabilityReady: true, ExecutionReady: true,
		Checks: []scheduling.TargetReadinessCheck{},
	}
}

func newVoiceStatusService(store *fakeVoiceStore, cfg config.VoiceConfig, selected scheduling.TargetReadiness) *Service {
	service := NewService(store, newFakeConversationEngine(), cfg, AIProviders{})
	owner := readySchedulingTarget(booking.SchedulingAuthorityOwnerManual, selected.AuthorityVersion)
	owner.ExecutionReady = false
	owner.ExecutionBlockers = []scheduling.TargetReadinessBlocker{{Code: "OWNER_MANUAL_REQUEST_ONLY", Scope: "executor", Message: "Owner review is required."}}
	internal := readySchedulingTarget(booking.SchedulingAuthorityManleAICalendar, selected.AuthorityVersion)
	external := readySchedulingTarget(booking.SchedulingAuthorityExternalProvider, selected.AuthorityVersion)
	switch selected.TargetSchedulingAuthority {
	case booking.SchedulingAuthorityOwnerManual:
		owner = selected
	case booking.SchedulingAuthorityManleAICalendar:
		internal = selected
	case booking.SchedulingAuthorityExternalProvider:
		external = selected
	}
	service.SetSchedulingReadinessProviders(
		&fakeSchedulingReadinessProvider{result: owner},
		&fakeSchedulingReadinessProvider{result: internal},
		&fakeSchedulingReadinessProvider{result: external},
	)
	return service
}

type fakeTurnContractProvider struct {
	configured       bool
	check            TurnContractCheck
	err              error
	checkCalls       int
	interpretCalls   int
	interpretReply   TurnModelReply
	interpretErr     error
	interpretRequest TurnModelRequest
}

func (f *fakeTurnContractProvider) Name() string { return ProviderOpenAI }

func (f *fakeTurnContractProvider) Configured(ctx context.Context, salonID string) bool {
	return f.configured
}

func (f *fakeTurnContractProvider) InterpretTurn(ctx context.Context, req TurnModelRequest) (TurnModelReply, error) {
	f.interpretCalls++
	f.interpretRequest = req
	return f.interpretReply, f.interpretErr
}

func (f *fakeTurnContractProvider) CheckTurnContract(ctx context.Context, salonID string) (TurnContractCheck, error) {
	f.checkCalls++
	return f.check, f.err
}

func (f *fakeVoiceStore) GetPhoneBookingReadiness(ctx context.Context, salonID string, ownerUserID string) (*PhoneBookingReadiness, error) {
	if f.bookingReadiness == nil {
		return nil, ErrNotFound
	}
	return f.bookingReadiness, nil
}

func (f *fakeVoiceStore) FindSalonByPhone(ctx context.Context, phone string) (*InboundSalon, error) {
	if f.salon == nil {
		return nil, ErrNotFound
	}
	return f.salon, nil
}

func (f *fakeVoiceStore) FindCallRoute(ctx context.Context, provider string, providerCallID string) (*CallRoute, error) {
	if f.route == nil {
		return nil, ErrNotFound
	}
	return f.route, nil
}

func (f *fakeVoiceStore) RecordWebhookEvent(ctx context.Context, event WebhookEvent) error {
	f.events = append(f.events, event)
	return nil
}

func (f *fakeVoiceStore) HasTerminalRealtimeFailure(ctx context.Context, provider string, providerCallID string, sessionID string) (bool, error) {
	for _, event := range f.events {
		if event.Provider != provider || event.ProviderCallID != providerCallID || event.EventType != EventRealtimeFailed {
			continue
		}
		if sessionID != "" && event.CallSessionID != "" && event.CallSessionID != sessionID {
			continue
		}
		if event.Payload["terminal"] == "true" || strings.EqualFold(event.Payload["StreamEvent"], "stream-error") || strings.EqualFold(event.Payload["stream_event"], "stream-error") {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeVoiceStore) SaveAudioOutput(ctx context.Context, record AudioOutputRecord) (*AudioOutput, error) {
	f.audio = &AudioOutput{ID: "audio_1", ContentType: record.ContentType, Audio: record.Audio}
	return f.audio, nil
}

func (f *fakeVoiceStore) GetAudioOutput(ctx context.Context, id string) (*AudioOutput, error) {
	if f.audio == nil || f.audio.ID != id {
		return nil, ErrNotFound
	}
	return f.audio, nil
}

type fakeConversationEngine struct {
	startCalls           int
	messageCalls         int
	voiceInputCalls      int
	prewarmCalls         int
	startRequest         conversation.StartPhoneCallRequest
	startSalonID         string
	startOwnerUserID     string
	lastMessage          string
	lastVoiceInputEvent  string
	prewarmSalonID       string
	startSession         *conversation.Session
	messageSession       *conversation.Session
	transcriptionContext conversation.TranscriptionContext
	prewarmDone          chan struct{}
	messageTimings       []conversation.TurnTiming
}

type fakeRealtimeProvider struct {
	configured bool
}

func (f fakeRealtimeProvider) Name() string {
	return ProviderOpenAI
}

func (f fakeRealtimeProvider) Configured(ctx context.Context, salonID string) bool {
	return f.configured
}

func (f fakeRealtimeProvider) ConnectRealtime(ctx context.Context, salonID string, opts RealtimeSessionOptions) (RealtimeSession, error) {
	return nil, ErrProviderDisabled
}

type fakeSpeechToTextProvider struct {
	text       string
	configured bool
	request    SpeechToTextRequest
}

func (f *fakeSpeechToTextProvider) Name() string {
	return ProviderOpenAI
}

func (f *fakeSpeechToTextProvider) Configured(ctx context.Context, salonID string) bool {
	if f.configured {
		return true
	}
	return f.text != ""
}

func (f *fakeSpeechToTextProvider) Transcribe(ctx context.Context, salonID string, req SpeechToTextRequest) (string, error) {
	f.request = req
	return f.text, nil
}

func newFakeConversationEngine() *fakeConversationEngine {
	return &fakeConversationEngine{
		startSession: phoneSessionWithAIReply("Thank you for calling. How can I help you today?", conversation.StatusActive, conversation.OutcomeCollecting),
	}
}

func (f *fakeConversationEngine) StartPhoneCall(ctx context.Context, salonID string, ownerUserID string, req conversation.StartPhoneCallRequest) (*conversation.Session, error) {
	f.startCalls++
	f.startSalonID = salonID
	f.startOwnerUserID = ownerUserID
	f.startRequest = req
	return f.startSession, nil
}

func (f *fakeConversationEngine) PrewarmAnswerContext(ctx context.Context, salonID string) error {
	f.prewarmCalls++
	f.prewarmSalonID = salonID
	if f.prewarmDone != nil {
		select {
		case f.prewarmDone <- struct{}{}:
		default:
		}
	}
	return nil
}

func (f *fakeConversationEngine) Message(ctx context.Context, salonID string, ownerUserID string, sessionID string, req conversation.MessageRequest) (*conversation.Session, error) {
	f.messageCalls++
	f.lastMessage = req.Message
	for _, timing := range f.messageTimings {
		if req.TimingRecorder != nil {
			req.TimingRecorder(timing)
		}
	}
	if f.messageSession != nil {
		return f.messageSession, nil
	}
	return phoneSessionWithAIReply("I can help with appointments. What service would you like to book?", conversation.StatusActive, conversation.OutcomeCollecting), nil
}

func (f *fakeConversationEngine) HandleUnintelligibleVoiceInput(ctx context.Context, salonID string, ownerUserID string, sessionID string, req conversation.VoiceInputHandoffRequest) (*conversation.Session, error) {
	f.voiceInputCalls++
	f.lastVoiceInputEvent = req.EventKey
	if f.messageSession != nil {
		return f.messageSession, nil
	}
	return f.startSession, nil
}

func (f *fakeConversationEngine) Get(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*conversation.Session, error) {
	if f.messageSession != nil {
		return f.messageSession, nil
	}
	return f.startSession, nil
}

func (f *fakeConversationEngine) TranscriptionContext(ctx context.Context, salonID string, ownerUserID string, sessionID string) (conversation.TranscriptionContext, error) {
	return f.transcriptionContext, nil
}
