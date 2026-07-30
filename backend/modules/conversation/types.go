package conversation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

const (
	ChannelSimulator = "simulator"
	ChannelPhone     = "phone"

	StatusActive    = "active"
	StatusCompleted = "completed"
	StatusHandoff   = "handoff"
	StatusFailed    = "failed"

	IntentUnknown      = "unknown"
	IntentBooking      = "booking"
	IntentHandoff      = "handoff"
	IntentConsultation = "consultation"

	ReplyPolicyOperationalFact      = "operational_fact"
	ReplyPolicyStyleOnly            = "style_only"
	ReplyPolicyConsultationQuestion = "consultation_question"

	DialogStateVersion = 6

	DialogPhaseOpen         = "open"
	DialogPhaseDrafting     = "drafting"
	DialogPhaseClarifying   = "clarifying"
	DialogPhaseAvailability = "availability"
	DialogPhaseReview       = "review"
	DialogPhaseConsultation = "consultation"

	ConsultationStatusCollectingNeeds   = "collecting_needs"
	ConsultationStatusComparing         = "comparing"
	ConsultationStatusAwaitingSelection = "awaiting_selection"
	ConsultationStatusAwaitingBooking   = "awaiting_booking"
	ConsultationStatusCompleted         = "completed"
	ConsultationStatusHandedOff         = "handed_off"

	ConsultationProfileStatusReady = "ready"

	ConsultationSystemNatural       = "natural"
	ConsultationSystemRegularPolish = "regular_polish"
	ConsultationSystemGel           = "gel"
	ConsultationSystemDip           = "dip"
	ConsultationSystemAcrylic       = "acrylic"
	ConsultationSystemExtension     = "extension"
	ConsultationSystemUnknown       = "unknown"

	ConsultationOutcomeMaintain     = "maintain"
	ConsultationOutcomeShorten      = "shorten"
	ConsultationOutcomeAddLength    = "add_length"
	ConsultationOutcomeAddStrength  = "add_strength"
	ConsultationOutcomeRepair       = "repair"
	ConsultationOutcomeRemoval      = "removal"
	ConsultationOutcomeColorRefresh = "color_refresh"
	ConsultationOutcomeCompare      = "compare"
	ConsultationOutcomeUnknown      = "unknown"

	ConsultationLengthKeep      = "keep"
	ConsultationLengthShorten   = "shorten"
	ConsultationLengthAddLength = "add_length"
	ConsultationLengthUnknown   = "unknown"

	ConsultationPriorityDurability       = "durability"
	ConsultationPriorityLowerMaintenance = "lower_maintenance"
	ConsultationPriorityLowerCost        = "lower_cost"
	ConsultationPriorityShorterVisit     = "shorter_visit"

	ConsultationFinishNatural       = "natural"
	ConsultationFinishRegularPolish = "regular_polish"
	ConsultationFinishGelPolish     = "gel_polish"
	ConsultationFinishGlossy        = "glossy"
	ConsultationFinishMatte         = "matte"
	ConsultationFinishNailArt       = "nail_art"

	ConsultationNeedFieldCurrentSystem      = "current_system"
	ConsultationNeedFieldDesiredOutcome     = "desired_outcome"
	ConsultationNeedFieldLengthChange       = "length_change"
	ConsultationNeedFieldPriorities         = "priorities"
	ConsultationNeedFieldDesiredFinishes    = "desired_finishes"
	ConsultationNeedFieldComparedServiceIDs = "compared_service_ids"

	ConsultationNeedOperationSet     = "set"
	ConsultationNeedOperationReplace = "replace"
	ConsultationNeedOperationAdd     = "add"
	ConsultationNeedOperationRemove  = "remove"
	ConsultationNeedOperationClear   = "clear"

	SafetyCategoryPain               = "pain"
	SafetyCategoryInjury             = "injury"
	SafetyCategoryInfection          = "infection"
	SafetyCategoryAllergy            = "allergy"
	SafetyCategoryBleeding           = "bleeding"
	SafetyCategorySwelling           = "swelling"
	SafetyCategoryMedicalSuitability = "medical_suitability"
	SafetyCategoryOtherHealth        = "other_health"

	ConversationActUnknown   = "unknown"
	ConversationActAdd       = "add_service"
	ConversationActReplace   = "replace_service"
	ConversationActRemove    = "remove_service"
	ConversationActUndo      = "undo_service_edit"
	ConversationActSummarize = "summarize_booking"
	ConversationActReview    = "accept_review"
	ConversationActSet       = "set_field"
	ConversationActClear     = "clear_field"

	ConversationEntityService  = "service"
	ConversationEntityStaff    = "staff"
	ConversationEntityDateTime = "date_time"
	ConversationEntityGuest    = "guest"
	ConversationEntityCustomer = "customer"

	ConversationQuestionCurrentBooking = "current_booking"
	ConversationQuestionCatalog        = "catalog"
	ConversationQuestionAvailability   = "availability"
	ConversationQuestionPrice          = "price"
	ConversationQuestionHours          = "hours"
	ConversationQuestionStaff          = "staff"
	ConversationQuestionPolicy         = "policy"

	ConversationQuestionModeList      = "list"
	ConversationQuestionModeCount     = "count"
	ConversationQuestionModeExistence = "existence"
	ConversationQuestionModeDetails   = "details"
	ConversationQuestionModeCompare   = "compare"

	TimePreferenceBefore = "before"
	TimePreferenceAfter  = "after"
	TimePreferenceExact  = "exact"

	ConversationScopeOne         = "one"
	ConversationScopeAllMatching = "all_matching"
	ConversationScopeAll         = "all"

	ConversationGuestCaller  = "caller"
	ConversationGuestAnother = "another_guest"

	PendingPartyServiceTarget             = "party_service_target"
	PendingPartyServiceGuest              = "party_service_guest"
	PendingPartyServiceOperation          = "party_service_operation"
	PendingPartyServiceSource             = "party_service_source"
	PendingCustomerNameConfirmation       = "customer_name_confirmation"
	PendingStaffAlternative               = "staff_alternative"
	PendingManualAppointmentTarget        = "manual_appointment_target"
	PendingFuzzyServiceConfirmation       = "fuzzy_service_confirmation"
	PendingInternalRescheduleConfirmation = "internal_reschedule_confirmation"
	PendingInternalCancelReason           = "internal_cancel_reason"
	PendingInternalCancelConfirmation     = "internal_cancel_confirmation"

	OutcomeCollecting             = "collecting"
	OutcomeBookingConfirmed       = "booking_confirmed"
	OutcomeBookingRescheduled     = "booking_rescheduled"
	OutcomeBookingCancelled       = "booking_cancelled"
	OutcomeBookingFallbackPending = "booking_fallback_pending"
	OutcomeOwnerReviewPending     = "owner_review_pending"
	OutcomeHandoffRequested       = "handoff_requested"
	OutcomeConsultationCompleted  = "consultation_completed"
	OutcomeAIDisabled             = "ai_disabled"
	OutcomeFailed                 = "failed"

	LifecycleActive   = "active"
	LifecycleArchived = "archived"
	LifecycleRedacted = "redacted"

	SpeakerAI       = "ai"
	SpeakerCustomer = "customer"
	SpeakerTool     = "tool"

	BookingActionBook       = "book"
	BookingActionReschedule = "reschedule"
	BookingActionCancel     = "cancel"

	GuidanceActionBook           = "book"
	GuidanceActionServiceCatalog = "service_catalog"
	GuidanceActionConsultation   = "consultation"
	GuidanceActionSalonQuestion  = "salon_question"
	GuidanceActionNameService    = "name_service"
	GuidanceActionHumanHandoff   = "human_handoff"
	GuidanceActionReschedule     = "reschedule"
	GuidanceActionCancel         = "cancel"

	HandoffReasonHumanRequested              = "human_requested"
	HandoffReasonAIBookingDisabled           = "ai_booking_disabled"
	HandoffReasonBookingUnavailable          = "booking_unavailable"
	HandoffReasonCustomerDetailsUnavailable  = "customer_details_unavailable"
	HandoffReasonGroupBooking                = "group_booking"
	HandoffReasonConsultationSafety          = "consultation_safety"
	HandoffReasonConsultationUnresolved      = "consultation_unresolved"
	HandoffReasonGuidanceProviderUnavailable = "guidance_provider_unavailable"
	HandoffReasonServiceClarification        = "service_clarification_unresolved"
	HandoffReasonVoiceInputUnintelligible    = "voice_input_unintelligible"

	PartyRequestStatusPending   = "pending"
	PartyRequestStatusContacted = "contacted"
	PartyRequestStatusResolved  = "resolved"
	PartyRequestStatusDismissed = "dismissed"
)

