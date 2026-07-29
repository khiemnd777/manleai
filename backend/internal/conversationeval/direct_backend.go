package conversationeval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

var errEvaluationSideEffectBlocked = errors.New("evaluation blocked a booking side effect")

type FixtureBackendRunner struct{}

func (FixtureBackendRunner) Run(ctx context.Context, salonID string, corpus Corpus, scenario Scenario, actual voice.TurnModelReply, replies conversation.ReplyGenerator) (BackendTurnResult, error) {
	request, err := scenario.ResolvedRequest(corpus)
	if err != nil {
		return BackendTurnResult{}, err
	}
	fixture, ok := corpus.CatalogFixtures[scenario.CatalogFixture]
	if !ok {
		return BackendTurnResult{}, fmt.Errorf("catalog fixture %q not found", scenario.CatalogFixture)
	}
	store := newFixtureConversationStore(salonID, scenario.ID, request, fixture)
	tool := &fixtureBookingTool{services: store.services, staff: store.staff}
	service := conversation.NewService(store, tool)
	service.SetTurnInterpreter(retainedTurnInterpreter{turn: voice.TurnUnderstandingFromModelReply(actual)})
	service.SetReplyGenerator(replies)
	before := store.session
	updated, messageErr := service.Message(ctx, salonID, "evaluation-owner", store.session.ID, conversation.MessageRequest{
		Message: request.CustomerMessage, EventKey: "evaluation:" + scenario.ID,
	})
	result := BackendTurnResult{WouldCallTools: append([]ToolAttempt(nil), tool.attempts...)}
	if updated != nil {
		result.FinalReply = lastAIMessage(updated.Transcript)
	}
	if result.FinalReply == "" {
		result.FinalReply = strings.TrimSpace(store.lastTurn.AIMessage)
	}
	result.SafeReply = firstMetadataString(store.lastTurn.AIMetadata, "safe_reply")
	if result.SafeReply == "" {
		result.SafeReply = result.FinalReply
	}
	result.NextExpectedInput = firstMetadataString(store.lastTurn.AIMetadata, "next_required_field")
	if result.NextExpectedInput == "" {
		result.NextExpectedInput = firstMetadataString(store.lastTurn.CustomerMetadata, "next_required_field")
	}
	current := store.session
	if updated != nil {
		current = *updated
	}
	preferenceMinutes := -1
	preferenceDirection := ""
	if current.DialogState.TimePreference != nil {
		preferenceDirection = strings.TrimSpace(current.DialogState.TimePreference.Direction)
		preferenceMinutes = current.DialogState.TimePreference.Minutes
	}
	offeredSlotLocalMinutes := make([]int, 0, len(current.OfferedSlots))
	location, locationErr := time.LoadLocation(store.cfg.Timezone)
	if locationErr != nil {
		location = time.UTC
	}
	for _, slot := range current.OfferedSlots {
		local := slot.StartTime.In(location)
		offeredSlotLocalMinutes = append(offeredSlotLocalMinutes, local.Hour()*60+local.Minute())
	}
	handoffRequested := current.Status == conversation.StatusHandoff || current.Outcome == conversation.OutcomeHandoffRequested || current.Outcome == conversation.OutcomeBookingFallbackPending
	handoffMode := ""
	if handoffRequested {
		handoffMode = "owner_request"
	}
	providerBookingIDPresent := strings.TrimSpace(current.AppointmentID) != "" && strings.TrimSpace(current.BookingAttemptID) != ""
	result.Evidence = BackendEvidence{
		TurnRoute:             firstMetadataString(store.lastTurn.CustomerMetadata, "turn_route"),
		TurnRouteReason:       firstMetadataString(store.lastTurn.CustomerMetadata, "turn_route_reason"),
		DeterministicCoverage: firstMetadataString(store.lastTurn.CustomerMetadata, "turn_deterministic_coverage"),
		InterpreterOutcome:    firstMetadataString(store.lastTurn.CustomerMetadata, "turn_interpreter_outcome"),
		ReplySource:           firstMetadataString(store.lastTurn.AIMetadata, "reply_source"), ReplyPolicy: store.lastTurn.ReplyPolicy,
		IntentBefore: before.Intent, IntentAfter: current.Intent, OutcomeBefore: before.Outcome, OutcomeAfter: current.Outcome,
		DialogPhaseBefore: before.DialogState.Phase, DialogPhaseAfter: current.DialogState.Phase,
		SelectedServicesBefore: fixtureSessionServiceIDs(before), SelectedServicesAfter: fixtureSessionServiceIDs(current),
		StaffBefore: before.StaffID, StaffAfter: current.StaffID,
		RequestedDateBefore: before.RequestedDate, RequestedDateAfter: current.RequestedDate,
		TimePreferenceDirection: preferenceDirection, TimePreferenceMinutes: preferenceMinutes,
		TimePreferenceTimezone: store.cfg.Timezone, OfferedSlotLocalMinutes: offeredSlotLocalMinutes,
		HandoffRequested: handoffRequested, HandoffMode: handoffMode,
		BookingConfirmed:         current.Outcome == conversation.OutcomeBookingConfirmed && providerBookingIDPresent,
		ProviderBookingIDPresent: providerBookingIDPresent,
	}
	return result, messageErr
}

