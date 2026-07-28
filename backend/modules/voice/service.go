package voice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/validation"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/scheduling"
	tenantruntime "github.com/manleai/ai-receptionist/modules/tenant_runtime"
)

const (
	defaultRecordingConsentMessage = "This call may be recorded to help us manage appointments and improve service."
	audioCapabilityMaxTTL          = 15 * time.Minute
	audioCapabilityDomain          = "manleai:voice-audio:v1"
)

type Service struct {
	repo                Store
	conversation        ConversationEngine
	cfg                 config.VoiceConfig
	providers           AIProviders
	configResolver      ConfigResolver
	schedulingReadiness map[string]SchedulingReadinessProvider
	now                 func() time.Time
	runtimeLimiter      voiceRuntimeLimiter
}

type voiceRuntimeLimiter interface {
	AllowSystem(context.Context, string, string, int) (tenantruntime.Decision, error)
}

type answerContextPrewarmer interface {
	PrewarmAnswerContext(ctx context.Context, salonID string) error
}

type platformSalonOwnerResolver interface {
	ResolveSalonOwnerForPlatform(ctx context.Context, salonID string, platformUserID string) (string, error)
}

func NewService(repo Store, conversation ConversationEngine, cfg config.VoiceConfig, providers AIProviders) *Service {
	return &Service{
		repo:         repo,
		conversation: conversation,
		cfg:          cfg,
		providers:    providers,
		now:          time.Now,
	}
}

func (s *Service) SetConfigResolver(resolver ConfigResolver) {
	s.configResolver = resolver
}

func (s *Service) SetTenantRuntimeLimiter(limiter voiceRuntimeLimiter) {
	s.runtimeLimiter = limiter
}

func (s *Service) SetSchedulingReadinessProviders(ownerManual SchedulingReadinessProvider, internalCalendar SchedulingReadinessProvider, externalProvider SchedulingReadinessProvider) {
	s.schedulingReadiness = map[string]SchedulingReadinessProvider{
		booking.SchedulingAuthorityOwnerManual:      ownerManual,
		booking.SchedulingAuthorityManleAICalendar:  internalCalendar,
		booking.SchedulingAuthorityExternalProvider: externalProvider,
	}
}