// GuidanceActionValues exposes the stable semantic protocol vocabulary from
// its owning conversation contract. These are meanings the interpreter must
// always be able to recognize. Runtime capability is resolved separately and
// must never remove a caller intent from this vocabulary.
func GuidanceActionValues() []string {
	return []string{
		GuidanceActionBook,
		GuidanceActionServiceCatalog,
		GuidanceActionConsultation,
		GuidanceActionSalonQuestion,
		GuidanceActionNameService,
		GuidanceActionHumanHandoff,
		GuidanceActionReschedule,
		GuidanceActionCancel,
	}
}

func IsGuidanceAction(value string) bool {
	return containsString(GuidanceActionValues(), strings.TrimSpace(value))
}

// GuidanceGoalForAction derives the legacy/general turn goal from the compact
// guidance protocol. Guidance model output must not author both fields because
// that would create two competing sources for the same routing decision.
func GuidanceGoalForAction(action string) string {
	switch strings.TrimSpace(action) {
	case GuidanceActionBook, GuidanceActionNameService:
		return "book_appointment"
	case GuidanceActionServiceCatalog, GuidanceActionSalonQuestion:
		return "information"
	case GuidanceActionConsultation:
		return "consultation"
	case GuidanceActionHumanHandoff:
		return "human_handoff"
	case GuidanceActionReschedule:
		return "reschedule_appointment"
	case GuidanceActionCancel:
		return "cancel_appointment"
	default:
		return "unknown"
	}
}

func IsBookingAction(value string) bool {
	switch strings.TrimSpace(value) {
	case BookingActionBook, BookingActionReschedule, BookingActionCancel:
		return true
	default:
		return false
	}
}

func IsDialogPhase(value string) bool {
	switch strings.TrimSpace(value) {
	case DialogPhaseOpen, DialogPhaseDrafting, DialogPhaseClarifying,
		DialogPhaseAvailability, DialogPhaseReview, DialogPhaseConsultation:
		return true
	default:
		return false
	}
}

var (
	ErrValidation                = errors.New("conversation validation failed")
	ErrNotFound                  = errors.New("conversation record not found")
	ErrSessionClosed             = errors.New("conversation session is closed")
	ErrSessionStateConflict      = errors.New("conversation session state changed")
	ErrSessionIdentityConflict   = errors.New("conversation provider call identity conflict")
	ErrSessionRouteFenceConflict = errors.New("conversation provider route changed")
	ErrLifecycle                 = errors.New("conversation lifecycle action is not allowed")
)

type BookingTool interface {
	AvailableSlots(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest) (*booking.AvailabilityResult, error)
	Create(ctx context.Context, salonID string, ownerUserID string, req booking.CreateBookingRequest) (*booking.BookingAttempt, error)
	Cancel(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req booking.CancelRequest) (*booking.Appointment, *booking.BookingAttempt, error)
	RescheduleCandidates(ctx context.Context, salonID string, ownerUserID string, req booking.RescheduleLookupRequest) ([]booking.AppointmentActionRef, error)
	Reschedule(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req booking.RescheduleRequest) (*booking.Appointment, *booking.BookingAttempt, error)
}

// NeutralSchedulingTool is the authority-aware production scheduling
// boundary. BookingTool remains temporarily available for provider-history
// reads and legacy tests while scheduling writes use this contract whenever it
// is implemented by the configured tool.
type NeutralSchedulingTool interface {
	CheckAvailability(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest) (*scheduling.AvailabilityResult, error)
	CheckConversationAvailability(ctx context.Context, salonID string, ownerUserID string, reviewedMode scheduling.BookingMode, req booking.AvailabilityRequest) (*scheduling.AvailabilityResult, error)
	ExecuteAction(ctx context.Context, salonID string, ownerUserID string, req scheduling.ActionRequest) (*scheduling.ActionResult, error)
	ExecuteConversationAction(ctx context.Context, salonID string, ownerUserID string, reviewed scheduling.ConversationPolicyFence, req scheduling.ActionRequest) (*scheduling.ActionResult, error)
	CurrentSchedulingAuthority(ctx context.Context, salonID string, ownerUserID string) (string, error)
}

type ReplyGenerator interface {
	GenerateReply(ctx context.Context, req ReplyGenerationRequest) (ReplyGenerationResult, error)
}