type retainedTurnInterpreter struct {
	turn conversation.TurnUnderstanding
}

func (r retainedTurnInterpreter) InterpretTurn(context.Context, conversation.TurnInterpretationRequest) (conversation.TurnUnderstanding, error) {
	return r.turn, nil
}

type fixtureConversationStore struct {
	cfg             conversation.RuntimeConfig
	session         conversation.Session
	services        []conversation.ServiceOption
	serviceAliases  []conversation.ServiceAlias
	categoryAliases []conversation.ServiceCategoryAlias
	staff           []conversation.StaffOption
	businessHours   []conversation.BusinessHourPeriod
	lastTurn        conversation.TurnRecord
}

func newFixtureConversationStore(salonID string, scenarioID string, request voice.SemanticEvaluationRequest, fixture CatalogFixture) *fixtureConversationStore {
	store := &fixtureConversationStore{
		cfg: conversation.RuntimeConfig{
			SalonName: "Evaluation Nail Studio", Timezone: "America/Chicago", AIEnabled: true,
			HandoffEnabled: true, ConsultationEnabled: true, AITone: "warm and professional",
		},
	}
	for _, ref := range fixture.Services {
		service := conversation.ServiceOption{
			ID: ref.ServiceID, Name: ref.ServiceName, CategoryID: ref.CategoryID, CategoryName: ref.CategoryName,
			DurationMinutes: 45,
		}
		if profile := ref.ConsultationProfile; profile != nil {
			service.Description = profile.OwnerApprovedSummary
			service.AIDescription = profile.OwnerApprovedSummary
			service.ConsultationProfile = &conversation.ServiceConsultationProfile{
				Status: profile.Status, RecommendedOutcomes: append([]string(nil), profile.RecommendedOutcomes...),
				CompatibleCurrentSystems: append([]string(nil), profile.CompatibleCurrentSystems...),
				LengthCapabilities:       append([]string(nil), profile.LengthCapabilities...),
				PriorityTags:             append([]string(nil), profile.PriorityTags...), FinishOptions: append([]string(nil), profile.FinishOptions...),
				MaintenanceNote: profile.MaintenanceNote, OwnerApprovedSummary: profile.OwnerApprovedSummary, Revision: profile.Revision,
			}
		}
		store.services = append(store.services, service)
	}
	serviceNames := map[string]string{}
	for _, service := range store.services {
		serviceNames[service.ID] = service.Name
	}
	for index, alias := range fixture.Aliases {
		store.serviceAliases = append(store.serviceAliases, conversation.ServiceAlias{
			ID: fmt.Sprintf("evaluation-service-alias-%d", index+1), ServiceID: alias.ServiceID, ServiceName: serviceNames[alias.ServiceID],
			Alias: alias.Alias, NormalizedAlias: strings.ToLower(strings.TrimSpace(alias.Alias)), Source: "evaluation_fixture", Confidence: 1,
		})
	}
	for _, category := range fixture.Categories {
		for index, alias := range category.Aliases {
			store.categoryAliases = append(store.categoryAliases, conversation.ServiceCategoryAlias{
				ID: fmt.Sprintf("evaluation-category-alias-%s-%d", category.CategoryID, index+1), CategoryID: category.CategoryID,
				CategoryName: category.CategoryName, Alias: alias, NormalizedAlias: strings.ToLower(strings.TrimSpace(alias)),
				Source: "evaluation_fixture", Confidence: 1,
			})
		}
	}
	for _, ref := range fixture.Staff {
		store.staff = append(store.staff, conversation.StaffOption{ID: ref.StaffID, Name: ref.StaffName, AIBookable: true})
	}
	for _, period := range fixture.BusinessHours {
		store.businessHours = append(store.businessHours, conversation.BusinessHourPeriod{
			ID: period.ID, DayOfWeek: period.DayOfWeek, StartLocalTime: period.StartLocalTime,
			EndLocalTime: period.EndLocalTime, Source: "evaluation_fixture", Provider: "evaluation_fixture",
		})
	}
	store.session = fixtureSession(salonID, scenarioID, request)
	return store
}

