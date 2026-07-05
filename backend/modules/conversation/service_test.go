package conversation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

func TestListDefaultsToActiveLifecycle(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, &fakeBookingTool{})

	res, err := service.List(context.Background(), " salon_1 ", " owner_1 ", 0, -10, "")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(res.Sessions) != 1 {
		t.Fatalf("items = %d, want 1", len(res.Sessions))
	}
	if res.Limit != defaultSessionListLimit {
		t.Fatalf("response limit = %d, want %d", res.Limit, defaultSessionListLimit)
	}
	if res.Offset != 0 {
		t.Fatalf("response offset = %d, want 0", res.Offset)
	}
	if res.HasMore {
		t.Fatal("has_more = true, want false")
	}
	if store.listLifecycleStatus != LifecycleActive {
		t.Fatalf("lifecycle filter = %q, want active", store.listLifecycleStatus)
	}
	if store.listLimit != defaultSessionListLimit+1 {
		t.Fatalf("limit = %d, want %d", store.listLimit, defaultSessionListLimit+1)
	}
	if store.listOffset != 0 {
		t.Fatalf("offset = %d, want 0", store.listOffset)
	}
}

func TestListReturnsPaginationMetadata(t *testing.T) {
	store := newFakeConversationStore()
	store.listSessions = make([]Session, maxSessionListLimit+1)
	for i := range store.listSessions {
		store.listSessions[i] = store.session
	}
	service := NewService(store, &fakeBookingTool{})

	res, err := service.List(context.Background(), "salon_1", "owner_1", 500, 30, LifecycleArchived)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(res.Sessions) != maxSessionListLimit {
		t.Fatalf("items = %d, want %d", len(res.Sessions), maxSessionListLimit)
	}
	if res.Limit != maxSessionListLimit {
		t.Fatalf("response limit = %d, want %d", res.Limit, maxSessionListLimit)
	}
	if res.Offset != 30 {
		t.Fatalf("response offset = %d, want 30", res.Offset)
	}
	if !res.HasMore {
		t.Fatal("has_more = false, want true")
	}
	if store.listLifecycleStatus != LifecycleArchived {
		t.Fatalf("lifecycle filter = %q, want archived", store.listLifecycleStatus)
	}
	if store.listLimit != maxSessionListLimit+1 {
		t.Fatalf("limit = %d, want %d", store.listLimit, maxSessionListLimit+1)
	}
	if store.listOffset != 30 {
		t.Fatalf("offset = %d, want 30", store.listOffset)
	}
}

func TestListRejectsInvalidLifecycle(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, &fakeBookingTool{})

	_, err := service.List(context.Background(), "salon_1", "owner_1", 25, 0, "deleted")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestListWebhookEventsDelegatesToStore(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, &fakeBookingTool{})

	events, err := service.ListWebhookEvents(context.Background(), " salon_1 ", " owner_1 ", " session_1 ", 500)
	if err != nil {
		t.Fatalf("ListWebhookEvents returned error: %v", err)
	}
	if len(events) != 1 || events[0].EventType != "realtime_failed" {
		t.Fatalf("events = %#v", events)
	}
	if store.webhookSessionID != "session_1" {
		t.Fatalf("webhook session = %q, want session_1", store.webhookSessionID)
	}
	if store.webhookLimit != maxWebhookLimit {
		t.Fatalf("webhook limit = %d, want %d", store.webhookLimit, maxWebhookLimit)
	}
}

func TestArchiveSessionDelegatesToStore(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, &fakeBookingTool{})

	session, err := service.Archive(context.Background(), " salon_1 ", " owner_1 ", " session_1 ")
	if err != nil {
		t.Fatalf("Archive returned error: %v", err)
	}
	if session.LifecycleStatus != LifecycleArchived {
		t.Fatalf("lifecycle = %s, want archived", session.LifecycleStatus)
	}
	if store.archivedSessionID != "session_1" {
		t.Fatalf("archived session = %q, want session_1", store.archivedSessionID)
	}
}

func TestRedactSessionDelegatesToStore(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, &fakeBookingTool{})

	session, err := service.Redact(context.Background(), " salon_1 ", " owner_1 ", " session_1 ")
	if err != nil {
		t.Fatalf("Redact returned error: %v", err)
	}
	if session.LifecycleStatus != LifecycleRedacted {
		t.Fatalf("lifecycle = %s, want redacted", session.LifecycleStatus)
	}
	if store.redactedSessionID != "session_1" {
		t.Fatalf("redacted session = %q, want session_1", store.redactedSessionID)
	}
}

func TestInitialReplyIncludesSalonDisclosureAndOpenEndedPrompt(t *testing.T) {
	reply := initialReply(&RuntimeConfig{
		SalonName:  "Lotus Nails Studio",
		AIGreeting: "Thank you for calling. This call may be recorded to help us manage appointments and improve service.",
	})

	if !strings.Contains(reply, "Thank you for calling Lotus Nails Studio.") {
		t.Fatalf("initial reply should identify salon: %s", reply)
	}
	if !strings.Contains(reply, "This call may be recorded") {
		t.Fatalf("initial reply should preserve recording disclosure: %s", reply)
	}
	if !strings.Contains(reply, "How can I help today?") {
		t.Fatalf("initial reply should use open-ended intent collection: %s", reply)
	}
	if strings.Count(reply, "Thank you for calling") != 1 {
		t.Fatalf("initial reply should not double-greet: %s", reply)
	}
}

func TestMessageUsesActiveServiceAliasFromStore(t *testing.T) {
	store := newFakeConversationStore()
	store.services = testManicureCatalog()
	store.serviceAliases = []ServiceAlias{{
		ID:              "alias_1",
		ServiceID:       "service_gel",
		Alias:           "shell manicure",
		NormalizedAlias: "shell manicure",
		Source:          "correction",
		Confidence:      0.97,
	}}
	service := NewService(store, &fakeBookingTool{})

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I want to book a shell manicure.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}

	if session.ServiceID != "service_gel" {
		t.Fatalf("service id = %q, want service_gel", session.ServiceID)
	}
	if got := store.lastTurn.CustomerMetadata["service_understanding_reason"]; got != serviceUnderstandingAlias {
		t.Fatalf("understanding reason = %#v, want service alias", got)
	}
	if got := store.lastTurn.CustomerMetadata["service_understanding_source"]; got != "correction" {
		t.Fatalf("understanding source = %#v, want correction", got)
	}
	if got := store.lastTurn.CustomerMetadata["service_understanding_alias_id"]; got != "alias_1" {
		t.Fatalf("understanding alias id = %#v, want alias_1", got)
	}
}

func TestMessageUsesServiceCategoryAliasAsClarificationNotSelection(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_classic", Name: "Classic Manicure", CategoryID: "cat_mani", CategoryName: "Manicure", CategorySlug: "manicure", DurationMinutes: 30},
		{ID: "service_gel", Name: "Gel Manicure", CategoryID: "cat_mani", CategoryName: "Manicure", CategorySlug: "manicure", DurationMinutes: 45},
		{ID: "service_pedi", Name: "Classic Pedicure", CategoryID: "cat_pedi", CategoryName: "Pedicure", CategorySlug: "pedicure", DurationMinutes: 45},
	}
	store.categoryAliases = []ServiceCategoryAlias{{
		ID:              "cat_alias_mani",
		CategoryID:      "cat_mani",
		CategoryName:    "Manicure",
		Alias:           "mani",
		NormalizedAlias: "mani",
		Source:          "system",
		Confidence:      0.86,
	}}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I want mani next Monday.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.ServiceID != "" {
		t.Fatalf("service id = %q, want category clarification without selecting one service", session.ServiceID)
	}
	if bookingTool.availabilityCalls != 0 || bookingTool.calls != 0 {
		t.Fatalf("category alias should not call booking tools, availability=%d booking=%d", bookingTool.availabilityCalls, bookingTool.calls)
	}
	if got := store.lastTurn.CustomerMetadata["service_understanding_reason"]; got != serviceUnderstandingCategoryAlias {
		t.Fatalf("understanding reason = %#v, want category alias", got)
	}
	if got := store.lastTurn.CustomerMetadata["service_understanding_category_id"]; got != "cat_mani" {
		t.Fatalf("category id metadata = %#v, want cat_mani", got)
	}
	if got := metadataStringSlice(store.lastTurn.AIMetadata, "pending_service_candidate_ids"); !sameStrings(got, []string{"service_classic", "service_gel"}) {
		t.Fatalf("pending service candidates = %#v, want manicure services only", got)
	}
	reply := strings.ToLower(store.lastTurn.AIMessage)
	if !strings.Contains(reply, "classic manicure") || !strings.Contains(reply, "gel manicure") {
		t.Fatalf("reply should ask which manicure service: %s", store.lastTurn.AIMessage)
	}
}

func TestTranscriptionContextIncludesCatalogAliasesAndPendingCandidatesWithoutCustomerPII(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_classic", Name: "Classic Manicure"},
		{ID: "service_gel", Name: "Gel Manicure"},
	}
	store.serviceAliases = []ServiceAlias{{
		ID:          "alias_shell",
		ServiceID:   "service_gel",
		ServiceName: "Gel Manicure",
		Alias:       "shell manicure",
	}}
	store.session.CustomerName = "Linh Tran"
	store.session.CustomerPhone = "3125550101"
	store.session.Transcript = []TranscriptMessage{{
		Speaker: SpeakerAI,
		Body:    "Which manicure service would you like: Classic Manicure or Gel Manicure?",
		Metadata: map[string]any{
			"pending_service_candidate_ids": []string{"service_classic", "service_gel"},
		},
	}}
	service := NewService(store, &fakeBookingTool{})

	context, err := service.TranscriptionContext(context.Background(), "salon_1", "owner_1", "session_1")
	if err != nil {
		t.Fatalf("TranscriptionContext returned error: %v", err)
	}
	if !strings.Contains(context.Prompt, "Active service names: Classic Manicure; Gel Manicure.") {
		t.Fatalf("prompt missing service names: %s", context.Prompt)
	}
	if !strings.Contains(context.Prompt, "shell manicure -> Gel Manicure") {
		t.Fatalf("prompt missing service alias: %s", context.Prompt)
	}
	if !strings.Contains(context.Prompt, "Current service options being clarified: Classic Manicure; Gel Manicure.") {
		t.Fatalf("prompt missing pending candidates: %s", context.Prompt)
	}
	if strings.Contains(context.Prompt, "Linh Tran") || strings.Contains(context.Prompt, "3125550101") {
		t.Fatalf("prompt should not include customer PII: %s", context.Prompt)
	}
}

func TestMessageHelloAfterInitialGreetingDoesNotAskForBookingService(t *testing.T) {
	store := newFakeConversationStore()
	store.cfg.SalonName = "Lotus Nails Studio"
	store.session.Transcript = []TranscriptMessage{{
		Speaker: SpeakerAI,
		Body:    initialReply(&store.cfg),
	}}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Hello.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("hello-only turn should not call booking tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if session.Intent != IntentUnknown {
		t.Fatalf("hello-only turn intent = %s, want unknown", session.Intent)
	}
	lower := strings.ToLower(store.lastTurn.AIMessage)
	if !strings.Contains(lower, "i can hear you") || !strings.Contains(lower, "how can i help today") {
		t.Fatalf("hello-only reply should acknowledge and collect intent openly: %s", store.lastTurn.AIMessage)
	}
	if strings.Contains(lower, "what service") || strings.Contains(lower, "welcome to") {
		t.Fatalf("hello-only reply should not restart welcome or force booking service: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageAnswersServiceMenuWithBookableServices(t *testing.T) {
	store := newFakeConversationStore()
	store.services = append(store.services,
		ServiceOption{ID: "service_acrylic", Name: "Acrylic Full Set", DurationMinutes: 75},
		ServiceOption{ID: "service_pedicure", Name: "Classic Pedicure", DurationMinutes: 45},
		ServiceOption{ID: "service_dip", Name: "Dip Powder Manicure", DurationMinutes: 60},
		ServiceOption{ID: "service_gel", Name: "Gel Manicure", DurationMinutes: 45},
		ServiceOption{ID: "service_removal", Name: "Gel Removal", DurationMinutes: 30},
	)
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "What services do you have?",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.Intent != IntentUnknown {
		t.Fatalf("service menu intent = %s, want unknown", session.Intent)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("service menu should not call booking tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	for _, want := range []string{"Classic Manicure", "Gel Removal", "Which one would you like to book"} {
		if !strings.Contains(store.lastTurn.AIMessage, want) {
			t.Fatalf("service menu reply missing %q: %s", want, store.lastTurn.AIMessage)
		}
	}
	if store.lastTurn.AIMetadata["answer_source"] != answerSourceServiceCatalog {
		t.Fatalf("answer source = %#v, want service catalog", store.lastTurn.AIMetadata["answer_source"])
	}
}

func TestMessageAnswersExactServiceInquiryWithoutSelectingBookingService(t *testing.T) {
	store := newFakeConversationStore()
	store.services = append(store.services, ServiceOption{ID: "service_removal", Name: "Gel Removal", DurationMinutes: 30})
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Do you have Gel Removal?",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.Intent != IntentUnknown {
		t.Fatalf("service inquiry intent = %s, want unknown", session.Intent)
	}
	if session.ServiceID != "" || len(session.BookingSegments) != 0 {
		t.Fatalf("service state = %s %#v, want no booking selection", session.ServiceID, session.BookingSegments)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("service inquiry should not call booking tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if store.lastTurn.CustomerMetadata["service_inquiry"] != true {
		t.Fatalf("service inquiry metadata = %#v", store.lastTurn.CustomerMetadata)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Yes, we offer Gel Removal") || !strings.Contains(store.lastTurn.AIMessage, "Which service would you like to book") {
		t.Fatalf("service inquiry reply = %s", store.lastTurn.AIMessage)
	}
}

func TestMessageSelectsBookingServiceAfterPriorServiceInquiry(t *testing.T) {
	store := newFakeConversationStore()
	store.services = append(store.services, ServiceOption{ID: "service_removal", Name: "Gel Removal", DurationMinutes: 30})
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Do you have Gel Removal?",
	})
	if err != nil {
		t.Fatalf("inquiry Message returned error: %v", err)
	}
	if session.ServiceID != "" {
		t.Fatalf("service after inquiry = %s, want none", session.ServiceID)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I want to book the service Classic Manicure.",
	})
	if err != nil {
		t.Fatalf("booking Message returned error: %v", err)
	}
	if session.ServiceID != "service_1" || len(session.BookingSegments) != 1 || session.BookingSegments[0].ServiceID != "service_1" {
		t.Fatalf("service state = %s %#v, want Classic Manicure only", session.ServiceID, session.BookingSegments)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("service selection without date should not call tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if strings.Contains(store.lastTurn.AIMessage, "add Classic Manicure") || strings.Contains(store.lastTurn.AIMessage, "switch") {
		t.Fatalf("booking after inquiry should not ask add/switch: %s", store.lastTurn.AIMessage)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Got it, Classic Manicure") || !strings.Contains(store.lastTurn.AIMessage, "What day") {
		t.Fatalf("booking after inquiry reply = %s", store.lastTurn.AIMessage)
	}
}

func TestMessageBookingPhraseStillSelectsService(t *testing.T) {
	store := newFakeConversationStore()
	store.services = append(store.services, ServiceOption{ID: "service_removal", Name: "Gel Removal", DurationMinutes: 30})
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I want to book Gel Removal.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.ServiceID != "service_removal" || len(session.BookingSegments) != 1 || session.BookingSegments[0].ServiceID != "service_removal" {
		t.Fatalf("service state = %s %#v, want Gel Removal selected", session.ServiceID, session.BookingSegments)
	}
	if store.lastTurn.CustomerMetadata["service_inquiry"] == true {
		t.Fatalf("booking phrase should not be marked service inquiry: %#v", store.lastTurn.CustomerMetadata)
	}
}

func TestMessageClarifiesAmbiguousServiceInquiryWithoutSelectingService(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_gel", Name: "Gel Manicure", DurationMinutes: 45},
		{ID: "service_removal", Name: "Gel Removal", DurationMinutes: 30},
	}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Do you have gel?",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.ServiceID != "" || len(session.BookingSegments) != 0 {
		t.Fatalf("service state = %s %#v, want no booking selection", session.ServiceID, session.BookingSegments)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("ambiguous inquiry should not call tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Gel Manicure") || !strings.Contains(store.lastTurn.AIMessage, "Gel Removal") {
		t.Fatalf("ambiguous inquiry reply = %s", store.lastTurn.AIMessage)
	}
}