type ConsultationQuestionGenerator interface {
	GenerateConsultationQuestion(ctx context.Context, req ConsultationQuestionRequest) (ReplyGenerationResult, error)
}

type TurnInterpreter interface {
	InterpretTurn(ctx context.Context, req TurnInterpretationRequest) (TurnUnderstanding, error)
}

const (
	TurnSemanticContractFull     = "full_turn"
	TurnSemanticContractGuidance = "guidance_turn"
)

type TurnInterpretationRequest struct {
	SalonID                     string
	SessionID                   string
	Channel                     string
	CustomerMessage             string
	ExpectedInput               string
	SemanticContract            string
	RecognizableGuidanceActions []string
	SelectedServices            []ConversationServiceRef
	CatalogServices             []ConversationServiceRef
	CatalogServiceAliases       []ConversationServiceAliasRef
	CatalogCategories           []ConversationCategoryRef
	SelectedStaff               []ConversationStaffRef
	CatalogStaff                []ConversationStaffRef
	Pending                     *PendingConversationAct
	CurrentBookingStage         string
	BookingAction               string
	CurrentDraft                ConversationDraftRef
	Consultation                *ConsultationState
}

type ConversationServiceRef struct {
	ServiceID           string                              `json:"service_id"`
	ServiceName         string                              `json:"service_name"`
	CategoryID          string                              `json:"category_id,omitempty"`
	CategoryName        string                              `json:"category_name,omitempty"`
	ConsultationProfile *ConversationConsultationProfileRef `json:"consultation_profile,omitempty"`
}

// ConversationServiceAliasRef exposes only the salon-scoped alias and its
// validated active-catalog target. The model cannot use an alias to invent a
// service ID that is not already present in CatalogServices.
type ConversationServiceAliasRef struct {
	ServiceID string `json:"service_id"`
	Alias     string `json:"alias"`
}

// ConversationCategoryRef is a non-bookable grouping contract. ServiceIDs are
// the only concrete choices that may be offered after a category-level match.
type ConversationCategoryRef struct {
	CategoryID   string   `json:"category_id"`
	CategoryName string   `json:"category_name"`
	Aliases      []string `json:"aliases"`
	ServiceIDs   []string `json:"service_ids"`
}

type ConversationConsultationProfileRef struct {
	Status                   string   `json:"status"`
	RecommendedOutcomes      []string `json:"recommended_outcomes"`
	CompatibleCurrentSystems []string `json:"compatible_current_systems"`
	LengthCapabilities       []string `json:"length_capabilities"`
	PriorityTags             []string `json:"priority_tags"`
	FinishOptions            []string `json:"finish_options"`
	MaintenanceNote          string   `json:"maintenance_note,omitempty"`
	OwnerApprovedSummary     string   `json:"owner_approved_summary,omitempty"`
	Revision                 int      `json:"revision"`
}

type ConversationStaffRef struct {
	StaffID   string `json:"staff_id"`
	StaffName string `json:"staff_name"`
}

type ConversationDraftRef struct {
	ServiceIDs        []string                    `json:"service_ids"`
	StaffID           string                      `json:"staff_id,omitempty"`
	RequestedDate     string                      `json:"requested_date,omitempty"`
	RequestedStartISO string                      `json:"requested_start_iso,omitempty"`
	PartySize         int                         `json:"party_size,omitempty"`
	PartyGroups       []ConversationPartyGroupRef `json:"party_groups,omitempty"`
	HasCustomerName   bool                        `json:"has_customer_name"`
	HasCustomerPhone  bool                        `json:"has_customer_phone"`
	DraftRevision     int                         `json:"draft_revision"`
}

type ConversationPartyGroupRef struct {
	GuestRef   string   `json:"guest_ref"`
	Count      int      `json:"count"`
	ServiceIDs []string `json:"service_ids"`
}

type ConversationAct struct {
	Kind               string   `json:"kind"`
	Entity             string   `json:"entity"`
	SourceServiceIDs   []string `json:"source_service_ids"`
	TargetServiceIDs   []string `json:"target_service_ids"`
	SourceCategoryID   string   `json:"source_category_id"`
	SourceCategoryName string   `json:"source_category_name"`
	TargetCategoryID   string   `json:"target_category_id"`
	TargetCategoryName string   `json:"target_category_name"`
	Scope              string   `json:"scope"`
	GuestScope         string   `json:"guest_scope"`
	GuestRef           string   `json:"guest_ref"`
	Subject            string   `json:"subject"`
	Value              string   `json:"value"`
	Count              int      `json:"count"`
	Confidence         float64  `json:"confidence"`
	Reason             string   `json:"reason"`
	Source             string   `json:"source"`
}

type ConversationQuestion struct {
	Subject        string          `json:"subject"`
	Mode           string          `json:"mode"`
	ServiceIDs     []string        `json:"service_ids"`
	StaffIDs       []string        `json:"staff_ids"`
	TimePreference *TimePreference `json:"time_preference,omitempty"`
	Confidence     float64         `json:"confidence"`
	Reason         string          `json:"reason"`
}

type TurnUnderstanding struct {
	Goal                    string                     `json:"goal"`
	GuidanceAction          string                     `json:"guidance_action,omitempty"`
	GuidanceCatalogMode     string                     `json:"guidance_catalog_mode,omitempty"`
	GuidanceQuestionSubject string                     `json:"guidance_question_subject,omitempty"`
	GuidancePartySize       int                        `json:"guidance_party_size,omitempty"`
	Acts                    []ConversationAct          `json:"acts"`
	Questions               []ConversationQuestion     `json:"questions"`
	Confidence              float64                    `json:"confidence"`
	Reason                  string                     `json:"reason"`
	Consultation            ConsultationNeedProfile    `json:"consultation"`
	ConsultationMutations   []ConsultationNeedMutation `json:"consultation_mutations,omitempty"`
	Safety                  SafetyAssessment           `json:"safety"`
	Source                  string                     `json:"source"`
	ModelInvoked            bool                       `json:"-"`
	CatalogFallback         bool                       `json:"-"`
	InterpreterOutcome      string                     `json:"-"`
	InterpreterDiagnostics  map[string]string          `json:"-"`
}