func fixtureSession(salonID string, scenarioID string, request voice.SemanticEvaluationRequest) conversation.Session {
	now := time.Now().UTC()
	phase := strings.TrimSpace(request.CurrentBookingStage)
	if !conversation.IsDialogPhase(phase) {
		phase = conversation.DialogPhaseOpen
	}
	intent := conversation.IntentUnknown
	if phase == conversation.DialogPhaseConsultation || request.Consultation != nil {
		intent = conversation.IntentConsultation
	} else if len(request.CurrentDraft.ServiceIDs) > 0 || request.ExpectedInput != "caller_goal" {
		intent = conversation.IntentBooking
	}
	bookingAction := strings.TrimSpace(request.BookingAction)
	if !conversation.IsBookingAction(bookingAction) {
		bookingAction = conversation.BookingActionBook
	}
	session := conversation.Session{
		ID: "evaluation-session:" + scenarioID, SalonID: salonID, Channel: request.Channel,
		Status: conversation.StatusActive, Intent: intent, Outcome: conversation.OutcomeCollecting,
		BookingAction: bookingAction, LifecycleStatus: conversation.LifecycleActive,
		DialogState: conversation.DialogState{
			Version: conversation.DialogStateVersion, Phase: phase, Pending: request.Pending,
			Consultation: request.Consultation, DraftRevision: request.CurrentDraft.DraftRevision,
		},
		StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if request.CurrentDraft.HasCustomerName {
		session.CustomerName = "Evaluation Customer"
	}
	if request.CurrentDraft.HasCustomerPhone {
		session.CustomerPhone = "+13125550199"
	}
	if len(request.CurrentDraft.ServiceIDs) > 0 {
		session.ServiceID = request.CurrentDraft.ServiceIDs[0]
		for _, serviceID := range request.CurrentDraft.ServiceIDs {
			session.BookingSegments = append(session.BookingSegments, booking.BookingSegmentRequest{ServiceID: serviceID, StaffSelectionMode: booking.StaffSelectionAnyone})
		}
	}
	session.StaffID = request.CurrentDraft.StaffID
	session.RequestedDate = request.CurrentDraft.RequestedDate
	if parsed, err := time.Parse(time.RFC3339, request.CurrentDraft.RequestedStartISO); err == nil {
		session.RequestedStartTime = &parsed
	}
	if request.CurrentDraft.PartySize > 1 || len(request.CurrentDraft.PartyGroups) > 0 {
		plan := &conversation.PartyPlan{PartySize: request.CurrentDraft.PartySize, ParseSource: "evaluation_fixture", ParseConfidence: 1}
		for _, group := range request.CurrentDraft.PartyGroups {
			plan.Groups = append(plan.Groups, conversation.PartyPlanGroup{
				Label: group.GuestRef, Count: group.Count, ResolvedServiceIDs: append([]string(nil), group.ServiceIDs...), Source: "evaluation_fixture",
			})
		}
		session.PartyPlan = plan
	}
	return session
}

func (s *fixtureConversationStore) GetRuntimeConfig(context.Context, string, string) (*conversation.RuntimeConfig, error) {
	cfg := s.cfg
	return &cfg, nil
}

func (s *fixtureConversationStore) GetAnswerContextFence(context.Context, string) (conversation.AnswerContextFence, error) {
	return conversation.AnswerContextFence{ActiveProvider: "evaluation_fixture", ConnectionStatus: "active", LocationID: "fixture", SnapshotGeneration: 1, LastSyncAtRFC3339: "2026-07-17T00:00:00Z"}, nil
}

func (s *fixtureConversationStore) CreateSession(context.Context, conversation.NewSessionRecord) (*conversation.Session, error) {
	copySession := s.session
	return &copySession, nil
}

func (s *fixtureConversationStore) GetSessionForOwner(context.Context, string, string, string) (*conversation.Session, error) {
	copySession := s.session
	return &copySession, nil
}

func (s *fixtureConversationStore) GetSessionByTurnEventKey(context.Context, string, string, string, string) (*conversation.Session, bool, error) {
	return nil, false, nil
}

func (s *fixtureConversationStore) ListSessions(context.Context, string, string, string, int, int) ([]conversation.Session, error) {
	return []conversation.Session{s.session}, nil
}

func (s *fixtureConversationStore) ListWebhookEvents(context.Context, string, string, string, int, int) ([]conversation.WebhookEventLog, error) {
	return nil, nil
}

func (s *fixtureConversationStore) ArchiveSession(context.Context, string, string, string) (*conversation.Session, error) {
	copySession := s.session
	copySession.LifecycleStatus = conversation.LifecycleArchived
	return &copySession, nil
}

func (s *fixtureConversationStore) RedactSession(context.Context, string, string, string) (*conversation.Session, error) {
	copySession := s.session
	copySession.LifecycleStatus = conversation.LifecycleRedacted
	copySession.CustomerName, copySession.CustomerPhone, copySession.CustomerEmail = "", "", ""
	return &copySession, nil
}

func (s *fixtureConversationStore) ListBookableServices(context.Context, string) ([]conversation.ServiceOption, error) {
	return append([]conversation.ServiceOption(nil), s.services...), nil
}

func (s *fixtureConversationStore) ListGuidanceServices(context.Context, string) ([]conversation.ServiceOption, error) {
	return append([]conversation.ServiceOption(nil), s.services...), nil
}

func (s *fixtureConversationStore) ListBookableStaff(context.Context, string) ([]conversation.StaffOption, error) {
	return append([]conversation.StaffOption(nil), s.staff...), nil
}

func (s *fixtureConversationStore) ListActiveStaff(context.Context, string) ([]conversation.StaffOption, error) {
	return append([]conversation.StaffOption(nil), s.staff...), nil
}

func (s *fixtureConversationStore) ListStaffAssignmentStats(_ context.Context, _ string, staffIDs []string, _ time.Time, _ time.Time) (map[string]conversation.StaffAssignmentStat, error) {
	result := make(map[string]conversation.StaffAssignmentStat, len(staffIDs))
	for _, staffID := range staffIDs {
		result[staffID] = conversation.StaffAssignmentStat{StaffID: staffID}
	}
	return result, nil
}

func (s *fixtureConversationStore) ListActiveServiceAliases(context.Context, string) ([]conversation.ServiceAlias, error) {
	return append([]conversation.ServiceAlias(nil), s.serviceAliases...), nil
}

func (s *fixtureConversationStore) ListActiveServiceCategoryAliases(context.Context, string) ([]conversation.ServiceCategoryAlias, error) {
	return append([]conversation.ServiceCategoryAlias(nil), s.categoryAliases...), nil
}

func (s *fixtureConversationStore) ListActiveKnowledge(context.Context, string) ([]conversation.KnowledgeSnippet, error) {
	return nil, nil
}

func (s *fixtureConversationStore) ListExternalProviderBusinessHourPeriods(context.Context, string) ([]conversation.BusinessHourPeriod, error) {
	return append([]conversation.BusinessHourPeriod(nil), s.businessHours...), nil
}

func (s *fixtureConversationStore) ListPartyBookingRequests(context.Context, string, string, string, int, int) ([]conversation.PartyBookingRequest, error) {
	return nil, nil
}

func (s *fixtureConversationStore) UpdatePartyBookingRequestStatus(context.Context, string, string, string, string) (*conversation.PartyBookingRequest, error) {
	return nil, conversation.ErrNotFound
}

func (s *fixtureConversationStore) SaveTurn(_ context.Context, record conversation.TurnRecord) (*conversation.Session, error) {
	s.lastTurn = record
	session := record.Session
	session.StateRevision++
	session.Status = record.Update.Status
	session.Intent = record.Update.Intent
	session.Outcome = record.Update.Outcome
	session.BookingAction = record.Update.BookingAction
	session.TargetAppointmentID = record.Update.TargetAppointmentID
	session.RescheduleCandidates = append([]conversation.RescheduleCandidate(nil), record.Update.RescheduleCandidates...)
	session.CustomerName, session.CustomerPhone, session.CustomerEmail = record.Update.CustomerName, record.Update.CustomerPhone, record.Update.CustomerEmail
	session.ServiceID, session.StaffID, session.StaffSelectionMode = record.Update.ServiceID, record.Update.StaffID, record.Update.StaffSelectionMode
	session.RequestedDate, session.RequestedStartTime = record.Update.RequestedDate, record.Update.RequestedStartTime
	session.AvailabilityQuoteID, session.SlotFingerprint = record.Update.AvailabilityQuoteID, record.Update.SlotFingerprint
	session.OfferedSlots = append([]conversation.OfferedSlot(nil), record.Update.OfferedSlots...)
	session.BookingSegments = append([]booking.BookingSegmentRequest(nil), record.Update.BookingSegments...)
	session.PartyPlan = record.Update.PartyPlan
	session.DialogState = record.Update.DialogState
	session.BookingAttemptID, session.AppointmentID, session.Summary = record.Update.BookingAttemptID, record.Update.AppointmentID, record.Update.Summary
	if record.Handoff != nil {
		session.Handoff = &conversation.HandoffRequest{Reason: record.Handoff.Reason, CustomerName: record.Handoff.CustomerName, CustomerPhone: record.Handoff.CustomerPhone, Summary: record.Handoff.Summary}
	}
	sequence := len(session.Transcript)
	if strings.TrimSpace(record.CustomerMessage) != "" {
		sequence++
		session.Transcript = append(session.Transcript, conversation.TranscriptMessage{Speaker: conversation.SpeakerCustomer, Body: record.CustomerMessage, Metadata: record.CustomerMetadata, Sequence: sequence})
	}
	if strings.TrimSpace(record.ToolMessage) != "" {
		sequence++
		session.Transcript = append(session.Transcript, conversation.TranscriptMessage{Speaker: conversation.SpeakerTool, Body: record.ToolMessage, Metadata: record.ToolMetadata, Sequence: sequence})
	}
	sequence++
	session.Transcript = append(session.Transcript, conversation.TranscriptMessage{Speaker: conversation.SpeakerAI, Body: record.AIMessage, Metadata: record.AIMetadata, Sequence: sequence})
	s.session = session
	return &session, nil
}

type fixtureBookingTool struct {
	services []conversation.ServiceOption
	staff    []conversation.StaffOption
	attempts []ToolAttempt
}

func (f *fixtureBookingTool) AvailableSlots(_ context.Context, _ string, _ string, req booking.AvailabilityRequest) (*booking.AvailabilityResult, error) {
	f.attempts = append(f.attempts, ToolAttempt{Tool: "available_slots", SideEffect: false, Blocked: false, Description: "served from deterministic evaluation fixture; POS was not called"})
	serviceName := fixtureServiceName(req.ServiceID, f.services)
	staffID, staffName := "", ""
	mode := req.StaffSelectionMode
	if mode == "" {
		mode = booking.StaffSelectionAnyone
	}
	if req.StaffID != "" {
		staffID = req.StaffID
		staffName = fixtureStaffName(staffID, f.staff)
	} else if len(f.staff) > 0 {
		staffID, staffName = f.staff[0].ID, f.staff[0].Name
	}
	date := strings.TrimSpace(req.PreferredDate)
	if date == "" {
		date = time.Now().In(time.FixedZone("evaluation-central", -5*60*60)).AddDate(0, 0, 1).Format("2006-01-02")
	}
	start, err := time.ParseInLocation("2006-01-02 15:04", date+" 10:00", time.FixedZone("evaluation-central", -5*60*60))
	if err != nil {
		return nil, err
	}
	duration := 45 * time.Minute
	segment := booking.AvailabilitySegment{ServiceID: req.ServiceID, ServiceName: serviceName, StaffID: staffID, StaffName: staffName, StaffSelectionMode: mode, DurationMinutes: 45}
	return &booking.AvailabilityResult{
		QuoteID: "00000000-0000-0000-0000-000000000001", ServiceID: req.ServiceID, ServiceName: serviceName,
		StaffID: staffID, StaffName: staffName, StaffSelectionMode: mode, PreferredDate: date, DurationMinutes: 45, Timezone: "America/Chicago",
		Segments: []booking.AvailabilitySegment{segment},
		Slots: []booking.AvailabilitySlot{
			{Fingerprint: strings.Repeat("1", 64), StartTime: start, EndTime: start.Add(duration), StaffID: staffID, StaffName: staffName, StaffSelectionMode: mode, Segments: []booking.AvailabilitySegment{segment}},
			{Fingerprint: strings.Repeat("2", 64), StartTime: start.Add(3 * time.Hour), EndTime: start.Add(3*time.Hour + duration), StaffID: staffID, StaffName: staffName, StaffSelectionMode: mode, Segments: []booking.AvailabilitySegment{segment}},
			{Fingerprint: strings.Repeat("3", 64), StartTime: start.Add(5 * time.Hour), EndTime: start.Add(5*time.Hour + duration), StaffID: staffID, StaffName: staffName, StaffSelectionMode: mode, Segments: []booking.AvailabilitySegment{segment}},
		},
	}, nil
}

func (f *fixtureBookingTool) Create(context.Context, string, string, booking.CreateBookingRequest) (*booking.BookingAttempt, error) {
	f.attempts = append(f.attempts, ToolAttempt{Tool: "create_booking", SideEffect: true, Blocked: true, Description: "evaluation never calls POS or creates an appointment"})
	return nil, errEvaluationSideEffectBlocked
}

func (f *fixtureBookingTool) Cancel(context.Context, string, string, string, booking.CancelRequest) (*booking.Appointment, *booking.BookingAttempt, error) {
	f.attempts = append(f.attempts, ToolAttempt{Tool: "cancel_booking", SideEffect: true, Blocked: true, Description: "evaluation never calls POS or mutates an appointment"})
	return nil, nil, errEvaluationSideEffectBlocked
}

func (f *fixtureBookingTool) RescheduleCandidates(context.Context, string, string, booking.RescheduleLookupRequest) ([]booking.AppointmentActionRef, error) {
	f.attempts = append(f.attempts, ToolAttempt{Tool: "reschedule_candidates", SideEffect: false, Blocked: false, Description: "evaluation fixture has no customer appointments"})
	return nil, nil
}

func (f *fixtureBookingTool) Reschedule(context.Context, string, string, string, booking.RescheduleRequest) (*booking.Appointment, *booking.BookingAttempt, error) {
	f.attempts = append(f.attempts, ToolAttempt{Tool: "reschedule_booking", SideEffect: true, Blocked: true, Description: "evaluation never calls POS or mutates an appointment"})
	return nil, nil, errEvaluationSideEffectBlocked
}

func lastAIMessage(transcript []conversation.TranscriptMessage) string {
	for index := len(transcript) - 1; index >= 0; index-- {
		if transcript[index].Speaker == conversation.SpeakerAI {
			return strings.TrimSpace(transcript[index].Body)
		}
	}
	return ""
}

func firstMetadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func fixtureKnownBookingFields(session conversation.Session) []string {
	fields := make([]string, 0, 6)
	if session.ServiceID != "" || len(session.BookingSegments) > 0 {
		fields = append(fields, "service")
	}
	if session.RequestedDate != "" {
		fields = append(fields, "requested_date")
	}
	if session.RequestedStartTime != nil {
		fields = append(fields, "requested_time")
	}
	if session.CustomerName != "" {
		fields = append(fields, "customer_name")
	}
	if session.CustomerPhone != "" {
		fields = append(fields, "customer_phone")
	}
	if session.StaffID != "" || session.StaffSelectionMode != "" {
		fields = append(fields, "staff")
	}
	return fields
}

func fixtureSelectedServiceNames(session conversation.Session, services []conversation.ServiceOption) []string {
	selected := map[string]bool{}
	if session.ServiceID != "" {
		selected[session.ServiceID] = true
	}
	for _, segment := range session.BookingSegments {
		selected[segment.ServiceID] = true
	}
	result := make([]string, 0, len(selected))
	for _, service := range services {
		if selected[service.ID] {
			result = append(result, service.Name)
		}
	}
	return result
}

func fixtureSessionServiceIDs(session conversation.Session) []string {
	seen := map[string]bool{}
	result := make([]string, 0, 1+len(session.BookingSegments))
	for _, serviceID := range append([]string{session.ServiceID}, func() []string {
		ids := make([]string, 0, len(session.BookingSegments))
		for _, segment := range session.BookingSegments {
			ids = append(ids, segment.ServiceID)
		}
		return ids
	}()...) {
		serviceID = strings.TrimSpace(serviceID)
		if serviceID != "" && !seen[serviceID] {
			seen[serviceID] = true
			result = append(result, serviceID)
		}
	}
	return result
}

func fixtureServiceName(serviceID string, services []conversation.ServiceOption) string {
	for _, service := range services {
		if service.ID == serviceID {
			return service.Name
		}
	}
	return "Service"
}

func fixtureStaffName(staffID string, staff []conversation.StaffOption) string {
	for _, item := range staff {
		if item.ID == staffID {
			return item.Name
		}
	}
	return ""
}
