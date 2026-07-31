package tenant_registration

import "time"

type Status string

const (
	StatusNew             Status = "new"
	StatusInReview        Status = "in_review"
	StatusQualified       Status = "qualified"
	StatusSetupInProgress Status = "setup_in_progress"
	StatusConverted       Status = "converted"
	StatusDeclined        Status = "declined"
	StatusSpam            Status = "spam"

	ConsentVersion = "tenant-registration-contact-v1"
	RetentionDays  = 180
)

var statusTransitions = map[Status][]Status{
	StatusNew:             {StatusInReview, StatusQualified, StatusDeclined, StatusSpam},
	StatusInReview:        {StatusQualified, StatusDeclined, StatusSpam},
	StatusQualified:       {StatusSetupInProgress, StatusDeclined},
	StatusSetupInProgress: {StatusQualified, StatusConverted},
	StatusConverted:       {},
	StatusDeclined:        {},
	StatusSpam:            {},
}

type PublicSubmissionRequest struct {
	SubmissionKey             string `json:"submission_key"`
	ContactFullName           string `json:"contact_full_name"`
	ContactEmail              string `json:"contact_email"`
	ContactPhone              string `json:"contact_phone"`
	SalonName                 string `json:"salon_name"`
	SalonPhone                string `json:"salon_phone"`
	City                      string `json:"city"`
	State                     string `json:"state"`
	ZipCode                   string `json:"zip_code"`
	SalonWebsite              string `json:"salon_website,omitempty"`
	LocationCount             int    `json:"location_count"`
	PreferredContactLanguage  string `json:"preferred_contact_language"`
	CurrentBookingSystem      string `json:"current_booking_system,omitempty"`
	EstimatedWeeklyCallVolume string `json:"estimated_weekly_call_volume,omitempty"`
	RequestedHelp             string `json:"requested_help,omitempty"`
	Notes                     string `json:"notes,omitempty"`
	Locale                    string `json:"locale"`
	SourcePage                string `json:"source_page"`
	MarketingPlanInterest     string `json:"marketing_plan_interest,omitempty"`
	ConsentVersion            string `json:"consent_version"`
	ContactConsent            bool   `json:"contact_consent"`
	WebsiteConfirmation       string `json:"website_confirmation,omitempty"`

	ContactEmailNormalized string `json:"-"`
	ContactPhoneNormalized string `json:"-"`
	SalonPhoneNormalized   string `json:"-"`
}

type PublicSubmissionResponse struct {
	Status           string `json:"status"`
	RequestReference string `json:"request_reference"`
	Replayed         bool   `json:"replayed"`
}

type ListFilter struct {
	Status      Status
	Query       string
	AssignedTo  string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Limit       int
	Offset      int
}