type PendingConversationAct struct {
	Kind               string   `json:"kind"`
	Entity             string   `json:"entity,omitempty"`
	SourceServiceIDs   []string `json:"source_service_ids,omitempty"`
	TargetServiceIDs   []string `json:"target_service_ids,omitempty"`
	SourceCategoryID   string   `json:"source_category_id,omitempty"`
	SourceCategoryName string   `json:"source_category_name,omitempty"`
	TargetCategoryID   string   `json:"target_category_id,omitempty"`
	TargetCategoryName string   `json:"target_category_name,omitempty"`
	Scope              string   `json:"scope,omitempty"`
	GuestScope         string   `json:"guest_scope,omitempty"`
	GuestRef           string   `json:"guest_ref,omitempty"`
	Subject            string   `json:"subject,omitempty"`
	Value              string   `json:"value,omitempty"`
	ProposedDate       string   `json:"proposed_date,omitempty"`
	ProposedStartISO   string   `json:"proposed_start_iso,omitempty"`
	PromptKey          string   `json:"prompt_key"`
}

type DraftMutation struct {
	Kind              string                          `json:"kind"`
	BeforeServiceID   string                          `json:"before_service_id,omitempty"`
	BeforeServiceName string                          `json:"before_service_name,omitempty"`
	BeforeServiceIDs  []string                        `json:"before_service_ids"`
	BeforeSegments    []booking.BookingSegmentRequest `json:"before_segments"`
	AfterServiceIDs   []string                        `json:"after_service_ids"`
	AfterSegments     []booking.BookingSegmentRequest `json:"after_segments"`
}

type TimePreference struct {
	Direction string `json:"direction"`
	Minutes   int    `json:"minutes"`
}

type ConsultationNeedProfile struct {
	CurrentSystem        string   `json:"current_system,omitempty"`
	DesiredOutcome       string   `json:"desired_outcome,omitempty"`
	LengthChange         string   `json:"length_change,omitempty"`
	Priorities           []string `json:"priorities,omitempty"`
	DesiredFinishes      []string `json:"desired_finishes,omitempty"`
	ComparedServiceIDs   []string `json:"compared_service_ids,omitempty"`
	BookingRequested     bool     `json:"booking_requested,omitempty"`
	ConversationComplete bool     `json:"conversation_complete,omitempty"`
	Confidence           float64  `json:"confidence,omitempty"`
	Reason               string   `json:"reason,omitempty"`
}

// ConsultationNeedMutation carries field-level edit intent from the semantic
// interpreter. The reducer remains the only owner allowed to mutate persisted
// consultation state.
type ConsultationNeedMutation struct {
	Field      string   `json:"field"`
	Operation  string   `json:"operation"`
	Values     []string `json:"values"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason"`
}

