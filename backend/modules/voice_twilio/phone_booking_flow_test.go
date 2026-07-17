package voice_twilio

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

func TestSignedTwilioWebhookDrivesPhoneBookingFlowThroughConversation(t *testing.T) {
	adapter := NewAdapter(config.TwilioVoiceConfig{
		AuthToken:     "secret",
		IncomingPath:  "/api/voice/twilio/incoming",
		TurnPath:      "/api/voice/twilio/turn",
		RecordingPath: "/api/voice/twilio/recording",
	}, "")
	conversationStore := newPhoneFlowConversationStore()
	bookingTool := &phoneFlowBookingTool{
		attempt: &booking.BookingAttempt{
			ID:           "attempt_voice",
			Status:       booking.StatusConfirmed,
			POSBookingID: "square_booking_voice",
			Appointment:  &booking.Appointment{ID: "appointment_voice", Status: booking.StatusConfirmed},
		},
	}
	conversationService := conversation.NewService(conversationStore, bookingTool)
	voiceStore := newPhoneFlowVoiceStore(conversationStore)
	voiceService := voice.NewService(voiceStore, conversationService, config.VoiceConfig{
		Provider: voice.ProviderTwilio,
		Twilio: config.TwilioVoiceConfig{
			AuthToken:     "secret",
			IncomingPath:  "/api/voice/twilio/incoming",
			TurnPath:      "/api/voice/twilio/turn",
			RecordingPath: "/api/voice/twilio/recording",
		},
	}, voice.AIProviders{})
	app := testTwilioApp(adapter, voiceService)

	incoming := url.Values{
		"CallSid": {"CA_FLOW"},
		"From":    {"+13125550199"},
		"To":      {"+13125550101"},
	}
	incomingRes := signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/incoming", incoming)
	incomingBody := readBody(t, incomingRes)
	if incomingRes.StatusCode != fiber.StatusOK {
		t.Fatalf("incoming status = %d, body = %s", incomingRes.StatusCode, incomingBody)
	}
	if !strings.Contains(incomingBody, "<Gather") || !strings.Contains(incomingBody, "Thank you for calling") {
		t.Fatalf("incoming should return gather greeting: %s", incomingBody)
	}

	firstTurn := url.Values{
		"CallSid":      {"CA_FLOW"},
		"From":         {"+13125550199"},
		"To":           {"+13125550101"},
		"SpeechResult": {"I need a classic manicure on 2026-06-10."},
	}
	firstTurnRes := signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/turn", firstTurn)
	firstTurnBody := readBody(t, firstTurnRes)
	if firstTurnRes.StatusCode != fiber.StatusOK {
		t.Fatalf("first turn status = %d, body = %s", firstTurnRes.StatusCode, firstTurnBody)
	}
	if !strings.Contains(firstTurnBody, "<Gather") || !strings.Contains(firstTurnBody, "I have openings") || !strings.Contains(firstTurnBody, "10:00 AM or 11:00 AM") {
		t.Fatalf("first turn should offer available slots: %s", firstTurnBody)
	}
	if strings.Contains(firstTurnBody, "available technician") || strings.Contains(firstTurnBody, "Mai Nguyen") {
		t.Fatalf("availability offer should avoid generic technician wording and assigned technician names for anyone mode: %s", firstTurnBody)
	}
	if strings.Contains(strings.ToLower(firstTurnBody), "confirmed") {
		t.Fatalf("availability offer must not confirm booking: %s", firstTurnBody)
	}
	if bookingTool.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1", bookingTool.availabilityCalls)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking calls = %d, want 0 before slot selection", bookingTool.calls)
	}

	secondTurn := url.Values{
		"CallSid":      {"CA_FLOW"},
		"From":         {"+13125550199"},
		"To":           {"+13125550101"},
		"SpeechResult": {"The first one works. My name is Linh Tran and my phone is 312-555-0101."},
	}
	secondTurnRes := signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/turn", secondTurn)
	secondTurnBody := readBody(t, secondTurnRes)
	if secondTurnRes.StatusCode != fiber.StatusOK {
		t.Fatalf("second turn status = %d, body = %s", secondTurnRes.StatusCode, secondTurnBody)
	}
	if !strings.Contains(secondTurnBody, "<Gather") || !strings.Contains(secondTurnBody, "Let me review everything") || !strings.Contains(secondTurnBody, "Would you like me to book it") {
		t.Fatalf("second turn should request final review authorization: %s", secondTurnBody)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking calls = %d, want 0 before final review authorization", bookingTool.calls)
	}

	thirdTurn := url.Values{
		"CallSid":      {"CA_FLOW"},
		"From":         {"+13125550199"},
		"To":           {"+13125550101"},
		"SpeechResult": {"Yes."},
	}
	thirdTurnRes := signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/turn", thirdTurn)
	thirdTurnBody := readBody(t, thirdTurnRes)
	if thirdTurnRes.StatusCode != fiber.StatusOK {
		t.Fatalf("third turn status = %d, body = %s", thirdTurnRes.StatusCode, thirdTurnBody)
	}
	if strings.Contains(thirdTurnBody, "<Gather") {
		t.Fatalf("confirmed booking should end gather loop: %s", thirdTurnBody)
	}
	if strings.Contains(thirdTurnBody, "Let me review everything") {
		t.Fatalf("natural review authorization should not repeat the final review: %s", thirdTurnBody)
	}
	if !strings.Contains(thirdTurnBody, "confirmed with Lotus Nails") || strings.Contains(thirdTurnBody, "available technician") || strings.Contains(thirdTurnBody, "Mai Nguyen") || !strings.Contains(thirdTurnBody, "under Linh Tran") || !strings.Contains(thirdTurnBody, "<Hangup/>") {
		t.Fatalf("third turn should return final confirmed TwiML: %s", thirdTurnBody)
	}
	if strings.Contains(strings.ToLower(thirdTurnBody), "square") || strings.Contains(strings.ToLower(thirdTurnBody), "provider") || strings.Contains(strings.ToLower(thirdTurnBody), "pos") {
		t.Fatalf("confirmed TwiML should not expose provider internals: %s", thirdTurnBody)
	}
	if bookingTool.calls != 1 {
		t.Fatalf("booking calls = %d, want 1 after slot selection", bookingTool.calls)
	}
	if bookingTool.request.CustomerName != "Linh Tran" {
		t.Fatalf("booking customer name = %q, want Linh Tran", bookingTool.request.CustomerName)
	}
	if bookingTool.request.Source != booking.SourceAIVoiceCall {
		t.Fatalf("booking source = %s, want %s", bookingTool.request.Source, booking.SourceAIVoiceCall)
	}
	if bookingTool.request.StaffSelectionMode != booking.StaffSelectionAnyone {
		t.Fatalf("booking staff selection mode = %s, want anyone", bookingTool.request.StaffSelectionMode)
	}
	if got := bookingTool.request.Segments; len(got) != 1 || got[0].ServiceID != "service_1" || got[0].StaffID != "staff_1" || got[0].StaffSelectionMode != booking.StaffSelectionAnyone {
		t.Fatalf("booking segments = %#v, want selected staff assignment with anyone mode", got)
	}
	if bookingTool.request.StaffID != "staff_1" || !bookingTool.request.StartTime.Equal(phoneFlowFirstSlotStart()) {
		t.Fatalf("booking request = %#v, want selected offered slot", bookingTool.request)
	}
	if len(voiceStore.events) != 4 {
		t.Fatalf("webhook events = %#v, want incoming plus three speech turns", voiceStore.events)
	}
}