type ListItem struct {
	ID                    string     `json:"id"`
	PublicReference       string     `json:"public_reference"`
	Status                Status     `json:"status"`
	Version               int64      `json:"version"`
	ContactFullName       string     `json:"contact_full_name"`
	ContactEmailMasked    string     `json:"contact_email_masked"`
	ContactPhoneMasked    string     `json:"contact_phone_masked"`
	SalonName             string     `json:"salon_name"`
	SalonPhoneMasked      string     `json:"salon_phone_masked"`
	City                  string     `json:"city,omitempty"`
	State                 string     `json:"state,omitempty"`
	MarketingPlanInterest string     `json:"marketing_plan_interest,omitempty"`
	AssignedToUserID      string     `json:"assigned_to_user_id,omitempty"`
	AssignedToName        string     `json:"assigned_to_name,omitempty"`
	PossibleDuplicate     bool       `json:"possible_duplicate"`
	ConvertedSalonID      string     `json:"converted_salon_id,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	RetentionExpiresAt    *time.Time `json:"retention_expires_at,omitempty"`
	RedactedAt            *time.Time `json:"redacted_at,omitempty"`
}

type ListResponse struct {
	Requests []ListItem       `json:"requests"`
	Counts   map[Status]int64 `json:"counts"`
	Limit    int              `json:"limit"`
	Offset   int              `json:"offset"`
	HasMore  bool             `json:"has_more"`
}

type Event struct {
	ID             string         `json:"id"`
	ActorUserID    string         `json:"actor_user_id,omitempty"`
	EventType      string         `json:"event_type"`
	FromStatus     Status         `json:"from_status,omitempty"`
	ToStatus       Status         `json:"to_status,omitempty"`
	RequestVersion int64          `json:"request_version"`
	Details        map[string]any `json:"details"`
	CreatedAt      time.Time      `json:"created_at"`
}

type Note struct {
	ID             string     `json:"id"`
	AuthorUserID   string     `json:"author_user_id,omitempty"`
	AuthorName     string     `json:"author_name,omitempty"`
	RequestVersion int64      `json:"request_version"`
	Content        string     `json:"content,omitempty"`
	RedactedAt     *time.Time `json:"redacted_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type Detail struct {
	ListItem
	SubmissionKey                    string             `json:"submission_key"`
	ContactEmail                     string             `json:"contact_email,omitempty"`
	ContactPhone                     string             `json:"contact_phone,omitempty"`
	SalonPhone                       string             `json:"salon_phone,omitempty"`
	SalonWebsite                     string             `json:"salon_website,omitempty"`
	ZipCode                          string             `json:"zip_code,omitempty"`
	LocationCount                    int                `json:"location_count,omitempty"`
	PreferredContactLanguage         string             `json:"preferred_contact_language,omitempty"`
	CurrentBookingSystem             string             `json:"current_booking_system,omitempty"`
	EstimatedWeeklyCallVolume        string             `json:"estimated_weekly_call_volume,omitempty"`
	RequestedHelp                    string             `json:"requested_help,omitempty"`
	ApplicantNotes                   string             `json:"notes,omitempty"`
	Locale                           string             `json:"locale"`
	SourcePage                       string             `json:"source_page"`
	ConsentVersion                   string             `json:"consent_version"`
	ConsentAt                        time.Time          `json:"consent_at"`
	ConvertedAt                      *time.Time         `json:"converted_at,omitempty"`
	TerminalAt                       *time.Time         `json:"terminal_at,omitempty"`
	AllowedTransitions               []Status           `json:"allowed_transitions"`
	Events                           []Event            `json:"events"`
	InternalNotes                    []Note             `json:"internal_notes"`
	ProvisioningDraft                *ProvisioningDraft `json:"provisioning_draft,omitempty"`
	ProvisioningDraftUpdatedByUserID string             `json:"provisioning_draft_updated_by_user_id,omitempty"`
	ProvisioningDraftUpdatedAt       *time.Time         `json:"provisioning_draft_updated_at,omitempty"`
}

type ProvisioningDraft struct {
	OwnerEmail        string `json:"owner_email"`
	OwnerFullName     string `json:"owner_full_name"`
	OwnerPhone        string `json:"owner_phone,omitempty"`
	SalonName         string `json:"salon_name"`
	SalonPhone        string `json:"salon_phone"`
	Address           string `json:"address,omitempty"`
	City              string `json:"city"`
	State             string `json:"state"`
	ZipCode           string `json:"zip_code"`
	Timezone          string `json:"timezone"`
	PrimaryLanguage   string `json:"primary_language"`
	SecondaryLanguage string `json:"secondary_language"`
	HandoffPhone      string `json:"handoff_phone,omitempty"`
}

type MutationRequest struct {
	ActionKey         string             `json:"action_key"`
	ExpectedVersion   int64              `json:"expected_version"`
	Status            *Status            `json:"status,omitempty"`
	AssignedToUserID  *string            `json:"assigned_to_user_id,omitempty"`
	ProvisioningDraft *ProvisioningDraft `json:"provisioning_draft,omitempty"`
}

type MutationResult struct {
	RequestID        string `json:"request_id"`
	Status           Status `json:"status"`
	Version          int64  `json:"version"`
	AssignedToUserID string `json:"assigned_to_user_id,omitempty"`
	Replayed         bool   `json:"replayed"`
}

type AddNoteRequest struct {
	ActionKey       string `json:"action_key"`
	ExpectedVersion int64  `json:"expected_version"`
	Content         string `json:"content"`
}

type AddNoteResult struct {
	RequestID string `json:"request_id"`
	NoteID    string `json:"note_id"`
	Version   int64  `json:"version"`
	Replayed  bool   `json:"replayed"`
}