// SafetyAssessment is extraction-only evidence. A validated concern is handled
// before any booking, availability, or consultation mutation is applied.
type SafetyAssessment struct {
	Concern    bool    `json:"concern"`
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

type ConsultationState struct {
	Status                     string                  `json:"status"`
	ResumePhase                string                  `json:"resume_phase"`
	Needs                      ConsultationNeedProfile `json:"needs"`
	CandidateServiceIDs        []string                `json:"candidate_service_ids,omitempty"`
	RecommendedServiceIDs      []string                `json:"recommended_service_ids,omitempty"`
	SelectedServiceID          string                  `json:"selected_service_id,omitempty"`
	LastAskedField             string                  `json:"last_asked_field,omitempty"`
	LastQuestionOptions        []string                `json:"last_question_options,omitempty"`
	ProfileRevisions           map[string]int          `json:"profile_revisions,omitempty"`
	RecommendationReasons      map[string][]string     `json:"recommendation_reasons,omitempty"`
	NoProgressCount            int                     `json:"no_progress_count"`
	ProviderFailureCount       int                     `json:"provider_failure_count"`
	ProgressFingerprint        string                  `json:"progress_fingerprint,omitempty"`
	LastInterpreterOutcome     string                  `json:"last_interpreter_outcome,omitempty"`
	LastInterpreterDiagnostics map[string]string       `json:"last_interpreter_diagnostics,omitempty"`
	ExitReason                 string                  `json:"exit_reason,omitempty"`
}

// GuidanceRecoveryState owns the bounded recovery conversation used while the
// caller's goal or requested service is unresolved. OfferedActions contains
// state tokens derived from the current catalog and runtime capabilities; it
// is presentation context, not a phrase-matching vocabulary.
type GuidanceRecoveryState struct {
	Stage                string   `json:"stage"`
	OfferedActions       []string `json:"offered_actions,omitempty"`
	NoProgressCount      int      `json:"no_progress_count"`
	ProviderFailureCount int      `json:"provider_failure_count"`
	ProgressFingerprint  string   `json:"progress_fingerprint,omitempty"`
	LastProviderOutcome  string   `json:"last_provider_outcome,omitempty"`
}

type DialogState struct {
	Version                     int                      `json:"version"`
	Phase                       string                   `json:"phase"`
	Pending                     *PendingConversationAct  `json:"pending,omitempty"`
	LastMutation                *DraftMutation           `json:"last_mutation,omitempty"`
	MutationHistory             []DraftMutation          `json:"mutation_history,omitempty"`
	ReviewRequired              bool                     `json:"review_required"`
	ReviewAccepted              bool                     `json:"review_accepted"`
	NoProgressCount             int                      `json:"no_progress_count"`
	ProviderFailureCount        int                      `json:"provider_failure_count"`
	ProgressFingerprint         string                   `json:"progress_fingerprint,omitempty"`
	LastPromptKey               string                   `json:"last_prompt_key,omitempty"`
	LastActKind                 string                   `json:"last_act_kind,omitempty"`
	DraftRevision               int                      `json:"draft_revision"`
	ReviewedRevision            int                      `json:"reviewed_revision"`
	AuthorizedRevision          int                      `json:"authorized_revision"`
	LastMutationRevision        int                      `json:"last_mutation_revision,omitempty"`
	TimePreference              *TimePreference          `json:"time_preference,omitempty"`
	Consultation                *ConsultationState       `json:"consultation,omitempty"`
	Guidance                    *GuidanceRecoveryState   `json:"guidance,omitempty"`
	ManualTarget                *ManualAppointmentTarget `json:"manual_appointment_target,omitempty"`
	SchedulingRequestOnly       bool                     `json:"scheduling_request_only,omitempty"`
	ReviewedBookingMode         scheduling.BookingMode   `json:"reviewed_booking_mode,omitempty"`
	SelectedSchedulingAuthority string                   `json:"selected_scheduling_authority,omitempty"`
	CustomerSMSConsent          *CustomerSMSConsentState `json:"customer_sms_consent,omitempty"`
}

type CustomerSMSConsentState struct {
	Status               string `json:"status"`
	DestinationHash      string `json:"destination_hash"`
	DestinationMasked    string `json:"destination_masked"`
	DraftRevision        int    `json:"draft_revision"`
	ConsentVersion       int    `json:"consent_version,omitempty"`
	RequestEventKey      string `json:"request_event_key,omitempty"`
	LastResponseEventKey string `json:"last_response_event_key,omitempty"`
}

// ManualAppointmentTarget is caller-supplied identification context used only
// when owner_manual has no durable appointment target. It cannot override a
// persisted appointment ID or its originating scheduling authority.
type ManualAppointmentTarget struct {
	Description string `json:"description"`
}

type Store interface {
	GetRuntimeConfig(ctx context.Context, salonID string, ownerUserID string) (*RuntimeConfig, error)
	GetAnswerContextFence(ctx context.Context, salonID string) (AnswerContextFence, error)
	CreateSession(ctx context.Context, record NewSessionRecord) (*Session, error)
	GetSessionForOwner(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error)
	GetSessionByTurnEventKey(ctx context.Context, salonID string, ownerUserID string, sessionID string, eventKey string) (*Session, bool, error)
	ListSessions(ctx context.Context, salonID string, ownerUserID string, lifecycleStatus string, limit int, offset int) ([]Session, error)
	ListWebhookEvents(ctx context.Context, salonID string, ownerUserID string, sessionID string, limit int, offset int) ([]WebhookEventLog, error)
	ArchiveSession(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error)
	RedactSession(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error)
	ListGuidanceServices(ctx context.Context, salonID string) ([]ServiceOption, error)
	ListBookableServices(ctx context.Context, salonID string) ([]ServiceOption, error)
	ListBookableStaff(ctx context.Context, salonID string) ([]StaffOption, error)
	ListActiveStaff(ctx context.Context, salonID string) ([]StaffOption, error)
	ListStaffAssignmentStats(ctx context.Context, salonID string, staffIDs []string, from time.Time, to time.Time) (map[string]StaffAssignmentStat, error)
	ListActiveServiceAliases(ctx context.Context, salonID string) ([]ServiceAlias, error)
	ListActiveServiceCategoryAliases(ctx context.Context, salonID string) ([]ServiceCategoryAlias, error)
	ListActiveKnowledge(ctx context.Context, salonID string) ([]KnowledgeSnippet, error)
	ListExternalProviderBusinessHourPeriods(ctx context.Context, salonID string) ([]BusinessHourPeriod, error)
	ListPartyBookingRequests(ctx context.Context, salonID string, ownerUserID string, status string, limit int, offset int) ([]PartyBookingRequest, error)
	UpdatePartyBookingRequestStatus(ctx context.Context, salonID string, ownerUserID string, requestID string, status string) (*PartyBookingRequest, error)
	SaveTurn(ctx context.Context, record TurnRecord) (*Session, error)
}

// AnswerContextFence identifies the persisted common-resource and authority-
// scoped versions that own structured conversation context. Common catalog,
// alias, category, consultation, staff, and knowledge versions apply wherever
// those projections are consumed. Owner Manual additionally uses the owner-
// managed local-hours version; External Provider uses the current provider
// snapshot; ManleAI Calendar uses its root config and activation versions.
// It is read on every turn so replicas reject stale cached context after any
// relevant source-of-truth change. Runtime readiness is deliberately not part
// of this comparable value: it is authoritatively evaluated on cache misses and
// remains reusable only while this complete persisted fence is unchanged.
type AnswerContextFence struct {
	SchedulingAuthority         string
	SchedulingAuthorityVersion  int64
	ServiceCatalogVersion       int64
	ServiceAliasesVersion       int64
	ServiceCategoriesVersion    int64
	ConsultationProfilesVersion int64
	StaffCatalogVersion         int64
	KnowledgeBaseVersion        int64
	LocalBusinessHoursVersion   int64
	CalendarConfigVersion       int64
	CalendarActivatedVersion    int64
	ActiveProvider              string
	ConnectionStatus            string
	LocationID                  string
	SnapshotGeneration          int64
	LastSyncAtRFC3339           string
}

// manleAICalendarAnswerContextEvidence is an ephemeral projection of the exact
// aggregate version on which EvaluateReadiness ran. It is not persisted and is
// accepted only when its versions match the database-owned answer-context
// fence read around the fresh context load.
type manleAICalendarAnswerContextEvidence struct {
	SchedulingAuthority        string
	SchedulingAuthorityVersion int64
	CalendarConfigVersion      int64
	CalendarActivatedVersion   int64
	Ready                      bool
}

type StartSessionRequest struct {
	Channel       string `json:"channel"`
	CustomerName  string `json:"customer_name"`
	CustomerPhone string `json:"customer_phone"`
	CustomerEmail string `json:"customer_email"`
}

type ListSessionsResponse struct {
	Sessions []Session `json:"sessions"`
	Limit    int       `json:"limit"`
	Offset   int       `json:"offset"`
	HasMore  bool      `json:"has_more"`
}

type ListPartyBookingRequestsResponse struct {
	PartyBookingRequests []PartyBookingRequest `json:"party_booking_requests"`
	Limit                int                   `json:"limit"`
	Offset               int                   `json:"offset"`
	HasMore              bool                  `json:"has_more"`
}

type ListWebhookEventsResponse struct {
	Events  []WebhookEventLog `json:"events"`
	Limit   int               `json:"limit"`
	Offset  int               `json:"offset"`
	HasMore bool              `json:"has_more"`
}

type StartPhoneCallRequest struct {
	Provider            string
	ProviderCallID      string
	FromPhone           string
	ToPhone             string
	VoiceRouteID        string
	VoiceRouteUpdatedAt time.Time
}

type MessageRequest struct {
	Message        string             `json:"message"`
	EventKey       string             `json:"event_key,omitempty"`
	TimingRecorder TurnTimingRecorder `json:"-"`
}

type TurnTimingRecorder func(TurnTiming)

type TurnTiming struct {
	Stage      string
	Duration   time.Duration
	Result     string
	Attributes map[string]string
}

type VoiceInputHandoffRequest struct {
	EventKey string
}

type ReplyGenerationRequest struct {
	SalonID              string
	SessionID            string
	Channel              string
	Intent               string
	Outcome              string
	CustomerMessage      string
	SafeReply            string
	SalonName            string
	AITone               string
	BookingConfirmed     bool
	FallbackOrHandoff    bool
	MissingBookingField  string
	KnownBookingFields   []string
	NextRequiredField    string
	SelectedServiceNames []string
	Summary              string
	KnowledgeContext     string
	ReplyPolicy          string
	ConsultationQuestion *ConsultationQuestionSpec
}

type ConsultationQuestionRequest struct {
	SalonID   string
	SessionID string
	Channel   string
	AITone    string
	Question  ConsultationQuestionSpec
}

type ConsultationQuestionSpec struct {
	Field               string         `json:"field"`
	Options             []string       `json:"options"`
	CandidateServiceIDs []string       `json:"candidate_service_ids"`
	ProfileRevisions    map[string]int `json:"profile_revisions"`
}

type ReplyGenerationResult struct {
	Message    string
	Confidence float64
	Handoff    bool
	Reason     string
}

type TranscriptionContext struct {
	Prompt string
}

type RuntimeConfig struct {
	SalonName                string
	Timezone                 string
	AIEnabled                bool
	HandoffPhone             string
	HandoffEnabled           bool
	ConsultationEnabled      bool
	AIGreeting               string
	AITone                   string
	RecordingEnabled         bool
	RecordingConsentMessage  string
	BookingMode              scheduling.BookingMode
	SchedulingAuthority      string
	CustomerSMSEnabled       bool
	CustomerSMSQuietStart    string
	CustomerSMSQuietEnd      string
	CustomerSMSPolicyVersion int64
}

type ServiceOption struct {
	ID                  string                      `json:"id"`
	Name                string                      `json:"name"`
	Description         string                      `json:"description,omitempty"`
	AIDescription       string                      `json:"ai_description,omitempty"`
	DurationMinutes     int                         `json:"duration_minutes"`
	PriceFrom           float64                     `json:"price_from,omitempty"`
	PriceDisplay        string                      `json:"price_display,omitempty"`
	CategoryID          string                      `json:"category_id,omitempty"`
	CategoryName        string                      `json:"category_name,omitempty"`
	CategorySlug        string                      `json:"category_slug,omitempty"`
	ConsultationProfile *ServiceConsultationProfile `json:"consultation_profile,omitempty"`
	// BookingReady is runtime-only evidence that this service also passed the
	// current provider snapshot fence. Guidance may use a canonical linked
	// service while this is false; availability and booking may not.
	BookingReady bool `json:"-"`
}

type ServiceConsultationProfile struct {
	Status                   string   `json:"status"`
	RecommendedOutcomes      []string `json:"recommended_outcomes"`
	CompatibleCurrentSystems []string `json:"compatible_current_systems"`
	LengthCapabilities       []string `json:"length_capabilities"`
	PriorityTags             []string `json:"priority_tags"`
	FinishOptions            []string `json:"finish_options"`
	MaintenanceNote          string   `json:"maintenance_note,omitempty"`
	OwnerApprovedSummary     string   `json:"owner_approved_summary,omitempty"`
	Revision                 int      `json:"revision"`
}

type ServiceAlias struct {
	ID              string  `json:"id"`
	ServiceID       string  `json:"service_id"`
	ServiceName     string  `json:"service_name"`
	Alias           string  `json:"alias"`
	NormalizedAlias string  `json:"normalized_alias"`
	Source          string  `json:"source"`
	Confidence      float64 `json:"confidence"`
}

type ServiceCategoryAlias struct {
	ID              string  `json:"id"`
	CategoryID      string  `json:"category_id"`
	CategoryName    string  `json:"category_name"`
	Alias           string  `json:"alias"`
	NormalizedAlias string  `json:"normalized_alias"`
	Source          string  `json:"source"`
	Confidence      float64 `json:"confidence"`
}

type StaffOption struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AIBookable bool   `json:"ai_bookable"`
}

