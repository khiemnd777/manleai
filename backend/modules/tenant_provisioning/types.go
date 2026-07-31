package tenant_provisioning

import "time"

const (
	OwnerModeCreateInvited = "create_invited"
	OwnerModeUseExisting   = "use_existing"
	InvitationTTL          = 72 * time.Hour
)

type OwnerIdentityInput struct {
	Mode     string `json:"mode"`
	UserID   string `json:"user_id,omitempty"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Phone    string `json:"phone,omitempty"`
}

type SalonProfileInput struct {
	Name              string `json:"name"`
	Phone             string `json:"phone"`
	Address           string `json:"address,omitempty"`
	City              string `json:"city"`
	State             string `json:"state"`
	ZipCode           string `json:"zip_code"`
	Timezone          string `json:"timezone"`
	PrimaryLanguage   string `json:"primary_language"`
	SecondaryLanguage string `json:"secondary_language"`
	HandoffPhone      string `json:"handoff_phone,omitempty"`
}

type ProvisionRequest struct {
	ActionKey       string             `json:"action_key"`
	ExpectedVersion int64              `json:"expected_version"`
	Owner           OwnerIdentityInput `json:"owner"`
	Salon           SalonProfileInput  `json:"salon"`
}

type ProvisionResult struct {
	RequestID           string `json:"request_id"`
	SalonID             string `json:"salon_id"`
	OwnerUserID         string `json:"owner_user_id"`
	RequestVersion      int64  `json:"request_version"`
	SchedulingAuthority string `json:"scheduling_authority"`
	Replayed            bool   `json:"replayed"`
}

type InvitationRequest struct {
	ActionKey       string `json:"action_key"`
	ExpectedVersion int64  `json:"expected_version"`
	Rotate          bool   `json:"rotate"`
}

type InvitationResult struct {
	RequestID      string    `json:"request_id"`
	InvitationID   string    `json:"invitation_id"`
	RequestVersion int64     `json:"request_version"`
	RawToken       string    `json:"raw_token,omitempty"`
	ExpiresAt      time.Time `json:"expires_at"`
	TokenAvailable bool      `json:"token_available"`
	Replayed       bool      `json:"replayed"`
}

type AcceptInvitationRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type AcceptInvitationResult struct {
	Status string `json:"status"`
}

type TenantIdentity struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Status   string `json:"status"`
}

type TenantIdentityList struct {
	Users []TenantIdentity `json:"users"`
}