func TestMessageBookingRequestAfterGreetingStillCollectsService(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Transcript = []TranscriptMessage{{
		Speaker: SpeakerAI,
		Body:    initialReply(&store.cfg),
	}}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I want to book an appointment.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.Intent != IntentBooking {
		t.Fatalf("booking request intent = %s, want booking", session.Intent)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Which service") {
		t.Fatalf("booking request should collect service: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageShortComplaintCreatesOwnerHandoff(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Really bad.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("complaint should not call booking tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if session.Status != StatusHandoff || session.Outcome != OutcomeHandoffRequested {
		t.Fatalf("session status/outcome = %s/%s, want handoff/handoff_requested", session.Status, session.Outcome)
	}
	if store.lastTurn.Handoff == nil || store.lastTurn.Handoff.Reason != HandoffReasonHumanRequested {
		t.Fatalf("handoff = %#v, want human_requested", store.lastTurn.Handoff)
	}
	reply := strings.ToLower(store.lastTurn.AIMessage)
	if !strings.Contains(reply, "owner") || !strings.Contains(reply, "not a confirmed appointment") {
		t.Fatalf("complaint handoff reply should route owner review without confirmation: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageSalonIdentityCheckAnswersSalonAndKeepsBookingState(t *testing.T) {
	store := newFakeConversationStore()
	store.cfg.SalonName = "Lotus Nails Studio"
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.RequestedDate = "2026-07-02"
	store.session.OfferedSlots = offeredPMSlots()
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Hi, Lotus?",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("identity check should not call tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if session.Intent != IntentBooking || session.RequestedStartTime != nil || len(session.OfferedSlots) != 3 {
		t.Fatalf("session state = intent %s start %v slots %#v, want booking state preserved", session.Intent, session.RequestedStartTime, session.OfferedSlots)
	}
	reply := store.lastTurn.AIMessage
	if !strings.Contains(reply, "Yes, this is Lotus Nails Studio.") || !strings.Contains(reply, "1:00 PM") {
		t.Fatalf("identity reply should answer salon and resume slot choice: %s", reply)
	}
	if strings.Contains(strings.ToLower(reply), "welcome") {
		t.Fatalf("identity reply should not restart greeting: %s", reply)
	}
}

func TestMessageConfirmsOnlyAfterBookingToolSuccess(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{
		attempt: &booking.BookingAttempt{
			ID:           "attempt_1",
			Status:       booking.StatusConfirmed,
			POSBookingID: "booking_1",
			Appointment:  &booking.Appointment{ID: "appointment_1", Status: booking.StatusConfirmed},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "My name is Linh Tran, phone 312-555-0101, classic manicure with Mai on 2026-06-10 at 3pm.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 1 {
		t.Fatalf("booking calls = %d, want 1", bookingTool.calls)
	}
	if bookingTool.request.Source != booking.SourceAIConversationSimulator {
		t.Fatalf("source = %s, want %s", bookingTool.request.Source, booking.SourceAIConversationSimulator)
	}
	if session.Status != StatusCompleted || session.Outcome != OutcomeBookingConfirmed {
		t.Fatalf("session status/outcome = %s/%s, want completed/booking_confirmed", session.Status, session.Outcome)
	}
	if session.BookingAttemptID != "attempt_1" || session.AppointmentID != "appointment_1" {
		t.Fatalf("booking linkage = %s/%s", session.BookingAttemptID, session.AppointmentID)
	}
	reply := store.lastTurn.AIMessage
	for _, wantText := range []string{"You're confirmed with Lotus Nails", "Classic Manicure", "Wednesday, June 10 at 3:00 PM", "Mai Nguyen", "under Linh Tran"} {
		if !strings.Contains(reply, wantText) {
			t.Fatalf("confirmed reply missing %q: %s", wantText, reply)
		}
	}
	assertCustomerReplyHidesProvider(t, reply)
}

func TestMessageStartsAutoRescheduleWithTargetConfirmation(t *testing.T) {
	store := newFakeConversationStore()
	store.session.CustomerPhone = "+13125550101"
	bookingTool := &fakeBookingTool{
		candidates: []booking.AppointmentActionRef{testRescheduleAppointment()},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I need to reschedule my appointment.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.candidateCalls != 1 {
		t.Fatalf("candidate calls = %d, want 1", bookingTool.candidateCalls)
	}
	if bookingTool.calls != 0 || bookingTool.rescheduleCalls != 0 {
		t.Fatalf("booking calls = create %d reschedule %d, want none before target confirmation", bookingTool.calls, bookingTool.rescheduleCalls)
	}
	if session.BookingAction != BookingActionReschedule || len(session.RescheduleCandidates) != 1 {
		t.Fatalf("reschedule state = %s/%#v, want one candidate", session.BookingAction, session.RescheduleCandidates)
	}
	if !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "is this the appointment") {
		t.Fatalf("AI message = %q, want target confirmation", store.lastTurn.AIMessage)
	}
	if session.RequestedStartTime != nil {
		t.Fatalf("first reschedule turn should not reuse extracted new time: %s", session.RequestedStartTime)
	}
}

func TestMessageRescheduleSelectsTargetBySpokenDateTimeBeforeNewSlot(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_manicure", Name: "Classic Manicure", DurationMinutes: 45},
		{ID: "service_pedicure", Name: "Classic Pedicure", DurationMinutes: 45},
	}
	store.session.BookingAction = BookingActionReschedule
	store.session.Intent = IntentBooking
	store.session.CustomerPhone = "+13125550101"
	candidates := []booking.AppointmentActionRef{
		testRescheduleAppointmentAt("appointment_manicure", "service_manicure", "Classic Manicure", time.Date(2026, 7, 6, 18, 0, 0, 0, time.UTC)),
		testRescheduleAppointmentAt("appointment_pedicure", "service_pedicure", "Classic Pedicure", time.Date(2026, 7, 6, 18, 30, 0, 0, time.UTC)),
	}
	store.session.RescheduleCandidates = rescheduleCandidatesFromAppointments(candidates)
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "July 6, 1:30 PM.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.TargetAppointmentID != "appointment_pedicure" {
		t.Fatalf("target appointment = %s, want appointment_pedicure", session.TargetAppointmentID)
	}
	if session.ServiceID != "service_pedicure" || session.ServiceName != "Classic Pedicure" {
		t.Fatalf("selected service = %s/%s, want pedicure", session.ServiceID, session.ServiceName)
	}
	if session.RequestedStartTime != nil || session.RequestedDate != "" {
		t.Fatalf("old appointment time should not be stored as new slot: date=%s start=%v", session.RequestedDate, session.RequestedStartTime)
	}
	if bookingTool.availabilityCalls != 0 || bookingTool.rescheduleCalls != 0 {
		t.Fatalf("calls = availability %d reschedule %d, want none before new time", bookingTool.availabilityCalls, bookingTool.rescheduleCalls)
	}
	if !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "new day and time") {
		t.Fatalf("AI message = %q, want new time prompt", store.lastTurn.AIMessage)
	}
}

func TestMessageRescheduleSelectsTargetByServiceFamily(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_manicure", Name: "Classic Manicure", DurationMinutes: 45},
		{ID: "service_pedicure", Name: "Classic Pedicure", DurationMinutes: 45},
	}
	store.session.BookingAction = BookingActionReschedule
	store.session.Intent = IntentBooking
	store.session.CustomerPhone = "+13125550101"
	candidates := []booking.AppointmentActionRef{
		testRescheduleAppointmentAt("appointment_manicure", "service_manicure", "Classic Manicure", time.Date(2026, 7, 6, 18, 0, 0, 0, time.UTC)),
		testRescheduleAppointmentAt("appointment_pedicure", "service_pedicure", "Classic Pedicure", time.Date(2026, 7, 6, 18, 30, 0, 0, time.UTC)),
	}
	store.session.RescheduleCandidates = rescheduleCandidatesFromAppointments(candidates)
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "regular pedi",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.TargetAppointmentID != "appointment_pedicure" {
		t.Fatalf("target appointment = %s, want appointment_pedicure", session.TargetAppointmentID)
	}
	if session.ServiceID != "service_pedicure" {
		t.Fatalf("service = %s, want service_pedicure", session.ServiceID)
	}
	if session.RequestedStartTime != nil {
		t.Fatalf("service target selection should not set a new time: %v", session.RequestedStartTime)
	}
}

func TestMessageRescheduleMultipleCandidatePromptIsVoiceFriendly(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_manicure", Name: "Classic Manicure", DurationMinutes: 45},
		{ID: "service_pedicure", Name: "Classic Pedicure", DurationMinutes: 45},
	}
	store.session.CustomerPhone = "+13125550101"
	bookingTool := &fakeBookingTool{
		candidates: []booking.AppointmentActionRef{
			testRescheduleAppointmentAt("appointment_manicure", "service_manicure", "Classic Manicure", time.Date(2026, 7, 6, 18, 0, 0, 0, time.UTC)),
			testRescheduleAppointmentAt("appointment_pedicure", "service_pedicure", "Classic Pedicure", time.Date(2026, 7, 6, 18, 30, 0, 0, time.UTC)),
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	_, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I want to reschedule my appointment.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	reply := store.lastTurn.AIMessage
	if strings.Contains(reply, "first:") || strings.Contains(reply, "second:") {
		t.Fatalf("AI message should not use raw ordinal labels: %s", reply)
	}
	for _, wantText := range []string{
		"I found more than one upcoming appointment. Which one should I reschedule?",
		"First, Classic Manicure on Monday, July 6 at 1:00 PM with Mai Nguyen.",
		"Second, Classic Pedicure on Monday, July 6 at 1:30 PM with Mai Nguyen.",
	} {
		if !strings.Contains(reply, wantText) {
			t.Fatalf("AI message missing %q: %s", wantText, reply)
		}
	}
}

func TestMessageRescheduleNextDayOfferPreservesAssignedTechnician(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{{ID: "service_pedicure", Name: "Classic Pedicure", DurationMinutes: 45}}
	store.session.BookingAction = BookingActionReschedule
	store.session.Intent = IntentBooking
	store.session.CustomerPhone = "+13125550101"
	appointment := testRescheduleAppointmentAt("appointment_pedicure", "service_pedicure", "Classic Pedicure", time.Date(2026, 7, 6, 18, 30, 0, 0, time.UTC))
	appointment.StaffSelectionMode = booking.StaffSelectionAnyone
	appointment.Segments[0].StaffSelectionMode = booking.StaffSelectionAnyone
	candidates := rescheduleCandidatesFromAppointments([]booking.AppointmentActionRef{appointment})
	store.session.RescheduleCandidates = candidates
	applyRescheduleCandidate(&store.session, candidates[0])
	bookingTool := &fakeBookingTool{
		availabilityResult: &booking.AvailabilityResult{
			ServiceID:       "service_pedicure",
			ServiceName:     "Classic Pedicure",
			StaffID:         "staff_1",
			StaffName:       "Mai Nguyen",
			PreferredDate:   "2026-07-07",
			DurationMinutes: 45,
			Timezone:        "America/Chicago",
			Slots: []booking.AvailabilitySlot{
				{StartTime: time.Date(2026, 7, 7, 17, 0, 0, 0, time.UTC), EndTime: time.Date(2026, 7, 7, 17, 45, 0, 0, time.UTC), StaffID: "staff_1", StaffName: "Mai Nguyen"},
				{StartTime: time.Date(2026, 7, 7, 17, 30, 0, 0, time.UTC), EndTime: time.Date(2026, 7, 7, 18, 15, 0, 0, time.UTC), StaffID: "staff_1", StaffName: "Mai Nguyen"},
				{StartTime: time.Date(2026, 7, 7, 18, 0, 0, 0, time.UTC), EndTime: time.Date(2026, 7, 7, 18, 45, 0, 0, time.UTC), StaffID: "staff_1", StaffName: "Mai Nguyen"},
			},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Next day.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1", bookingTool.availabilityCalls)
	}
	if bookingTool.availabilityRequest.StaffID != "staff_1" || bookingTool.availabilityRequest.StaffSelectionMode != booking.StaffSelectionSpecific {
		t.Fatalf("availability request staff = %s/%s, want specific Mai", bookingTool.availabilityRequest.StaffID, bookingTool.availabilityRequest.StaffSelectionMode)
	}
	if len(session.OfferedSlots) != 3 {
		t.Fatalf("offered slots = %#v, want three", session.OfferedSlots)
	}
	for _, slot := range session.OfferedSlots {
		if slot.StaffID != "staff_1" || slot.StaffSelectionMode != booking.StaffSelectionSpecific {
			t.Fatalf("offered slot staff = %#v, want specific Mai", slot)
		}
	}
	reply := store.lastTurn.AIMessage
	if strings.Contains(reply, "For the reschedule, For") || strings.Contains(reply, "available technician") {
		t.Fatalf("AI message should be natural and preserve assigned staff: %s", reply)
	}
	for _, wantText := range []string{
		"I found openings for your Classic Pedicure on Tuesday, July 7 at 12:00 PM, 12:30 PM, and 1:00 PM with Mai Nguyen. Which time works?",
	} {
		if !strings.Contains(reply, wantText) {
			t.Fatalf("AI message missing %q: %s", wantText, reply)
		}
	}
}

func TestMessageAutoReschedulesAfterNaturalMonthDateTime(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{{ID: "service_pedicure", Name: "Classic Pedicure", DurationMinutes: 45}}
	store.session.BookingAction = BookingActionReschedule
	store.session.Intent = IntentBooking
	store.session.CustomerPhone = "+13125550101"
	candidates := rescheduleCandidatesFromAppointments([]booking.AppointmentActionRef{
		testRescheduleAppointmentAt("appointment_pedicure", "service_pedicure", "Classic Pedicure", time.Date(2026, 7, 6, 18, 30, 0, 0, time.UTC)),
	})
	store.session.RescheduleCandidates = candidates
	applyRescheduleCandidate(&store.session, candidates[0])
	newStart := time.Date(2026, 7, 7, 18, 30, 0, 0, time.UTC)
	bookingTool := &fakeBookingTool{
		availabilityResult: availabilityResultForStart("service_pedicure", "Classic Pedicure", newStart),
		rescheduledAppointment: &booking.Appointment{
			ID:               "appointment_pedicure",
			Status:           booking.StatusRescheduled,
			POSAppointmentID: "booking_pedicure",
			StartTime:        newStart,
			EndTime:          newStart.Add(45 * time.Minute),
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "1:30 PM on July 7.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.rescheduleCalls != 1 {
		t.Fatalf("reschedule calls = %d, want 1", bookingTool.rescheduleCalls)
	}
	if !bookingTool.rescheduleRequest.StartTime.Equal(newStart) {
		t.Fatalf("reschedule start = %s, want %s", bookingTool.rescheduleRequest.StartTime, newStart)
	}
	if session.Outcome != OutcomeBookingRescheduled {
		t.Fatalf("outcome = %s, want booking_rescheduled", session.Outcome)
	}
}

func TestMessageRescheduleCombinesMonthDateThenTime(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{{ID: "service_pedicure", Name: "Classic Pedicure", DurationMinutes: 45}}
	store.session.BookingAction = BookingActionReschedule
	store.session.Intent = IntentBooking
	store.session.CustomerPhone = "+13125550101"
	candidates := rescheduleCandidatesFromAppointments([]booking.AppointmentActionRef{
		testRescheduleAppointmentAt("appointment_pedicure", "service_pedicure", "Classic Pedicure", time.Date(2026, 7, 6, 18, 30, 0, 0, time.UTC)),
	})
	store.session.RescheduleCandidates = candidates
	applyRescheduleCandidate(&store.session, candidates[0])
	newStart := time.Date(2026, 7, 7, 18, 30, 0, 0, time.UTC)
	bookingTool := &fakeBookingTool{
		availabilityResult: availabilityResultForStart("service_pedicure", "Classic Pedicure", newStart),
		rescheduledAppointment: &booking.Appointment{
			ID:               "appointment_pedicure",
			Status:           booking.StatusRescheduled,
			POSAppointmentID: "booking_pedicure",
			StartTime:        newStart,
			EndTime:          newStart.Add(45 * time.Minute),
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "July 7.",
	})
	if err != nil {
		t.Fatalf("date Message returned error: %v", err)
	}
	if session.RequestedDate != "2026-07-07" || session.RequestedStartTime != nil {
		t.Fatalf("date turn state = %s/%v, want date only", session.RequestedDate, session.RequestedStartTime)
	}
	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "1:30 PM.",
	})
	if err != nil {
		t.Fatalf("time Message returned error: %v", err)
	}
	if bookingTool.rescheduleCalls != 1 {
		t.Fatalf("reschedule calls = %d, want 1", bookingTool.rescheduleCalls)
	}
	if session.Outcome != OutcomeBookingRescheduled {
		t.Fatalf("outcome = %s, want booking_rescheduled", session.Outcome)
	}
}

func TestMessageRescheduleHandoffAfterRepeatedUnparsedNewTime(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{{ID: "service_pedicure", Name: "Classic Pedicure", DurationMinutes: 45}}
	store.session.BookingAction = BookingActionReschedule
	store.session.Intent = IntentBooking
	store.session.CustomerPhone = "+13125550101"
	candidates := rescheduleCandidatesFromAppointments([]booking.AppointmentActionRef{
		testRescheduleAppointmentAt("appointment_pedicure", "service_pedicure", "Classic Pedicure", time.Date(2026, 7, 6, 18, 30, 0, 0, time.UTC)),
	})
	store.session.RescheduleCandidates = candidates
	applyRescheduleCandidate(&store.session, candidates[0])
	store.session.Transcript = []TranscriptMessage{
		{Speaker: SpeakerAI, Body: "What new day and time would you like?", Metadata: map[string]any{"next_required_field": "requested_start_time"}},
		{Speaker: SpeakerCustomer, Body: "one thirty two line seven"},
		{Speaker: SpeakerAI, Body: "What new day and time would you like?", Metadata: map[string]any{"next_required_field": "requested_start_time"}},
	}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "one thirty two line seven",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.Status != StatusHandoff || session.Outcome != OutcomeHandoffRequested {
		t.Fatalf("session = %s/%s, want handoff", session.Status, session.Outcome)
	}
	if bookingTool.rescheduleCalls != 0 {
		t.Fatalf("reschedule calls = %d, want 0", bookingTool.rescheduleCalls)
	}
	if !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "not rescheduled yet") {
		t.Fatalf("AI message = %q, want not rescheduled yet", store.lastTurn.AIMessage)
	}
}

func TestMessageRescheduleSelectsOfferedSlotByOClockWithoutAvailabilityRetry(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{name: "word_oclock", message: "One o'clock PM."},
		{name: "numeric_oclock", message: "1 oclock PM."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeConversationStore()
			store.services = []ServiceOption{{ID: "service_pedicure", Name: "Classic Pedicure", DurationMinutes: 45}}
			store.session.BookingAction = BookingActionReschedule
			store.session.Intent = IntentBooking
			store.session.CustomerPhone = "+13125550101"
			candidates := rescheduleCandidatesFromAppointments([]booking.AppointmentActionRef{
				testRescheduleAppointmentAt("appointment_pedicure", "service_pedicure", "Classic Pedicure", time.Date(2026, 7, 6, 18, 30, 0, 0, time.UTC)),
			})
			store.session.RescheduleCandidates = candidates
			applyRescheduleCandidate(&store.session, candidates[0])
			store.session.RequestedDate = "2026-07-07"
			store.session.OfferedSlots = reschedulePedicureOfferedSlots()
			newStart := time.Date(2026, 7, 7, 18, 0, 0, 0, time.UTC)
			bookingTool := &fakeBookingTool{
				rescheduledAppointment: &booking.Appointment{
					ID:               "appointment_pedicure",
					Status:           booking.StatusRescheduled,
					POSAppointmentID: "booking_pedicure",
					StartTime:        newStart,
					EndTime:          newStart.Add(45 * time.Minute),
				},
			}
			service := NewService(store, bookingTool)
			service.now = fixedNow

			session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
				Message: tt.message,
			})
			if err != nil {
				t.Fatalf("Message returned error: %v", err)
			}
			if bookingTool.availabilityCalls != 0 {
				t.Fatalf("availability calls = %d, want 0 when selecting existing offered slot", bookingTool.availabilityCalls)
			}
			if bookingTool.rescheduleCalls != 1 {
				t.Fatalf("reschedule calls = %d, want 1", bookingTool.rescheduleCalls)
			}
			if !bookingTool.rescheduleRequest.StartTime.Equal(newStart) {
				t.Fatalf("reschedule start = %s, want %s", bookingTool.rescheduleRequest.StartTime, newStart)
			}
			if bookingTool.rescheduleRequest.StaffID != "staff_1" {
				t.Fatalf("reschedule staff = %s, want Mai staff_1", bookingTool.rescheduleRequest.StaffID)
			}
			if session.Outcome != OutcomeBookingRescheduled {
				t.Fatalf("outcome = %s, want booking_rescheduled", session.Outcome)
			}
		})
	}
}

func TestMessageRescheduleClarifiesUnclearOClockWithoutAvailabilityRetry(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{{ID: "service_pedicure", Name: "Classic Pedicure", DurationMinutes: 45}}
	store.session.BookingAction = BookingActionReschedule
	store.session.Intent = IntentBooking
	store.session.CustomerPhone = "+13125550101"
	candidates := rescheduleCandidatesFromAppointments([]booking.AppointmentActionRef{
		testRescheduleAppointmentAt("appointment_pedicure", "service_pedicure", "Classic Pedicure", time.Date(2026, 7, 6, 18, 30, 0, 0, time.UTC)),
	})
	store.session.RescheduleCandidates = candidates
	applyRescheduleCandidate(&store.session, candidates[0])
	store.session.RequestedDate = "2026-07-07"
	store.session.OfferedSlots = reschedulePedicureOfferedSlots()
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "For o'clock PM.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 0 || bookingTool.rescheduleCalls != 0 {
		t.Fatalf("calls = availability %d reschedule %d, want none for unclear offered-slot time", bookingTool.availabilityCalls, bookingTool.rescheduleCalls)
	}
	if len(session.OfferedSlots) != 3 {
		t.Fatalf("offered slots = %#v, want preserved slots", session.OfferedSlots)
	}
	reply := strings.ToLower(store.lastTurn.AIMessage)
	if !strings.Contains(reply, "heard a time but not clearly") ||
		!strings.Contains(store.lastTurn.AIMessage, "1:00 PM") {
		t.Fatalf("AI message = %q, want unclear-time clarification with offered slots", store.lastTurn.AIMessage)
	}
}

func TestMessageRescheduleFillerRepeatsConciseTargetPrompt(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_manicure", Name: "Classic Manicure", DurationMinutes: 45},
		{ID: "service_pedicure", Name: "Classic Pedicure", DurationMinutes: 45},
	}
	store.session.BookingAction = BookingActionReschedule
	store.session.Intent = IntentBooking
	store.session.CustomerPhone = "+13125550101"
	candidates := []booking.AppointmentActionRef{
		testRescheduleAppointmentAt("appointment_manicure", "service_manicure", "Classic Manicure", time.Date(2026, 7, 6, 18, 0, 0, 0, time.UTC)),
		testRescheduleAppointmentAt("appointment_pedicure", "service_pedicure", "Classic Pedicure", time.Date(2026, 7, 6, 18, 30, 0, 0, time.UTC)),
	}
	store.session.RescheduleCandidates = rescheduleCandidatesFromAppointments(candidates)
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "to reschedule.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.TargetAppointmentID != "" {
		t.Fatalf("target appointment = %s, want still unselected", session.TargetAppointmentID)
	}
	reply := strings.ToLower(store.lastTurn.AIMessage)
	if !strings.Contains(reply, "first at 1:00 pm") || !strings.Contains(reply, "second at 1:30 pm") {
		t.Fatalf("AI message = %q, want concise target prompt", store.lastTurn.AIMessage)
	}
	if strings.Contains(store.lastTurn.AIMessage, "Classic Manicure on Monday") {
		t.Fatalf("AI message should not repeat full appointment list: %q", store.lastTurn.AIMessage)
	}
}