func (s *Service) Status(ctx context.Context, salonID string, ownerUserID string) (*Status, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	if salonID == "" || ownerUserID == "" {
		return nil, ErrValidation
	}
	salon, err := s.repo.GetSalonVoiceStatus(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	bookingReadiness, err := s.repo.GetPhoneBookingReadiness(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	voiceCfg, err := s.voiceConfig(ctx, salonID)
	if err != nil {
		return nil, err
	}

	status := &Status{
		Provider:              defaultProvider(voiceCfg.Provider),
		Configured:            s.configured(voiceCfg),
		SignatureVerification: strings.TrimSpace(voiceCfg.Twilio.AuthToken) != "",
		InboundWebhookURL:     s.webhookURL(voiceCfg, voiceCfg.Twilio.IncomingPath),
		TurnWebhookURL:        s.webhookURL(voiceCfg, voiceCfg.Twilio.TurnPath),
		RecordingWebhookURL:   s.webhookURL(voiceCfg, voiceCfg.Twilio.RecordingPath),
		StreamWebhookURL:      s.streamWebhookURL(voiceCfg, voiceCfg.Twilio.StreamPath),
		SalonPhone:            salon.Phone,
		SchedulingAuthority:   salon.SchedulingAuthority,
		AuthorityVersion:      salon.SchedulingAuthorityVersion,
		BookingMode:           salon.BookingMode,
		AI:                    s.aiStatus(ctx, salonID, voiceCfg),
		Booking:               *bookingReadiness,
		InputMode:             s.inputMode(ctx, salonID),
	}
	status.PhoneAnswering = phoneAnsweringDimension(status.Configured, salon.Phone)
	status.PhoneAnsweringReady = status.PhoneAnswering.Ready
	status.Ready = status.PhoneAnsweringReady
	if len(status.PhoneAnswering.Blockers) > 0 {
		status.BlockedReason = status.PhoneAnswering.Blockers[0].Message
	}
	authorityReadiness := s.schedulingTargetReadiness(ctx, salonID, ownerUserID, salon.SchedulingAuthority)
	status.RequestCapture = composeRequestCaptureReadiness(status.PhoneAnswering, *salon, authorityReadiness)
	status.RequestCaptureReady = status.RequestCapture.Ready
	status.AutomatedBooking = composeAutomatedBookingReadiness(status.RequestCapture, *salon, authorityReadiness)
	status.AutomatedBookingReady = status.AutomatedBooking.Ready
	status.PhoneBookingReady = status.AutomatedBookingReady
	return status, nil
}

// StatusForPlatform preserves the existing owner-scoped readiness calculation,
// while returning only business-operational evidence on the Calls surface.
// Provider configuration remains owned by the Platform Technical surface.
func (s *Service) StatusForPlatform(ctx context.Context, salonID string, platformUserID string) (*Status, error) {
	resolver, ok := s.repo.(platformSalonOwnerResolver)
	if !ok {
		return nil, ErrNotFound
	}
	ownerUserID, err := resolver.ResolveSalonOwnerForPlatform(ctx, strings.TrimSpace(salonID), strings.TrimSpace(platformUserID))
	if err != nil {
		return nil, err
	}
	status, err := s.Status(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	status.Provider = "managed"
	status.InboundWebhookURL = ""
	status.TurnWebhookURL = ""
	status.RecordingWebhookURL = ""
	status.StreamWebhookURL = ""
	status.AI.Provider = "managed"
	status.AI.STT.Provider = "managed"
	status.AI.STT.Model = ""
	status.AI.LLM.Provider = "managed"
	status.AI.LLM.Model = ""
	status.AI.TTS.Provider = "managed"
	status.AI.TTS.Model = ""
	status.AI.TTS.Voice = ""
	status.AI.Realtime.Provider = "managed"
	status.AI.Realtime.Model = ""
	status.AI.Realtime.Voice = ""
	return status, nil
}

func (s *Service) schedulingTargetReadiness(ctx context.Context, salonID string, ownerUserID string, authority string) scheduling.TargetReadiness {
	result := scheduling.TargetReadiness{TargetSchedulingAuthority: authority}
	provider := s.schedulingReadiness[authority]
	if provider == nil {
		blocker := scheduling.TargetReadinessBlocker{
			Code: "SCHEDULING_READINESS_UNAVAILABLE", Scope: "scheduling",
			Message: "Scheduling readiness is unavailable for the selected authority.",
		}
		result.AvailabilityBlockers = []scheduling.TargetReadinessBlocker{blocker}
		result.ExecutionBlockers = []scheduling.TargetReadinessBlocker{blocker}
		return result
	}
	loaded, err := provider.SchedulingTargetReadiness(ctx, salonID, ownerUserID)
	if err != nil {
		blocker := scheduling.TargetReadinessBlocker{
			Code: "SCHEDULING_READINESS_LOAD_FAILED", Scope: "scheduling",
			Message: "Scheduling readiness could not be verified.",
		}
		result.AvailabilityBlockers = []scheduling.TargetReadinessBlocker{blocker}
		result.ExecutionBlockers = []scheduling.TargetReadinessBlocker{blocker}
		return result
	}
	return loaded
}

func phoneAnsweringDimension(configured bool, phone string) VoiceReadinessDimension {
	result := VoiceReadinessDimension{Ready: true, Blockers: make([]VoiceReadinessBlocker, 0)}
	if !configured {
		result.Blockers = append(result.Blockers, VoiceReadinessBlocker{
			Code: "VOICE_PROVIDER_NOT_CONFIGURED", Scope: "voice",
			Message: "Twilio voice provider is not configured.",
		})
	}
	if strings.TrimSpace(phone) == "" {
		result.Blockers = append(result.Blockers, VoiceReadinessBlocker{
			Code: "SALON_PHONE_NOT_CONFIGURED", Scope: "salon",
			Message: "Salon phone is not configured.",
		})
	}
	result.Ready = len(result.Blockers) == 0
	return result
}

func composeRequestCaptureReadiness(phone VoiceReadinessDimension, salon SalonVoiceStatus, target scheduling.TargetReadiness) VoiceReadinessDimension {
	result := VoiceReadinessDimension{Blockers: append([]VoiceReadinessBlocker(nil), phone.Blockers...)}
	if !salon.AIEnabled {
		result.Blockers = appendVoiceBlocker(result.Blockers, VoiceReadinessBlocker{
			Code: "AI_RECEPTIONIST_DISABLED", Scope: "salon",
			Message: "Enable the AI receptionist for this salon.",
		})
	}
	switch salon.BookingMode {
	case "pending_approval", "confirmed_booking":
	case "disabled":
		result.Blockers = appendVoiceBlocker(result.Blockers, VoiceReadinessBlocker{
			Code: "SCHEDULING_DISABLED", Scope: "settings",
			Message: "Scheduling is disabled in salon settings.",
		})
	default:
		result.Blockers = appendVoiceBlocker(result.Blockers, VoiceReadinessBlocker{
			Code: "BOOKING_MODE_UNSUPPORTED", Scope: "settings",
			Message: "The selected booking mode is not supported.",
		})
	}
	if !knownSchedulingAuthority(salon.SchedulingAuthority) {
		result.Blockers = appendVoiceBlocker(result.Blockers, VoiceReadinessBlocker{
			Code: "SCHEDULING_AUTHORITY_UNSUPPORTED", Scope: "scheduling",
			Message: "The selected scheduling authority is not supported.",
		})
	}
	if salon.SchedulingAuthorityVersion < 1 || target.AuthorityVersion != salon.SchedulingAuthorityVersion || target.TargetSchedulingAuthority != salon.SchedulingAuthority {
		result.Blockers = appendVoiceBlocker(result.Blockers, VoiceReadinessBlocker{
			Code: "SCHEDULING_AUTHORITY_FENCE_STALE", Scope: "scheduling",
			Message: "Scheduling authority changed while readiness was being checked. Refresh status.",
		})
	}
	for _, blocker := range target.AvailabilityBlockers {
		result.Blockers = appendVoiceBlocker(result.Blockers, voiceBlocker(blocker))
	}
	result.Ready = phone.Ready && salon.AIEnabled && salon.BookingMode != "disabled" &&
		(salon.BookingMode == "pending_approval" || salon.BookingMode == "confirmed_booking") &&
		knownSchedulingAuthority(salon.SchedulingAuthority) && salon.SchedulingAuthorityVersion > 0 &&
		target.TargetSchedulingAuthority == salon.SchedulingAuthority && target.AuthorityVersion == salon.SchedulingAuthorityVersion &&
		target.AvailabilityReady && len(result.Blockers) == 0
	return result
}

func composeAutomatedBookingReadiness(request VoiceReadinessDimension, salon SalonVoiceStatus, target scheduling.TargetReadiness) VoiceReadinessDimension {
	result := VoiceReadinessDimension{Blockers: append([]VoiceReadinessBlocker(nil), request.Blockers...)}
	if salon.BookingMode != "confirmed_booking" {
		if salon.BookingMode == "pending_approval" {
			result.Blockers = appendVoiceBlocker(result.Blockers, VoiceReadinessBlocker{
				Code: "OWNER_REVIEW_MODE_SELECTED", Scope: "settings",
				Message: "Booking mode requires owner review and does not confirm appointments automatically.",
			})
		}
	} else {
		for _, blocker := range target.ExecutionBlockers {
			result.Blockers = appendVoiceBlocker(result.Blockers, voiceBlocker(blocker))
		}
	}
	result.Ready = request.Ready && salon.BookingMode == "confirmed_booking" && target.ExecutionReady && len(result.Blockers) == 0
	return result
}

func knownSchedulingAuthority(authority string) bool {
	return authority == booking.SchedulingAuthorityOwnerManual || authority == booking.SchedulingAuthorityManleAICalendar || authority == booking.SchedulingAuthorityExternalProvider
}

func voiceBlocker(item scheduling.TargetReadinessBlocker) VoiceReadinessBlocker {
	return VoiceReadinessBlocker{Code: item.Code, Scope: item.Scope, EntityID: item.EntityID, Message: item.Message}
}

func appendVoiceBlocker(items []VoiceReadinessBlocker, candidate VoiceReadinessBlocker) []VoiceReadinessBlocker {
	for _, item := range items {
		if item.Code == candidate.Code && item.Scope == candidate.Scope && item.EntityID == candidate.EntityID {
			return items
		}
	}
	return append(items, candidate)
}

func (s *Service) SemanticCheck(ctx context.Context, salonID string, ownerUserID string) (*SemanticCheckStatus, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	if salonID == "" || ownerUserID == "" {
		return nil, ErrValidation
	}
	if _, err := s.repo.GetSalonVoiceStatus(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	status := &SemanticCheckStatus{Provider: ProviderOpenAI}
	if s.providers.TurnModel == nil || !s.providers.TurnModel.Configured(ctx, salonID) {
		return status, nil
	}
	status.Configured = true
	verifier, ok := s.providers.TurnModel.(TurnContractVerifier)
	if !ok {
		return status, ErrProviderDisabled
	}
	check, err := verifier.CheckTurnContract(ctx, salonID)
	status.Provider = strings.TrimSpace(check.Provider)
	status.SchemaFingerprint = strings.TrimSpace(check.SchemaFingerprint)
	status.RequestID = strings.TrimSpace(check.RequestID)
	if err != nil {
		status.Diagnostics = safeTurnProviderDiagnostics(err)
		if status.SchemaFingerprint == "" {
			status.SchemaFingerprint = status.Diagnostics["schema_fingerprint"]
		}
		if status.RequestID == "" {
			status.RequestID = status.Diagnostics["request_id"]
		}
		return status, nil
	}
	status.Verified = true
	return status, nil
}

func (s *Service) SemanticEvaluate(ctx context.Context, salonID string, ownerUserID string, req SemanticEvaluationRequest) (*SemanticEvaluationResponse, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	if salonID == "" || ownerUserID == "" || !ValidSemanticEvaluationRequest(req) {
		return nil, ErrValidation
	}
	if _, err := s.repo.GetSalonVoiceStatus(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	if s.providers.TurnModel == nil || !s.providers.TurnModel.Configured(ctx, salonID) {
		return nil, ErrProviderDisabled
	}
	startedAt := time.Now()
	result, err := s.providers.TurnModel.InterpretTurn(ctx, TurnModelRequest{
		SalonID:                     salonID,
		SessionID:                   "semantic-evaluation:" + strings.TrimSpace(req.ScenarioID),
		Channel:                     strings.TrimSpace(req.Channel),
		CustomerMessage:             strings.TrimSpace(req.CustomerMessage),
		ExpectedInput:               strings.TrimSpace(req.ExpectedInput),
		SemanticContract:            strings.TrimSpace(req.SemanticContract),
		RecognizableGuidanceActions: append([]string(nil), req.RecognizableGuidanceActions...),
		SelectedServices:            append([]conversation.ConversationServiceRef(nil), req.SelectedServices...),
		CatalogServices:             append([]conversation.ConversationServiceRef(nil), req.CatalogServices...),
		CatalogServiceAliases:       append([]conversation.ConversationServiceAliasRef(nil), req.CatalogServiceAliases...),
		CatalogCategories:           append([]conversation.ConversationCategoryRef(nil), req.CatalogCategories...),
		SelectedStaff:               append([]conversation.ConversationStaffRef(nil), req.SelectedStaff...),
		CatalogStaff:                append([]conversation.ConversationStaffRef(nil), req.CatalogStaff...),
		Pending:                     req.Pending,
		CurrentBookingStage:         strings.TrimSpace(req.CurrentBookingStage),
		BookingAction:               strings.TrimSpace(req.BookingAction),
		CurrentDraft:                req.CurrentDraft,
		Consultation:                req.Consultation,
	})
	if err != nil {
		return nil, err
	}
	return &SemanticEvaluationResponse{
		ScenarioID: strings.TrimSpace(req.ScenarioID),
		Result:     result,
		DurationMS: time.Since(startedAt).Milliseconds(),
	}, nil
}

// ValidSemanticEvaluationRequest validates the read-only evaluation contract
// against the same runtime vocabulary and catalog ownership invariants used by
// the authenticated endpoint. Offline corpus validation calls this owner too.
func ValidSemanticEvaluationRequest(req SemanticEvaluationRequest) bool {
	message := strings.TrimSpace(req.CustomerMessage)
	if message == "" || len(message) > 1000 || !boundedSemanticValue(req.ScenarioID, 128) {
		return false
	}
	channel := strings.TrimSpace(req.Channel)
	if channel != conversation.ChannelSimulator && channel != conversation.ChannelPhone {
		return false
	}
	contract := strings.TrimSpace(req.SemanticContract)
	if contract != conversation.TurnSemanticContractFull && contract != conversation.TurnSemanticContractGuidance {
		return false
	}
	if !conversation.IsExpectedInput(req.ExpectedInput) {
		return false
	}
	if !conversation.IsBookingAction(req.BookingAction) {
		return false
	}
	if stage := strings.TrimSpace(req.CurrentBookingStage); stage != "" {
		if !conversation.IsDialogPhase(stage) {
			return false
		}
	}
	if len(req.CatalogServices) > 100 || len(req.CatalogServiceAliases) > 500 ||
		len(req.CatalogCategories) > 100 || len(req.CatalogStaff) > 100 ||
		len(req.SelectedServices) > 20 || len(req.SelectedStaff) > 20 ||
		len(req.RecognizableGuidanceActions) > 10 {
		return false
	}
	serviceIDs := make(map[string]conversation.ConversationServiceRef, len(req.CatalogServices))
	for _, service := range req.CatalogServices {
		id := strings.TrimSpace(service.ServiceID)
		if !boundedSemanticValue(id, 128) || !boundedSemanticValue(service.ServiceName, 256) {
			return false
		}
		if _, exists := serviceIDs[id]; exists {
			return false
		}
		serviceIDs[id] = service
	}
	for _, service := range req.SelectedServices {
		catalogService, exists := serviceIDs[strings.TrimSpace(service.ServiceID)]
		if !exists || strings.TrimSpace(service.ServiceName) != strings.TrimSpace(catalogService.ServiceName) ||
			strings.TrimSpace(service.CategoryID) != strings.TrimSpace(catalogService.CategoryID) ||
			strings.TrimSpace(service.CategoryName) != strings.TrimSpace(catalogService.CategoryName) {
			return false
		}
	}
	seenAliases := map[string]bool{}
	for _, alias := range req.CatalogServiceAliases {
		aliasKey := strings.ToLower(strings.Join(strings.Fields(alias.Alias), " "))
		if !boundedSemanticValue(alias.Alias, 256) || seenAliases[aliasKey] {
			return false
		}
		if _, exists := serviceIDs[strings.TrimSpace(alias.ServiceID)]; !exists {
			return false
		}
		seenAliases[aliasKey] = true
	}
	categoryIDs := map[string]conversation.ConversationCategoryRef{}
	categoryAliasKeys := map[string]bool{}
	serviceCategoryOwners := map[string]string{}
	for _, category := range req.CatalogCategories {
		categoryID := strings.TrimSpace(category.CategoryID)
		if !boundedSemanticValue(categoryID, 128) || !boundedSemanticValue(category.CategoryName, 256) {
			return false
		}
		if _, exists := categoryIDs[categoryID]; exists {
			return false
		}
		categoryIDs[categoryID] = category
		for _, alias := range category.Aliases {
			aliasKey := strings.ToLower(strings.Join(strings.Fields(alias), " "))
			if !boundedSemanticValue(alias, 256) || categoryAliasKeys[aliasKey] || seenAliases[aliasKey] {
				return false
			}
			categoryAliasKeys[aliasKey] = true
		}
		seenCategoryServices := map[string]bool{}
		for _, serviceID := range category.ServiceIDs {
			serviceID = strings.TrimSpace(serviceID)
			if _, exists := serviceIDs[serviceID]; !exists || seenCategoryServices[serviceID] || serviceCategoryOwners[serviceID] != "" {
				return false
			}
			seenCategoryServices[serviceID] = true
			serviceCategoryOwners[serviceID] = categoryID
		}
	}
	for _, service := range req.CatalogServices {
		if categoryID := strings.TrimSpace(service.CategoryID); categoryID != "" {
			category, exists := categoryIDs[categoryID]
			if !exists || strings.TrimSpace(service.CategoryName) != strings.TrimSpace(category.CategoryName) || serviceCategoryOwners[strings.TrimSpace(service.ServiceID)] != categoryID {
				return false
			}
		} else if strings.TrimSpace(service.CategoryName) != "" || serviceCategoryOwners[strings.TrimSpace(service.ServiceID)] != "" {
			return false
		}
	}
	staffIDs := make(map[string]bool, len(req.CatalogStaff))
	for _, staff := range req.CatalogStaff {
		id := strings.TrimSpace(staff.StaffID)
		if !boundedSemanticValue(id, 128) || !boundedSemanticValue(staff.StaffName, 256) || staffIDs[id] {
			return false
		}
		staffIDs[id] = true
	}
	for _, staff := range req.SelectedStaff {
		staffID := strings.TrimSpace(staff.StaffID)
		if !staffIDs[staffID] {
			return false
		}
		for _, catalogStaff := range req.CatalogStaff {
			if strings.TrimSpace(catalogStaff.StaffID) == staffID && strings.TrimSpace(catalogStaff.StaffName) != strings.TrimSpace(staff.StaffName) {
				return false
			}
		}
	}
	for _, serviceID := range req.CurrentDraft.ServiceIDs {
		if _, exists := serviceIDs[strings.TrimSpace(serviceID)]; !exists {
			return false
		}
	}
	if staffID := strings.TrimSpace(req.CurrentDraft.StaffID); staffID != "" && !staffIDs[staffID] {
		return false
	}
	seenGuestRefs := map[string]bool{}
	partyCount := 0
	for _, group := range req.CurrentDraft.PartyGroups {
		guestRef := strings.TrimSpace(group.GuestRef)
		if !boundedSemanticValue(guestRef, 128) || seenGuestRefs[guestRef] || group.Count < 1 || group.Count > 20 {
			return false
		}
		seenGuestRefs[guestRef] = true
		partyCount += group.Count
		for _, serviceID := range group.ServiceIDs {
			if _, exists := serviceIDs[strings.TrimSpace(serviceID)]; !exists {
				return false
			}
		}
	}
	if req.CurrentDraft.PartySize < 0 || req.CurrentDraft.PartySize > 20 {
		return false
	}
	if len(req.CurrentDraft.PartyGroups) > 0 && (req.CurrentDraft.PartySize < 2 || partyCount != req.CurrentDraft.PartySize) {
		return false
	}
	if req.CurrentDraft.DraftRevision < 0 {
		return false
	}
	if req.Pending != nil {
		for _, serviceID := range append(append([]string(nil), req.Pending.SourceServiceIDs...), req.Pending.TargetServiceIDs...) {
			if _, exists := serviceIDs[strings.TrimSpace(serviceID)]; !exists {
				return false
			}
		}
		for _, categoryID := range []string{req.Pending.SourceCategoryID, req.Pending.TargetCategoryID} {
			if categoryID = strings.TrimSpace(categoryID); categoryID != "" {
				if _, exists := categoryIDs[categoryID]; !exists {
					return false
				}
			}
		}
	}
	if req.Consultation != nil {
		consultationServiceIDs := append(append(append([]string(nil), req.Consultation.CandidateServiceIDs...), req.Consultation.RecommendedServiceIDs...), req.Consultation.Needs.ComparedServiceIDs...)
		if selected := strings.TrimSpace(req.Consultation.SelectedServiceID); selected != "" {
			consultationServiceIDs = append(consultationServiceIDs, selected)
		}
		for _, serviceID := range consultationServiceIDs {
			if _, exists := serviceIDs[strings.TrimSpace(serviceID)]; !exists {
				return false
			}
		}
	}
	seenGuidance := map[string]bool{}
	for _, action := range req.RecognizableGuidanceActions {
		action = strings.TrimSpace(action)
		if !conversation.IsGuidanceAction(action) || seenGuidance[action] {
			return false
		}
		seenGuidance[action] = true
	}
	if contract != conversation.TurnSemanticContractGuidance {
		return len(req.RecognizableGuidanceActions) == 0
	}
	stableVocabulary := conversation.GuidanceActionValues()
	if len(seenGuidance) != len(stableVocabulary) {
		return false
	}
	for _, action := range stableVocabulary {
		if !seenGuidance[action] {
			return false
		}
	}
	return true
}

func boundedSemanticValue(value string, max int) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && len(trimmed) <= max
}

func (s *Service) Audio(ctx context.Context, id string, expires string, signature string) (*AudioOutput, error) {
	id = strings.TrimSpace(id)
	expires = strings.TrimSpace(expires)
	signature = strings.TrimSpace(signature)
	store, ok := s.repo.(AudioCapabilityStore)
	if !ok || id == "" || expires == "" || signature == "" {
		return nil, ErrAudioUnavailable
	}
	metadata, err := store.GetAudioOutputMetadata(ctx, id)
	if err != nil || metadata == nil || metadata.ID != id || !validAudioCapabilityMetadata(*metadata) {
		return nil, ErrAudioUnavailable
	}
	expiresAt, err := strconv.ParseInt(expires, 10, 64)
	now := s.nowUTC()
	if err != nil || strconv.FormatInt(expiresAt, 10) != expires || expiresAt <= now.Unix() ||
		expiresAt > metadata.ExpiresAt.UTC().Unix() || expiresAt > now.Add(audioCapabilityMaxTTL).Unix() {
		return nil, ErrAudioUnavailable
	}
	token, err := s.storedTwilioAuthToken(ctx, metadata.SalonID)
	if err != nil || !verifyAudioCapability(*metadata, expiresAt, token, signature) {
		return nil, ErrAudioUnavailable
	}
	output, err := store.GetAudioOutputContent(ctx, id)
	if err != nil || output == nil || output.ID != id {
		return nil, ErrAudioUnavailable
	}
	return output, nil
}

func (s *Service) HandleIncomingCall(ctx context.Context, req IncomingCallRequest) (*CallReply, error) {
	req = normalizeIncomingCall(req)
	if req.Provider == "" || req.ProviderCallID == "" || req.ToPhone == "" {
		return nil, ErrValidation
	}
	salon, err := s.repo.FindSalonByPhone(ctx, req.ToPhone)
	if errors.Is(err, ErrNotFound) {
		_ = s.repo.RecordWebhookEvent(ctx, WebhookEvent{
			Provider:       req.Provider,
			ProviderCallID: req.ProviderCallID,
			EventType:      EventIncomingCall,
			Payload:        req.Payload,
		})
		return nil, ErrRouteNotFound
	}
	if err != nil {
		return nil, err
	}
	voiceCfg, err := s.voiceConfig(ctx, salon.SalonID)
	if err != nil {
		return nil, err
	}
	if !s.configured(voiceCfg) {
		return nil, ErrProviderDisabled
	}
	if s.runtimeLimiter != nil {
		if _, err := s.runtimeLimiter.AllowSystem(ctx, salon.SalonID, tenantruntime.MetricVoiceStart, 1); err != nil {
			if errors.Is(err, tenantruntime.ErrQuotaExceeded) {
				return nil, ErrTenantQuotaExceeded
			}
			return nil, err
		}
	}

	session, err := s.getOrStartPhoneSession(ctx, salon.SalonID, salon.OwnerUserID, req.Provider, req.ProviderCallID, req.FromPhone, req.ToPhone)
	if err != nil {
		return nil, err
	}
	go s.prewarmAnswerContext(ctx, salon.SalonID)
	_ = s.repo.RecordWebhookEvent(ctx, WebhookEvent{
		SalonID:        salon.SalonID,
		CallSessionID:  session.ID,
		Provider:       req.Provider,
		ProviderCallID: req.ProviderCallID,
		EventType:      EventIncomingCall,
		Payload:        req.Payload,
	})
	return s.buildReplyWithInputMode(ctx, CallReply{
		Message:       lastAIMessage(session),
		OpeningNotice: recordingNotice(salon),
		Continue:      session.Status == conversation.StatusActive,
		Session:       session,
	}, session, req.Provider, req.ProviderCallID, s.inputModeFromConfig(ctx, salon.SalonID, voiceCfg)), nil
}

func (s *Service) prewarmAnswerContext(ctx context.Context, salonID string) {
	prewarmer, ok := s.conversation.(answerContextPrewarmer)
	if !ok {
		return
	}
	prewarmCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = prewarmer.PrewarmAnswerContext(prewarmCtx, salonID)
}

func (s *Service) HandleSpeechTurn(ctx context.Context, req SpeechTurnRequest) (*CallReply, error) {
	routeConfigStartedAt := time.Now()
	backendDiagnostics := newBackendTurnDiagnostics()
	finishReply := func(reply *CallReply) *CallReply {
		if reply != nil {
			reply.BackendDiagnostics = backendDiagnostics.Snapshot()
		}
		return reply
	}
	req = normalizeSpeechTurn(req)
	if req.Provider == "" || req.ProviderCallID == "" {
		return nil, ErrValidation
	}
	route, err := s.repo.FindCallRoute(ctx, req.Provider, req.ProviderCallID)
	if errors.Is(err, ErrNotFound) && req.ToPhone != "" {
		salon, routeErr := s.repo.FindSalonByPhone(ctx, req.ToPhone)
		if routeErr != nil {
			err = routeErr
		} else {
			session, startErr := s.getOrStartPhoneSession(ctx, salon.SalonID, salon.OwnerUserID, req.Provider, req.ProviderCallID, req.FromPhone, req.ToPhone)
			if startErr != nil {
				return nil, startErr
			}
			route = &CallRoute{SalonID: salon.SalonID, OwnerUserID: salon.OwnerUserID, SessionID: session.ID}
			err = nil
		}
	}
	if errors.Is(err, ErrNotFound) {
		_ = s.repo.RecordWebhookEvent(ctx, WebhookEvent{
			Provider:       req.Provider,
			ProviderCallID: req.ProviderCallID,
			EventType:      EventSpeechTurn,
			Payload:        req.Payload,
		})
		return nil, ErrRouteNotFound
	}
	if err != nil {
		return nil, err
	}
	voiceCfg, err := s.voiceConfig(ctx, route.SalonID)
	if err != nil {
		return nil, err
	}
	if !s.configured(voiceCfg) {
		return nil, ErrProviderDisabled
	}
	backendDiagnostics.add(backendTimingStageRouteConfig, time.Since(routeConfigStartedAt), conversation.TurnTimingResultOK)

	if req.SpeechText == "" && len(req.Audio) > 0 {
		text, transcribeErr := s.transcribe(ctx, route.SalonID, route.OwnerUserID, route.SessionID, req)
		if transcribeErr != nil {
			_ = s.repo.RecordWebhookEvent(ctx, WebhookEvent{
				SalonID:        route.SalonID,
				CallSessionID:  route.SessionID,
				Provider:       req.Provider,
				ProviderCallID: req.ProviderCallID,
				EventType:      EventSTTFailed,
				Payload:        req.Payload,
			})
			session, _ := s.conversation.Get(ctx, route.SalonID, route.OwnerUserID, route.SessionID)
			return finishReply(s.buildReplyWithInputMode(ctx, CallReply{
				Message:  "I could not hear that clearly. Please say it again, or the owner can help directly.",
				Continue: true,
				Session:  session,
			}, session, req.Provider, req.ProviderCallID, req.InputModeOverride)), nil
		}
		req.SpeechText = strings.TrimSpace(text)
	}

	if req.SpeechText == "" {
		_ = s.repo.RecordWebhookEvent(ctx, WebhookEvent{
			SalonID:        route.SalonID,
			CallSessionID:  route.SessionID,
			Provider:       req.Provider,
			ProviderCallID: req.ProviderCallID,
			EventType:      EventNoSpeech,
			Payload:        req.Payload,
		})
		session, _ := s.conversation.Get(ctx, route.SalonID, route.OwnerUserID, route.SessionID)
		return finishReply(s.buildReplyWithInputMode(ctx, CallReply{
			Message:  "I did not hear that. How can I help you today?",
			Continue: true,
			Session:  session,
		}, session, req.Provider, req.ProviderCallID, req.InputModeOverride)), nil
	}

	session, err := s.conversation.Message(ctx, route.SalonID, route.OwnerUserID, route.SessionID, conversation.MessageRequest{
		Message:        req.SpeechText,
		EventKey:       speechTurnEventKey(req),
		TimingRecorder: backendDiagnostics.Record,
	})
	if errors.Is(err, conversation.ErrSessionClosed) {
		session, _ = s.conversation.Get(ctx, route.SalonID, route.OwnerUserID, route.SessionID)
		return finishReply(s.buildReplyWithInputMode(ctx, CallReply{
			Message:  "This request is already complete. The owner can help with anything else.",
			Continue: false,
			Session:  session,
		}, session, req.Provider, req.ProviderCallID, req.InputModeOverride)), nil
	}
	if err != nil {
		return nil, err
	}
	_ = s.repo.RecordWebhookEvent(ctx, WebhookEvent{
		SalonID:        route.SalonID,
		CallSessionID:  route.SessionID,
		Provider:       req.Provider,
		ProviderCallID: req.ProviderCallID,
		EventType:      EventSpeechTurn,
		Payload:        req.Payload,
	})
	return finishReply(s.buildReplyWithInputMode(ctx, CallReply{
		Message:  lastAIMessage(session),
		Continue: session.Status == conversation.StatusActive,
		Session:  session,
	}, session, req.Provider, req.ProviderCallID, req.InputModeOverride)), nil
}

func (s *Service) configured(cfg config.VoiceConfig) bool {
	return defaultProvider(cfg.Provider) == ProviderTwilio && strings.TrimSpace(cfg.Twilio.AuthToken) != ""
}

func (s *Service) transcribe(ctx context.Context, salonID string, ownerUserID string, sessionID string, req SpeechTurnRequest) (string, error) {
	if s.providers.STT == nil || !s.providers.STT.Configured(ctx, salonID) {
		return "", ErrProviderDisabled
	}
	return s.providers.STT.Transcribe(ctx, salonID, SpeechToTextRequest{
		Audio:       req.Audio,
		ContentType: req.AudioContentType,
		Prompt:      s.transcriptionContextPrompt(ctx, salonID, ownerUserID, sessionID),
	})
}

func speechTurnEventKey(req SpeechTurnRequest) string {
	callID := strings.TrimSpace(req.ProviderCallID)
	if callID == "" {
		callID = strings.TrimSpace(req.Payload["CallSid"])
	}
	if callID == "" {
		return ""
	}
	for _, key := range []string{"RealtimeTranscriptID", "RecordingSid", "EventSid", "TwilioIdempotencyToken"} {
		value := strings.TrimSpace(req.Payload[key])
		if value != "" {
			return strings.Join([]string{defaultProvider(req.Provider), callID, strings.ToLower(key), value}, ":")
		}
	}
	return ""
}

func (s *Service) buildReply(ctx context.Context, reply CallReply, session *conversation.Session, provider string, providerCallID string) *CallReply {
	return s.buildReplyWithInputMode(ctx, reply, session, provider, providerCallID, "")
}

func (s *Service) buildReplyWithInputMode(ctx context.Context, reply CallReply, session *conversation.Session, provider string, providerCallID string, inputModeOverride string) *CallReply {
	salonID := ""
	if session != nil {
		salonID = session.SalonID
	}
	if strings.TrimSpace(inputModeOverride) == "" {
		reply.InputMode = s.inputMode(ctx, salonID)
	} else if strings.TrimSpace(inputModeOverride) == InputModeGather {
		reply.InputMode = InputModeGather
	} else {
		reply.InputMode = normalizeVoiceTransport(inputModeOverride)
	}
	if reply.InputMode == InputModeRealtimeStream {
		return &reply
	}
	if session == nil || strings.TrimSpace(reply.Message) == "" || s.providers.TTS == nil || !s.providers.TTS.Configured(ctx, session.SalonID) {
		return &reply
	}
	audio, err := s.providers.TTS.Synthesize(ctx, session.SalonID, reply.Message, s.ttsVoice(ctx, session.SalonID))
	if err != nil || len(audio) == 0 {
		_ = s.repo.RecordWebhookEvent(ctx, WebhookEvent{
			SalonID:        session.SalonID,
			CallSessionID:  session.ID,
			Provider:       provider,
			ProviderCallID: providerCallID,
			EventType:      EventTTSFailed,
			Payload:        map[string]string{"provider": s.providers.TTS.Name()},
		})
		return &reply
	}
	output, err := s.repo.SaveAudioOutput(ctx, AudioOutputRecord{
		SalonID:        session.SalonID,
		CallSessionID:  session.ID,
		Provider:       defaultProvider(provider),
		ProviderCallID: providerCallID,
		ContentType:    s.providers.TTS.ContentType(),
		Audio:          audio,
	})
	if err != nil {
		_ = s.repo.RecordWebhookEvent(ctx, WebhookEvent{
			SalonID:        session.SalonID,
			CallSessionID:  session.ID,
			Provider:       provider,
			ProviderCallID: providerCallID,
			EventType:      EventTTSFailed,
			Payload:        map[string]string{"provider": s.providers.TTS.Name()},
		})
		return &reply
	}
	reply.AudioURL = s.audioURL(ctx, output)
	return &reply
}

func (s *Service) audioURL(ctx context.Context, output *AudioOutput) string {
	if output == nil || !validAudioCapabilityMetadata(*output) {
		return ""
	}
	now := s.nowUTC()
	expiresAt := output.ExpiresAt.UTC().Unix()
	if expiresAt <= now.Unix() || expiresAt > now.Add(audioCapabilityMaxTTL).Unix() || s.configResolver == nil {
		return ""
	}
	token, err := s.storedTwilioAuthToken(ctx, output.SalonID)
	if err != nil {
		return ""
	}
	_, publicBaseURL, err := s.configResolver.ResolveTwilioConfig(ctx, output.SalonID)
	if err != nil {
		return ""
	}
	signature := signAudioCapability(*output, expiresAt, token)
	if signature == "" {
		return ""
	}
	query := url.Values{}
	query.Set("expires", strconv.FormatInt(expiresAt, 10))
	query.Set("signature", signature)
	cfg := config.VoiceConfig{PublicBaseURL: strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")}
	return s.webhookURL(cfg, "/api/voice/audio/"+strings.TrimSpace(output.ID)) + "?" + query.Encode()
}

func (s *Service) storedTwilioAuthToken(ctx context.Context, salonID string) (string, error) {
	if s.configResolver == nil || strings.TrimSpace(salonID) == "" {
		return "", ErrAudioUnavailable
	}
	token, err := s.configResolver.ResolveStoredTwilioAuthToken(ctx, strings.TrimSpace(salonID))
	if err != nil || strings.TrimSpace(token) == "" {
		return "", ErrAudioUnavailable
	}
	return strings.TrimSpace(token), nil
}

func (s *Service) nowUTC() time.Time {
	if s != nil && s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func validAudioCapabilityMetadata(output AudioOutput) bool {
	return strings.TrimSpace(output.ID) != "" && strings.TrimSpace(output.SalonID) != "" &&
		strings.TrimSpace(output.CallSessionID) != "" && strings.TrimSpace(output.Provider) != "" &&
		strings.TrimSpace(output.ProviderCallID) != "" && !output.ExpiresAt.IsZero()
}

func signAudioCapability(output AudioOutput, expiresAt int64, token string) string {
	token = strings.TrimSpace(token)
	if token == "" || !validAudioCapabilityMetadata(output) || expiresAt <= 0 {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write(audioCapabilityPayload(output, expiresAt))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verifyAudioCapability(output AudioOutput, expiresAt int64, token string, signature string) bool {
	expected := signAudioCapability(output, expiresAt, token)
	provided, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(signature))
	if err != nil || len(provided) != sha256.Size || expected == "" {
		return false
	}
	expectedBytes, err := base64.RawURLEncoding.DecodeString(expected)
	return err == nil && hmac.Equal(expectedBytes, provided)
}

func audioCapabilityPayload(output AudioOutput, expiresAt int64) []byte {
	fields := []string{
		audioCapabilityDomain,
		strings.TrimSpace(output.ID),
		strings.TrimSpace(output.SalonID),
		strings.TrimSpace(output.Provider),
		strings.TrimSpace(output.ProviderCallID),
		strings.TrimSpace(output.CallSessionID),
		strconv.FormatInt(expiresAt, 10),
	}
	var payload strings.Builder
	for _, field := range fields {
		payload.WriteString(strconv.Itoa(len(field)))
		payload.WriteByte(':')
		payload.WriteString(field)
		payload.WriteByte('\n')
	}
	return []byte(payload.String())
}

func (s *Service) inputMode(ctx context.Context, salonID string) string {
	cfg, err := s.voiceConfig(ctx, salonID)
	if err != nil {
		return InputModeGather
	}
	return s.inputModeFromConfig(ctx, salonID, cfg)
}

func (s *Service) inputModeFromConfig(ctx context.Context, salonID string, cfg config.VoiceConfig) string {
	if normalizeVoiceTransport(cfg.Twilio.VoiceTransport) == InputModeRealtimeStream && s.realtimeReady(ctx, salonID, cfg) {
		return InputModeRealtimeStream
	}
	if s.providers.STT != nil && s.providers.STT.Configured(ctx, salonID) {
		return InputModeRecording
	}
	return InputModeGather
}

func (s *Service) ttsVoice(ctx context.Context, salonID string) string {
	cfg, _, err := s.openAIConfig(ctx, salonID)
	if err != nil {
		return strings.TrimSpace(s.cfg.AI.OpenAI.SpeechVoice)
	}
	return strings.TrimSpace(cfg.SpeechVoice)
}

func (s *Service) ConnectRealtime(ctx context.Context, salonID string, sessionID string, providerCallID string) (RealtimeSession, error) {
	if strings.TrimSpace(salonID) == "" || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(providerCallID) == "" {
		return nil, ErrValidation
	}
	cfg, err := s.voiceConfig(ctx, salonID)
	if err != nil {
		return nil, err
	}
	if !s.realtimeReady(ctx, salonID, cfg) {
		return nil, ErrProviderDisabled
	}
	ownerUserID := ""
	if route, err := s.repo.FindCallRoute(ctx, ProviderTwilio, providerCallID); err == nil && route.SessionID == strings.TrimSpace(sessionID) {
		ownerUserID = route.OwnerUserID
	}
	return s.providers.Realtime.ConnectRealtime(ctx, salonID, RealtimeSessionOptions{
		SessionID:           strings.TrimSpace(sessionID),
		CallID:              strings.TrimSpace(providerCallID),
		Voice:               s.realtimeVoice(ctx, salonID),
		Instructions:        s.realtimeInstructions(ctx, salonID),
		TranscriptionPrompt: s.transcriptionContextPrompt(ctx, salonID, ownerUserID, sessionID),
	})
}

func (s *Service) RecordRealtimeEvent(ctx context.Context, provider string, providerCallID string, sessionID string, eventType string, payload map[string]string) error {
	provider = defaultProvider(provider)
	providerCallID = strings.TrimSpace(providerCallID)
	sessionID = strings.TrimSpace(sessionID)
	eventType = strings.TrimSpace(eventType)
	if providerCallID == "" || eventType == "" {
		return ErrValidation
	}
	event := WebhookEvent{
		Provider:       provider,
		ProviderCallID: providerCallID,
		EventType:      eventType,
		Payload:        payload,
	}
	route, err := s.repo.FindCallRoute(ctx, provider, providerCallID)
	if err == nil {
		if sessionID != "" && route.SessionID != sessionID {
			return ErrRouteNotFound
		}
		event.SalonID = route.SalonID
		event.CallSessionID = route.SessionID
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.repo.RecordWebhookEvent(ctx, event)
}

func (s *Service) HandleUnintelligibleRealtimeInput(ctx context.Context, provider string, providerCallID string, sessionID string, itemID string) (*CallReply, error) {
	provider = defaultProvider(provider)
	providerCallID = strings.TrimSpace(providerCallID)
	sessionID = strings.TrimSpace(sessionID)
	itemID = strings.TrimSpace(itemID)
	if providerCallID == "" || sessionID == "" || itemID == "" {
		return nil, ErrValidation
	}
	route, err := s.repo.FindCallRoute(ctx, provider, providerCallID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrRouteNotFound
		}
		return nil, err
	}
	if route.SessionID != sessionID {
		return nil, ErrRouteNotFound
	}
	session, err := s.conversation.HandleUnintelligibleVoiceInput(ctx, route.SalonID, route.OwnerUserID, route.SessionID, conversation.VoiceInputHandoffRequest{
		EventKey: "voice-input-unintelligible:" + route.SessionID,
	})
	if err != nil {
		return nil, err
	}
	return &CallReply{
		Message:   lastAIMessage(session),
		Continue:  session != nil && session.Status == conversation.StatusActive,
		Session:   session,
		InputMode: InputModeRealtimeStream,
	}, nil
}

func (s *Service) RealtimeFallbackMessage(ctx context.Context, provider string, providerCallID string) (string, error) {
	provider = defaultProvider(provider)
	providerCallID = strings.TrimSpace(providerCallID)
	if providerCallID == "" {
		return "", ErrValidation
	}
	route, err := s.repo.FindCallRoute(ctx, provider, providerCallID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	hasFailure, err := s.repo.HasTerminalRealtimeFailure(ctx, provider, providerCallID, route.SessionID)
	if err != nil {
		return "", err
	}
	if !hasFailure {
		return "", nil
	}
	session, err := s.conversation.Get(ctx, route.SalonID, route.OwnerUserID, route.SessionID)
	if err != nil {
		return "", err
	}
	if session == nil || session.Status != conversation.StatusActive {
		return "", nil
	}
	approvedReply := strings.TrimSpace(lastAIMessage(session))
	if approvedReply == "" {
		approvedReply = "How can I help you today?"
	}
	return "I had an audio issue, but we can continue. " + approvedReply, nil
}

func (s *Service) realtimeVoice(ctx context.Context, salonID string) string {
	cfg, _, err := s.openAIConfig(ctx, salonID)
	if err != nil {
		return strings.TrimSpace(s.cfg.AI.OpenAI.RealtimeVoice)
	}
	return strings.TrimSpace(cfg.RealtimeVoice)
}

func (s *Service) realtimeInstructions(ctx context.Context, salonID string) string {
	cfg, _, err := s.openAIConfig(ctx, salonID)
	if err != nil {
		return strings.TrimSpace(s.cfg.AI.OpenAI.RealtimeInstructions)
	}
	return strings.TrimSpace(cfg.RealtimeInstructions)
}

func (s *Service) transcriptionContextPrompt(ctx context.Context, salonID string, ownerUserID string, sessionID string) string {
	if strings.TrimSpace(salonID) == "" || strings.TrimSpace(ownerUserID) == "" || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	context, err := s.conversation.TranscriptionContext(ctx, salonID, ownerUserID, sessionID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(context.Prompt)
}

func (s *Service) aiStatus(ctx context.Context, salonID string, cfg config.VoiceConfig) VoiceAIStatus {
	provider := defaultAIProvider(cfg.AI.Provider)
	status := VoiceAIStatus{
		Provider: provider,
		STT:      s.capabilityStatus(ctx, salonID, cfg, provider, "stt"),
		LLM:      s.capabilityStatus(ctx, salonID, cfg, provider, "llm"),
		TTS:      s.capabilityStatus(ctx, salonID, cfg, provider, "tts"),
		Realtime: s.capabilityStatus(ctx, salonID, cfg, provider, "realtime"),
	}
	status.Configured = status.STT.Configured || status.LLM.Configured || status.TTS.Configured || status.Realtime.Configured
	status.Ready = status.STT.Ready && status.LLM.Ready && status.TTS.Ready
	return status
}

func (s *Service) capabilityStatus(ctx context.Context, salonID string, cfg config.VoiceConfig, provider string, capability string) ProviderCapabilityStatus {
	status := ProviderCapabilityStatus{Provider: provider}
	if provider != ProviderOpenAI {
		status.BlockedReason = "External AI voice provider is not configured."
		return status
	}
	status.Configured = strings.TrimSpace(cfg.AI.OpenAI.APIKey) != ""
	switch capability {
	case "stt":
		status.Model = strings.TrimSpace(cfg.AI.OpenAI.TranscriptionModel)
		status.Ready = status.Configured && status.Model != "" && s.providers.STT != nil && s.providers.STT.Configured(ctx, salonID)
	case "llm":
		status.Model = strings.TrimSpace(cfg.AI.OpenAI.ReplyModel)
		status.Ready = status.Configured && status.Model != "" && s.providers.LLM != nil && s.providers.LLM.Configured(ctx, salonID)
	case "tts":
		status.Model = strings.TrimSpace(cfg.AI.OpenAI.SpeechModel)
		status.Voice = strings.TrimSpace(cfg.AI.OpenAI.SpeechVoice)
		status.Ready = status.Configured && status.Model != "" && status.Voice != "" && s.providers.TTS != nil && s.providers.TTS.Configured(ctx, salonID)
	case "realtime":
		status.Model = strings.TrimSpace(cfg.AI.OpenAI.RealtimeModel)
		status.Voice = strings.TrimSpace(cfg.AI.OpenAI.RealtimeVoice)
		if !cfg.AI.OpenAI.RealtimeEnabled {
			status.BlockedReason = "OpenAI Realtime is disabled."
			return status
		}
		status.Configured = status.Configured && cfg.AI.OpenAI.RealtimeEnabled
		status.Ready = status.Configured && status.Model != "" && status.Voice != "" && s.providers.Realtime != nil && s.providers.Realtime.Configured(ctx, salonID)
	}
	if !status.Configured {
		status.BlockedReason = "OpenAI API key is not configured."
	} else if !status.Ready {
		status.BlockedReason = "OpenAI model configuration is incomplete."
	}
	return status
}

func (s *Service) webhookURL(cfg config.VoiceConfig, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if cfg.PublicBaseURL == "" {
		return path
	}
	return strings.TrimRight(cfg.PublicBaseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func (s *Service) streamWebhookURL(cfg config.VoiceConfig, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "ws://") || strings.HasPrefix(path, "wss://") {
		return path
	}
	webhookURL := s.webhookURL(cfg, path)
	switch {
	case strings.HasPrefix(webhookURL, "https://"):
		return "wss://" + strings.TrimPrefix(webhookURL, "https://")
	case strings.HasPrefix(webhookURL, "http://"):
		return "ws://" + strings.TrimPrefix(webhookURL, "http://")
	default:
		return webhookURL
	}
}

func (s *Service) TwilioWebhookConfig(ctx context.Context, providerCallID string, toPhone string) (config.TwilioVoiceConfig, string, error) {
	salonID := ""
	providerCallID = strings.TrimSpace(providerCallID)
	if providerCallID != "" {
		route, err := s.repo.FindCallRoute(ctx, ProviderTwilio, providerCallID)
		if err == nil {
			salonID = route.SalonID
		} else if !errors.Is(err, ErrNotFound) {
			return config.TwilioVoiceConfig{}, "", err
		}
	}
	if salonID == "" && strings.TrimSpace(toPhone) != "" {
		salon, err := s.repo.FindSalonByPhone(ctx, validation.NormalizePhone(toPhone))
		if err == nil {
			salonID = salon.SalonID
		} else if !errors.Is(err, ErrNotFound) {
			return config.TwilioVoiceConfig{}, "", err
		}
	}
	voiceCfg, err := s.voiceConfig(ctx, salonID)
	if err != nil {
		return config.TwilioVoiceConfig{}, "", err
	}
	return voiceCfg.Twilio, voiceCfg.PublicBaseURL, nil
}

func (s *Service) StreamRoute(ctx context.Context, provider string, providerCallID string, sessionID string) (*CallRoute, error) {
	provider = defaultProvider(provider)
	providerCallID = strings.TrimSpace(providerCallID)
	sessionID = strings.TrimSpace(sessionID)
	if providerCallID == "" || sessionID == "" {
		return nil, ErrValidation
	}
	route, err := s.repo.FindCallRoute(ctx, provider, providerCallID)
	if err != nil {
		return nil, err
	}
	if route.SessionID != sessionID {
		return nil, ErrRouteNotFound
	}
	return route, nil
}

func (s *Service) voiceConfig(ctx context.Context, salonID string) (config.VoiceConfig, error) {
	cfg := s.cfg
	cfg.Provider = defaultProvider(cfg.Provider)
	cfg.PublicBaseURL = strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/")
	cfg.Twilio.IncomingPath = defaultString(strings.TrimSpace(cfg.Twilio.IncomingPath), "/api/voice/twilio/incoming")
	cfg.Twilio.TurnPath = defaultString(strings.TrimSpace(cfg.Twilio.TurnPath), "/api/voice/twilio/turn")
	cfg.Twilio.RecordingPath = defaultString(strings.TrimSpace(cfg.Twilio.RecordingPath), "/api/voice/twilio/recording")
	cfg.Twilio.StreamPath = defaultString(strings.TrimSpace(cfg.Twilio.StreamPath), "/api/voice/twilio/stream")
	cfg.Twilio.VoiceTransport = normalizeVoiceTransport(defaultString(cfg.Twilio.VoiceTransport, InputModeRecording))
	cfg.AI.OpenAI.BaseURL = defaultString(strings.TrimRight(strings.TrimSpace(cfg.AI.OpenAI.BaseURL), "/"), "https://api.openai.com/v1")
	cfg.AI.OpenAI.TranscriptionModel = defaultString(strings.TrimSpace(cfg.AI.OpenAI.TranscriptionModel), "gpt-4o-mini-transcribe")
	cfg.AI.OpenAI.ReplyModel = defaultString(strings.TrimSpace(cfg.AI.OpenAI.ReplyModel), "gpt-4.1-mini")
	cfg.AI.OpenAI.SpeechModel = defaultString(strings.TrimSpace(cfg.AI.OpenAI.SpeechModel), "tts-1")
	cfg.AI.OpenAI.SpeechVoice = defaultString(strings.TrimSpace(cfg.AI.OpenAI.SpeechVoice), "alloy")
	cfg.AI.OpenAI.SpeechOutputMode = config.NormalizeOpenAISpeechOutputMode(cfg.AI.OpenAI.SpeechOutputMode)
	cfg.AI.OpenAI.RealtimeModel = config.NormalizeOpenAIRealtimeModel(cfg.AI.OpenAI.RealtimeModel)
	cfg.AI.OpenAI.RealtimeVoice = defaultString(strings.TrimSpace(cfg.AI.OpenAI.RealtimeVoice), cfg.AI.OpenAI.SpeechVoice)
	cfg.AI.OpenAI.RealtimeNoiseProfile = config.NormalizeOpenAIRealtimeNoiseProfile(cfg.AI.OpenAI.RealtimeNoiseProfile)
	cfg.AI.OpenAI.RealtimeInstructions = strings.TrimSpace(cfg.AI.OpenAI.RealtimeInstructions)
	if s.configResolver == nil || strings.TrimSpace(salonID) == "" {
		return cfg, nil
	}
	twilioCfg, publicBaseURL, err := s.configResolver.ResolveTwilioConfig(ctx, salonID)
	if err != nil {
		return config.VoiceConfig{}, err
	}
	openAICfg, openAIEnabled, err := s.configResolver.ResolveOpenAIConfig(ctx, salonID)
	if err != nil {
		return config.VoiceConfig{}, err
	}
	cfg.Twilio = twilioCfg
	cfg.Twilio.StreamPath = defaultString(strings.TrimSpace(cfg.Twilio.StreamPath), "/api/voice/twilio/stream")
	cfg.Twilio.VoiceTransport = normalizeVoiceTransport(defaultString(cfg.Twilio.VoiceTransport, InputModeRecording))
	cfg.PublicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	cfg.AI.OpenAI = openAICfg
	if openAIEnabled {
		cfg.AI.Provider = ProviderOpenAI
	} else {
		cfg.AI.Provider = ""
	}
	return cfg, nil
}

func (s *Service) openAIConfig(ctx context.Context, salonID string) (config.OpenAIVoiceConfig, bool, error) {
	if s.configResolver == nil || strings.TrimSpace(salonID) == "" {
		return s.cfg.AI.OpenAI, strings.TrimSpace(s.cfg.AI.Provider) == ProviderOpenAI, nil
	}
	return s.configResolver.ResolveOpenAIConfig(ctx, salonID)
}

func (s *Service) StreamingSpeechEnabled(ctx context.Context, salonID string) (bool, error) {
	cfg, enabled, err := s.openAIConfig(ctx, salonID)
	if err != nil {
		return false, err
	}
	return enabled && cfg.RealtimeEnabled && config.NormalizeOpenAISpeechOutputMode(cfg.SpeechOutputMode) == config.OpenAISpeechOutputStreamingTTS && s.providers.StreamingTTS != nil && s.providers.StreamingTTS.Configured(ctx, salonID), nil
}

func (s *Service) StreamSpeech(ctx context.Context, salonID string, requestID string, text string, onChunk func(SpeechChunk) error) (SpeechStreamResult, error) {
	if s.providers.StreamingTTS == nil || !s.providers.StreamingTTS.Configured(ctx, salonID) {
		return SpeechStreamResult{}, ErrProviderDisabled
	}
	return s.providers.StreamingTTS.StreamSpeech(ctx, salonID, SpeechStreamRequest{
		RequestID: strings.TrimSpace(requestID),
		Text:      strings.TrimSpace(text),
		Voice:     s.ttsVoice(ctx, salonID),
	}, onChunk)
}

func (s *Service) getOrStartPhoneSession(ctx context.Context, salonID string, ownerUserID string, provider string, providerCallID string, fromPhone string, toPhone string) (*conversation.Session, error) {
	route, err := s.repo.FindCallRoute(ctx, provider, providerCallID)
	if err == nil {
		return s.conversation.Get(ctx, route.SalonID, route.OwnerUserID, route.SessionID)
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	return s.conversation.StartPhoneCall(ctx, salonID, ownerUserID, conversation.StartPhoneCallRequest{
		Provider:       provider,
		ProviderCallID: providerCallID,
		FromPhone:      fromPhone,
		ToPhone:        toPhone,
	})
}

func normalizeIncomingCall(req IncomingCallRequest) IncomingCallRequest {
	req.Provider = defaultProvider(req.Provider)
	req.ProviderCallID = strings.TrimSpace(req.ProviderCallID)
	req.FromPhone = validation.NormalizePhone(req.FromPhone)
	req.ToPhone = validation.NormalizePhone(req.ToPhone)
	if req.Payload == nil {
		req.Payload = map[string]string{}
	}
	return req
}

func normalizeSpeechTurn(req SpeechTurnRequest) SpeechTurnRequest {
	req.Provider = defaultProvider(req.Provider)
	req.ProviderCallID = strings.TrimSpace(req.ProviderCallID)
	req.FromPhone = validation.NormalizePhone(req.FromPhone)
	req.ToPhone = validation.NormalizePhone(req.ToPhone)
	req.SpeechText = strings.TrimSpace(req.SpeechText)
	req.AudioContentType = strings.TrimSpace(req.AudioContentType)
	if req.Payload == nil {
		req.Payload = map[string]string{}
	}
	return req
}

func defaultProvider(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ProviderTwilio
	}
	return value
}

func defaultAIProvider(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "not_configured"
	}
	return value
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func normalizeVoiceTransport(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case InputModeRealtimeStream:
		return InputModeRealtimeStream
	default:
		return InputModeRecording
	}
}

func (s *Service) realtimeReady(ctx context.Context, salonID string, cfg config.VoiceConfig) bool {
	return cfg.AI.OpenAI.RealtimeEnabled &&
		strings.TrimSpace(cfg.AI.OpenAI.APIKey) != "" &&
		strings.TrimSpace(cfg.AI.OpenAI.RealtimeModel) != "" &&
		strings.TrimSpace(cfg.AI.OpenAI.RealtimeVoice) != "" &&
		s.providers.Realtime != nil &&
		s.providers.Realtime.Configured(ctx, salonID)
}

func lastAIMessage(session *conversation.Session) string {
	if session == nil {
		return "Thank you for calling. How can I help you today?"
	}
	if session.ReplayEventKey != "" {
		return session.ReplayAIMessage
	}
	for i := len(session.Transcript) - 1; i >= 0; i-- {
		if session.Transcript[i].Speaker == conversation.SpeakerAI {
			return session.Transcript[i].Body
		}
	}
	return "Thank you for calling. How can I help you today?"
}

func recordingNotice(salon *InboundSalon) string {
	if salon == nil || !salon.RecordingEnabled {
		return ""
	}
	notice := strings.TrimSpace(salon.RecordingConsentMessage)
	if notice == "" {
		return defaultRecordingConsentMessage
	}
	return notice
}