type StaffAssignmentStat struct {
	StaffID        string     `json:"staff_id"`
	AssignedCount  int        `json:"assigned_count"`
	LastAssignedAt *time.Time `json:"last_assigned_at,omitempty"`
}

type KnowledgeSnippet struct {
	ID       string
	Title    string
	Category string
	Body     string
}

type BusinessHourPeriod struct {
	ID             string
	DayOfWeek      int
	StartLocalTime string
	EndLocalTime   string
	Source         string
	Provider       string
}

type Session struct {
	ID                       string                          `json:"id"`
	SalonID                  string                          `json:"salon_id"`
	Channel                  string                          `json:"channel"`
	Provider                 string                          `json:"provider,omitempty"`
	ProviderCallID           string                          `json:"provider_call_id,omitempty"`
	InboundPhone             string                          `json:"inbound_phone,omitempty"`
	OutboundPhone            string                          `json:"outbound_phone,omitempty"`
	Status                   string                          `json:"status"`
	Intent                   string                          `json:"intent"`
	Outcome                  string                          `json:"outcome"`
	StateRevision            int64                           `json:"state_revision"`
	BookingAction            string                          `json:"booking_action"`
	TargetAppointmentID      string                          `json:"target_appointment_id,omitempty"`
	RescheduleCandidates     []RescheduleCandidate           `json:"reschedule_candidates,omitempty"`
	CustomerName             string                          `json:"customer_name,omitempty"`
	CustomerPhone            string                          `json:"customer_phone,omitempty"`
	CustomerEmail            string                          `json:"customer_email,omitempty"`
	ServiceID                string                          `json:"service_id,omitempty"`
	ServiceName              string                          `json:"service_name,omitempty"`
	StaffID                  string                          `json:"staff_id,omitempty"`
	StaffName                string                          `json:"staff_name,omitempty"`
	StaffSelectionMode       string                          `json:"staff_selection_mode,omitempty"`
	RequestedDate            string                          `json:"requested_date,omitempty"`
	RequestedStartTime       *time.Time                      `json:"requested_start_time,omitempty"`
	AvailabilityQuoteID      string                          `json:"availability_quote_id,omitempty"`
	SlotFingerprint          string                          `json:"availability_slot_fingerprint,omitempty"`
	OfferedSlots             []OfferedSlot                   `json:"offered_slots,omitempty"`
	BookingSegments          []booking.BookingSegmentRequest `json:"booking_segments,omitempty"`
	PartyPlan                *PartyPlan                      `json:"party_plan,omitempty"`
	DialogState              DialogState                     `json:"dialog_state"`
	BookingAttemptID         string                          `json:"booking_attempt_id,omitempty"`
	AppointmentID            string                          `json:"appointment_id,omitempty"`
	SchedulingRequestID      string                          `json:"scheduling_request_id,omitempty"`
	SchedulingResultEvidence *SchedulingResultEvidence       `json:"scheduling_result_evidence,omitempty"`
	Summary                  string                          `json:"summary,omitempty"`
	LifecycleStatus          string                          `json:"lifecycle_status"`
	ArchivedAt               *time.Time                      `json:"archived_at,omitempty"`
	RedactedAt               *time.Time                      `json:"redacted_at,omitempty"`
	RetentionExpiresAt       time.Time                       `json:"retention_expires_at"`
	StartedAt                time.Time                       `json:"started_at"`
	EndedAt                  *time.Time                      `json:"ended_at,omitempty"`
	CreatedAt                time.Time                       `json:"created_at"`
	UpdatedAt                time.Time                       `json:"updated_at"`
	Transcript               []TranscriptMessage             `json:"transcript,omitempty"`
	Handoff                  *HandoffRequest                 `json:"handoff,omitempty"`
	PartyRequest             *PartyBookingRequest            `json:"party_request,omitempty"`
	// ReplayAIMessage is an internal response override for a deduplicated
	// provider event. The persisted session and transcript remain at their
	// newest state while the voice layer replays the exact historical reply.
	ReplayEventKey  string `json:"-"`
	ReplayAIMessage string `json:"-"`
}