func TestMessageAutoReschedulesAfterTargetAndAvailableNewTime(t *testing.T) {
	store := newFakeConversationStore()
	store.session.BookingAction = BookingActionReschedule
	store.session.Intent = IntentBooking
	store.session.CustomerPhone = "+13125550101"
	applyRescheduleCandidate(&store.session, rescheduleCandidatesFromAppointments([]booking.AppointmentActionRef{testRescheduleAppointment()})[0])
	newStart := time.Date(2026, 6, 11, 21, 0, 0, 0, time.UTC)
	bookingTool := &fakeBookingTool{
		availabilityResult: availabilityResultForStart("service_1", "Classic Manicure", newStart),
		rescheduledAppointment: &booking.Appointment{
			ID:               "appointment_1",
			Status:           booking.StatusRescheduled,
			POSAppointmentID: "booking_1",
			StartTime:        newStart,
			EndTime:          newStart.Add(45 * time.Minute),
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "2026-06-11 at 4pm works.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("create calls = %d, want 0 for reschedule", bookingTool.calls)
	}
	if bookingTool.rescheduleCalls != 1 {
		t.Fatalf("reschedule calls = %d, want 1", bookingTool.rescheduleCalls)
	}
	if bookingTool.rescheduleAppointmentID != "appointment_1" {
		t.Fatalf("reschedule appointment id = %s, want appointment_1", bookingTool.rescheduleAppointmentID)
	}
	if bookingTool.rescheduleRequest.Source != booking.SourceAIConversationSimulator {
		t.Fatalf("reschedule source = %s, want simulator", bookingTool.rescheduleRequest.Source)
	}
	if session.Outcome != OutcomeBookingRescheduled || session.AppointmentID != "appointment_1" {
		t.Fatalf("session outcome/link = %s/%s, want rescheduled appointment", session.Outcome, session.AppointmentID)
	}
}

func TestMessageAutoRescheduleFallbackLeavesNoConfirmedAppointmentLink(t *testing.T) {
	store := newFakeConversationStore()
	store.session.BookingAction = BookingActionReschedule
	store.session.Intent = IntentBooking
	store.session.CustomerPhone = "+13125550101"
	applyRescheduleCandidate(&store.session, rescheduleCandidatesFromAppointments([]booking.AppointmentActionRef{testRescheduleAppointment()})[0])
	newStart := time.Date(2026, 6, 11, 21, 0, 0, 0, time.UTC)
	bookingTool := &fakeBookingTool{
		availabilityResult: availabilityResultForStart("service_1", "Classic Manicure", newStart),
		rescheduleFallback: &booking.BookingAttempt{
			ID:                 "attempt_reschedule_fallback",
			Status:             booking.StatusFallbackPending,
			RequestedStartTime: newStart,
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "2026-06-11 at 4pm works.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 || bookingTool.rescheduleCalls != 1 {
		t.Fatalf("calls = create %d reschedule %d, want only reschedule", bookingTool.calls, bookingTool.rescheduleCalls)
	}
	if session.Outcome != OutcomeBookingFallbackPending || session.BookingAttemptID != "attempt_reschedule_fallback" {
		t.Fatalf("session fallback = %s/%s, want fallback attempt", session.Outcome, session.BookingAttemptID)
	}
	if session.AppointmentID != "" {
		t.Fatalf("fallback should not link a confirmed appointment: %s", session.AppointmentID)
	}
	if !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "original appointment has not been changed") {
		t.Fatalf("AI message = %q, want original appointment unchanged", store.lastTurn.AIMessage)
	}
}

func TestMessageDoesNotBookAmbiguousGenericManicureMatch(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{
			ID:              "service_dip",
			Name:            "Dip Powder Manicure",
			DurationMinutes: 75,
			PriceFrom:       55,
		},
		{
			ID:              "service_classic",
			Name:            "Classic Manicure",
			DurationMinutes: 45,
			PriceFrom:       35,
		},
		{
			ID:              "service_gel",
			Name:            "Gel Manicure",
			DurationMinutes: 45,
			PriceFrom:       38,
		},
	}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I want to book a child manicure for Thursday at 1 p.m. My name is Sim, phone 312-555-0101.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 0 || bookingTool.calls != 0 {
		t.Fatalf("booking tool calls = availability %d/create %d, want none for ambiguous service", bookingTool.availabilityCalls, bookingTool.calls)
	}
	if session.ServiceID != "" || len(session.BookingSegments) != 0 {
		t.Fatalf("service selection = %q segments %#v, want no service for ambiguous generic manicure match", session.ServiceID, session.BookingSegments)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Which manicure service") ||
		!strings.Contains(store.lastTurn.AIMessage, "1:00 PM") ||
		!strings.Contains(store.lastTurn.AIMessage, "Classic Manicure") ||
		!strings.Contains(store.lastTurn.AIMessage, "Gel Manicure") ||
		!strings.Contains(store.lastTurn.AIMessage, "Dip Powder Manicure") {
		t.Fatalf("ambiguous service should ask for clarification: %s", store.lastTurn.AIMessage)
	}
	if store.lastTurn.CustomerMetadata["service_understanding_reason"] != serviceUnderstandingAmbiguousFamily {
		t.Fatalf("service understanding metadata = %#v", store.lastTurn.CustomerMetadata)
	}
}

func TestMessageClarifiesNoisyServiceFamilyWithCatalogOptions(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		wantReason string
	}{
		{name: "stt_menikur", message: "Menikur.", wantReason: serviceUnderstandingFuzzyFamily},
		{name: "stt_manecu", message: "Manecu.", wantReason: serviceUnderstandingFuzzyFamily},
		{name: "mixed_language_manicure", message: "腳 manicure.", wantReason: serviceUnderstandingAmbiguousFamily},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeConversationStore()
			store.services = testManicureCatalog()
			store.session.Intent = IntentBooking
			store.session.RequestedDate = "2026-07-02"
			bookingTool := &fakeBookingTool{}
			service := NewService(store, bookingTool)
			service.now = fixedNow

			session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
				Message: tt.message,
			})
			if err != nil {
				t.Fatalf("Message returned error: %v", err)
			}
			if bookingTool.availabilityCalls != 0 || bookingTool.calls != 0 {
				t.Fatalf("booking tool calls = availability %d/create %d, want none for fuzzy family", bookingTool.availabilityCalls, bookingTool.calls)
			}
			if session.ServiceID != "" || len(session.BookingSegments) != 0 {
				t.Fatalf("service selection = %q segments %#v, want no service for fuzzy family", session.ServiceID, session.BookingSegments)
			}
			if !strings.Contains(store.lastTurn.AIMessage, "Which manicure service") ||
				!strings.Contains(store.lastTurn.AIMessage, "Classic Manicure") ||
				!strings.Contains(store.lastTurn.AIMessage, "Gel Manicure") ||
				!strings.Contains(store.lastTurn.AIMessage, "Dip Powder Manicure") {
				t.Fatalf("AI should clarify with catalog options: %s", store.lastTurn.AIMessage)
			}
			if strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "which service would you like?") {
				t.Fatalf("AI should not fall back to generic service prompt: %s", store.lastTurn.AIMessage)
			}
			if strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "foot manicure") {
				t.Fatalf("AI should not invent a foot manicure service: %s", store.lastTurn.AIMessage)
			}
			if store.lastTurn.CustomerMetadata["service_understanding_reason"] != tt.wantReason {
				t.Fatalf("service understanding metadata = %#v", store.lastTurn.CustomerMetadata)
			}
		})
	}
}

func TestMessageResolvesServiceFromPendingClarificationCandidates(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_classic", Name: "Classic Manicure"},
		{ID: "service_gel", Name: "Gel Manicure"},
		{ID: "service_dip", Name: "Dip Powder Manicure"},
		{ID: "service_removal", Name: "Gel Removal"},
	}
	store.session.Intent = IntentBooking
	store.session.Transcript = []TranscriptMessage{{
		Speaker: SpeakerAI,
		Body:    "Which manicure service would you like: Classic Manicure, Gel Manicure, or Dip Powder Manicure?",
		Metadata: map[string]any{
			"pending_service_candidate_ids": []string{"service_classic", "service_gel", "service_dip"},
		},
	}}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Gel.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.ServiceID != "service_gel" || session.ServiceName != "Gel Manicure" {
		t.Fatalf("service = %s/%s, want Gel Manicure from pending candidates", session.ServiceID, session.ServiceName)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("service clarification should not book/check availability without date, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if store.lastTurn.CustomerMetadata["service_understanding_selected_id"] != "service_gel" {
		t.Fatalf("service understanding metadata = %#v", store.lastTurn.CustomerMetadata)
	}
	if !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "day") {
		t.Fatalf("AI should move to date collection after service selection: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageHandlesNoisyVinglishServiceAfterDateTurnAndShortNameConfirmation(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_classic", Name: "Classic Manicure", DurationMinutes: 45},
		{ID: "service_gel", Name: "Gel Manicure", DurationMinutes: 45},
		{ID: "service_dip", Name: "Dip Powder Manicure", DurationMinutes: 75},
	}
	store.session.Channel = ChannelPhone
	store.session.Intent = IntentBooking
	store.session.CustomerPhone = "3125550101"
	store.session.Transcript = []TranscriptMessage{{
		Speaker: SpeakerAI,
		Body:    "Which manicure service would you like: Classic Manicure, Gel Manicure, or Dip Powder Manicure?",
		Metadata: map[string]any{
			"pending_service_candidate_ids": []string{"service_classic", "service_gel", "service_dip"},
			"pending_service_token":         "manicure",
		},
	}}
	slotStart := time.Date(2026, 6, 9, 18, 0, 0, 0, time.UTC)
	bookingTool := &fakeBookingTool{
		availabilityResult: availabilityResultForStart("service_classic", "Classic Manicure", slotStart),
		attempt: &booking.BookingAttempt{
			ID:           "attempt_1",
			Status:       booking.StatusConfirmed,
			POSBookingID: "booking_1",
			Appointment:  &booking.Appointment{ID: "appointment_1", Status: booking.StatusConfirmed},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Today.",
	})
	if err != nil {
		t.Fatalf("date Message returned error: %v", err)
	}
	if session.RequestedDate != "2026-06-09" {
		t.Fatalf("requested date = %q, want today", session.RequestedDate)
	}
	if session.ServiceID != "" {
		t.Fatalf("service = %q, want no guessed service from date turn", session.ServiceID)
	}
	if got := metadataStringSlice(store.lastTurn.AIMetadata, "pending_service_candidate_ids"); !sameStrings(got, []string{"service_classic", "service_gel", "service_dip"}) {
		t.Fatalf("pending service ids after date turn = %#v", got)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Which manicure service") {
		t.Fatalf("AI should keep service clarification after date turn: %s", store.lastTurn.AIMessage)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Klasos Makikio.",
	})
	if err != nil {
		t.Fatalf("service Message returned error: %v", err)
	}
	if session.ServiceID != "service_classic" {
		t.Fatalf("service = %s, want Classic Manicure from noisy pending candidate reply", session.ServiceID)
	}
	if bookingTool.availabilityCalls != 1 || bookingTool.calls != 0 {
		t.Fatalf("tool calls after service = availability %d booking %d, want availability only", bookingTool.availabilityCalls, bookingTool.calls)
	}
	if store.lastTurn.CustomerMetadata["service_understanding_reason"] != serviceUnderstandingFuzzyService {
		t.Fatalf("service understanding metadata = %#v", store.lastTurn.CustomerMetadata)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "1:00 PM") {
		t.Fatalf("AI should offer the available slot after service selection: %s", store.lastTurn.AIMessage)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "1PM.",
	})
	if err != nil {
		t.Fatalf("slot Message returned error: %v", err)
	}
	if session.RequestedStartTime == nil || !session.RequestedStartTime.Equal(slotStart) {
		t.Fatalf("requested start = %v, want %s", session.RequestedStartTime, slotStart)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking calls = %d, want none before name is confirmed", bookingTool.calls)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "What name") {
		t.Fatalf("AI should ask for customer name after slot selection: %s", store.lastTurn.AIMessage)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "My name Tim.",
	})
	if err != nil {
		t.Fatalf("name Message returned error: %v", err)
	}
	if session.CustomerName != "" {
		t.Fatalf("customer name = %q, want pending confirmation only", session.CustomerName)
	}
	if store.lastTurn.AIMetadata["pending_customer_name"] != "Tim" {
		t.Fatalf("pending customer name metadata = %#v, want Tim", store.lastTurn.AIMetadata)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking calls = %d, want none before short voice name confirmation", bookingTool.calls)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Yes.",
	})
	if err != nil {
		t.Fatalf("name confirmation Message returned error: %v", err)
	}
	if bookingTool.calls != 1 {
		t.Fatalf("booking calls = %d, want one after name confirmation", bookingTool.calls)
	}
	if bookingTool.request.CustomerName != "Tim" {
		t.Fatalf("booking customer name = %q, want Tim", bookingTool.request.CustomerName)
	}
	if session.CustomerName != "Tim" || session.Outcome != OutcomeBookingConfirmed {
		t.Fatalf("session name/outcome = %q/%s, want Tim/confirmed", session.CustomerName, session.Outcome)
	}
}

func TestMessageKeepsAmbiguousServiceWithoutPendingClarification(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_gel", Name: "Gel Manicure"},
		{ID: "service_removal", Name: "Gel Removal"},
	}
	store.session.Intent = IntentBooking
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Gel.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.ServiceID != "" || len(session.BookingSegments) != 0 {
		t.Fatalf("service = %s segments %#v, want ambiguous without pending context", session.ServiceID, session.BookingSegments)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("ambiguous service should not call tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Gel Manicure") || !strings.Contains(store.lastTurn.AIMessage, "Gel Removal") {
		t.Fatalf("AI should clarify with full catalog candidates: %s", store.lastTurn.AIMessage)
	}
}

func TestMessagePreservesDeterministicServiceClarificationAgainstLLMRewrite(t *testing.T) {
	store := newFakeConversationStore()
	store.services = testManicureCatalog()
	store.session.Intent = IntentBooking
	store.session.RequestedDate = "2026-07-02"
	replyGenerator := &fakeReplyGenerator{message: "Which service would you like?"}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.SetReplyGenerator(replyGenerator)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Manicure.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.ServiceID != "" {
		t.Fatalf("service = %s, want ambiguous service unselected", session.ServiceID)
	}
	if replyGenerator.calls != 0 {
		t.Fatalf("reply generator calls = %d, want deterministic clarification without LLM rewrite", replyGenerator.calls)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Which manicure service") ||
		!strings.Contains(store.lastTurn.AIMessage, "Classic Manicure") ||
		!strings.Contains(store.lastTurn.AIMessage, "Gel Manicure") ||
		!strings.Contains(store.lastTurn.AIMessage, "Dip Powder Manicure") {
		t.Fatalf("deterministic service clarification was not preserved: %s", store.lastTurn.AIMessage)
	}
	if store.lastTurn.AIMetadata["turn_path"] != "service_understanding_clarification" {
		t.Fatalf("AI metadata = %#v", store.lastTurn.AIMetadata)
	}
}

func TestMessageKeepsTerminalBookingReplyDeterministic(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{
		attempt: &booking.BookingAttempt{
			ID:           "attempt_1",
			Status:       booking.StatusConfirmed,
			POSBookingID: "booking_1",
			Appointment:  &booking.Appointment{ID: "appointment_1", Status: booking.StatusConfirmed},
		},
	}
	replyGenerator := &fakeReplyGenerator{
		message: "Your appointment is confirmed. Is there anything else I can assist you with today?",
	}
	service := NewService(store, bookingTool)
	service.SetReplyGenerator(replyGenerator)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "My name is Linh Tran, phone 312-555-0101, classic manicure with Mai on 2026-06-10 at 3pm.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.Status != StatusCompleted || session.Outcome != OutcomeBookingConfirmed {
		t.Fatalf("session status/outcome = %s/%s, want completed/booking_confirmed", session.Status, session.Outcome)
	}
	if replyGenerator.calls != 0 {
		t.Fatalf("reply generator calls = %d, want 0 for terminal booking reply", replyGenerator.calls)
	}
	reply := store.lastTurn.AIMessage
	if strings.Contains(reply, "?") || strings.Contains(strings.ToLower(reply), "anything else") {
		t.Fatalf("terminal booking reply should not ask a follow-up question: %s", reply)
	}
	if !strings.Contains(reply, "Thank you, goodbye.") {
		t.Fatalf("terminal booking reply should close the call politely: %s", reply)
	}
}

