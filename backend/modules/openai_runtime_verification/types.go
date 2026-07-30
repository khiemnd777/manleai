package openairuntimeverification

import (
	"errors"
	"time"
)

var (
	ErrValidation      = errors.New("OpenAI verification validation failed")
	ErrNotFound        = errors.New("OpenAI verification not found")
	ErrVersionConflict = errors.New("OpenAI verification config version conflict")
	ErrActionConflict  = errors.New("OpenAI verification action conflict")
)

type VerifyRequest struct {
	ActionKey             string `json:"action_key"`
	ExpectedConfigVersion int64  `json:"expected_config_version"`
}

type CapabilityStatus struct {
	Capability        string     `json:"capability"`
	Required          bool       `json:"required"`
	Status            string     `json:"status"`
	LatencyMS         *int64     `json:"latency_ms,omitempty"`
	ProviderRequestID string     `json:"provider_request_id,omitempty"`
	ErrorCode         string     `json:"error_code,omitempty"`
	VerifiedAt        *time.Time `json:"verified_at,omitempty"`
}

type RunStatus struct {
	ID                          string             `json:"id"`
	SalonID                     string             `json:"salon_id"`
	Status                      string             `json:"status"`
	Fresh                       bool               `json:"fresh"`
	ConfigVersion               int64              `json:"config_version"`
	CredentialRevision          int64              `json:"credential_revision"`
	DestinationPolicyVersion    string             `json:"destination_policy_version"`
	VerificationContractVersion string             `json:"verification_contract_version"`
	AttemptCount                int                `json:"attempt_count"`
	ErrorCode                   string             `json:"error_code,omitempty"`
	Capabilities                []CapabilityStatus `json:"capabilities"`
	StartedAt                   *time.Time         `json:"started_at,omitempty"`
	CompletedAt                 *time.Time         `json:"completed_at,omitempty"`
	CreatedAt                   time.Time          `json:"created_at"`
	UpdatedAt                   time.Time          `json:"updated_at"`
}

type Claim struct {
	RunID      string
	SalonID    string
	ClaimToken string
}

type claimedRun struct {
	RunStatus
	IntegrationConfigID string
	ClaimToken          string
}

type capabilityPlan struct {
	Capability string
	Required   bool
}