const (
	SchedulingEvidenceKindIncomplete         = "incomplete"
	SchedulingEvidenceKindPendingOwnerReview = "pending_owner_review"
	SchedulingEvidenceKindCompletedOperation = "completed_operation"

	SchedulingEvidenceStatusIncomplete = "incomplete"
	SchedulingEvidenceStatusMixed      = "mixed"
)

// SchedulingResultEvidence is an owner-scoped, backend-validated projection of
// authority-native scheduling evidence. Call/session clients must not infer a
// completed operation from the session's local appointment or attempt IDs.
// ResultStatus describes the exact historical operation. CurrentStatus and
// IsCurrent describe the present durable appointment state after any later
// lifecycle mutation.
type SchedulingResultEvidence struct {
	Complete                           bool   `json:"complete"`
	Kind                               string `json:"kind"`
	SchedulingAuthority                string `json:"scheduling_authority,omitempty"`
	TargetSchedulingAuthority          string `json:"target_scheduling_authority,omitempty"`
	OperationType                      string `json:"operation_type,omitempty"`
	ResultStatus                       string `json:"result_status"`
	CurrentStatus                      string `json:"current_status"`
	IsCurrent                          bool   `json:"is_current"`
	AppointmentID                      string `json:"appointment_id,omitempty"`
	BookingAttemptID                   string `json:"booking_attempt_id,omitempty"`
	SchedulingRequestID                string `json:"scheduling_request_id,omitempty"`
	AuthorityAppointmentVersion        int    `json:"authority_appointment_version,omitempty"`
	CurrentAuthorityAppointmentVersion int    `json:"current_authority_appointment_version,omitempty"`
	RootCount                          int    `json:"root_count"`
	ResultChildCount                   int    `json:"result_child_count"`
	CurrentActiveChildCount            int    `json:"current_active_child_count"`
	IncompleteReason                   string `json:"incomplete_reason,omitempty"`
}

type OfferedSlot struct {
	AvailabilityQuoteID string               `json:"availability_quote_id,omitempty"`
	SlotFingerprint     string               `json:"availability_slot_fingerprint,omitempty"`
	StartTime           time.Time            `json:"start_time"`
	EndTime             time.Time            `json:"end_time"`
	StaffID             string               `json:"staff_id"`
	StaffName           string               `json:"staff_name"`
	StaffSelectionMode  string               `json:"staff_selection_mode,omitempty"`
	Segments            []OfferedSlotSegment `json:"segments,omitempty"`
}

type OfferedSlotSegment struct {
	ServiceID          string `json:"service_id"`
	ServiceName        string `json:"service_name,omitempty"`
	StaffID            string `json:"staff_id,omitempty"`
	StaffName          string `json:"staff_name,omitempty"`
	StaffSelectionMode string `json:"staff_selection_mode"`
	DurationMinutes    int    `json:"duration_minutes,omitempty"`
}

type RescheduleCandidate struct {
	AppointmentID               string                          `json:"appointment_id"`
	SchedulingAuthority         string                          `json:"scheduling_authority,omitempty"`
	AuthorityAppointmentVersion int                             `json:"authority_appointment_version,omitempty"`
	PartySize                   int                             `json:"party_size,omitempty"`
	Status                      string                          `json:"status,omitempty"`
	ActiveChildCount            int                             `json:"active_child_count,omitempty"`
	ServiceLabel                string                          `json:"service_label"`
	StaffLabel                  string                          `json:"staff_label"`
	ServiceID                   string                          `json:"service_id,omitempty"`
	StaffID                     string                          `json:"staff_id,omitempty"`
	StaffSelectionMode          string                          `json:"staff_selection_mode,omitempty"`
	Segments                    []booking.BookingSegmentRequest `json:"segments,omitempty"`
	StartTime                   time.Time                       `json:"start_time"`
	EndTime                     time.Time                       `json:"end_time"`
}