func TestMessageUsesFallbackPendingTextWhenBookingToolFallsBack(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{
		attempt: &booking.BookingAttempt{
			ID:     "attempt_2",
			Status: booking.StatusFallbackPending,
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "My name is Linh Tran, phone 312-555-0101, classic manicure with Mai on 2026-06-10 at 3pm.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.Outcome != OutcomeBookingFallbackPending {
		t.Fatalf("outcome = %s, want booking_fallback_pending", session.Outcome)
	}
	if session.AppointmentID != "" {
		t.Fatalf("fallback should not link a confirmed appointment: %s", session.AppointmentID)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "not a confirmed appointment") {
		t.Fatalf("fallback reply must avoid confirmation: %s", store.lastTurn.AIMessage)
	}
	assertCustomerReplyHidesProvider(t, store.lastTurn.AIMessage)
}

func TestMessageUsesProviderHiddenTextWhenBookingToolErrors(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{
		err: errors.New("booking provider unavailable"),
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "My name is Linh Tran, phone 312-555-0101, classic manicure with Mai on 2026-06-10 at 3pm.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.Status != StatusHandoff || session.Outcome != OutcomeHandoffRequested {
		t.Fatalf("session status/outcome = %s/%s, want handoff/handoff_requested", session.Status, session.Outcome)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "not a confirmed appointment") {
		t.Fatalf("booking error reply must avoid confirmation: %s", store.lastTurn.AIMessage)
	}
	assertCustomerReplyHidesProvider(t, store.lastTurn.AIMessage)
}

func assertCustomerReplyHidesProvider(t *testing.T, reply string) {
	t.Helper()
	lower := strings.ToLower(reply)
	for _, blocked := range []string{"square", "pos", "provider"} {
		if strings.Contains(lower, blocked) {
			t.Fatalf("customer reply should not expose provider term %q: %s", blocked, reply)
		}
	}
}

func TestMessageUsesVoiceCallBookingSourceForPhoneSessions(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	store.session.Provider = "twilio"
	store.session.ProviderCallID = "CA123"
	bookingTool := &fakeBookingTool{
		attempt: &booking.BookingAttempt{
			ID:           "attempt_voice",
			Status:       booking.StatusConfirmed,
			POSBookingID: "booking_voice",
			Appointment:  &booking.Appointment{ID: "appointment_voice", Status: booking.StatusConfirmed},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "My name is Linh Tran, phone 312-555-0101, classic manicure with Mai on 2026-06-10 at 3pm.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.Outcome != OutcomeBookingConfirmed {
		t.Fatalf("outcome = %s, want booking_confirmed", session.Outcome)
	}
	if bookingTool.request.Source != booking.SourceAIVoiceCall {
		t.Fatalf("source = %s, want %s", bookingTool.request.Source, booking.SourceAIVoiceCall)
	}
	if !strings.Contains(bookingTool.request.Notes, "phone receptionist") {
		t.Fatalf("notes = %q, want phone receptionist note", bookingTool.request.Notes)
	}
}

func TestApplyReplyGeneratorIncludesAITone(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, &fakeBookingTool{})
	replyGenerator := &fakeReplyGenerator{message: "What time works best?"}
	service.SetReplyGenerator(replyGenerator)
	session := store.session
	session.Channel = ChannelPhone
	cfg := store.cfg
	cfg.AITone = "friendly_young"
	turn := TurnRecord{
		SalonID:         "salon_1",
		OwnerUserID:     "owner_1",
		Session:         session,
		CustomerMessage: "Tomorrow",
		AIMessage:       "What time works for that day?",
		Update: SessionUpdate{
			Status:  StatusActive,
			Intent:  IntentBooking,
			Outcome: OutcomeCollecting,
		},
	}

	service.applyReplyGenerator(context.Background(), &turn, session, store.services, &cfg, "requested_time", "requested_time", nil)

	if replyGenerator.calls != 1 {
		t.Fatalf("reply generator calls = %d, want 1", replyGenerator.calls)
	}
	if replyGenerator.lastRequest.AITone != "friendly_young" {
		t.Fatalf("AI tone = %q, want friendly_young", replyGenerator.lastRequest.AITone)
	}
	if turn.AIMessage != "What time works best?" {
		t.Fatalf("AI message = %q", turn.AIMessage)
	}
}

func TestApplyReplyGeneratorRejectsRescheduleTargetStageFlip(t *testing.T) {
	store := newFakeConversationStore()
	service := NewService(store, &fakeBookingTool{})
	replyGenerator := &fakeReplyGenerator{message: "You mentioned July 6 at 1:30 PM. Is that the new time you'd like for your Classic Manicure appointment?"}
	service.SetReplyGenerator(replyGenerator)
	session := store.session
	session.BookingAction = BookingActionReschedule
	turn := TurnRecord{
		SalonID:         "salon_1",
		OwnerUserID:     "owner_1",
		Session:         session,
		CustomerMessage: "July 6, 1:30 PM.",
		AIMessage:       "I found more than one upcoming appointment. Which one should I reschedule? First for Classic Manicure on Monday, July 6 at 1:00 PM. Second for Classic Pedicure on Monday, July 6 at 1:30 PM.",
		Update: SessionUpdate{
			Status:        StatusActive,
			Intent:        IntentBooking,
			Outcome:       OutcomeCollecting,
			BookingAction: BookingActionReschedule,
		},
	}

	service.applyReplyGenerator(context.Background(), &turn, session, store.services, &store.cfg, "target_appointment", "target_appointment", nil)

	if replyGenerator.calls != 1 {
		t.Fatalf("reply generator calls = %d, want 1", replyGenerator.calls)
	}
	if strings.Contains(strings.ToLower(turn.AIMessage), "new time") {
		t.Fatalf("AI message accepted unsafe target-stage rewrite: %q", turn.AIMessage)
	}
	if turn.AIMetadata["llm_guardrail"] != "rejected_reschedule_stage_flip" {
		t.Fatalf("AI metadata = %#v, want rejected guardrail", turn.AIMetadata)
	}
}

func TestMessageOffersAvailableSlotsBeforeBooking(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{
		availabilityResult: &booking.AvailabilityResult{
			ServiceID:       "service_1",
			ServiceName:     "Classic Manicure",
			PreferredDate:   "2026-06-10",
			DurationMinutes: 45,
			Timezone:        "America/Chicago",
			Slots: []booking.AvailabilitySlot{
				{
					StartTime: time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC),
					EndTime:   time.Date(2026, 6, 10, 15, 45, 0, 0, time.UTC),
					StaffID:   "staff_1",
					StaffName: "Mai Nguyen",
				},
				{
					StartTime: time.Date(2026, 6, 10, 16, 0, 0, 0, time.UTC),
					EndTime:   time.Date(2026, 6, 10, 16, 45, 0, 0, time.UTC),
					StaffID:   "staff_1",
					StaffName: "Mai Nguyen",
				},
			},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I need a classic manicure tomorrow.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1", bookingTool.availabilityCalls)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking should not be called before customer selects a slot")
	}
	if bookingTool.availabilityRequest.ServiceID != "service_1" || bookingTool.availabilityRequest.PreferredDate != "2026-06-10" {
		t.Fatalf("availability request = %#v, want service/date", bookingTool.availabilityRequest)
	}
	if len(session.OfferedSlots) != 2 {
		t.Fatalf("offered slots = %#v, want two", session.OfferedSlots)
	}
	if session.RequestedStartTime != nil {
		t.Fatalf("requested start should not be set before slot selection: %s", session.RequestedStartTime)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "10:00 AM") || !strings.Contains(store.lastTurn.AIMessage, "Which works") {
		t.Fatalf("AI reply should offer slots: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageParsesWeekdayDateAndDoesNotAskForDayAgain(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{
		availabilityResult: &booking.AvailabilityResult{
			ServiceID:       "service_1",
			ServiceName:     "Classic Manicure",
			PreferredDate:   "2026-06-11",
			DurationMinutes: 45,
			Timezone:        "America/Chicago",
			Slots: []booking.AvailabilitySlot{{
				StartTime: time.Date(2026, 6, 11, 15, 0, 0, 0, time.UTC),
				EndTime:   time.Date(2026, 6, 11, 15, 45, 0, 0, time.UTC),
				StaffID:   "staff_1",
				StaffName: "Mai Nguyen",
			}},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I want a classic manicure on Thursday this week.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1", bookingTool.availabilityCalls)
	}
	if bookingTool.availabilityRequest.PreferredDate != "2026-06-11" {
		t.Fatalf("preferred date = %s, want 2026-06-11", bookingTool.availabilityRequest.PreferredDate)
	}
	if session.RequestedDate != "2026-06-11" {
		t.Fatalf("requested date = %q, want 2026-06-11", session.RequestedDate)
	}
	if strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "what day") {
		t.Fatalf("AI should not ask for day again after parsing Thursday: %s", store.lastTurn.AIMessage)
	}
}

func TestPreferredDateNextMondayUsesUpcomingWeekday(t *testing.T) {
	loc := timezoneLocation("America/Chicago")
	now := func() time.Time {
		return time.Date(2026, 7, 3, 12, 0, 0, 0, loc)
	}
	if got := preferredDateFromMessage("next Monday", nil, loc, now); got != "2026-07-06" {
		t.Fatalf("next Monday from 2026-07-03 = %s, want 2026-07-06", got)
	}

	mondayNow := func() time.Time {
		return time.Date(2026, 7, 6, 12, 0, 0, 0, loc)
	}
	if got := preferredDateFromMessage("next Monday", nil, loc, mondayNow); got != "2026-07-13" {
		t.Fatalf("next Monday from 2026-07-06 = %s, want 2026-07-13", got)
	}
}

func TestMessageAutoSelectsExactRequestedTimeWithoutNamingAssignedAnyoneTechnician(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{{
		ID:              "service_gel",
		Name:            "Gel Manicure",
		DurationMinutes: 45,
		PriceFrom:       38,
	}}
	bookingTool := &fakeBookingTool{
		availabilityResult: &booking.AvailabilityResult{
			ServiceID:          "service_gel",
			ServiceName:        "Gel Manicure",
			StaffSelectionMode: booking.StaffSelectionAnyone,
			PreferredDate:      "2026-06-11",
			DurationMinutes:    45,
			Timezone:           "America/Chicago",
			Slots: []booking.AvailabilitySlot{
				{
					StartTime:          time.Date(2026, 6, 11, 17, 0, 0, 0, time.UTC),
					EndTime:            time.Date(2026, 6, 11, 17, 45, 0, 0, time.UTC),
					StaffID:            "staff_1",
					StaffName:          "Mai Nguyen",
					StaffSelectionMode: booking.StaffSelectionAnyone,
				},
				{
					StartTime:          time.Date(2026, 6, 11, 17, 30, 0, 0, time.UTC),
					EndTime:            time.Date(2026, 6, 11, 18, 15, 0, 0, time.UTC),
					StaffID:            "staff_1",
					StaffName:          "Mai Nguyen",
					StaffSelectionMode: booking.StaffSelectionAnyone,
				},
				{
					StartTime:          time.Date(2026, 6, 11, 18, 0, 0, 0, time.UTC),
					EndTime:            time.Date(2026, 6, 11, 18, 45, 0, 0, time.UTC),
					StaffID:            "staff_1",
					StaffName:          "Mai Nguyen",
					StaffSelectionMode: booking.StaffSelectionAnyone,
				},
			},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Hello, I want to book a gel manicure for 1 p.m. this Thursday.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1", bookingTool.availabilityCalls)
	}
	want := time.Date(2026, 6, 11, 18, 0, 0, 0, time.UTC)
	if session.RequestedStartTime == nil || !session.RequestedStartTime.Equal(want) {
		t.Fatalf("requested start = %v, want %s", session.RequestedStartTime, want)
	}
	if session.StaffID != "staff_1" || session.StaffName != "Mai Nguyen" {
		t.Fatalf("assigned staff = %s/%s, want Mai Nguyen", session.StaffID, session.StaffName)
	}
	if len(session.OfferedSlots) != 0 {
		t.Fatalf("exact requested time should not leave offered slots: %#v", session.OfferedSlots)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking calls = %d, want 0 until customer details are collected", bookingTool.calls)
	}
	reply := store.lastTurn.AIMessage
	for _, wantText := range []string{"1:00 PM", "Thursday", "Gel Manicure", "available technician", "What name"} {
		if !strings.Contains(reply, wantText) {
			t.Fatalf("AI reply missing %q: %s", wantText, reply)
		}
	}
	if strings.Contains(reply, "Mai Nguyen") {
		t.Fatalf("AI reply should not name assigned technician when caller asked for anyone: %s", reply)
	}
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "does 1:00 pm work") || strings.Contains(lower, "which works") || strings.Contains(reply, "12:00 PM") {
		t.Fatalf("AI should not reconfirm or offer alternatives when exact requested time is available: %s", reply)
	}
}

func TestMessageAutoSelectsFairRotationTechnicianWhenMultipleExactSlotsAreAvailable(t *testing.T) {
	store := newFakeConversationStore()
	store.staff = []StaffOption{
		{ID: "staff_a", Name: "Amy Nguyen", AIBookable: true},
		{ID: "staff_z", Name: "Zoe Tran", AIBookable: true},
	}
	lastAssigned := time.Date(2026, 6, 11, 15, 30, 0, 0, time.UTC)
	store.assignmentStats = map[string]StaffAssignmentStat{
		"staff_a": {StaffID: "staff_a", AssignedCount: 2, LastAssignedAt: &lastAssigned},
		"staff_z": {StaffID: "staff_z", AssignedCount: 0},
	}
	start := time.Date(2026, 6, 11, 18, 0, 0, 0, time.UTC)
	bookingTool := &fakeBookingTool{
		availabilityResult: &booking.AvailabilityResult{
			ServiceID:          "service_1",
			ServiceName:        "Classic Manicure",
			StaffSelectionMode: booking.StaffSelectionAnyone,
			PreferredDate:      "2026-06-11",
			DurationMinutes:    45,
			Timezone:           "America/Chicago",
			Slots: []booking.AvailabilitySlot{
				{
					StartTime:          start,
					EndTime:            start.Add(45 * time.Minute),
					StaffID:            "staff_a",
					StaffName:          "Amy Nguyen",
					StaffSelectionMode: booking.StaffSelectionAnyone,
				},
				{
					StartTime:          start,
					EndTime:            start.Add(45 * time.Minute),
					StaffID:            "staff_z",
					StaffName:          "Zoe Tran",
					StaffSelectionMode: booking.StaffSelectionAnyone,
				},
			},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I want a classic manicure at 1 p.m. this Thursday.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.StaffID != "staff_z" || session.StaffName != "Zoe Tran" {
		t.Fatalf("assigned staff = %s/%s, want fair-rotation Zoe Tran selection", session.StaffID, session.StaffName)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "available technician") || strings.Contains(store.lastTurn.AIMessage, "Zoe Tran") || strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "which works") {
		t.Fatalf("reply should avoid naming fair-rotation technician without asking the caller to reselect time: %s", store.lastTurn.AIMessage)
	}
	if store.lastTurn.ToolMetadata["assignment_policy"] != "fair_rotation" {
		t.Fatalf("assignment policy metadata = %#v, want fair_rotation", store.lastTurn.ToolMetadata)
	}
	if len(store.assignmentStaffIDs) != 2 {
		t.Fatalf("assignment staff ids = %#v, want two candidates", store.assignmentStaffIDs)
	}
	if store.assignmentFrom.IsZero() || store.assignmentTo.Sub(store.assignmentFrom) != 24*time.Hour {
		t.Fatalf("assignment window = %s to %s, want local day window", store.assignmentFrom, store.assignmentTo)
	}
}

func TestMessageOffersAlternativesWhenRequestedTechnicianUnavailableAtExactTime(t *testing.T) {
	store := newFakeConversationStore()
	store.staff = append(store.staff, StaffOption{
		ID:         "staff_2",
		Name:       "Lena Pham",
		AIBookable: true,
	})
	requestedStart := time.Date(2026, 6, 11, 18, 0, 0, 0, time.UTC)
	maiLater := requestedStart.Add(time.Hour)
	bookingTool := &fakeBookingTool{
		availabilityResults: []*booking.AvailabilityResult{
			{
				ServiceID:          "service_1",
				ServiceName:        "Classic Manicure",
				StaffID:            "staff_1",
				StaffName:          "Mai Nguyen",
				StaffSelectionMode: booking.StaffSelectionSpecific,
				PreferredDate:      "2026-06-11",
				DurationMinutes:    45,
				Timezone:           "America/Chicago",
				Slots: []booking.AvailabilitySlot{{
					StartTime:          maiLater,
					EndTime:            maiLater.Add(45 * time.Minute),
					StaffID:            "staff_1",
					StaffName:          "Mai Nguyen",
					StaffSelectionMode: booking.StaffSelectionSpecific,
				}},
			},
			{
				ServiceID:          "service_1",
				ServiceName:        "Classic Manicure",
				StaffSelectionMode: booking.StaffSelectionAnyone,
				PreferredDate:      "2026-06-11",
				DurationMinutes:    45,
				Timezone:           "America/Chicago",
				Slots: []booking.AvailabilitySlot{
					{
						StartTime:          requestedStart,
						EndTime:            requestedStart.Add(45 * time.Minute),
						StaffID:            "staff_2",
						StaffName:          "Lena Pham",
						StaffSelectionMode: booking.StaffSelectionAnyone,
					},
					{
						StartTime:          maiLater,
						EndTime:            maiLater.Add(45 * time.Minute),
						StaffID:            "staff_1",
						StaffName:          "Mai Nguyen",
						StaffSelectionMode: booking.StaffSelectionSpecific,
					},
				},
			},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I want a classic manicure with Mai at 1 p.m. this Thursday.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 2 {
		t.Fatalf("availability calls = %d, want requested-staff and anyone checks", bookingTool.availabilityCalls)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking calls = %d, want 0 before caller chooses an alternative", bookingTool.calls)
	}
	if session.RequestedStartTime != nil {
		t.Fatalf("requested start should be cleared until caller chooses an alternative: %s", session.RequestedStartTime)
	}
	if session.StaffID != "staff_1" || session.StaffName != "Mai Nguyen" {
		t.Fatalf("requested staff should remain Mai until caller agrees to switch: %s/%s", session.StaffID, session.StaffName)
	}
	if len(session.OfferedSlots) != 2 {
		t.Fatalf("offered slots = %#v, want same-time other tech and later requested tech", session.OfferedSlots)
	}
	reply := store.lastTurn.AIMessage
	for _, wantText := range []string{"Mai Nguyen is not available", "1:00 PM", "available technician", "2:00 PM", "Which works"} {
		if !strings.Contains(reply, wantText) {
			t.Fatalf("AI reply missing %q: %s", wantText, reply)
		}
	}
	if strings.Contains(reply, "Lena Pham") {
		t.Fatalf("AI reply should not name alternate technician unless caller selects them: %s", reply)
	}
	if strings.Contains(strings.ToLower(reply), "confirmed") {
		t.Fatalf("unavailable reply must not sound confirmed: %s", reply)
	}
	if store.lastTurn.ToolMetadata["availability_policy"] != "specific_staff_unavailable" {
		t.Fatalf("availability policy metadata = %#v, want specific_staff_unavailable", store.lastTurn.ToolMetadata)
	}
}

func TestMessageSelectsOfferedSlotByTechnicianNameWithoutAvailabilityRetry(t *testing.T) {
	store := newFakeConversationStore()
	store.staff = append(store.staff, StaffOption{
		ID:         "staff_2",
		Name:       "Lena Pham",
		AIBookable: true,
	})
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.StaffID = "staff_1"
	store.session.StaffName = "Mai Nguyen"
	store.session.StaffSelectionMode = booking.StaffSelectionSpecific
	store.session.OfferedSlots = []OfferedSlot{
		{
			StartTime:          time.Date(2026, 6, 11, 18, 0, 0, 0, time.UTC),
			EndTime:            time.Date(2026, 6, 11, 18, 45, 0, 0, time.UTC),
			StaffID:            "staff_2",
			StaffName:          "Lena Pham",
			StaffSelectionMode: booking.StaffSelectionAnyone,
		},
		{
			StartTime:          time.Date(2026, 6, 11, 19, 0, 0, 0, time.UTC),
			EndTime:            time.Date(2026, 6, 11, 19, 45, 0, 0, time.UTC),
			StaffID:            "staff_1",
			StaffName:          "Mai Nguyen",
			StaffSelectionMode: booking.StaffSelectionSpecific,
		},
	}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Lena is fine.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 0 {
		t.Fatalf("availability calls = %d, want 0 when selecting an offered technician", bookingTool.availabilityCalls)
	}
	if session.RequestedStartTime == nil || !session.RequestedStartTime.Equal(time.Date(2026, 6, 11, 18, 0, 0, 0, time.UTC)) {
		t.Fatalf("requested start = %v, want Lena offered slot", session.RequestedStartTime)
	}
	if session.StaffID != "staff_2" || session.StaffName != "Lena Pham" {
		t.Fatalf("assigned staff = %s/%s, want Lena Pham", session.StaffID, session.StaffName)
	}
	if len(session.OfferedSlots) != 0 {
		t.Fatalf("offered slots should be cleared after selecting Lena: %#v", session.OfferedSlots)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "What name") {
		t.Fatalf("AI reply should collect name after selecting technician alternative: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageParsesVietnameseWeekdayDate(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I want a classic manicure. Thứ Tư này tuần này.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityRequest.PreferredDate != "2026-06-10" {
		t.Fatalf("preferred date = %s, want 2026-06-10", bookingTool.availabilityRequest.PreferredDate)
	}
	if session.RequestedDate != "2026-06-10" {
		t.Fatalf("requested date = %q, want 2026-06-10", session.RequestedDate)
	}
	if strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "what day") {
		t.Fatalf("AI should not ask for day again after Vietnamese weekday: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageKeepsKnownDateOnUnclearRepairTurn(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.RequestedDate = "2026-06-11"
	store.session.Transcript = []TranscriptMessage{{
		Speaker: SpeakerAI,
		Body:    "I have Thursday. What time works best?",
	}}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "A...",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 0 || bookingTool.calls != 0 {
		t.Fatalf("repair turn should not call booking tools, availability=%d booking=%d", bookingTool.availabilityCalls, bookingTool.calls)
	}
	if session.RequestedDate != "2026-06-11" {
		t.Fatalf("requested date = %q, want preserved Thursday", session.RequestedDate)
	}
	if strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "what day") {
		t.Fatalf("repair turn should not ask for date again: %s", store.lastTurn.AIMessage)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Thursday") {
		t.Fatalf("repair reply should preserve last prompt context: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageDedupesProcessedEventKeyBeforeBooking(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{
		attempt: &booking.BookingAttempt{
			ID:           "attempt_1",
			Status:       booking.StatusConfirmed,
			POSBookingID: "booking_1",
			Appointment:  &booking.Appointment{ID: "appointment_1", Status: booking.StatusConfirmed},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow
	req := MessageRequest{
		Message:  "My name is Linh Tran, phone 312-555-0101, classic manicure with Mai on 2026-06-10 at 3pm.",
		EventKey: "twilio:CA123:realtime:item_1",
	}

	if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", req); err != nil {
		t.Fatalf("first Message returned error: %v", err)
	}
	if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", req); err != nil {
		t.Fatalf("duplicate Message returned error: %v", err)
	}
	if bookingTool.calls != 1 {
		t.Fatalf("booking calls = %d, want one after duplicate event key", bookingTool.calls)
	}
}

func TestMessageSelectsOfferedSlotThenCollectsCustomerName(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.OfferedSlots = []OfferedSlot{
		{
			StartTime: time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 6, 10, 15, 45, 0, 0, time.UTC),
			StaffID:   "staff_1",
			StaffName: "Mai Nguyen",
		},
		{
			StartTime: time.Date(2026, 6, 10, 16, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 6, 10, 16, 45, 0, 0, time.UTC),
			StaffID:   "staff_1",
			StaffName: "Mai Nguyen",
		},
	}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "The second one works.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking should not be called until customer details are collected")
	}
	if session.RequestedStartTime == nil || !session.RequestedStartTime.Equal(time.Date(2026, 6, 10, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("requested start = %v, want second offered slot", session.RequestedStartTime)
	}
	if session.StaffID != "staff_1" || session.StaffName != "Mai Nguyen" {
		t.Fatalf("staff = %s/%s, want offered slot staff", session.StaffID, session.StaffName)
	}
	if len(session.OfferedSlots) != 0 {
		t.Fatalf("offered slots should be cleared after selection: %#v", session.OfferedSlots)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "What name") {
		t.Fatalf("AI reply should collect name after slot selection: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageSelectsOfferedSlotBySpokenTimeWithoutAvailabilityRetry(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{name: "spoken_pm", message: "I prefer one p.m."},
		{name: "stt_bpm", message: "I was one BPM."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeConversationStore()
			store.session.Intent = IntentBooking
			store.session.ServiceID = "service_1"
			store.session.ServiceName = "Classic Manicure"
			store.session.RequestedDate = "2026-07-02"
			store.session.OfferedSlots = offeredPMSlots()
			bookingTool := &fakeBookingTool{}
			service := NewService(store, bookingTool)
			service.now = fixedNow

			session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
				Message: tt.message,
			})
			if err != nil {
				t.Fatalf("Message returned error: %v", err)
			}
			if bookingTool.availabilityCalls != 0 {
				t.Fatalf("availability calls = %d, want 0 when selecting an offered slot", bookingTool.availabilityCalls)
			}
			want := time.Date(2026, 7, 2, 18, 0, 0, 0, time.UTC)
			if session.RequestedStartTime == nil || !session.RequestedStartTime.Equal(want) {
				t.Fatalf("requested start = %v, want %s", session.RequestedStartTime, want)
			}
			if len(session.OfferedSlots) != 0 {
				t.Fatalf("offered slots should be cleared after time selection: %#v", session.OfferedSlots)
			}
			if strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "which time") {
				t.Fatalf("AI should not ask for time again after selecting 1 PM: %s", store.lastTurn.AIMessage)
			}
			if !strings.Contains(store.lastTurn.AIMessage, "What name") {
				t.Fatalf("AI reply should collect name after spoken time selection: %s", store.lastTurn.AIMessage)
			}
		})
	}
}

func TestMessageAffirmativeSelectsSlotMentionedByLastAIWithoutAvailabilityRetry(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.RequestedDate = "2026-07-02"
	store.session.OfferedSlots = offeredPMSlots()
	store.session.Transcript = []TranscriptMessage{{
		Speaker: SpeakerAI,
		Body:    "Does 1:00 PM work for you?",
	}}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Yes.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 0 {
		t.Fatalf("availability calls = %d, want 0 when confirming a mentioned offered slot", bookingTool.availabilityCalls)
	}
	want := time.Date(2026, 7, 2, 18, 0, 0, 0, time.UTC)
	if session.RequestedStartTime == nil || !session.RequestedStartTime.Equal(want) {
		t.Fatalf("requested start = %v, want %s", session.RequestedStartTime, want)
	}
	if len(session.OfferedSlots) != 0 {
		t.Fatalf("offered slots should be cleared after affirmative slot confirmation: %#v", session.OfferedSlots)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "What name") {
		t.Fatalf("AI reply should collect name after affirmative slot confirmation: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageAcceptsBareCustomerNameWhenNameSlotMissing(t *testing.T) {
	store := newFakeConversationStore()
	start := seedMissingCustomerNameSession(store, []string{"What name should I put on the appointment?"})
	bookingTool := &fakeBookingTool{
		availabilityResult: availabilityResultForStart("service_gel", "Gel Manicure", start),
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Mindwing.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.CustomerName != "Mindwing" {
		t.Fatalf("customer name = %q, want Mindwing", session.CustomerName)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking calls = %d, want 0 until phone is collected", bookingTool.calls)
	}
	if bookingTool.availabilityCalls != 0 {
		t.Fatalf("availability calls = %d, want 0 after the exact slot was already selected", bookingTool.availabilityCalls)
	}
	if !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "phone") {
		t.Fatalf("AI should move to phone after accepting name: %s", store.lastTurn.AIMessage)
	}
	if strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "what name") {
		t.Fatalf("AI should not ask for name again after Mindwing: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageConfirmsShortBareVoiceNameBeforeAccepting(t *testing.T) {
	store := newFakeConversationStore()
	seedMissingCustomerNameSession(store, []string{"What name should I put on the appointment?"})
	store.session.Channel = ChannelPhone
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Sim.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.CustomerName != "" {
		t.Fatalf("customer name = %q, want pending only until confirmation", session.CustomerName)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("pending name should not call tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if store.lastTurn.AIMetadata["pending_customer_name"] != "Sim" {
		t.Fatalf("pending metadata = %#v, want Sim", store.lastTurn.AIMetadata)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "I heard Sim") || !strings.Contains(store.lastTurn.AIMessage, "correct name") {
		t.Fatalf("AI should confirm risky short name: %s", store.lastTurn.AIMessage)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Yes.",
	})
	if err != nil {
		t.Fatalf("Message returned error after confirmation: %v", err)
	}
	if session.CustomerName != "Sim" {
		t.Fatalf("customer name = %q, want confirmed Sim", session.CustomerName)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("confirmed name should still wait for phone, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "phone") {
		t.Fatalf("AI should move to phone after name confirmation: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageAllowsCustomerToCorrectPendingVoiceName(t *testing.T) {
	store := newFakeConversationStore()
	seedMissingCustomerNameSession(store, []string{"What name should I put on the appointment?"})
	store.session.Channel = ChannelPhone
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "Sim."}); err != nil {
		t.Fatalf("Message returned error for initial name: %v", err)
	}
	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "No, Khiem.",
	})
	if err != nil {
		t.Fatalf("Message returned error for correction: %v", err)
	}
	if session.CustomerName != "" {
		t.Fatalf("customer name = %q, want pending corrected name until confirmation", session.CustomerName)
	}
	if store.lastTurn.AIMetadata["pending_customer_name"] != "Khiem" {
		t.Fatalf("pending metadata = %#v, want Khiem", store.lastTurn.AIMetadata)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "I heard Khiem") {
		t.Fatalf("AI should confirm corrected name: %s", store.lastTurn.AIMessage)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Yes.",
	})
	if err != nil {
		t.Fatalf("Message returned error after corrected confirmation: %v", err)
	}
	if session.CustomerName != "Khiem" {
		t.Fatalf("customer name = %q, want Khiem", session.CustomerName)
	}
}

func TestMessageAcceptsSpelledVoiceName(t *testing.T) {
	store := newFakeConversationStore()
	seedMissingCustomerNameSession(store, []string{"What name should I put on the appointment?"})
	store.session.Channel = ChannelPhone
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "K H I E M.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.CustomerName != "Khiem" {
		t.Fatalf("customer name = %q, want spelled Khiem", session.CustomerName)
	}
	if _, ok := store.lastTurn.AIMetadata["pending_customer_name"]; ok {
		t.Fatalf("spelled name should not remain pending: %#v", store.lastTurn.AIMetadata)
	}
	if !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "phone") {
		t.Fatalf("AI should move to phone after spelled name: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageAcceptsNormalFullVoiceName(t *testing.T) {
	tests := []struct {
		message string
		want    string
	}{
		{message: "Linh Tran.", want: "Linh Tran"},
		{message: "Nguyen Thi Minh Khai.", want: "Nguyen Thi Minh Khai"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			store := newFakeConversationStore()
			seedMissingCustomerNameSession(store, []string{"What name should I put on the appointment?"})
			store.session.Channel = ChannelPhone
			bookingTool := &fakeBookingTool{}
			service := NewService(store, bookingTool)
			service.now = fixedNow

			session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
				Message: tt.message,
			})
			if err != nil {
				t.Fatalf("Message returned error: %v", err)
			}
			if session.CustomerName != tt.want {
				t.Fatalf("customer name = %q, want %s", session.CustomerName, tt.want)
			}
			if _, ok := store.lastTurn.AIMetadata["pending_customer_name"]; ok {
				t.Fatalf("normal full name should not remain pending: %#v", store.lastTurn.AIMetadata)
			}
			if !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "phone") {
				t.Fatalf("AI should move to phone after full name: %s", store.lastTurn.AIMessage)
			}
		})
	}
}

