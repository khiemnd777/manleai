package scheduling_authority_switch

import (
	"context"
	"database/sql"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

const (
	StatusPreviewReady   = "preview_ready"
	StatusPreviewBlocked = "preview_blocked"

	TargetOwnerManual      = booking.SchedulingAuthorityOwnerManual
	TargetManleAICalendar  = booking.SchedulingAuthorityManleAICalendar
	TargetExternalProvider = booking.SchedulingAuthorityExternalProvider
)

type PreviewRequest struct {
	OperationKey                   string `json:"operation_key"`
	SourceSchedulingAuthority      string `json:"source_scheduling_authority"`
	TargetSchedulingAuthority      string `json:"target_scheduling_authority"`
	ExpectedSourceAuthorityVersion int64  `json:"expected_source_authority_version"`
	RollbackOfSwitchRunID          string `json:"rollback_of_switch_run_id,omitempty"`
}

type CommitRequest struct {
	ActionKey string `json:"action_key"`
}

type ReadinessCheck = scheduling.TargetReadinessCheck
type ReadinessBlocker = scheduling.TargetReadinessBlocker
type ReadinessSnapshot = scheduling.TargetReadiness

type SwitchRun struct {
	ID                             string             `json:"id"`
	SalonID                        string             `json:"salon_id"`
	SourceSchedulingAuthority      string             `json:"source_scheduling_authority"`
	TargetSchedulingAuthority      string             `json:"target_scheduling_authority"`
	ExpectedSourceAuthorityVersion int64              `json:"expected_source_authority_version"`
	OperationKey                   string             `json:"operation_key"`
	ActorUserID                    string             `json:"-"`
	RollbackOfSwitchRunID          string             `json:"rollback_of_switch_run_id,omitempty"`
	ReadinessSnapshot              ReadinessSnapshot  `json:"readiness_snapshot"`
	Blockers                       []ReadinessBlocker `json:"blockers"`
	Status                         string             `json:"status"`
	PreviewedAt                    time.Time          `json:"previewed_at"`
	BlockedAt                      *time.Time         `json:"blocked_at,omitempty"`
	CommittedAt                    *time.Time         `json:"committed_at,omitempty"`
	CreatedAt                      time.Time          `json:"created_at"`
	UpdatedAt                      time.Time          `json:"updated_at"`
	payloadFingerprint             string
}

type PreviewResponse struct {
	SwitchRun *SwitchRun `json:"scheduling_authority_switch"`
	Replayed  bool       `json:"replayed"`
}

type authorityState struct {
	Authority string
	Version   int64
}

type persistPreviewInput struct {
	SalonID                        string
	OwnerUserID                    string
	SourceSchedulingAuthority      string
	TargetSchedulingAuthority      string
	ExpectedSourceAuthorityVersion int64
	OperationKey                   string
	PayloadFingerprint             string
	ReadinessSnapshot              ReadinessSnapshot
	Blockers                       []ReadinessBlocker
	Status                         string
	RollbackOfSwitchRunID          string
}

type commitInput struct {
	SalonID                 string
	OwnerUserID             string
	RunID                   string
	ActionKey               string
	ActionFingerprint       string
	ExpectedReadiness       ReadinessSnapshot
	ValidateTargetReadiness func(context.Context, *sql.Tx) error
}