type TranscriptMessage struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	SalonID   string         `json:"salon_id"`
	Speaker   string         `json:"speaker"`
	Body      string         `json:"body"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Sequence  int            `json:"sequence"`
	CreatedAt time.Time      `json:"created_at"`
}

type HandoffRequest struct {
	ID            string     `json:"id"`
	SalonID       string     `json:"salon_id"`
	CallSessionID string     `json:"call_session_id"`
	Status        string     `json:"status"`
	Reason        string     `json:"reason"`
	CustomerName  string     `json:"customer_name,omitempty"`
	CustomerPhone string     `json:"customer_phone,omitempty"`
	Summary       string     `json:"summary"`
	CreatedAt     time.Time  `json:"created_at"`
	ResolvedAt    *time.Time `json:"resolved_at,omitempty"`
}

type PartyBookingRequest struct {
	ID                   string              `json:"id"`
	SalonID              string              `json:"salon_id"`
	CallSessionID        string              `json:"call_session_id"`
	EventKey             string              `json:"event_key,omitempty"`
	Status               string              `json:"status"`
	PartySize            int                 `json:"party_size,omitempty"`
	RepresentativeName   string              `json:"representative_name,omitempty"`
	RepresentativePhone  string              `json:"representative_phone,omitempty"`
	RequestedDate        string              `json:"requested_date,omitempty"`
	RequestedTimeWindow  string              `json:"requested_time_window,omitempty"`
	GuestServiceRequests []PartyGuestService `json:"guest_service_requests,omitempty"`
	FlexibilityNotes     string              `json:"flexibility_notes,omitempty"`
	Summary              string              `json:"summary"`
	CreatedAt            time.Time           `json:"created_at"`
	UpdatedAt            time.Time           `json:"updated_at"`
	ResolvedAt           *time.Time          `json:"resolved_at,omitempty"`
	ResolvedBy           string              `json:"resolved_by,omitempty"`
}

type PartyGuestService struct {
	GuestReference string `json:"guest_reference,omitempty"`
	ServiceID      string `json:"service_id,omitempty"`
	ServiceName    string `json:"service_name,omitempty"`
	Quantity       int    `json:"quantity,omitempty"`
	SortOrder      int    `json:"sort_order,omitempty"`
	Notes          string `json:"notes,omitempty"`
}

type PartyPlan struct {
	PartySize              int                 `json:"party_size,omitempty"`
	Groups                 []PartyPlanGroup    `json:"groups,omitempty"`
	ParseSource            string              `json:"parse_source,omitempty"`
	ParseConfidence        float64             `json:"parse_confidence,omitempty"`
	ClarifyReason          string              `json:"clarify_reason,omitempty"`
	Evidence               []PartyPlanEvidence `json:"evidence,omitempty"`
	SplitOptions           []PartySplitOption  `json:"split_options,omitempty"`
	SelectedSplitOptionID  string              `json:"selected_split_option_id,omitempty"`
	SplitBookingAttemptIDs []string            `json:"split_booking_attempt_ids,omitempty"`
	SplitAppointmentIDs    []string            `json:"split_appointment_ids,omitempty"`
}

type PartyPlanGroup struct {
	Label               string   `json:"label,omitempty"`
	Count               int      `json:"count,omitempty"`
	CandidateServiceIDs []string `json:"candidate_service_ids,omitempty"`
	ResolvedServiceIDs  []string `json:"resolved_service_ids,omitempty"`
	Source              string   `json:"source,omitempty"`
}

type PartyPlanEvidence struct {
	Kind       string  `json:"kind,omitempty"`
	Source     string  `json:"source,omitempty"`
	Text       string  `json:"text,omitempty"`
	Value      int     `json:"value,omitempty"`
	Start      int     `json:"start,omitempty"`
	End        int     `json:"end,omitempty"`
	ServiceID  string  `json:"service_id,omitempty"`
	CategoryID string  `json:"category_id,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

type PartySplitOption struct {
	ID                   string            `json:"id"`
	Blocks               []PartySplitBlock `json:"blocks,omitempty"`
	DatePolicy           string            `json:"date_policy,omitempty"`
	RequiresDateConsent  bool              `json:"requires_date_consent,omitempty"`
	DateConsentConfirmed bool              `json:"date_consent_confirmed,omitempty"`
	SpanMinutes          int               `json:"span_minutes,omitempty"`
	FinishSpreadMinutes  int               `json:"finish_spread_minutes,omitempty"`
}

type PartySplitBlock struct {
	StartTime time.Time                       `json:"start_time"`
	EndTime   time.Time                       `json:"end_time"`
	Segments  []booking.BookingSegmentRequest `json:"segments,omitempty"`
	QuoteRefs []PartySplitQuoteRef            `json:"quote_refs,omitempty"`
}

type PartySplitQuoteRef struct {
	ServiceID           string    `json:"service_id"`
	GuestReference      string    `json:"guest_reference,omitempty"`
	Quantity            int       `json:"quantity,omitempty"`
	RequestedStartTime  time.Time `json:"requested_start_time,omitempty"`
	RequestedEndTime    time.Time `json:"requested_end_time,omitempty"`
	AvailabilityQuoteID string    `json:"availability_quote_id"`
	SlotFingerprint     string    `json:"availability_slot_fingerprint"`
}

type WebhookEventLog struct {
	ID             string            `json:"id"`
	Provider       string            `json:"provider"`
	ProviderCallID string            `json:"provider_call_id,omitempty"`
	EventType      string            `json:"event_type"`
	Stage          string            `json:"stage,omitempty"`
	StreamSID      string            `json:"stream_sid,omitempty"`
	StreamEvent    string            `json:"stream_event,omitempty"`
	StreamError    string            `json:"stream_error,omitempty"`
	Error          string            `json:"error,omitempty"`
	Diagnostics    map[string]string `json:"diagnostics,omitempty"`
	Redacted       bool              `json:"redacted,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
}

type NewSessionRecord struct {
	SalonID             string
	OwnerUserID         string
	Channel             string
	Provider            string
	ProviderCallID      string
	InboundPhone        string
	OutboundPhone       string
	CustomerName        string
	CustomerPhone       string
	CustomerEmail       string
	InitialReply        string
	VoiceRouteID        string
	VoiceRouteUpdatedAt time.Time
}

type TurnRecord struct {
	SalonID               string
	OwnerUserID           string
	Session               Session
	ExpectedStateRevision int64
	CustomerMessage       string
	ToolMessage           string
	AIMessage             string
	EventKey              string
	CustomerMetadata      map[string]any
	ToolMetadata          map[string]any
	AIMetadata            map[string]any
	Update                SessionUpdate
	Handoff               *HandoffRecord
	PartyRequest          *PartyRequestRecord
	ReplyPolicy           string
}

type SessionUpdate struct {
	Status               string
	Intent               string
	Outcome              string
	BookingAction        string
	TargetAppointmentID  string
	RescheduleCandidates []RescheduleCandidate
	CustomerName         string
	CustomerPhone        string
	CustomerEmail        string
	ServiceID            string
	StaffID              string
	StaffSelectionMode   string
	RequestedDate        string
	RequestedStartTime   *time.Time
	AvailabilityQuoteID  string
	SlotFingerprint      string
	OfferedSlots         []OfferedSlot
	BookingSegments      []booking.BookingSegmentRequest
	PartyPlan            *PartyPlan
	DialogState          DialogState
	BookingAttemptID     string
	AppointmentID        string
	SchedulingRequestID  string
	Summary              string
	EndSession           bool
}

type HandoffRecord struct {
	Reason        string
	CustomerName  string
	CustomerPhone string
	Summary       string
}

type PartyRequestRecord struct {
	EventKey             string
	PartySize            int
	RepresentativeName   string
	RepresentativePhone  string
	RequestedDate        string
	RequestedTimeWindow  string
	GuestServiceRequests []PartyGuestService
	FlexibilityNotes     string
	Summary              string
}