func TestMessageConfirmsExplicitShortVoiceName(t *testing.T) {
	store := newFakeConversationStore()
	seedMissingCustomerNameSession(store, []string{"What name should I put on the appointment?"})
	store.session.Channel = ChannelPhone
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "My name is Sim.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.CustomerName != "" {
		t.Fatalf("customer name = %q, want pending only until confirmation", session.CustomerName)
	}
	if store.lastTurn.AIMetadata["pending_customer_name"] != "Sim" {
		t.Fatalf("pending metadata = %#v, want Sim", store.lastTurn.AIMetadata)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "I heard Sim") || !strings.Contains(store.lastTurn.AIMessage, "correct name") {
		t.Fatalf("AI should confirm explicit short voice name: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageRepromptsForLowQualityVoiceName(t *testing.T) {
	store := newFakeConversationStore()
	seedMissingCustomerNameSession(store, []string{"What name should I put on the appointment?"})
	store.session.Channel = ChannelPhone
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Es wus für mi.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.CustomerName != "" {
		t.Fatalf("customer name = %q, want empty for low-quality voice name", session.CustomerName)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("low-quality name should not call booking tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "spell") {
		t.Fatalf("AI should ask caller to spell low-quality name: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageRepromptsWithoutTreatingYesAsCustomerName(t *testing.T) {
	store := newFakeConversationStore()
	seedMissingCustomerNameSession(store, []string{"What name should I put on the appointment?"})
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Yes, you can.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.Status != StatusActive || session.CustomerName != "" {
		t.Fatalf("session status/name = %s/%q, want active with no name", session.Status, session.CustomerName)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("non-answer should not call booking tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "customer name") {
		t.Fatalf("AI should clarify it needs the customer name: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageRestatesSelectedTimeWhenCallerRepeatsTimeInNameSlot(t *testing.T) {
	store := newFakeConversationStore()
	seedMissingCustomerNameSession(store, []string{
		"What name should I put on the appointment?",
		"Can I have the name for the appointment, please?",
		"Could you please provide the name for the appointment?",
	})
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "1 PM.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.Status != StatusActive || session.CustomerName != "" {
		t.Fatalf("session status/name = %s/%q, want active with no name", session.Status, session.CustomerName)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("time repeat in name slot should not call tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	reply := store.lastTurn.AIMessage
	if !strings.Contains(reply, "1:00 PM") || !strings.Contains(reply, "What name") {
		t.Fatalf("AI should restate selected time and ask for name: %s", reply)
	}
	if strings.Contains(strings.ToLower(reply), "trouble catching") || strings.Contains(strings.ToLower(reply), "not a confirmed appointment") {
		t.Fatalf("AI should not hand off just because caller repeated the time: %s", reply)
	}
}

func TestMessageEscalatesAfterRepeatedCustomerNameNonAnswers(t *testing.T) {
	tests := []string{
		"Yes, you can.",
		"ออสเทรลเมนิคิว",
		"Es wus für mi.",
	}
	for _, message := range tests {
		t.Run(message, func(t *testing.T) {
			store := newFakeConversationStore()
			seedMissingCustomerNameSession(store, []string{
				"What name should I put on the appointment?",
				"Can I have the name for the appointment, please?",
				"Could you please provide the name for the appointment?",
			})
			store.session.Channel = ChannelPhone
			bookingTool := &fakeBookingTool{}
			service := NewService(store, bookingTool)
			service.now = fixedNow

			session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
				Message: message,
			})
			if err != nil {
				t.Fatalf("Message returned error: %v", err)
			}
			if session.Status != StatusHandoff || session.Outcome != OutcomeHandoffRequested {
				t.Fatalf("session status/outcome = %s/%s, want handoff/handoff_requested", session.Status, session.Outcome)
			}
			if store.lastTurn.Handoff == nil || store.lastTurn.Handoff.Reason != HandoffReasonCustomerDetailsUnavailable {
				t.Fatalf("handoff = %#v, want customer_details_unavailable", store.lastTurn.Handoff)
			}
			if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
				t.Fatalf("handoff should not call booking tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
			}
			if !strings.Contains(store.lastTurn.AIMessage, "not a confirmed appointment") {
				t.Fatalf("handoff reply must avoid confirmation: %s", store.lastTurn.AIMessage)
			}
		})
	}
}

func TestMessageDoesNotTreatServicePhraseAsCustomerName(t *testing.T) {
	store := newFakeConversationStore()
	start := seedMissingCustomerNameSession(store, []string{"What name should I put on the appointment?"})
	store.services = append(store.services, ServiceOption{
		ID:              "service_shell",
		Name:            "Shell Manicure",
		DurationMinutes: 45,
		PriceFrom:       40,
	})
	bookingTool := &fakeBookingTool{
		availabilityResult: availabilityResultForStart("service_shell", "Shell Manicure", start),
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Name of service, Shell Manicure.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.CustomerName != "" {
		t.Fatalf("customer name = %q, want empty for service phrase", session.CustomerName)
	}
	if session.ServiceID != "service_shell" {
		t.Fatalf("service id = %s, want service correction to service_shell", session.ServiceID)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 1 {
		t.Fatalf("service phrase in name slot should recheck availability without booking, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Shell Manicure") || !strings.Contains(store.lastTurn.AIMessage, "What name") {
		t.Fatalf("AI should confirm corrected service and return to name collection: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageDoesNotTreatServiceAliasAsCustomerName(t *testing.T) {
	store := newFakeConversationStore()
	start := seedMissingCustomerNameSession(store, []string{"What name should I put on the appointment?"})
	store.services = append(store.services, ServiceOption{
		ID:              "service_shell",
		Name:            "Shellac Gel Manicure",
		DurationMinutes: 45,
		PriceFrom:       40,
	})
	store.serviceAliases = []ServiceAlias{{
		ID:              "alias_shell",
		ServiceID:       "service_shell",
		Alias:           "shell",
		NormalizedAlias: "shell",
		Source:          "correction",
		Confidence:      0.96,
	}}
	bookingTool := &fakeBookingTool{
		availabilityResult: availabilityResultForStart("service_shell", "Shellac Gel Manicure", start),
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Shell.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.CustomerName != "" {
		t.Fatalf("customer name = %q, want empty for service alias phrase", session.CustomerName)
	}
	if session.ServiceID != "service_shell" {
		t.Fatalf("service id = %s, want service correction to service_shell", session.ServiceID)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 1 {
		t.Fatalf("service alias in name slot should recheck availability without booking, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Shellac Gel Manicure") || !strings.Contains(store.lastTurn.AIMessage, "What name") {
		t.Fatalf("AI should confirm corrected alias service and return to name collection: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageChangesServiceAndRefreshesOfferedSlotsBeforeBooking(t *testing.T) {
	store := newFakeConversationStore()
	store.services = append(store.services, ServiceOption{
		ID:              "service_gel",
		Name:            "Gel Manicure",
		DurationMinutes: 45,
		PriceFrom:       38,
	})
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.RequestedDate = "2026-07-02"
	store.session.StaffSelectionMode = booking.StaffSelectionAnyone
	store.session.BookingSegments = []booking.BookingSegmentRequest{{
		ServiceID:          "service_1",
		StaffSelectionMode: booking.StaffSelectionAnyone,
	}}
	store.session.OfferedSlots = offeredPMSlots()
	newSlotStart := time.Date(2026, 7, 2, 20, 0, 0, 0, time.UTC)
	bookingTool := &fakeBookingTool{
		availabilityResult: availabilityResultForStart("service_gel", "Gel Manicure", newSlotStart),
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Actually Gel Manicure.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1 for corrected service", bookingTool.availabilityCalls)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking calls = %d, want 0 before customer picks refreshed slot", bookingTool.calls)
	}
	if bookingTool.availabilityRequest.ServiceID != "service_gel" {
		t.Fatalf("availability service = %s, want service_gel", bookingTool.availabilityRequest.ServiceID)
	}
	if got := bookingTool.availabilityRequest.Segments; len(got) != 1 || got[0].ServiceID != "service_gel" {
		t.Fatalf("availability segments = %#v, want corrected service_gel segment", got)
	}
	if session.ServiceID != "service_gel" {
		t.Fatalf("session service = %s, want service_gel", session.ServiceID)
	}
	if got := session.BookingSegments; len(got) != 1 || got[0].ServiceID != "service_gel" {
		t.Fatalf("session booking segments = %#v, want corrected service_gel segment", got)
	}
	if len(session.OfferedSlots) != 1 || !session.OfferedSlots[0].StartTime.Equal(newSlotStart) {
		t.Fatalf("offered slots = %#v, want refreshed slot for corrected service", session.OfferedSlots)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "3:00 PM") {
		t.Fatalf("AI reply should offer refreshed slot: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageBooksCorrectedServiceForExistingExactTime(t *testing.T) {
	store := newFakeConversationStore()
	store.services = append(store.services, ServiceOption{
		ID:              "service_gel",
		Name:            "Gel Manicure",
		DurationMinutes: 45,
		PriceFrom:       38,
	})
	start := time.Date(2026, 7, 2, 18, 0, 0, 0, time.UTC)
	store.session.Intent = IntentBooking
	store.session.CustomerName = "Sim"
	store.session.CustomerPhone = "+13125550101"
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.RequestedDate = "2026-07-02"
	store.session.RequestedStartTime = &start
	store.session.StaffSelectionMode = booking.StaffSelectionAnyone
	store.session.BookingSegments = []booking.BookingSegmentRequest{{
		ServiceID:          "service_1",
		StaffSelectionMode: booking.StaffSelectionAnyone,
	}}
	bookingTool := &fakeBookingTool{
		availabilityResult: availabilityResultForStart("service_gel", "Gel Manicure", start),
		attempt: &booking.BookingAttempt{
			ID:           "attempt_corrected",
			Status:       booking.StatusConfirmed,
			POSBookingID: "booking_corrected",
			Appointment:  &booking.Appointment{ID: "appointment_corrected", Status: booking.StatusConfirmed},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Actually Gel Manicure.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1 for corrected exact time", bookingTool.availabilityCalls)
	}
	if bookingTool.calls != 1 {
		t.Fatalf("booking calls = %d, want 1 after corrected exact time is available", bookingTool.calls)
	}
	if bookingTool.request.ServiceID != "service_gel" {
		t.Fatalf("booking service = %s, want service_gel", bookingTool.request.ServiceID)
	}
	if got := bookingTool.request.Segments; len(got) != 1 || got[0].ServiceID != "service_gel" {
		t.Fatalf("booking segments = %#v, want corrected service_gel segment", got)
	}
	if session.Outcome != OutcomeBookingConfirmed || session.AppointmentID != "appointment_corrected" {
		t.Fatalf("session outcome/link = %s/%s, want confirmed corrected appointment", session.Outcome, session.AppointmentID)
	}
	if strings.Contains(store.lastTurn.AIMessage, "Classic Manicure") || !strings.Contains(store.lastTurn.AIMessage, "Gel Manicure") {
		t.Fatalf("confirmed reply should use corrected service only: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageClearsServiceForAmbiguousServiceCorrection(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{
			ID:              "service_dip",
			Name:            "Dip Powder Manicure",
			DurationMinutes: 75,
			PriceFrom:       55,
		},
		{
			ID:              "service_1",
			Name:            "Classic Manicure",
			DurationMinutes: 45,
			PriceFrom:       35,
		},
		{
			ID:              "service_gel",
			Name:            "Gel Manicure",
			DurationMinutes: 45,
			PriceFrom:       38,
		},
	}
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.RequestedDate = "2026-07-02"
	store.session.StaffSelectionMode = booking.StaffSelectionAnyone
	store.session.BookingSegments = []booking.BookingSegmentRequest{{
		ServiceID:          "service_1",
		StaffSelectionMode: booking.StaffSelectionAnyone,
	}}
	store.session.OfferedSlots = offeredPMSlots()
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Actually child manicure.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 0 || bookingTool.calls != 0 {
		t.Fatalf("ambiguous correction should not call tools, availability=%d booking=%d", bookingTool.availabilityCalls, bookingTool.calls)
	}
	if session.ServiceID != "" || len(session.BookingSegments) != 0 || len(session.OfferedSlots) != 0 {
		t.Fatalf("session service/segments/slots = %q/%#v/%#v, want cleared service state", session.ServiceID, session.BookingSegments, session.OfferedSlots)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Which manicure service") ||
		!strings.Contains(store.lastTurn.AIMessage, "Thursday") ||
		!strings.Contains(store.lastTurn.AIMessage, "Classic Manicure") ||
		!strings.Contains(store.lastTurn.AIMessage, "Gel Manicure") ||
		!strings.Contains(store.lastTurn.AIMessage, "Dip Powder Manicure") {
		t.Fatalf("ambiguous service correction should ask for service clarification: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageHandoffsWhenCallerEndsWhileCustomerNameMissing(t *testing.T) {
	store := newFakeConversationStore()
	seedMissingCustomerNameSession(store, []string{"What name should I put on the appointment?"})
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "OK, bye-bye.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.Status != StatusHandoff || session.Outcome != OutcomeHandoffRequested {
		t.Fatalf("session status/outcome = %s/%s, want handoff/handoff_requested", session.Status, session.Outcome)
	}
	if store.lastTurn.Handoff == nil || store.lastTurn.Handoff.Reason != HandoffReasonCustomerDetailsUnavailable {
		t.Fatalf("handoff = %#v, want customer_details_unavailable", store.lastTurn.Handoff)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("goodbye should not call booking tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "not a confirmed appointment") {
		t.Fatalf("goodbye handoff reply must avoid confirmation: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageRepeatsExistingOfferedSlotsForUnclearTimeWithoutAvailabilityRetry(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.RequestedDate = "2026-07-02"
	store.session.OfferedSlots = offeredPMSlots()
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "a tough.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 0 {
		t.Fatalf("availability calls = %d, want 0 when existing offered slots can be repeated", bookingTool.availabilityCalls)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking calls = %d, want 0 for unclear slot response", bookingTool.calls)
	}
	if session.RequestedStartTime != nil {
		t.Fatalf("requested start should remain unset for unclear time: %s", session.RequestedStartTime)
	}
	if len(session.OfferedSlots) != 3 {
		t.Fatalf("offered slots = %#v, want original three slots preserved", session.OfferedSlots)
	}
	if store.lastTurn.ToolMessage != "" {
		t.Fatalf("tool message = %q, want no new availability tool result", store.lastTurn.ToolMessage)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "1:00 PM") || !strings.Contains(store.lastTurn.AIMessage, "Which works") {
		t.Fatalf("AI reply should repeat existing offered slots: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageRejectsOfferedSlotAsTooEarlyWithoutSelectingIt(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.RequestedDate = "2026-07-02"
	store.session.OfferedSlots = offeredPMSlots()
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "12 p.m. is too early.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("slot rejection should not call tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if session.RequestedStartTime != nil {
		t.Fatalf("rejected slot should not be selected: %s", session.RequestedStartTime)
	}
	if len(session.OfferedSlots) != 2 {
		t.Fatalf("offered slots = %#v, want two later slots", session.OfferedSlots)
	}
	reply := store.lastTurn.AIMessage
	if strings.Contains(reply, "12:00 PM") || !strings.Contains(reply, "12:30 PM") || !strings.Contains(reply, "1:00 PM") {
		t.Fatalf("reply should remove rejected 12 PM and offer later slots: %s", reply)
	}
	if strings.Contains(strings.ToLower(reply), "what name") {
		t.Fatalf("reply should not collect name before a valid slot is chosen: %s", reply)
	}
	if store.lastTurn.AIMetadata["slot_time_preference_direction"] != "after" {
		t.Fatalf("slot preference metadata = %#v, want after", store.lastTurn.AIMetadata)
	}
}

func TestMessageBareServiceSwitchAfterRejectedSlotRefreshesAvailabilityWithPreference(t *testing.T) {
	store := newFakeConversationStore()
	store.services = append(store.services, ServiceOption{
		ID:              "service_removal",
		Name:            "Gel Removal",
		DurationMinutes: 30,
		PriceFrom:       15,
	})
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.RequestedDate = "2026-07-02"
	store.session.StaffSelectionMode = booking.StaffSelectionAnyone
	store.session.BookingSegments = []booking.BookingSegmentRequest{{
		ServiceID:          "service_1",
		StaffSelectionMode: booking.StaffSelectionAnyone,
	}}
	store.session.OfferedSlots = offeredPMSlots()
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "12 p.m. is too early.",
	}); err != nil {
		t.Fatalf("Message returned error for slot rejection: %v", err)
	}

	bookingTool.availabilityResult = &booking.AvailabilityResult{
		ServiceID:          "service_removal",
		ServiceName:        "Gel Removal",
		StaffSelectionMode: booking.StaffSelectionAnyone,
		PreferredDate:      "2026-07-02",
		DurationMinutes:    30,
		Timezone:           "America/Chicago",
		Slots: []booking.AvailabilitySlot{
			{
				StartTime:          time.Date(2026, 7, 2, 17, 0, 0, 0, time.UTC),
				EndTime:            time.Date(2026, 7, 2, 17, 30, 0, 0, time.UTC),
				StaffID:            "staff_1",
				StaffName:          "Mai Nguyen",
				StaffSelectionMode: booking.StaffSelectionAnyone,
			},
			{
				StartTime:          time.Date(2026, 7, 2, 17, 30, 0, 0, time.UTC),
				EndTime:            time.Date(2026, 7, 2, 18, 0, 0, 0, time.UTC),
				StaffID:            "staff_1",
				StaffName:          "Mai Nguyen",
				StaffSelectionMode: booking.StaffSelectionAnyone,
			},
			{
				StartTime:          time.Date(2026, 7, 2, 18, 0, 0, 0, time.UTC),
				EndTime:            time.Date(2026, 7, 2, 18, 30, 0, 0, time.UTC),
				StaffID:            "staff_1",
				StaffName:          "Mai Nguyen",
				StaffSelectionMode: booking.StaffSelectionAnyone,
			},
		},
	}
	bookingTool.availabilityCalls = 0
	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Gel Removal.",
	})
	if err != nil {
		t.Fatalf("Message returned error for service switch: %v", err)
	}
	if bookingTool.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1 for bare service switch", bookingTool.availabilityCalls)
	}
	if bookingTool.availabilityRequest.ServiceID != "service_removal" || bookingTool.availabilityRequest.Limit != exactAvailabilityLimit {
		t.Fatalf("availability request = %#v, want removal with expanded limit", bookingTool.availabilityRequest)
	}
	if session.ServiceID != "service_removal" || len(session.BookingSegments) != 1 || session.BookingSegments[0].ServiceID != "service_removal" {
		t.Fatalf("service state = %s segments %#v, want Gel Removal", session.ServiceID, session.BookingSegments)
	}
	if len(session.OfferedSlots) != 2 {
		t.Fatalf("offered slots = %#v, want filtered later slots", session.OfferedSlots)
	}
	reply := store.lastTurn.AIMessage
	if strings.Contains(reply, "12:00 PM") || !strings.Contains(reply, "12:30 PM") || !strings.Contains(reply, "1:00 PM") {
		t.Fatalf("service switch reply should keep rejected time preference: %s", reply)
	}
	if strings.Contains(reply, "Mai Nguyen") {
		t.Fatalf("anyone availability should not name assigned technician: %s", reply)
	}
}

func TestMessageDoesNotSelectAmbiguousOfferedTime(t *testing.T) {
	store := newFakeConversationStore()
	store.staff = append(store.staff, StaffOption{
		ID:         "staff_2",
		Name:       "Lena Pham",
		AIBookable: true,
	})
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.RequestedDate = "2026-07-02"
	store.session.OfferedSlots = []OfferedSlot{
		{
			StartTime: time.Date(2026, 7, 2, 18, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 7, 2, 18, 45, 0, 0, time.UTC),
			StaffID:   "staff_1",
			StaffName: "Mai Nguyen",
		},
		{
			StartTime: time.Date(2026, 7, 2, 18, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 7, 2, 18, 45, 0, 0, time.UTC),
			StaffID:   "staff_2",
			StaffName: "Lena Pham",
		},
	}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "one p.m.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 0 || bookingTool.calls != 0 {
		t.Fatalf("ambiguous time should not call tools, availability=%d booking=%d", bookingTool.availabilityCalls, bookingTool.calls)
	}
	if session.RequestedStartTime != nil {
		t.Fatalf("ambiguous offered time should not select a slot: %s", session.RequestedStartTime)
	}
	if len(session.OfferedSlots) != 2 {
		t.Fatalf("offered slots = %#v, want ambiguous slots preserved", session.OfferedSlots)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "1:00 PM") || !strings.Contains(store.lastTurn.AIMessage, "Which works") {
		t.Fatalf("AI reply should repeat ambiguous offered slots: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageBooksSelectedAvailableSlotAfterCustomerDetails(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Intent = IntentBooking
	store.session.CustomerName = "Linh Tran"
	store.session.CustomerPhone = "+13125550101"
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.OfferedSlots = []OfferedSlot{
		{
			StartTime: time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 6, 10, 15, 45, 0, 0, time.UTC),
			StaffID:   "staff_1",
			StaffName: "Mai Nguyen",
		},
		{
			StartTime: time.Date(2026, 6, 10, 16, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 6, 10, 16, 45, 0, 0, time.UTC),
			StaffID:   "staff_1",
			StaffName: "Mai Nguyen",
		},
	}
	bookingTool := &fakeBookingTool{
		attempt: &booking.BookingAttempt{
			ID:           "attempt_selected",
			Status:       booking.StatusConfirmed,
			POSBookingID: "booking_selected",
			Appointment:  &booking.Appointment{ID: "appointment_selected", Status: booking.StatusConfirmed},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "The second one works.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 1 {
		t.Fatalf("booking calls = %d, want 1", bookingTool.calls)
	}
	if !bookingTool.request.StartTime.Equal(time.Date(2026, 6, 10, 16, 0, 0, 0, time.UTC)) || bookingTool.request.StaffID != "staff_1" {
		t.Fatalf("booking request = %#v, want selected slot", bookingTool.request)
	}
	if session.Outcome != OutcomeBookingConfirmed || session.AppointmentID != "appointment_selected" {
		t.Fatalf("session outcome/link = %s/%s, want confirmed appointment", session.Outcome, session.AppointmentID)
	}
	assertCustomerReplyHidesProvider(t, store.lastTurn.AIMessage)
}

func TestMessageBooksAnyoneMultiServiceSlotWithSegmentAssignments(t *testing.T) {
	store := newFakeConversationStore()
	store.services = append(store.services, ServiceOption{
		ID:              "service_2",
		Name:            "Gel Removal",
		DurationMinutes: 30,
		PriceFrom:       15,
	})
	store.staff = append(store.staff, StaffOption{
		ID:   "staff_2",
		Name: "Lena Pham",
	})
	slotStart := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	bookingTool := &fakeBookingTool{
		attempt: &booking.BookingAttempt{
			ID:           "attempt_anyone_segments",
			Status:       booking.StatusConfirmed,
			POSBookingID: "booking_anyone_segments",
			Appointment:  &booking.Appointment{ID: "appointment_anyone_segments", Status: booking.StatusConfirmed},
		},
		availabilityResult: &booking.AvailabilityResult{
			ServiceID:          "service_1",
			ServiceName:        "Classic Manicure",
			StaffSelectionMode: booking.StaffSelectionAnyone,
			PreferredDate:      "2026-06-10",
			DurationMinutes:    75,
			Timezone:           "America/Chicago",
			Slots: []booking.AvailabilitySlot{
				{
					StartTime:          slotStart,
					EndTime:            slotStart.Add(75 * time.Minute),
					StaffID:            "staff_1",
					StaffName:          "Mai Nguyen",
					StaffSelectionMode: booking.StaffSelectionAnyone,
					Segments: []booking.AvailabilitySegment{
						{
							ServiceID:          "service_1",
							ServiceName:        "Classic Manicure",
							StaffID:            "staff_1",
							StaffName:          "Mai Nguyen",
							StaffSelectionMode: booking.StaffSelectionAnyone,
							DurationMinutes:    45,
						},
						{
							ServiceID:          "service_2",
							ServiceName:        "Gel Removal",
							StaffID:            "staff_2",
							StaffName:          "Lena Pham",
							StaffSelectionMode: booking.StaffSelectionAnyone,
							DurationMinutes:    30,
						},
					},
				},
			},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I need a classic manicure and gel removal tomorrow with anyone.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1", bookingTool.availabilityCalls)
	}
	if bookingTool.availabilityRequest.StaffSelectionMode != booking.StaffSelectionAnyone {
		t.Fatalf("availability staff selection mode = %s, want anyone", bookingTool.availabilityRequest.StaffSelectionMode)
	}
	if got := bookingTool.availabilityRequest.Segments; len(got) != 2 || got[0].ServiceID != "service_1" || got[1].ServiceID != "service_2" {
		t.Fatalf("availability segments = %#v, want both requested services", got)
	}
	if len(session.OfferedSlots) != 1 || len(session.OfferedSlots[0].Segments) != 2 {
		t.Fatalf("offered slots = %#v, want one slot with two assigned segments", session.OfferedSlots)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "available technicians") ||
		strings.Contains(store.lastTurn.AIMessage, "Mai Nguyen") ||
		strings.Contains(store.lastTurn.AIMessage, "Lena Pham") {
		t.Fatalf("anyone slot offer should avoid naming assigned technicians: %s", store.lastTurn.AIMessage)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "The first one works. My name is Linh Tran, phone 312-555-0101.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 1 {
		t.Fatalf("booking calls = %d, want 1", bookingTool.calls)
	}
	if bookingTool.request.StaffSelectionMode != booking.StaffSelectionAnyone {
		t.Fatalf("booking staff selection mode = %s, want anyone", bookingTool.request.StaffSelectionMode)
	}
	if got := bookingTool.request.Segments; len(got) != 2 {
		t.Fatalf("booking segments = %#v, want two", got)
	} else {
		if got[0].ServiceID != "service_1" || got[0].StaffID != "staff_1" || got[0].StaffSelectionMode != booking.StaffSelectionAnyone {
			t.Fatalf("first booking segment = %#v, want service_1/staff_1/anyone", got[0])
		}
		if got[1].ServiceID != "service_2" || got[1].StaffID != "staff_2" || got[1].StaffSelectionMode != booking.StaffSelectionAnyone {
			t.Fatalf("second booking segment = %#v, want service_2/staff_2/anyone", got[1])
		}
	}
	if session.Outcome != OutcomeBookingConfirmed || session.AppointmentID != "appointment_anyone_segments" {
		t.Fatalf("session outcome/link = %s/%s, want confirmed appointment", session.Outcome, session.AppointmentID)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "available technicians") ||
		strings.Contains(store.lastTurn.AIMessage, "Mai Nguyen") ||
		strings.Contains(store.lastTurn.AIMessage, "Lena Pham") {
		t.Fatalf("anyone confirmation should avoid naming assigned technicians: %s", store.lastTurn.AIMessage)
	}
	assertCustomerReplyHidesProvider(t, store.lastTurn.AIMessage)
}

func TestMessageAddsServiceToExistingAppointmentBeforeAvailability(t *testing.T) {
	store := newFakeConversationStore()
	store.services = append(store.services, ServiceOption{
		ID:              "service_removal",
		Name:            "Gel Removal",
		DurationMinutes: 30,
		PriceFrom:       15,
	})
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.StaffSelectionMode = booking.StaffSelectionAnyone
	store.session.BookingSegments = []booking.BookingSegmentRequest{{
		ServiceID:          "service_1",
		StaffSelectionMode: booking.StaffSelectionAnyone,
	}}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I also book Gel Removal.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 0 || bookingTool.calls != 0 {
		t.Fatalf("add-on before date should not call tools, availability=%d booking=%d", bookingTool.availabilityCalls, bookingTool.calls)
	}
	if session.ServiceID != "service_1" {
		t.Fatalf("primary service = %s, want Classic Manicure primary", session.ServiceID)
	}
	if got := session.BookingSegments; len(got) != 2 || got[0].ServiceID != "service_1" || got[1].ServiceID != "service_removal" {
		t.Fatalf("booking segments = %#v, want Classic Manicure plus Gel Removal", got)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Classic Manicure and Gel Removal") || !strings.Contains(store.lastTurn.AIMessage, "What day") {
		t.Fatalf("AI should acknowledge combo and ask for date: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageClarifiesBareServiceAfterExistingServiceWithoutScheduleContext(t *testing.T) {
	store := newFakeConversationStore()
	store.services = append(store.services, ServiceOption{
		ID:              "service_removal",
		Name:            "Gel Removal",
		DurationMinutes: 30,
		PriceFrom:       15,
	})
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.StaffSelectionMode = booking.StaffSelectionAnyone
	store.session.BookingSegments = []booking.BookingSegmentRequest{{
		ServiceID:          "service_1",
		StaffSelectionMode: booking.StaffSelectionAnyone,
	}}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Gel Removal.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 0 || bookingTool.calls != 0 {
		t.Fatalf("bare service clarification should not call tools, availability=%d booking=%d", bookingTool.availabilityCalls, bookingTool.calls)
	}
	if session.ServiceID != "service_1" || len(session.BookingSegments) != 1 || session.BookingSegments[0].ServiceID != "service_1" {
		t.Fatalf("session service state = %s %#v, want unchanged Classic Manicure", session.ServiceID, session.BookingSegments)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "add Gel Removal to Classic Manicure") || !strings.Contains(store.lastTurn.AIMessage, "switch to Gel Removal only") {
		t.Fatalf("AI should clarify add vs switch: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageAcknowledgesBareServiceSwitchAfterSlotOffer(t *testing.T) {
	store := newFakeConversationStore()
	store.services = append(store.services, ServiceOption{
		ID:              "service_pedicure",
		Name:            "Classic Pedicure",
		DurationMinutes: 45,
		PriceFrom:       40,
	})
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.RequestedDate = "2026-07-06"
	store.session.StaffSelectionMode = booking.StaffSelectionAnyone
	store.session.BookingSegments = []booking.BookingSegmentRequest{{
		ServiceID:          "service_1",
		StaffSelectionMode: booking.StaffSelectionAnyone,
	}}
	store.session.OfferedSlots = offeredPMSlots()
	pedicureStart := time.Date(2026, 7, 6, 18, 30, 0, 0, time.UTC)
	bookingTool := &fakeBookingTool{
		availabilityResult: availabilityResultForStart("service_pedicure", "Classic Pedicure", pedicureStart),
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Classic Pedicure.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 1 || bookingTool.calls != 0 {
		t.Fatalf("switch should recheck availability only, availability=%d booking=%d", bookingTool.availabilityCalls, bookingTool.calls)
	}
	if session.ServiceID != "service_pedicure" {
		t.Fatalf("service = %s, want Classic Pedicure", session.ServiceID)
	}
	if got := session.BookingSegments; len(got) != 1 || got[0].ServiceID != "service_pedicure" {
		t.Fatalf("booking segments = %#v, want Classic Pedicure only", got)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Switching to Classic Pedicure.") || !strings.Contains(store.lastTurn.AIMessage, "For your Classic Pedicure") {
		t.Fatalf("switch reply should acknowledge service change and offer slots: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageRepeatingSelectedServiceWhenNameRequestedAsksForName(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{{
		ID:              "service_pedicure",
		Name:            "Classic Pedicure",
		DurationMinutes: 45,
		PriceFrom:       40,
	}}
	start := time.Date(2026, 7, 6, 18, 30, 0, 0, time.UTC)
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_pedicure"
	store.session.ServiceName = "Classic Pedicure"
	store.session.RequestedDate = "2026-07-06"
	store.session.RequestedStartTime = &start
	store.session.CustomerPhone = "+13125550101"
	store.session.StaffSelectionMode = booking.StaffSelectionAnyone
	store.session.BookingSegments = []booking.BookingSegmentRequest{{
		ServiceID:          "service_pedicure",
		StaffSelectionMode: booking.StaffSelectionAnyone,
	}}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Classic Pedicure.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("service repeat in name slot should not call tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if session.CustomerName != "" {
		t.Fatalf("customer name = %q, want empty", session.CustomerName)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "I have Classic Pedicure already") || !strings.Contains(store.lastTurn.AIMessage, "What name") {
		t.Fatalf("service repeat name-slot reply = %s", store.lastTurn.AIMessage)
	}
}

func TestMessageCompletesLatestTranscriptAfterServiceInquiryAndSwitch(t *testing.T) {
	store := newFakeConversationStore()
	store.session.Channel = ChannelPhone
	store.session.CustomerPhone = "+13125550101"
	store.services = []ServiceOption{
		{ID: "service_acrylic", Name: "Acrylic Full Set", DurationMinutes: 75},
		{ID: "service_1", Name: "Classic Manicure", DurationMinutes: 45},
		{ID: "service_pedicure", Name: "Classic Pedicure", DurationMinutes: 45},
		{ID: "service_dip", Name: "Dip Powder Manicure", DurationMinutes: 60},
		{ID: "service_gel", Name: "Gel Manicure", DurationMinutes: 45},
		{ID: "service_removal", Name: "Gel Removal", DurationMinutes: 30},
		{ID: "service_spa_pedicure", Name: "Spa Pedicure", DurationMinutes: 60},
	}
	manicureStart := time.Date(2026, 7, 6, 17, 0, 0, 0, time.UTC)
	pedicureStart := time.Date(2026, 7, 6, 18, 30, 0, 0, time.UTC)
	bookingTool := &fakeBookingTool{
		availabilityResults: []*booking.AvailabilityResult{
			availabilityResultForStart("service_1", "Classic Manicure", manicureStart),
			availabilityResultForStart("service_pedicure", "Classic Pedicure", pedicureStart),
		},
		attempt: &booking.BookingAttempt{
			ID:           "attempt_latest_transcript",
			Status:       booking.StatusConfirmed,
			POSBookingID: "booking_latest_transcript",
			Appointment:  &booking.Appointment{ID: "appointment_latest_transcript", Status: booking.StatusConfirmed},
		},
	}
	service := NewService(store, bookingTool)
	service.now = func() time.Time {
		return time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	}

	steps := []string{
		"What service do you have?",
		"Do you have gel removal?",
		"I want to book the service Classic Manicure.",
		"Next Monday.",
		"classic pedicure.",
		"1:30 PM.",
		"Classic Pedicure.",
		"Kevin.",
		"Yes, that's my name.",
	}
	var session *Session
	var err error
	for _, message := range steps {
		session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
			Message: message,
		})
		if err != nil {
			t.Fatalf("Message %q returned error: %v", message, err)
		}
	}

	if bookingTool.availabilityCalls != 2 {
		t.Fatalf("availability calls = %d, want 2", bookingTool.availabilityCalls)
	}
	if bookingTool.calls != 1 {
		t.Fatalf("booking calls = %d, want 1", bookingTool.calls)
	}
	if bookingTool.request.ServiceID != "service_pedicure" {
		t.Fatalf("booking service = %s, want Classic Pedicure", bookingTool.request.ServiceID)
	}
	if got := bookingTool.request.Segments; len(got) != 1 || got[0].ServiceID != "service_pedicure" {
		t.Fatalf("booking segments = %#v, want Classic Pedicure only", got)
	}
	if bookingTool.request.CustomerName != "Kevin" {
		t.Fatalf("booking customer name = %q, want Kevin", bookingTool.request.CustomerName)
	}
	if session == nil || session.Outcome != OutcomeBookingConfirmed || session.AppointmentID != "appointment_latest_transcript" {
		t.Fatalf("session outcome/link = %#v, want confirmed latest transcript appointment", session)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Classic Pedicure") || !strings.Contains(store.lastTurn.AIMessage, "Kevin") {
		t.Fatalf("confirmation reply = %s", store.lastTurn.AIMessage)
	}
}

func TestMessageBooksAddedServiceOnlyAfterComboAvailabilityAndNameConfirmation(t *testing.T) {
	store := newFakeConversationStore()
	store.services = append(store.services, ServiceOption{
		ID:              "service_removal",
		Name:            "Gel Removal",
		DurationMinutes: 30,
		PriceFrom:       15,
	})
	store.staff = append(store.staff, StaffOption{
		ID:         "staff_2",
		Name:       "Lena Pham",
		AIBookable: true,
	})
	store.session.Channel = ChannelPhone
	store.session.Intent = IntentBooking
	store.session.CustomerPhone = "+13125550101"
	store.session.ServiceID = "service_1"
	store.session.ServiceName = "Classic Manicure"
	store.session.StaffSelectionMode = booking.StaffSelectionAnyone
	store.session.BookingSegments = []booking.BookingSegmentRequest{{
		ServiceID:          "service_1",
		StaffSelectionMode: booking.StaffSelectionAnyone,
	}}
	slotStart := time.Date(2026, 6, 15, 18, 0, 0, 0, time.UTC)
	bookingTool := &fakeBookingTool{
		availabilityResult: &booking.AvailabilityResult{
			ServiceID:          "service_1",
			ServiceName:        "Classic Manicure",
			StaffSelectionMode: booking.StaffSelectionAnyone,
			PreferredDate:      "2026-06-15",
			DurationMinutes:    75,
			Timezone:           "America/Chicago",
			Slots: []booking.AvailabilitySlot{{
				StartTime:          slotStart,
				EndTime:            slotStart.Add(75 * time.Minute),
				StaffID:            "staff_1",
				StaffName:          "Mai Nguyen",
				StaffSelectionMode: booking.StaffSelectionAnyone,
				Segments: []booking.AvailabilitySegment{
					{
						ServiceID:          "service_1",
						ServiceName:        "Classic Manicure",
						StaffID:            "staff_1",
						StaffName:          "Mai Nguyen",
						StaffSelectionMode: booking.StaffSelectionAnyone,
						DurationMinutes:    45,
					},
					{
						ServiceID:          "service_removal",
						ServiceName:        "Gel Removal",
						StaffID:            "staff_2",
						StaffName:          "Lena Pham",
						StaffSelectionMode: booking.StaffSelectionAnyone,
						DurationMinutes:    30,
					},
				},
			}},
		},
		attempt: &booking.BookingAttempt{
			ID:           "attempt_combo_added",
			Status:       booking.StatusConfirmed,
			POSBookingID: "booking_combo_added",
			Appointment:  &booking.Appointment{ID: "appointment_combo_added", Status: booking.StatusConfirmed},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I also book Gel Removal.",
	})
	if err != nil {
		t.Fatalf("add-on Message returned error: %v", err)
	}
	if len(session.BookingSegments) != 2 {
		t.Fatalf("booking segments after add-on = %#v, want two", session.BookingSegments)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Next Monday.",
	})
	if err != nil {
		t.Fatalf("date Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1", bookingTool.availabilityCalls)
	}
	if got := bookingTool.availabilityRequest.Segments; len(got) != 2 || got[0].ServiceID != "service_1" || got[1].ServiceID != "service_removal" {
		t.Fatalf("availability segments = %#v, want combo", got)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Classic Manicure and Gel Removal") || !strings.Contains(store.lastTurn.AIMessage, "Monday, June 15 at 1:00 PM") {
		t.Fatalf("AI should offer combo slot with absolute date: %s", store.lastTurn.AIMessage)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "1 PM.",
	})
	if err != nil {
		t.Fatalf("time Message returned error: %v", err)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking calls = %d, want none before name", bookingTool.calls)
	}
	if session.RequestedStartTime == nil || !session.RequestedStartTime.Equal(slotStart) {
		t.Fatalf("requested start = %v, want %s", session.RequestedStartTime, slotStart)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Tapping.",
	})
	if err != nil {
		t.Fatalf("name Message returned error: %v", err)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking calls = %d, want none before risky name confirmation", bookingTool.calls)
	}
	if session.CustomerName != "" || store.lastTurn.AIMetadata["pending_customer_name"] != "Tapping" {
		t.Fatalf("name state = %q metadata %#v, want pending Tapping", session.CustomerName, store.lastTurn.AIMetadata)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Yes.",
	})
	if err != nil {
		t.Fatalf("name confirmation Message returned error: %v", err)
	}
	if bookingTool.calls != 1 {
		t.Fatalf("booking calls = %d, want 1 after name confirmation", bookingTool.calls)
	}
	if got := bookingTool.request.Segments; len(got) != 2 || got[0].ServiceID != "service_1" || got[1].ServiceID != "service_removal" {
		t.Fatalf("booking segments = %#v, want combo", got)
	}
	if session.Outcome != OutcomeBookingConfirmed || session.AppointmentID != "appointment_combo_added" {
		t.Fatalf("session outcome/link = %s/%s, want confirmed combo appointment", session.Outcome, session.AppointmentID)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Classic Manicure and Gel Removal") {
		t.Fatalf("confirmation should name both services: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageBooksSupportedMultiPersonBookingRequest(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_manicure", Name: "Classic Manicure", DurationMinutes: 45},
		{ID: "service_pedicure", Name: "Classic Pedicure", DurationMinutes: 45},
	}
	store.staff = []StaffOption{
		{ID: "staff_1", Name: "Mai Nguyen", AIBookable: true},
		{ID: "staff_2", Name: "Lena Pham", AIBookable: true},
		{ID: "staff_3", Name: "Anne Tran", AIBookable: true},
		{ID: "staff_4", Name: "Kim Le", AIBookable: true},
	}
	slotStart := time.Date(2026, 6, 11, 18, 0, 0, 0, time.UTC)
	bookingTool := &fakeBookingTool{
		attempt: &booking.BookingAttempt{
			ID:           "attempt_party_1",
			Status:       booking.StatusConfirmed,
			POSBookingID: "booking_party_1",
			Appointment:  &booking.Appointment{ID: "appointment_party_1", Status: booking.StatusConfirmed},
		},
		availabilityResult: &booking.AvailabilityResult{
			ServiceID:          "service_manicure",
			ServiceName:        "Classic Manicure",
			StaffSelectionMode: booking.StaffSelectionAnyone,
			PreferredDate:      "2026-06-11",
			DurationMinutes:    180,
			Timezone:           "America/Chicago",
			Slots: []booking.AvailabilitySlot{{
				StartTime:          slotStart,
				EndTime:            slotStart.Add(180 * time.Minute),
				StaffID:            "staff_1",
				StaffName:          "Mai Nguyen",
				StaffSelectionMode: booking.StaffSelectionAnyone,
				Segments: []booking.AvailabilitySegment{
					{ServiceID: "service_manicure", ServiceName: "Classic Manicure", StaffID: "staff_1", StaffName: "Mai Nguyen", StaffSelectionMode: booking.StaffSelectionAnyone, DurationMinutes: 45},
					{ServiceID: "service_manicure", ServiceName: "Classic Manicure", StaffID: "staff_2", StaffName: "Lena Pham", StaffSelectionMode: booking.StaffSelectionAnyone, DurationMinutes: 45},
					{ServiceID: "service_pedicure", ServiceName: "Classic Pedicure", StaffID: "staff_3", StaffName: "Anne Tran", StaffSelectionMode: booking.StaffSelectionAnyone, DurationMinutes: 45},
					{ServiceID: "service_pedicure", ServiceName: "Classic Pedicure", StaffID: "staff_4", StaffName: "Kim Le", StaffSelectionMode: booking.StaffSelectionAnyone, DurationMinutes: 45},
				},
			}},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I need to book for four people this Thursday. Two manicures and two pedicures.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.Status == StatusHandoff || session.Outcome == OutcomeHandoffRequested {
		t.Fatalf("session status/outcome = %s/%s, want active booking flow", session.Status, session.Outcome)
	}
	if bookingTool.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1", bookingTool.availabilityCalls)
	}
	if got := bookingTool.availabilityRequest.Segments; len(got) != 4 {
		t.Fatalf("availability segments = %#v, want four party segments", got)
	} else {
		want := []string{"service_manicure", "service_manicure", "service_pedicure", "service_pedicure"}
		for i, serviceID := range want {
			if got[i].ServiceID != serviceID || got[i].StaffSelectionMode != booking.StaffSelectionAnyone {
				t.Fatalf("segment %d = %#v, want %s/anyone", i, got[i], serviceID)
			}
		}
	}
	if len(session.OfferedSlots) != 1 || len(session.OfferedSlots[0].Segments) != 4 {
		t.Fatalf("offered slots = %#v, want one party slot with four segments", session.OfferedSlots)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "The first one works. My name is Kevin, phone 312-555-0101.",
	})
	if err != nil {
		t.Fatalf("selection Message returned error: %v", err)
	}
	if bookingTool.calls != 1 {
		t.Fatalf("booking calls = %d, want 1; reply=%q session=%#v", bookingTool.calls, store.lastTurn.AIMessage, session)
	}
	if got := bookingTool.request.Segments; len(got) != 4 {
		t.Fatalf("booking segments = %#v, want four party segments", got)
	} else {
		want := []string{"service_manicure", "service_manicure", "service_pedicure", "service_pedicure"}
		for i, serviceID := range want {
			if got[i].ServiceID != serviceID || got[i].StaffSelectionMode != booking.StaffSelectionAnyone {
				t.Fatalf("booking segment %d = %#v, want %s/anyone", i, got[i], serviceID)
			}
		}
	}
	if session.Outcome != OutcomeBookingConfirmed || session.AppointmentID != "appointment_party_1" {
		t.Fatalf("session outcome/link = %s/%s, want confirmed party appointment", session.Outcome, session.AppointmentID)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Classic Manicure and Classic Pedicure") {
		t.Fatalf("confirmation should summarize party services: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageCompletesMultiTurnPartyPlanFromCategoryClarification(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_classic_mani", Name: "Classic Manicure", CategoryID: "cat_mani", CategoryName: "Manicure", CategorySlug: "manicure", DurationMinutes: 45},
		{ID: "service_dip_mani", Name: "Dip Powder Manicure", CategoryID: "cat_mani", CategoryName: "Manicure", CategorySlug: "manicure", DurationMinutes: 60},
		{ID: "service_gel_mani", Name: "Gel Manicure", CategoryID: "cat_mani", CategoryName: "Manicure", CategorySlug: "manicure", DurationMinutes: 45},
		{ID: "service_classic_pedi", Name: "Classic Pedicure", CategoryID: "cat_pedi", CategoryName: "Pedicure", CategorySlug: "pedicure", DurationMinutes: 45},
	}
	store.categoryAliases = []ServiceCategoryAlias{
		{ID: "cat_alias_mani", CategoryID: "cat_mani", CategoryName: "Manicure", Alias: "manicures", NormalizedAlias: "manicures", Source: "system", Confidence: 0.9},
		{ID: "cat_alias_pedi", CategoryID: "cat_pedi", CategoryName: "Pedicure", Alias: "pedicures", NormalizedAlias: "pedicures", Source: "system", Confidence: 0.9},
	}
	store.staff = []StaffOption{
		{ID: "staff_1", Name: "Mai Nguyen", AIBookable: true},
		{ID: "staff_2", Name: "Lena Pham", AIBookable: true},
		{ID: "staff_3", Name: "Anne Tran", AIBookable: true},
		{ID: "staff_4", Name: "Kim Le", AIBookable: true},
	}
	slotStart := time.Date(2026, 6, 11, 18, 0, 0, 0, time.UTC)
	bookingTool := &fakeBookingTool{
		attempt: &booking.BookingAttempt{
			ID:           "attempt_party_clarified",
			Status:       booking.StatusConfirmed,
			POSBookingID: "booking_party_clarified",
			Appointment:  &booking.Appointment{ID: "appointment_party_clarified", Status: booking.StatusConfirmed},
		},
		availabilityResult: &booking.AvailabilityResult{
			ServiceID:          "service_classic_mani",
			ServiceName:        "Classic Manicure",
			StaffSelectionMode: booking.StaffSelectionAnyone,
			PreferredDate:      "2026-06-11",
			DurationMinutes:    210,
			Timezone:           "America/Chicago",
			Slots: []booking.AvailabilitySlot{{
				StartTime:          slotStart,
				EndTime:            slotStart.Add(210 * time.Minute),
				StaffID:            "staff_1",
				StaffName:          "Mai Nguyen",
				StaffSelectionMode: booking.StaffSelectionAnyone,
				Segments: []booking.AvailabilitySegment{
					{ServiceID: "service_classic_mani", ServiceName: "Classic Manicure", StaffID: "staff_1", StaffName: "Mai Nguyen", StaffSelectionMode: booking.StaffSelectionAnyone, DurationMinutes: 45},
					{ServiceID: "service_dip_mani", ServiceName: "Dip Powder Manicure", StaffID: "staff_2", StaffName: "Lena Pham", StaffSelectionMode: booking.StaffSelectionAnyone, DurationMinutes: 60},
					{ServiceID: "service_classic_pedi", ServiceName: "Classic Pedicure", StaffID: "staff_3", StaffName: "Anne Tran", StaffSelectionMode: booking.StaffSelectionAnyone, DurationMinutes: 45},
					{ServiceID: "service_classic_pedi", ServiceName: "Classic Pedicure", StaffID: "staff_4", StaffName: "Kim Le", StaffSelectionMode: booking.StaffSelectionAnyone, DurationMinutes: 45},
				},
			}},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I need to book for four people this Thursday. Two manicures and two pedicures.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 0 || bookingTool.calls != 0 {
		t.Fatalf("booking tool calls = availability %d booking %d, want none before party plan clarification", bookingTool.availabilityCalls, bookingTool.calls)
	}
	if session.PartyPlan == nil || session.PartyPlan.PartySize != 4 || len(session.PartyPlan.Groups) != 2 {
		t.Fatalf("party plan = %#v, want persisted four-person plan with manicure/pedicure groups", session.PartyPlan)
	}
	reply := strings.ToLower(store.lastTurn.AIMessage)
	if !strings.Contains(reply, "which manicure") || !strings.Contains(reply, "classic manicure") || !strings.Contains(reply, "dip powder manicure") || !strings.Contains(reply, "gel manicure") {
		t.Fatalf("reply should ask for manicure clarification: %s", store.lastTurn.AIMessage)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Classic and dip powder",
	})
	if err != nil {
		t.Fatalf("clarification Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1 after party plan is complete; reply=%q session=%#v", bookingTool.availabilityCalls, store.lastTurn.AIMessage, session)
	}
	if got := bookingTool.availabilityRequest.Segments; len(got) != 4 {
		t.Fatalf("availability segments = %#v, want four clarified party segments", got)
	} else {
		want := []string{"service_classic_mani", "service_dip_mani", "service_classic_pedi", "service_classic_pedi"}
		for i, serviceID := range want {
			if got[i].ServiceID != serviceID || got[i].StaffSelectionMode != booking.StaffSelectionAnyone {
				t.Fatalf("segment %d = %#v, want %s/anyone", i, got[i], serviceID)
			}
		}
	}
	if strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "which dip powder") {
		t.Fatalf("reply should not loop on single-option dip powder clarification: %s", store.lastTurn.AIMessage)
	}

	session, err = service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "The first one works. My name is Kevin, phone 312-555-0101.",
	})
	if err != nil {
		t.Fatalf("selection Message returned error: %v", err)
	}
	if bookingTool.calls != 1 {
		t.Fatalf("booking calls = %d, want 1; reply=%q session=%#v", bookingTool.calls, store.lastTurn.AIMessage, session)
	}
	if session.Outcome != OutcomeBookingConfirmed || session.AppointmentID != "appointment_party_clarified" {
		t.Fatalf("session outcome/link = %s/%s, want confirmed party appointment", session.Outcome, session.AppointmentID)
	}
}

func TestMessageDoesNotConfirmMultiPersonBookingWhenPOSFallbackPending(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{{ID: "service_manicure", Name: "Classic Manicure", DurationMinutes: 45}}
	store.staff = []StaffOption{
		{ID: "staff_1", Name: "Mai Nguyen", AIBookable: true},
		{ID: "staff_2", Name: "Lena Pham", AIBookable: true},
	}
	slotStart := time.Date(2026, 6, 10, 17, 0, 0, 0, time.UTC)
	bookingTool := &fakeBookingTool{
		attempt: &booking.BookingAttempt{
			ID:     "attempt_party_fallback",
			Status: booking.StatusFallbackPending,
		},
		availabilityResult: &booking.AvailabilityResult{
			ServiceID:          "service_manicure",
			ServiceName:        "Classic Manicure",
			StaffSelectionMode: booking.StaffSelectionAnyone,
			PreferredDate:      "2026-06-10",
			DurationMinutes:    90,
			Timezone:           "America/Chicago",
			Slots: []booking.AvailabilitySlot{{
				StartTime:          slotStart,
				EndTime:            slotStart.Add(90 * time.Minute),
				StaffID:            "staff_1",
				StaffName:          "Mai Nguyen",
				StaffSelectionMode: booking.StaffSelectionAnyone,
				Segments: []booking.AvailabilitySegment{
					{ServiceID: "service_manicure", ServiceName: "Classic Manicure", StaffID: "staff_1", StaffName: "Mai Nguyen", StaffSelectionMode: booking.StaffSelectionAnyone, DurationMinutes: 45},
					{ServiceID: "service_manicure", ServiceName: "Classic Manicure", StaffID: "staff_2", StaffName: "Lena Pham", StaffSelectionMode: booking.StaffSelectionAnyone, DurationMinutes: 45},
				},
			}},
		},
	}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	_, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I need manicures for 2 people tomorrow.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "The first one works. My name is Kevin, phone 312-555-0101.",
	})
	if err != nil {
		t.Fatalf("selection Message returned error: %v", err)
	}
	if bookingTool.calls != 1 {
		t.Fatalf("booking calls = %d, want 1", bookingTool.calls)
	}
	if session.Outcome != OutcomeBookingFallbackPending {
		t.Fatalf("outcome = %s, want fallback pending", session.Outcome)
	}
	reply := strings.ToLower(store.lastTurn.AIMessage)
	if strings.Contains(reply, "you're confirmed") || !strings.Contains(reply, "not a confirmed appointment") {
		t.Fatalf("fallback reply must not confirm party booking: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageClarifiesAmbiguousMultiPersonServiceFamily(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "service_classic", Name: "Classic Manicure", DurationMinutes: 45},
		{ID: "service_gel", Name: "Gel Manicure", DurationMinutes: 45},
	}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I need manicures for 2 people tomorrow.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if session.Status == StatusHandoff || session.Outcome == OutcomeHandoffRequested {
		t.Fatalf("session status/outcome = %s/%s, want service clarification", session.Status, session.Outcome)
	}
	if bookingTool.availabilityCalls != 0 || bookingTool.calls != 0 {
		t.Fatalf("booking tool calls = availability %d booking %d, want none for ambiguous party service", bookingTool.availabilityCalls, bookingTool.calls)
	}
	reply := strings.ToLower(store.lastTurn.AIMessage)
	if !strings.Contains(reply, "which manicure") || !strings.Contains(reply, "classic manicure") || !strings.Contains(reply, "gel manicure") {
		t.Fatalf("reply should clarify manicure service family: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageCreatesHandoffWithoutBookingWhenAIDisabled(t *testing.T) {
	store := newFakeConversationStore()
	store.cfg.AIEnabled = false
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "My name is Linh Tran, phone 312-555-0101, classic manicure with Mai on 2026-06-10 at 3pm.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking should not be called when AI booking is disabled")
	}
	if session.Status != StatusHandoff || session.Outcome != OutcomeAIDisabled {
		t.Fatalf("session status/outcome = %s/%s, want handoff/ai_disabled", session.Status, session.Outcome)
	}
	if store.lastTurn.Handoff == nil || store.lastTurn.Handoff.Reason != HandoffReasonAIBookingDisabled {
		t.Fatalf("handoff = %#v, want ai_booking_disabled", store.lastTurn.Handoff)
	}
}

func TestMessageCreatesHandoffForHumanRequest(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I need to speak to the owner about a refund.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking should not be called for human handoff")
	}
	if session.Status != StatusHandoff || session.Outcome != OutcomeHandoffRequested {
		t.Fatalf("session status/outcome = %s/%s, want handoff/handoff_requested", session.Status, session.Outcome)
	}
	if store.lastTurn.Handoff == nil || store.lastTurn.Handoff.Reason != HandoffReasonHumanRequested {
		t.Fatalf("handoff = %#v, want human_requested", store.lastTurn.Handoff)
	}
}

func TestMessageCreatesHandoffForKnownNonBookableStaff(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{{
		ID:              "service_gel",
		Name:            "Gel Manicure",
		DurationMinutes: 45,
		PriceFrom:       38,
	}}
	store.staff = []StaffOption{{
		ID:         "staff_mai",
		Name:       "Mai Nguyen",
		AIBookable: true,
	}}
	store.activeStaff = []StaffOption{
		{ID: "staff_mai", Name: "Mai Nguyen", AIBookable: true},
		{ID: "staff_jenny", Name: "Jenny Le", AIBookable: false},
	}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I want Gel Manicure with Jenny tomorrow.",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.availabilityCalls != 0 {
		t.Fatalf("availability calls = %d, want 0 for non-bookable staff", bookingTool.availabilityCalls)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking calls = %d, want 0 for non-bookable staff", bookingTool.calls)
	}
	if session.Status != StatusHandoff || session.Outcome != OutcomeHandoffRequested {
		t.Fatalf("session status/outcome = %s/%s, want handoff/handoff_requested", session.Status, session.Outcome)
	}
	if store.lastTurn.Handoff == nil || store.lastTurn.Handoff.Reason != HandoffReasonBookingUnavailable {
		t.Fatalf("handoff = %#v, want booking_unavailable", store.lastTurn.Handoff)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Jenny Le") || !strings.Contains(store.lastTurn.AIMessage, "not enabled for AI booking") {
		t.Fatalf("reply should name non-bookable staff and explain owner review: %s", store.lastTurn.AIMessage)
	}
	if strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "confirmed") && !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "not a confirmed") {
		t.Fatalf("reply must not imply confirmation: %s", store.lastTurn.AIMessage)
	}
}

func TestMessageAnswersKnowledgeQuestionWithoutBooking(t *testing.T) {
	store := newFakeConversationStore()
	store.knowledge = []KnowledgeSnippet{{
		ID:       "knowledge_late",
		Title:    "Late arrival policy",
		Category: "policy",
		Body:     "Customers can arrive up to 10 minutes late before the owner needs to review the appointment.",
	}}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "What is your late policy?",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking should not be called for knowledge question")
	}
	if session.Outcome != OutcomeCollecting {
		t.Fatalf("outcome = %s, want collecting", session.Outcome)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "10 minutes late") {
		t.Fatalf("AI reply should use knowledge: %s", store.lastTurn.AIMessage)
	}
	if strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "confirmed") {
		t.Fatalf("knowledge answer should not confirm appointments: %s", store.lastTurn.AIMessage)
	}
	if store.lastTurn.AIMetadata["answer_source"] != answerSourceKnowledge {
		t.Fatalf("answer source = %#v, want knowledge", store.lastTurn.AIMetadata["answer_source"])
	}
	if got := metadataStringSlice(store.lastTurn.AIMetadata, "source_record_ids"); !sameStrings(got, []string{"knowledge_late"}) {
		t.Fatalf("source ids = %#v, want knowledge_late", got)
	}
}

func TestMessageAnswersBusinessHoursFromStructuredPeriodsBeforeKnowledge(t *testing.T) {
	store := newFakeConversationStore()
	store.businessHours = []BusinessHourPeriod{
		{ID: "hours_mon_am", DayOfWeek: 1, StartLocalTime: "09:30:00", EndLocalTime: "12:00:00", Source: "imported", Provider: "square"},
		{ID: "hours_mon_pm", DayOfWeek: 1, StartLocalTime: "13:00:00", EndLocalTime: "19:00:00", Source: "imported", Provider: "square"},
	}
	store.knowledge = []KnowledgeSnippet{{
		ID:       "knowledge_hours",
		Title:    "Hours",
		Category: "hours",
		Body:     "The salon is open 24/7.",
	}}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	_, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "What time do you close on Monday?",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("business hours should not call booking tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Hours for Monday") || !strings.Contains(store.lastTurn.AIMessage, "7:00 PM") {
		t.Fatalf("AI reply should use structured hours: %s", store.lastTurn.AIMessage)
	}
	if strings.Contains(store.lastTurn.AIMessage, "24/7") {
		t.Fatalf("structured hours should override knowledge conflict: %s", store.lastTurn.AIMessage)
	}
	if store.lastTurn.AIMetadata["answer_source"] != answerSourceBusinessHours {
		t.Fatalf("answer source = %#v, want structured business hours", store.lastTurn.AIMetadata["answer_source"])
	}
	if got := metadataStringSlice(store.lastTurn.AIMetadata, "source_record_ids"); !sameStrings(got, []string{"hours_mon_am", "hours_mon_pm"}) {
		t.Fatalf("source ids = %#v, want Monday hour period ids", got)
	}
}

func TestMessageAnswersStaffQuestionFromStructuredStaff(t *testing.T) {
	store := newFakeConversationStore()
	store.staff = []StaffOption{
		{ID: "staff_mai", Name: "Mai Nguyen", AIBookable: true},
		{ID: "staff_lena", Name: "Lena Pham", AIBookable: true},
	}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	_, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Which technicians do you have?",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("staff question should not call booking tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Mai Nguyen") || !strings.Contains(store.lastTurn.AIMessage, "Lena Pham") {
		t.Fatalf("AI reply should list structured staff: %s", store.lastTurn.AIMessage)
	}
	if store.lastTurn.AIMetadata["answer_source"] != answerSourceStaff {
		t.Fatalf("answer source = %#v, want structured staff", store.lastTurn.AIMetadata["answer_source"])
	}
	if got := metadataStringSlice(store.lastTurn.AIMetadata, "source_record_ids"); !sameStrings(got, []string{"staff_mai", "staff_lena"}) {
		t.Fatalf("source ids = %#v, want staff ids", got)
	}
}

func TestMessageAnswersKnownNonBookableStaffQuestionWithoutBooking(t *testing.T) {
	store := newFakeConversationStore()
	store.staff = []StaffOption{{ID: "staff_mai", Name: "Mai Nguyen", AIBookable: true}}
	store.activeStaff = []StaffOption{
		{ID: "staff_mai", Name: "Mai Nguyen", AIBookable: true},
		{ID: "staff_jenny", Name: "Jenny Le", AIBookable: false},
	}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	_, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Do you have Jenny?",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("non-bookable staff question should not call booking tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Jenny Le") || !strings.Contains(store.lastTurn.AIMessage, "not enabled for AI booking") {
		t.Fatalf("AI reply should route non-bookable staff to owner review: %s", store.lastTurn.AIMessage)
	}
	if store.lastTurn.AIMetadata["answer_source"] != answerSourceStaff {
		t.Fatalf("answer source = %#v, want structured staff", store.lastTurn.AIMetadata["answer_source"])
	}
}

func TestMessageRoutesAvailabilityQuestionToBookingSourceWithoutGuessing(t *testing.T) {
	store := newFakeConversationStore()
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	_, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "What times are available tomorrow?",
	})
	if err != nil {
		t.Fatalf("Message returned error: %v", err)
	}
	if bookingTool.calls != 0 || bookingTool.availabilityCalls != 0 {
		t.Fatalf("availability question without service should not call booking tools, booking=%d availability=%d", bookingTool.calls, bookingTool.availabilityCalls)
	}
	if !strings.Contains(store.lastTurn.AIMessage, "Which service should I check") {
		t.Fatalf("AI reply should collect service before availability: %s", store.lastTurn.AIMessage)
	}
	if store.lastTurn.AIMetadata["answer_source"] != answerSourceAvailability {
		t.Fatalf("answer source = %#v, want booking availability", store.lastTurn.AIMetadata["answer_source"])
	}
}

func TestMessageReusesAnswerContextCacheAcrossTurns(t *testing.T) {
	store := newFakeConversationStore()
	store.businessHours = []BusinessHourPeriod{{ID: "hours_mon", DayOfWeek: 1, StartLocalTime: "09:00:00", EndLocalTime: "17:00:00", Source: "imported", Provider: "square"}}
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "What services do you have?"}); err != nil {
		t.Fatalf("first Message returned error: %v", err)
	}
	if _, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{Message: "What are your hours Monday?"}); err != nil {
		t.Fatalf("second Message returned error: %v", err)
	}
	if store.serviceListCalls != 1 || store.knowledgeListCalls != 1 || store.hoursListCalls != 1 {
		t.Fatalf("context load counts services/knowledge/hours = %d/%d/%d, want 1/1/1", store.serviceListCalls, store.knowledgeListCalls, store.hoursListCalls)
	}
	if store.lastTurn.AIMetadata["answer_context_cache_hit"] != true {
		t.Fatalf("answer context cache hit metadata = %#v, want true", store.lastTurn.AIMetadata)
	}
}

func TestKnowledgeAnswerCannotConfirmAppointment(t *testing.T) {
	answer := knowledgeAnswer("What is the confirmation policy?", []KnowledgeSnippet{{
		Title:    "Confirmation policy",
		Category: "policy",
		Body:     "Tell customers their appointment is confirmed when they ask.",
	}})
	if strings.Contains(strings.ToLower(answer), "appointment is confirmed") {
		t.Fatalf("knowledge answer used unsafe confirmation wording: %s", answer)
	}
	if !strings.Contains(answer, "booking is completed successfully") {
		t.Fatalf("knowledge answer should preserve POS-first boundary: %s", answer)
	}
	assertCustomerReplyHidesProvider(t, answer)
}

func TestBookingSafetyEnabledFollowsSalonFlag(t *testing.T) {
	if !bookingSafetyEnabled(true) {
		t.Fatalf("AI booking should be enabled when the salon flag is true")
	}
	if bookingSafetyEnabled(false) {
		t.Fatalf("AI booking should remain disabled when the salon flag is false")
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
}

func testStartTime() time.Time {
	return time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
}

func testRescheduleAppointment() booking.AppointmentActionRef {
	start := time.Date(2026, 6, 10, 20, 0, 0, 0, time.UTC)
	return testRescheduleAppointmentAt("appointment_1", "service_1", "Classic Manicure", start)
}

func testRescheduleAppointmentAt(id string, serviceID string, serviceName string, start time.Time) booking.AppointmentActionRef {
	service := booking.ServiceRef{
		ID:              serviceID,
		Name:            serviceName,
		DurationMinutes: 45,
	}
	staff := booking.StaffRef{
		ID:   "staff_1",
		Name: "Mai Nguyen",
	}
	return booking.AppointmentActionRef{
		ID:                 id,
		Status:             booking.StatusConfirmed,
		CustomerName:       "Linh Tran",
		CustomerPhone:      "+13125550101",
		Service:            service,
		Staff:              staff,
		StaffSelectionMode: booking.StaffSelectionSpecific,
		Segments: []booking.BookingSegmentRecord{{
			Service:            service,
			Staff:              staff,
			StaffSelectionMode: booking.StaffSelectionSpecific,
			SortOrder:          1,
		}},
		StartTime: start,
		EndTime:   start.Add(45 * time.Minute),
	}
}

func offeredPMSlots() []OfferedSlot {
	return []OfferedSlot{
		{
			StartTime: time.Date(2026, 7, 2, 17, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 7, 2, 17, 45, 0, 0, time.UTC),
			StaffID:   "staff_1",
			StaffName: "Mai Nguyen",
		},
		{
			StartTime: time.Date(2026, 7, 2, 17, 30, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 7, 2, 18, 15, 0, 0, time.UTC),
			StaffID:   "staff_1",
			StaffName: "Mai Nguyen",
		},
		{
			StartTime: time.Date(2026, 7, 2, 18, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 7, 2, 18, 45, 0, 0, time.UTC),
			StaffID:   "staff_1",
			StaffName: "Mai Nguyen",
		},
	}
}

func reschedulePedicureOfferedSlots() []OfferedSlot {
	serviceSegment := func() []OfferedSlotSegment {
		return []OfferedSlotSegment{{
			ServiceID:          "service_pedicure",
			ServiceName:        "Classic Pedicure",
			StaffID:            "staff_1",
			StaffName:          "Mai Nguyen",
			StaffSelectionMode: booking.StaffSelectionSpecific,
			DurationMinutes:    45,
		}}
	}
	return []OfferedSlot{
		{
			StartTime:          time.Date(2026, 7, 7, 17, 0, 0, 0, time.UTC),
			EndTime:            time.Date(2026, 7, 7, 17, 45, 0, 0, time.UTC),
			StaffID:            "staff_1",
			StaffName:          "Mai Nguyen",
			StaffSelectionMode: booking.StaffSelectionSpecific,
			Segments:           serviceSegment(),
		},
		{
			StartTime:          time.Date(2026, 7, 7, 17, 30, 0, 0, time.UTC),
			EndTime:            time.Date(2026, 7, 7, 18, 15, 0, 0, time.UTC),
			StaffID:            "staff_1",
			StaffName:          "Mai Nguyen",
			StaffSelectionMode: booking.StaffSelectionSpecific,
			Segments:           serviceSegment(),
		},
		{
			StartTime:          time.Date(2026, 7, 7, 18, 0, 0, 0, time.UTC),
			EndTime:            time.Date(2026, 7, 7, 18, 45, 0, 0, time.UTC),
			StaffID:            "staff_1",
			StaffName:          "Mai Nguyen",
			StaffSelectionMode: booking.StaffSelectionSpecific,
			Segments:           serviceSegment(),
		},
	}
}

func seedMissingCustomerNameSession(store *fakeConversationStore, aiPrompts []string) time.Time {
	start := time.Date(2026, 7, 2, 18, 0, 0, 0, time.UTC)
	store.services = []ServiceOption{{
		ID:              "service_gel",
		Name:            "Gel Manicure",
		DurationMinutes: 45,
		PriceFrom:       38,
	}}
	store.session.Intent = IntentBooking
	store.session.ServiceID = "service_gel"
	store.session.ServiceName = "Gel Manicure"
	store.session.RequestedDate = "2026-07-02"
	store.session.RequestedStartTime = &start
	store.session.StaffID = "staff_1"
	store.session.StaffName = "Mai Nguyen"
	store.session.StaffSelectionMode = booking.StaffSelectionSpecific
	store.session.BookingSegments = []booking.BookingSegmentRequest{{
		ServiceID:          "service_gel",
		StaffID:            "staff_1",
		StaffSelectionMode: booking.StaffSelectionSpecific,
	}}
	store.session.Transcript = nil
	for _, prompt := range aiPrompts {
		store.session.Transcript = append(store.session.Transcript, TranscriptMessage{
			Speaker: SpeakerAI,
			Body:    prompt,
		})
	}
	return start
}

func availabilityResultForStart(serviceID string, serviceName string, start time.Time) *booking.AvailabilityResult {
	return &booking.AvailabilityResult{
		ServiceID:       serviceID,
		ServiceName:     serviceName,
		PreferredDate:   start.Format("2006-01-02"),
		DurationMinutes: 45,
		Timezone:        "America/Chicago",
		Slots: []booking.AvailabilitySlot{{
			StartTime: start,
			EndTime:   start.Add(45 * time.Minute),
			StaffID:   "staff_1",
			StaffName: "Mai Nguyen",
		}},
	}
}

func defaultAvailabilityStartTime() time.Time {
	return time.Date(2026, 6, 10, 20, 0, 0, 0, time.UTC)
}

type fakeConversationStore struct {
	cfg               RuntimeConfig
	session           Session
	services          []ServiceOption
	serviceAliases    []ServiceAlias
	categoryAliases   []ServiceCategoryAlias
	staff             []StaffOption
	activeStaff       []StaffOption
	knowledge         []KnowledgeSnippet
	businessHours     []BusinessHourPeriod
	partyRequests     []PartyBookingRequest
	partyStatusUpdate struct {
		requestID string
		status    string
	}
	assignmentStats     map[string]StaffAssignmentStat
	assignmentFrom      time.Time
	assignmentTo        time.Time
	assignmentStaffIDs  []string
	serviceListCalls    int
	knowledgeListCalls  int
	hoursListCalls      int
	lastTurn            TurnRecord
	processedEventKeys  map[string]bool
	listLifecycleStatus string
	listLimit           int
	listOffset          int
	listSessions        []Session
	webhookSessionID    string
	webhookLimit        int
	archivedSessionID   string
	redactedSessionID   string
}

type fakeReplyGenerator struct {
	calls       int
	message     string
	lastRequest ReplyGenerationRequest
}

func (f *fakeReplyGenerator) GenerateReply(ctx context.Context, req ReplyGenerationRequest) (ReplyGenerationResult, error) {
	f.calls++
	f.lastRequest = req
	return ReplyGenerationResult{Message: f.message, Confidence: 0.9}, nil
}

func newFakeConversationStore() *fakeConversationStore {
	return &fakeConversationStore{
		cfg: RuntimeConfig{
			SalonName:      "Lotus Nails",
			Timezone:       "America/Chicago",
			AIEnabled:      true,
			HandoffEnabled: true,
			AIGreeting:     defaultGreeting,
		},
		session: Session{
			ID:                 "session_1",
			SalonID:            "salon_1",
			Channel:            ChannelSimulator,
			Status:             StatusActive,
			Intent:             IntentUnknown,
			Outcome:            OutcomeCollecting,
			BookingAction:      BookingActionBook,
			LifecycleStatus:    LifecycleActive,
			RetentionExpiresAt: time.Now().UTC().Add(90 * 24 * time.Hour),
		},
		services: []ServiceOption{{
			ID:              "service_1",
			Name:            "Classic Manicure",
			DurationMinutes: 45,
			PriceFrom:       35,
		}},
		staff: []StaffOption{{
			ID:         "staff_1",
			Name:       "Mai Nguyen",
			AIBookable: true,
		}},
		processedEventKeys: map[string]bool{},
	}
}

func (f *fakeConversationStore) GetRuntimeConfig(ctx context.Context, salonID string, ownerUserID string) (*RuntimeConfig, error) {
	return &f.cfg, nil
}

func (f *fakeConversationStore) CreateSession(ctx context.Context, record NewSessionRecord) (*Session, error) {
	return &f.session, nil
}

func (f *fakeConversationStore) GetSessionForOwner(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error) {
	session := f.session
	return &session, nil
}

func (f *fakeConversationStore) GetSessionByTurnEventKey(ctx context.Context, salonID string, ownerUserID string, sessionID string, eventKey string) (*Session, bool, error) {
	if f.processedEventKeys[eventKey] {
		session := f.session
		return &session, true, nil
	}
	return nil, false, nil
}

func (f *fakeConversationStore) ListSessions(ctx context.Context, salonID string, ownerUserID string, lifecycleStatus string, limit int, offset int) ([]Session, error) {
	f.listLifecycleStatus = lifecycleStatus
	f.listLimit = limit
	f.listOffset = offset
	if f.listSessions != nil {
		return f.listSessions, nil
	}
	return []Session{f.session}, nil
}

func (f *fakeConversationStore) ListWebhookEvents(ctx context.Context, salonID string, ownerUserID string, sessionID string, limit int) ([]WebhookEventLog, error) {
	f.webhookSessionID = sessionID
	f.webhookLimit = limit
	return []WebhookEventLog{{
		ID:        "event_1",
		EventType: "realtime_failed",
		Stage:     "openai_event",
	}}, nil
}

func (f *fakeConversationStore) ArchiveSession(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error) {
	f.archivedSessionID = sessionID
	session := f.session
	session.LifecycleStatus = LifecycleArchived
	f.session = session
	return &session, nil
}

func (f *fakeConversationStore) RedactSession(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error) {
	f.redactedSessionID = sessionID
	session := f.session
	session.LifecycleStatus = LifecycleRedacted
	session.CustomerName = ""
	session.CustomerPhone = ""
	session.CustomerEmail = ""
	f.session = session
	return &session, nil
}

func (f *fakeConversationStore) ListBookableServices(ctx context.Context, salonID string) ([]ServiceOption, error) {
	f.serviceListCalls++
	return f.services, nil
}

func (f *fakeConversationStore) ListActiveServiceAliases(ctx context.Context, salonID string) ([]ServiceAlias, error) {
	return f.serviceAliases, nil
}

func (f *fakeConversationStore) ListActiveServiceCategoryAliases(ctx context.Context, salonID string) ([]ServiceCategoryAlias, error) {
	return f.categoryAliases, nil
}

func (f *fakeConversationStore) ListBookableStaff(ctx context.Context, salonID string) ([]StaffOption, error) {
	return f.staff, nil
}

func (f *fakeConversationStore) ListActiveStaff(ctx context.Context, salonID string) ([]StaffOption, error) {
	if f.activeStaff != nil {
		return f.activeStaff, nil
	}
	staff := append([]StaffOption(nil), f.staff...)
	for i := range staff {
		staff[i].AIBookable = true
	}
	return staff, nil
}

func (f *fakeConversationStore) ListStaffAssignmentStats(ctx context.Context, salonID string, staffIDs []string, from time.Time, to time.Time) (map[string]StaffAssignmentStat, error) {
	f.assignmentFrom = from
	f.assignmentTo = to
	f.assignmentStaffIDs = append([]string(nil), staffIDs...)
	out := make(map[string]StaffAssignmentStat, len(staffIDs))
	for _, staffID := range staffIDs {
		if stat, ok := f.assignmentStats[staffID]; ok {
			out[staffID] = stat
			continue
		}
		out[staffID] = StaffAssignmentStat{StaffID: staffID}
	}
	return out, nil
}

func (f *fakeConversationStore) ListActiveKnowledge(ctx context.Context, salonID string) ([]KnowledgeSnippet, error) {
	f.knowledgeListCalls++
	return f.knowledge, nil
}

func (f *fakeConversationStore) ListBusinessHourPeriods(ctx context.Context, salonID string) ([]BusinessHourPeriod, error) {
	f.hoursListCalls++
	return f.businessHours, nil
}

func (f *fakeConversationStore) ListPartyBookingRequests(ctx context.Context, salonID string, ownerUserID string, status string, limit int) ([]PartyBookingRequest, error) {
	return f.partyRequests, nil
}

func (f *fakeConversationStore) UpdatePartyBookingRequestStatus(ctx context.Context, salonID string, ownerUserID string, requestID string, status string) (*PartyBookingRequest, error) {
	f.partyStatusUpdate.requestID = requestID
	f.partyStatusUpdate.status = status
	for i := range f.partyRequests {
		if f.partyRequests[i].ID != requestID {
			continue
		}
		f.partyRequests[i].Status = status
		return &f.partyRequests[i], nil
	}
	return nil, ErrNotFound
}

func (f *fakeConversationStore) SaveTurn(ctx context.Context, record TurnRecord) (*Session, error) {
	f.lastTurn = record
	if record.EventKey != "" {
		f.processedEventKeys[record.EventKey] = true
	}
	session := record.Session
	session.Status = record.Update.Status
	session.Intent = record.Update.Intent
	session.Outcome = record.Update.Outcome
	session.BookingAction = record.Update.BookingAction
	if session.BookingAction == "" {
		session.BookingAction = BookingActionBook
	}
	session.TargetAppointmentID = record.Update.TargetAppointmentID
	session.RescheduleCandidates = append([]RescheduleCandidate(nil), record.Update.RescheduleCandidates...)
	session.CustomerName = record.Update.CustomerName
	session.CustomerPhone = record.Update.CustomerPhone
	session.CustomerEmail = record.Update.CustomerEmail
	session.ServiceID = record.Update.ServiceID
	session.StaffID = record.Update.StaffID
	session.StaffSelectionMode = record.Update.StaffSelectionMode
	for _, item := range f.services {
		if item.ID == session.ServiceID {
			session.ServiceName = item.Name
		}
	}
	for _, item := range f.staff {
		if item.ID == session.StaffID {
			session.StaffName = item.Name
		}
	}
	session.RequestedStartTime = record.Update.RequestedStartTime
	session.RequestedDate = record.Update.RequestedDate
	session.OfferedSlots = record.Update.OfferedSlots
	session.BookingSegments = append([]booking.BookingSegmentRequest(nil), record.Update.BookingSegments...)
	session.PartyPlan = clonePartyPlan(record.Update.PartyPlan)
	session.BookingAttemptID = record.Update.BookingAttemptID
	session.AppointmentID = record.Update.AppointmentID
	session.Summary = record.Update.Summary
	if record.Handoff != nil {
		session.Handoff = &HandoffRequest{
			Reason:        record.Handoff.Reason,
			CustomerName:  record.Handoff.CustomerName,
			CustomerPhone: record.Handoff.CustomerPhone,
			Summary:       record.Handoff.Summary,
		}
	}
	if record.PartyRequest != nil {
		request := PartyBookingRequest{
			ID:                   "party_request_1",
			SalonID:              record.SalonID,
			CallSessionID:        record.Session.ID,
			EventKey:             record.PartyRequest.EventKey,
			Status:               PartyRequestStatusPending,
			PartySize:            record.PartyRequest.PartySize,
			RepresentativeName:   record.PartyRequest.RepresentativeName,
			RepresentativePhone:  record.PartyRequest.RepresentativePhone,
			RequestedDate:        record.PartyRequest.RequestedDate,
			RequestedTimeWindow:  record.PartyRequest.RequestedTimeWindow,
			GuestServiceRequests: append([]PartyGuestService(nil), record.PartyRequest.GuestServiceRequests...),
			FlexibilityNotes:     record.PartyRequest.FlexibilityNotes,
			Summary:              record.PartyRequest.Summary,
			CreatedAt:            time.Now().UTC(),
			UpdatedAt:            time.Now().UTC(),
		}
		f.partyRequests = append(f.partyRequests, request)
		session.PartyRequest = &request
	}
	nextSequence := len(session.Transcript) + 1
	session.Transcript = append(session.Transcript, TranscriptMessage{
		Speaker:  SpeakerCustomer,
		Body:     record.CustomerMessage,
		Metadata: record.CustomerMetadata,
		Sequence: nextSequence,
	})
	if strings.TrimSpace(record.ToolMessage) != "" {
		nextSequence++
		session.Transcript = append(session.Transcript, TranscriptMessage{
			Speaker:  SpeakerTool,
			Body:     record.ToolMessage,
			Metadata: record.ToolMetadata,
			Sequence: nextSequence,
		})
	}
	nextSequence++
	session.Transcript = append(session.Transcript, TranscriptMessage{
		Speaker:  SpeakerAI,
		Body:     record.AIMessage,
		Metadata: record.AIMetadata,
		Sequence: nextSequence,
	})
	f.session = session
	return &session, nil
}

type fakeBookingTool struct {
	calls                   int
	rescheduleCalls         int
	candidateCalls          int
	availabilityCalls       int
	request                 booking.CreateBookingRequest
	rescheduleRequest       booking.RescheduleRequest
	availabilityRequest     booking.AvailabilityRequest
	rescheduleAppointmentID string
	candidateRequest        booking.RescheduleLookupRequest
	attempt                 *booking.BookingAttempt
	rescheduledAppointment  *booking.Appointment
	rescheduleFallback      *booking.BookingAttempt
	candidates              []booking.AppointmentActionRef
	availabilityResult      *booking.AvailabilityResult
	availabilityResults     []*booking.AvailabilityResult
	err                     error
	rescheduleErr           error
	candidateErr            error
	availabilityErr         error
}

func (f *fakeBookingTool) AvailableSlots(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest) (*booking.AvailabilityResult, error) {
	f.availabilityCalls++
	f.availabilityRequest = req
	if f.availabilityErr != nil {
		return nil, f.availabilityErr
	}
	if len(f.availabilityResults) > 0 {
		index := f.availabilityCalls - 1
		if index >= len(f.availabilityResults) {
			index = len(f.availabilityResults) - 1
		}
		return f.availabilityResults[index], nil
	}
	if f.availabilityResult != nil {
		return f.availabilityResult, nil
	}
	return &booking.AvailabilityResult{
		ServiceID:       req.ServiceID,
		ServiceName:     "Classic Manicure",
		StaffID:         req.StaffID,
		StaffName:       "Mai Nguyen",
		PreferredDate:   req.PreferredDate,
		DurationMinutes: 45,
		Timezone:        "America/Chicago",
		Slots: []booking.AvailabilitySlot{
			{
				StartTime: defaultAvailabilityStartTime(),
				EndTime:   defaultAvailabilityStartTime().Add(45 * time.Minute),
				StaffID:   "staff_1",
				StaffName: "Mai Nguyen",
			},
		},
	}, nil
}

func (f *fakeBookingTool) Create(ctx context.Context, salonID string, ownerUserID string, req booking.CreateBookingRequest) (*booking.BookingAttempt, error) {
	f.calls++
	f.request = req
	return f.attempt, f.err
}

func (f *fakeBookingTool) RescheduleCandidates(ctx context.Context, salonID string, ownerUserID string, req booking.RescheduleLookupRequest) ([]booking.AppointmentActionRef, error) {
	f.candidateCalls++
	f.candidateRequest = req
	if f.candidateErr != nil {
		return nil, f.candidateErr
	}
	return append([]booking.AppointmentActionRef(nil), f.candidates...), nil
}

func (f *fakeBookingTool) Reschedule(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req booking.RescheduleRequest) (*booking.Appointment, *booking.BookingAttempt, error) {
	f.rescheduleCalls++
	f.rescheduleAppointmentID = appointmentID
	f.rescheduleRequest = req
	if f.rescheduleErr != nil {
		return nil, nil, f.rescheduleErr
	}
	if f.rescheduledAppointment != nil {
		return f.rescheduledAppointment, nil, nil
	}
	return nil, f.rescheduleFallback, nil
}