func TestGoldenPhoneFlowKeepsOfferedSlotsWhenCallerAsksAvailabilityDuringUnconfirmedCorrection(t *testing.T) {
	adapter := NewAdapter(config.TwilioVoiceConfig{AuthToken: "secret", IncomingPath: "/api/voice/twilio/incoming", TurnPath: "/api/voice/twilio/turn", RecordingPath: "/api/voice/twilio/recording"}, "")
	conversationStore := newPhoneFlowConversationStore()
	bookingTool := &phoneFlowBookingTool{}
	conversationService := conversation.NewService(conversationStore, bookingTool)
	voiceStore := newPhoneFlowVoiceStore(conversationStore)
	voiceService := voice.NewService(voiceStore, conversationService, config.VoiceConfig{Provider: voice.ProviderTwilio, Twilio: config.TwilioVoiceConfig{AuthToken: "secret", IncomingPath: "/api/voice/twilio/incoming", TurnPath: "/api/voice/twilio/turn", RecordingPath: "/api/voice/twilio/recording"}}, voice.AIProviders{})
	app := testTwilioApp(adapter, voiceService)
	base := url.Values{"CallSid": {"CA_AVAILABILITY_GOLDEN"}, "From": {"+13125550199"}, "To": {"+13125550101"}}

	if res := signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/incoming", base); res.StatusCode != fiber.StatusOK {
		t.Fatalf("incoming status = %d", res.StatusCode)
	}
	book := cloneURLValues(base)
	book.Set("SpeechResult", "Classic Manicure on 2026-06-10.")
	bookBody := readBody(t, signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/turn", book))
	if !strings.Contains(bookBody, "I have openings") || bookingTool.availabilityCalls != 1 {
		t.Fatalf("initial availability offer failed: body=%s calls=%d", bookBody, bookingTool.availabilityCalls)
	}

	correction := cloneURLValues(base)
	correction.Set("SpeechResult", "Could we move it to Friday, June 12 at 3 PM?")
	correctionBody := readBody(t, signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/turn", correction))
	if !strings.Contains(correctionBody, "Did you want to change") || bookingTool.availabilityCalls != 1 {
		t.Fatalf("date correction should wait for confirmation: body=%s calls=%d", correctionBody, bookingTool.availabilityCalls)
	}
	if conversationStore.session.RequestedDate != "2026-06-10" || len(conversationStore.session.OfferedSlots) != 2 {
		t.Fatalf("unconfirmed phone correction mutated draft: %#v", conversationStore.session)
	}

	availability := cloneURLValues(base)
	availability.Set("SpeechResult", "Which appointment slots are still open?")
	availabilityBody := readBody(t, signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/turn", availability))
	if !strings.Contains(availabilityBody, "I have openings") || strings.Contains(availabilityBody, "You currently have") {
		t.Fatalf("availability request did not repeat the active offer: %s", availabilityBody)
	}
	if bookingTool.availabilityCalls != 1 || conversationStore.session.RequestedDate != "2026-06-10" || len(conversationStore.session.OfferedSlots) != 2 {
		t.Fatalf("availability request should preserve offered slots, calls=%d session=%#v", bookingTool.availabilityCalls, conversationStore.session)
	}
}

func cloneURLValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	for key, items := range values {
		cloned[key] = append([]string(nil), items...)
	}
	return cloned
}

func TestSignedTwilioWebhookConsultsThenBooksExplicitCatalogChoice(t *testing.T) {
	adapter := NewAdapter(config.TwilioVoiceConfig{AuthToken: "secret", IncomingPath: "/api/voice/twilio/incoming", TurnPath: "/api/voice/twilio/turn", RecordingPath: "/api/voice/twilio/recording"}, "")
	conversationStore := newPhoneFlowConversationStore()
	conversationStore.services[0].AIDescription = "Nail shaping and regular polish"
	conversationStore.services[0].ConsultationProfile = &conversation.ServiceConsultationProfile{
		Status:                   conversation.ConsultationProfileStatusReady,
		RecommendedOutcomes:      []string{conversation.ConsultationOutcomeMaintain},
		CompatibleCurrentSystems: []string{conversation.ConsultationSystemRegularPolish},
		Revision:                 1,
	}
	conversationStore.services = append(conversationStore.services, conversation.ServiceOption{
		ID: "service_gel", Name: "Gel Manicure", AIDescription: "Nail shaping and gel polish", DurationMinutes: 50, PriceFrom: 45,
		ConsultationProfile: &conversation.ServiceConsultationProfile{
			Status:                   conversation.ConsultationProfileStatusReady,
			RecommendedOutcomes:      []string{conversation.ConsultationOutcomeMaintain},
			CompatibleCurrentSystems: []string{conversation.ConsultationSystemNatural},
			Revision:                 1,
		},
	})
	bookingTool := &phoneFlowBookingTool{attempt: &booking.BookingAttempt{ID: "attempt_consultation", Status: booking.StatusConfirmed, POSBookingID: "square_booking_consultation", Appointment: &booking.Appointment{ID: "appointment_consultation", Status: booking.StatusConfirmed}}}
	conversationService := conversation.NewService(conversationStore, bookingTool)
	conversationService.SetTurnInterpreter(&phoneFlowConsultationInterpreter{})
	voiceStore := newPhoneFlowVoiceStore(conversationStore)
	voiceService := voice.NewService(voiceStore, conversationService, config.VoiceConfig{Provider: voice.ProviderTwilio, Twilio: config.TwilioVoiceConfig{AuthToken: "secret", IncomingPath: "/api/voice/twilio/incoming", TurnPath: "/api/voice/twilio/turn", RecordingPath: "/api/voice/twilio/recording"}}, voice.AIProviders{})
	app := testTwilioApp(adapter, voiceService)

	incoming := url.Values{"CallSid": {"CA_CONSULT"}, "From": {"+13125550199"}, "To": {"+13125550101"}}
	if res := signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/incoming", incoming); res.StatusCode != fiber.StatusOK {
		t.Fatalf("incoming status = %d", res.StatusCode)
	}

	consult := url.Values{"CallSid": {"CA_CONSULT"}, "From": {"+13125550199"}, "To": {"+13125550101"}, "SpeechResult": {"Help me choose between Classic Manicure and Gel Manicure."}}
	consultBody := readBody(t, signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/turn", consult))
	for _, fact := range []string{"Nail shaping and regular polish", "Nail shaping and gel polish", "Which service would you like"} {
		if !strings.Contains(consultBody, fact) {
			t.Fatalf("consultation response missing %q: %s", fact, consultBody)
		}
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("consultation called booking tools: booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}

	ambiguous := url.Values{"CallSid": {"CA_CONSULT"}, "From": {"+13125550199"}, "To": {"+13125550101"}, "SpeechResult": {"Yes."}}
	ambiguousBody := readBody(t, signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/turn", ambiguous))
	if !strings.Contains(ambiguousBody, "Which service would you like") || conversationStore.session.ServiceID != "" {
		t.Fatalf("ambiguous affirmative selected a service: body=%s session=%#v", ambiguousBody, conversationStore.session)
	}

	choose := url.Values{"CallSid": {"CA_CONSULT"}, "From": {"+13125550199"}, "To": {"+13125550101"}, "SpeechResult": {"Gel Manicure."}}
	chooseBody := readBody(t, signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/turn", choose))
	if !strings.Contains(chooseBody, "Would you like help booking Gel Manicure") || bookingTool.availabilityCalls != 0 || conversationStore.session.ServiceID != "" {
		t.Fatalf("explicit consultation choice should await booking intent: body=%s session=%#v", chooseBody, conversationStore.session)
	}

	startBooking := url.Values{"CallSid": {"CA_CONSULT"}, "From": {"+13125550199"}, "To": {"+13125550101"}, "SpeechResult": {"Yes, please book it on 2026-06-10."}}
	startBookingBody := readBody(t, signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/turn", startBooking))
	if !strings.Contains(startBookingBody, "I have openings") || bookingTool.availabilityCalls != 1 || bookingTool.availabilityRequest.ServiceID != "service_gel" {
		t.Fatalf("explicit booking intent did not check gel availability: body=%s request=%#v", startBookingBody, bookingTool.availabilityRequest)
	}

	book := url.Values{"CallSid": {"CA_CONSULT"}, "From": {"+13125550199"}, "To": {"+13125550101"}, "SpeechResult": {"The first one works. My name is Linh Tran and my phone is 312-555-0101."}}
	bookBody := readBody(t, signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/turn", book))
	if !strings.Contains(bookBody, "Let me review everything") || bookingTool.calls != 0 {
		t.Fatalf("consultation booking skipped final review: body=%s calls=%d", bookBody, bookingTool.calls)
	}
	authorize := url.Values{"CallSid": {"CA_CONSULT"}, "From": {"+13125550199"}, "To": {"+13125550101"}, "SpeechResult": {"Yes."}}
	authorizeBody := readBody(t, signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/turn", authorize))
	if !strings.Contains(authorizeBody, "confirmed with Lotus Nails") || bookingTool.calls != 1 || bookingTool.request.ServiceID != "service_gel" {
		t.Fatalf("consultation booking did not confirm selected catalog service after review: body=%s request=%#v", authorizeBody, bookingTool.request)
	}
}

type phoneFlowConsultationInterpreter struct {
	calls int
}

func (f *phoneFlowConsultationInterpreter) InterpretTurn(ctx context.Context, req conversation.TurnInterpretationRequest) (conversation.TurnUnderstanding, error) {
	f.calls++
	if req.SemanticContract != conversation.TurnSemanticContractGuidance && req.CurrentBookingStage != conversation.DialogPhaseConsultation {
		// This fixture follows the production semantic contract: consultation is
		// state-scoped and must not be reasserted after the caller has entered the
		// ordinary booking draft. Later booking turns may rely on deterministic
		// extraction while retaining a booking goal.
		return conversation.TurnUnderstanding{Goal: "book_appointment", Confidence: 0.97}, nil
	}
	turn := conversation.TurnUnderstanding{
		Goal: "consultation", Confidence: 0.97,
		Consultation: conversation.ConsultationNeedProfile{
			DesiredOutcome: conversation.ConsultationOutcomeCompare, ComparedServiceIDs: []string{"service_1", "service_gel"}, Confidence: 0.97,
		},
		ConsultationMutations: []conversation.ConsultationNeedMutation{
			{Field: conversation.ConsultationNeedFieldDesiredOutcome, Operation: conversation.ConsultationNeedOperationSet, Values: []string{conversation.ConsultationOutcomeCompare}, Confidence: 0.97},
			{Field: conversation.ConsultationNeedFieldComparedServiceIDs, Operation: conversation.ConsultationNeedOperationSet, Values: []string{"service_1", "service_gel"}, Confidence: 0.97},
		},
	}
	if req.SemanticContract == conversation.TurnSemanticContractGuidance {
		turn.GuidanceAction = conversation.GuidanceActionConsultation
	}
	return turn, nil
}

type phoneFlowConversationStore struct {
	cfg          conversation.RuntimeConfig
	session      conversation.Session
	services     []conversation.ServiceOption
	aliases      []conversation.ServiceAlias
	catAliases   []conversation.ServiceCategoryAlias
	staff        []conversation.StaffOption
	activeStaff  []conversation.StaffOption
	knowledge    []conversation.KnowledgeSnippet
	eventKeys    map[string]bool
	eventReplies map[string]string
}

func newPhoneFlowConversationStore() *phoneFlowConversationStore {
	return &phoneFlowConversationStore{
		cfg: conversation.RuntimeConfig{
			SalonName:           "Lotus Nails",
			Timezone:            "America/Chicago",
			AIEnabled:           true,
			HandoffEnabled:      true,
			ConsultationEnabled: true,
			AIGreeting:          "Thank you for calling. How can I help you today?",
		},
		services: []conversation.ServiceOption{{
			ID:              "service_1",
			Name:            "Classic Manicure",
			DurationMinutes: 45,
			PriceFrom:       35,
		}},
		staff: []conversation.StaffOption{{
			ID:         "staff_1",
			Name:       "Mai Nguyen",
			AIBookable: true,
		}},
		eventKeys:    map[string]bool{},
		eventReplies: map[string]string{},
	}
}

func (f *phoneFlowConversationStore) GetRuntimeConfig(ctx context.Context, salonID string, ownerUserID string) (*conversation.RuntimeConfig, error) {
	return &f.cfg, nil
}

func (f *phoneFlowConversationStore) GetAnswerContextFence(ctx context.Context, salonID string) (conversation.AnswerContextFence, error) {
	return conversation.AnswerContextFence{
		ActiveProvider:     "square",
		ConnectionStatus:   "active",
		LocationID:         "location-phone-flow",
		SnapshotGeneration: 1,
		LastSyncAtRFC3339:  "2026-06-10T14:00:00Z",
		Ready:              true,
	}, nil
}

func (f *phoneFlowConversationStore) CreateSession(ctx context.Context, record conversation.NewSessionRecord) (*conversation.Session, error) {
	now := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	f.session = conversation.Session{
		ID:             "session_phone_flow",
		SalonID:        record.SalonID,
		Channel:        record.Channel,
		Provider:       record.Provider,
		ProviderCallID: record.ProviderCallID,
		InboundPhone:   record.InboundPhone,
		OutboundPhone:  record.OutboundPhone,
		Status:         conversation.StatusActive,
		Intent:         conversation.IntentUnknown,
		Outcome:        conversation.OutcomeCollecting,
		CustomerPhone:  record.CustomerPhone,
		DialogState: conversation.DialogState{
			Version:        conversation.DialogStateVersion,
			Phase:          conversation.DialogPhaseOpen,
			ReviewRequired: true,
		},
		StartedAt: now,
		CreatedAt: now,
		UpdatedAt: now,
		Transcript: []conversation.TranscriptMessage{{
			ID:        "msg_1",
			SessionID: "session_phone_flow",
			SalonID:   record.SalonID,
			Speaker:   conversation.SpeakerAI,
			Body:      record.InitialReply,
			Sequence:  1,
			CreatedAt: now,
		}},
	}
	return f.copySession(), nil
}

func (f *phoneFlowConversationStore) GetSessionForOwner(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*conversation.Session, error) {
	return f.copySession(), nil
}

func (f *phoneFlowConversationStore) GetSessionByTurnEventKey(ctx context.Context, salonID string, ownerUserID string, sessionID string, eventKey string) (*conversation.Session, bool, error) {
	if f.eventKeys[eventKey] {
		session := f.copySession()
		session.ReplayEventKey = eventKey
		session.ReplayAIMessage = f.eventReplies[eventKey]
		return session, true, nil
	}
	return nil, false, nil
}

func (f *phoneFlowConversationStore) ListSessions(ctx context.Context, salonID string, ownerUserID string, lifecycleStatus string, limit int, offset int) ([]conversation.Session, error) {
	session := f.session
	return []conversation.Session{session}, nil
}

func (f *phoneFlowConversationStore) ListWebhookEvents(ctx context.Context, salonID string, ownerUserID string, sessionID string, limit int, offset int) ([]conversation.WebhookEventLog, error) {
	return []conversation.WebhookEventLog{}, nil
}

func (f *phoneFlowConversationStore) ArchiveSession(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*conversation.Session, error) {
	session := f.session
	session.LifecycleStatus = conversation.LifecycleArchived
	f.session = session
	return f.copySession(), nil
}

func (f *phoneFlowConversationStore) RedactSession(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*conversation.Session, error) {
	session := f.session
	session.LifecycleStatus = conversation.LifecycleRedacted
	session.CustomerName = ""
	session.CustomerPhone = ""
	session.CustomerEmail = ""
	f.session = session
	return f.copySession(), nil
}

func (f *phoneFlowConversationStore) ListBookableServices(ctx context.Context, salonID string) ([]conversation.ServiceOption, error) {
	return f.services, nil
}

func (f *phoneFlowConversationStore) ListActiveServiceAliases(ctx context.Context, salonID string) ([]conversation.ServiceAlias, error) {
	return f.aliases, nil
}

func (f *phoneFlowConversationStore) ListActiveServiceCategoryAliases(ctx context.Context, salonID string) ([]conversation.ServiceCategoryAlias, error) {
	return f.catAliases, nil
}

func (f *phoneFlowConversationStore) ListBookableStaff(ctx context.Context, salonID string) ([]conversation.StaffOption, error) {
	return f.staff, nil
}

func (f *phoneFlowConversationStore) ListActiveStaff(ctx context.Context, salonID string) ([]conversation.StaffOption, error) {
	if f.activeStaff != nil {
		return f.activeStaff, nil
	}
	staff := append([]conversation.StaffOption(nil), f.staff...)
	for i := range staff {
		staff[i].AIBookable = true
	}
	return staff, nil
}

func (f *phoneFlowConversationStore) ListStaffAssignmentStats(ctx context.Context, salonID string, staffIDs []string, from time.Time, to time.Time) (map[string]conversation.StaffAssignmentStat, error) {
	out := make(map[string]conversation.StaffAssignmentStat, len(staffIDs))
	for _, staffID := range staffIDs {
		out[staffID] = conversation.StaffAssignmentStat{StaffID: staffID}
	}
	return out, nil
}

func (f *phoneFlowConversationStore) ListActiveKnowledge(ctx context.Context, salonID string) ([]conversation.KnowledgeSnippet, error) {
	return f.knowledge, nil
}

func (f *phoneFlowConversationStore) ListBusinessHourPeriods(ctx context.Context, salonID string) ([]conversation.BusinessHourPeriod, error) {
	return []conversation.BusinessHourPeriod{}, nil
}

func (f *phoneFlowConversationStore) ListPartyBookingRequests(ctx context.Context, salonID string, ownerUserID string, status string, limit int, offset int) ([]conversation.PartyBookingRequest, error) {
	return []conversation.PartyBookingRequest{}, nil
}

func (f *phoneFlowConversationStore) UpdatePartyBookingRequestStatus(ctx context.Context, salonID string, ownerUserID string, requestID string, status string) (*conversation.PartyBookingRequest, error) {
	return nil, conversation.ErrNotFound
}

func (f *phoneFlowConversationStore) SaveTurn(ctx context.Context, record conversation.TurnRecord) (*conversation.Session, error) {
	if record.EventKey != "" {
		f.eventKeys[record.EventKey] = true
		f.eventReplies[record.EventKey] = record.AIMessage
	}
	session := record.Session
	session.Status = record.Update.Status
	session.Intent = record.Update.Intent
	session.Outcome = record.Update.Outcome
	session.CustomerName = record.Update.CustomerName
	session.CustomerPhone = record.Update.CustomerPhone
	session.CustomerEmail = record.Update.CustomerEmail
	session.ServiceID = record.Update.ServiceID
	session.StaffID = record.Update.StaffID
	session.StaffSelectionMode = record.Update.StaffSelectionMode
	for _, service := range f.services {
		if service.ID == session.ServiceID {
			session.ServiceName = service.Name
		}
	}
	for _, member := range f.staff {
		if member.ID == session.StaffID {
			session.StaffName = member.Name
		}
	}
	session.RequestedStartTime = record.Update.RequestedStartTime
	session.RequestedDate = record.Update.RequestedDate
	session.OfferedSlots = record.Update.OfferedSlots
	session.BookingSegments = append([]booking.BookingSegmentRequest(nil), record.Update.BookingSegments...)
	session.DialogState = record.Update.DialogState
	session.BookingAttemptID = record.Update.BookingAttemptID
	session.AppointmentID = record.Update.AppointmentID
	session.Summary = record.Update.Summary
	if record.Update.EndSession {
		session.Status = record.Update.Status
		endedAt := time.Date(2026, 6, 10, 14, 5, 0, 0, time.UTC)
		session.EndedAt = &endedAt
	}
	nextSequence := len(session.Transcript) + 1
	session.Transcript = append(session.Transcript, conversation.TranscriptMessage{
		ID:        "msg_customer",
		SessionID: session.ID,
		SalonID:   session.SalonID,
		Speaker:   conversation.SpeakerCustomer,
		Body:      record.CustomerMessage,
		Metadata:  record.CustomerMetadata,
		Sequence:  nextSequence,
		CreatedAt: time.Date(2026, 6, 10, 14, 1, 0, 0, time.UTC),
	})
	if strings.TrimSpace(record.ToolMessage) != "" {
		nextSequence++
		session.Transcript = append(session.Transcript, conversation.TranscriptMessage{
			ID:        "msg_tool",
			SessionID: session.ID,
			SalonID:   session.SalonID,
			Speaker:   conversation.SpeakerTool,
			Body:      record.ToolMessage,
			Metadata:  record.ToolMetadata,
			Sequence:  nextSequence,
			CreatedAt: time.Date(2026, 6, 10, 14, 1, 5, 0, time.UTC),
		})
	}
	nextSequence++
	session.Transcript = append(session.Transcript, conversation.TranscriptMessage{
		ID:        "msg_ai",
		SessionID: session.ID,
		SalonID:   session.SalonID,
		Speaker:   conversation.SpeakerAI,
		Body:      record.AIMessage,
		Metadata:  record.AIMetadata,
		Sequence:  nextSequence,
		CreatedAt: time.Date(2026, 6, 10, 14, 1, 10, 0, time.UTC),
	})
	f.session = session
	return f.copySession(), nil
}

func (f *phoneFlowConversationStore) copySession() *conversation.Session {
	session := f.session
	session.Transcript = append([]conversation.TranscriptMessage(nil), f.session.Transcript...)
	session.OfferedSlots = append([]conversation.OfferedSlot(nil), f.session.OfferedSlots...)
	session.BookingSegments = append([]booking.BookingSegmentRequest(nil), f.session.BookingSegments...)
	return &session
}

type phoneFlowBookingTool struct {
	calls               int
	availabilityCalls   int
	request             booking.CreateBookingRequest
	availabilityRequest booking.AvailabilityRequest
	attempt             *booking.BookingAttempt
}

func (f *phoneFlowBookingTool) AvailableSlots(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest) (*booking.AvailabilityResult, error) {
	f.availabilityCalls++
	f.availabilityRequest = req
	serviceID := strings.TrimSpace(req.ServiceID)
	serviceName := "Classic Manicure"
	durationMinutes := 45
	if serviceID == "service_gel" {
		serviceName = "Gel Manicure"
		durationMinutes = 50
	}
	return &booking.AvailabilityResult{
		ServiceID:          serviceID,
		ServiceName:        serviceName,
		StaffSelectionMode: req.StaffSelectionMode,
		Segments: []booking.AvailabilitySegment{
			{
				ServiceID:          serviceID,
				ServiceName:        serviceName,
				StaffID:            "staff_1",
				StaffName:          "Mai Nguyen",
				StaffSelectionMode: req.StaffSelectionMode,
				DurationMinutes:    durationMinutes,
			},
		},
		PreferredDate:   req.PreferredDate,
		DurationMinutes: durationMinutes,
		Timezone:        "America/Chicago",
		Slots: []booking.AvailabilitySlot{
			{
				StartTime:          phoneFlowFirstSlotStart(),
				EndTime:            phoneFlowFirstSlotStart().Add(time.Duration(durationMinutes) * time.Minute),
				StaffID:            "staff_1",
				StaffName:          "Mai Nguyen",
				StaffSelectionMode: req.StaffSelectionMode,
				Segments: []booking.AvailabilitySegment{
					{
						ServiceID:          serviceID,
						ServiceName:        serviceName,
						StaffID:            "staff_1",
						StaffName:          "Mai Nguyen",
						StaffSelectionMode: req.StaffSelectionMode,
						DurationMinutes:    durationMinutes,
					},
				},
			},
			{
				StartTime:          phoneFlowFirstSlotStart().Add(time.Hour),
				EndTime:            phoneFlowFirstSlotStart().Add(time.Hour + time.Duration(durationMinutes)*time.Minute),
				StaffID:            "staff_1",
				StaffName:          "Mai Nguyen",
				StaffSelectionMode: req.StaffSelectionMode,
				Segments: []booking.AvailabilitySegment{
					{
						ServiceID:          serviceID,
						ServiceName:        serviceName,
						StaffID:            "staff_1",
						StaffName:          "Mai Nguyen",
						StaffSelectionMode: req.StaffSelectionMode,
						DurationMinutes:    durationMinutes,
					},
				},
			},
		},
	}, nil
}

func (f *phoneFlowBookingTool) Create(ctx context.Context, salonID string, ownerUserID string, req booking.CreateBookingRequest) (*booking.BookingAttempt, error) {
	f.calls++
	f.request = req
	return f.attempt, nil
}

func (f *phoneFlowBookingTool) Cancel(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req booking.CancelRequest) (*booking.Appointment, *booking.BookingAttempt, error) {
	return &booking.Appointment{ID: appointmentID, Status: booking.StatusCancelled}, nil, nil
}

func (f *phoneFlowBookingTool) RescheduleCandidates(ctx context.Context, salonID string, ownerUserID string, req booking.RescheduleLookupRequest) ([]booking.AppointmentActionRef, error) {
	return nil, nil
}

func (f *phoneFlowBookingTool) Reschedule(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req booking.RescheduleRequest) (*booking.Appointment, *booking.BookingAttempt, error) {
	return nil, nil, nil
}

func phoneFlowFirstSlotStart() time.Time {
	return time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
}

type phoneFlowVoiceStore struct {
	conversationStore *phoneFlowConversationStore
	salon             *voice.InboundSalon
	events            []voice.WebhookEvent
}

func newPhoneFlowVoiceStore(conversationStore *phoneFlowConversationStore) *phoneFlowVoiceStore {
	return &phoneFlowVoiceStore{
		conversationStore: conversationStore,
		salon: &voice.InboundSalon{
			SalonID:                 "salon_1",
			OwnerUserID:             "owner_1",
			SalonName:               "Lotus Nails",
			Phone:                   "+13125550101",
			RecordingEnabled:        true,
			RecordingConsentMessage: "This call may be recorded to help us manage appointments and improve service.",
		},
	}
}

func (f *phoneFlowVoiceStore) GetSalonVoiceStatus(ctx context.Context, salonID string, ownerUserID string) (*voice.SalonVoiceStatus, error) {
	return &voice.SalonVoiceStatus{SalonID: salonID, Phone: "+13125550101"}, nil
}

func (f *phoneFlowVoiceStore) GetPhoneBookingReadiness(ctx context.Context, salonID string, ownerUserID string) (*voice.PhoneBookingReadiness, error) {
	return &voice.PhoneBookingReadiness{
		Ready:              true,
		AIEnabled:          true,
		SquareConnected:    true,
		SquareSynced:       true,
		ServiceCount:       1,
		StaffCount:         1,
		BusinessHoursCount: 6,
	}, nil
}

func (f *phoneFlowVoiceStore) FindSalonByPhone(ctx context.Context, phone string) (*voice.InboundSalon, error) {
	if strings.TrimSpace(phone) == "" {
		return nil, voice.ErrNotFound
	}
	return f.salon, nil
}

func (f *phoneFlowVoiceStore) FindCallRoute(ctx context.Context, provider string, providerCallID string) (*voice.CallRoute, error) {
	session := f.conversationStore.session
	if session.ID == "" || session.Provider != provider || session.ProviderCallID != providerCallID {
		return nil, voice.ErrNotFound
	}
	return &voice.CallRoute{SalonID: session.SalonID, OwnerUserID: "owner_1", SessionID: session.ID}, nil
}

func (f *phoneFlowVoiceStore) RecordWebhookEvent(ctx context.Context, event voice.WebhookEvent) error {
	f.events = append(f.events, event)
	return nil
}

func (f *phoneFlowVoiceStore) HasTerminalRealtimeFailure(ctx context.Context, provider string, providerCallID string, sessionID string) (bool, error) {
	for _, event := range f.events {
		if event.Provider != provider || event.ProviderCallID != providerCallID || event.EventType != voice.EventRealtimeFailed {
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

func (f *phoneFlowVoiceStore) SaveAudioOutput(ctx context.Context, record voice.AudioOutputRecord) (*voice.AudioOutput, error) {
	return &voice.AudioOutput{ID: "audio_1", ContentType: record.ContentType, Audio: record.Audio}, nil
}

func (f *phoneFlowVoiceStore) GetAudioOutput(ctx context.Context, id string) (*voice.AudioOutput, error) {
	return nil, voice.ErrNotFound
}
