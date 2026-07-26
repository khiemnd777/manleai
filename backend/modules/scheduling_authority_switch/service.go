package scheduling_authority_switch

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/manleai/ai-receptionist/modules/scheduling"
)

const maxOperationKeyBytes = 256

var (
	ErrValidation        = errors.New("scheduling authority switch validation failed")
	ErrNotFound          = errors.New("scheduling authority switch run not found")
	ErrOperationConflict = errors.New("scheduling authority switch operation conflict")
	ErrVersionConflict   = errors.New("scheduling authority switch authority version conflict")
	ErrReadinessConflict = errors.New("scheduling authority switch readiness changed")
	ErrLiveExecution     = errors.New("scheduling authority switch live external execution exists")
	ErrStateConflict     = errors.New("scheduling authority switch state conflict")
)

type Store interface {
	FindByOperationKey(ctx context.Context, salonID string, ownerUserID string, operationKey string) (*SwitchRun, error)
	CurrentAuthority(ctx context.Context, salonID string, ownerUserID string) (authorityState, error)
	EligibleServiceCount(ctx context.Context, salonID string, ownerUserID string) (int, error)
	BookingMode(ctx context.Context, salonID string, ownerUserID string) (string, error)
	CreateOrReplayPreview(ctx context.Context, input persistPreviewInput) (*SwitchRun, bool, error)
	Latest(ctx context.Context, salonID string, ownerUserID string) (*SwitchRun, error)
	Get(ctx context.Context, salonID string, ownerUserID string, runID string) (*SwitchRun, error)
	ReplayCommit(ctx context.Context, salonID string, ownerUserID string, runID string, actionKey string, actionFingerprint string) (*SwitchRun, bool, error)
	Commit(ctx context.Context, input commitInput) (*SwitchRun, bool, error)
}

type targetReadiness interface {
	SchedulingTargetReadiness(ctx context.Context, salonID string, ownerUserID string) (scheduling.TargetReadiness, error)
}

type transactionalTargetReadiness interface {
	SchedulingTargetReadinessTx(ctx context.Context, tx *sql.Tx, salonID string, ownerUserID string) (scheduling.TargetReadiness, error)
}

type Service struct {
	store                 Store
	calendar              targetReadiness
	external              targetReadiness
	ownerManualRegistered bool
}

func NewService(store Store, calendar targetReadiness, external targetReadiness, ownerManualRegistered bool) *Service {
	return &Service{store: store, calendar: calendar, external: external, ownerManualRegistered: ownerManualRegistered}
}

func (s *Service) Preview(ctx context.Context, salonID string, ownerUserID string, req PreviewRequest) (*PreviewResponse, error) {
	req = normalizePreviewRequest(req)
	if !validPreviewRequest(salonID, ownerUserID, req) {
		return nil, ErrValidation
	}
	fingerprint, err := previewFingerprint(req)
	if err != nil {
		return nil, err
	}
	existing, err := s.store.FindByOperationKey(ctx, salonID, ownerUserID, req.OperationKey)
	if err == nil {
		if existing.payloadFingerprint != fingerprint {
			return nil, ErrOperationConflict
		}
		return &PreviewResponse{SwitchRun: existing, Replayed: true}, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	current, err := s.store.CurrentAuthority(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if current.Authority != req.SourceSchedulingAuthority || current.Version != req.ExpectedSourceAuthorityVersion {
		return nil, ErrVersionConflict
	}
	if req.RollbackOfSwitchRunID != "" {
		prior, err := s.store.Get(ctx, salonID, ownerUserID, req.RollbackOfSwitchRunID)
		if err != nil {
			return nil, err
		}
		if prior.Status != "committed" || prior.SourceSchedulingAuthority != req.TargetSchedulingAuthority || prior.TargetSchedulingAuthority != req.SourceSchedulingAuthority {
			return nil, ErrStateConflict
		}
	}
	snapshot, blockers, err := s.evaluateTarget(ctx, salonID, ownerUserID, req.TargetSchedulingAuthority, current.Version)
	if err != nil {
		return nil, err
	}
	status := StatusPreviewReady
	if len(blockers) > 0 {
		status = StatusPreviewBlocked
	}
	run, replayed, err := s.store.CreateOrReplayPreview(ctx, persistPreviewInput{
		SalonID: salonID, OwnerUserID: ownerUserID,
		SourceSchedulingAuthority:      req.SourceSchedulingAuthority,
		TargetSchedulingAuthority:      req.TargetSchedulingAuthority,
		ExpectedSourceAuthorityVersion: req.ExpectedSourceAuthorityVersion,
		OperationKey:                   req.OperationKey, PayloadFingerprint: fingerprint,
		ReadinessSnapshot: snapshot, Blockers: blockers, Status: status,
		RollbackOfSwitchRunID: req.RollbackOfSwitchRunID,
	})
	if err != nil {
		return nil, err
	}
	return &PreviewResponse{SwitchRun: run, Replayed: replayed}, nil
}

func (s *Service) Commit(ctx context.Context, salonID string, ownerUserID string, runID string, req CommitRequest) (*PreviewResponse, error) {
	runID = strings.TrimSpace(runID)
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	if strings.TrimSpace(salonID) == "" || strings.TrimSpace(ownerUserID) == "" || runID == "" || req.ActionKey == "" || len(req.ActionKey) > maxOperationKeyBytes {
		return nil, ErrValidation
	}
	fingerprint, err := commitFingerprint(runID, req)
	if err != nil {
		return nil, err
	}
	if replayed, found, err := s.store.ReplayCommit(ctx, salonID, ownerUserID, runID, req.ActionKey, fingerprint); err != nil || found {
		if err != nil {
			return nil, err
		}
		return &PreviewResponse{SwitchRun: replayed, Replayed: true}, nil
	}
	run, err := s.store.Get(ctx, salonID, ownerUserID, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != StatusPreviewReady {
		return nil, ErrStateConflict
	}
	currentSnapshot, blockers, err := s.evaluateTarget(ctx, salonID, ownerUserID, run.TargetSchedulingAuthority, run.ExpectedSourceAuthorityVersion)
	if err != nil {
		return nil, err
	}
	storedSnapshot, _ := json.Marshal(run.ReadinessSnapshot)
	newSnapshot, _ := json.Marshal(currentSnapshot)
	if len(blockers) > 0 || string(storedSnapshot) != string(newSnapshot) {
		return nil, ErrReadinessConflict
	}
	input := commitInput{SalonID: salonID, OwnerUserID: ownerUserID, RunID: runID, ActionKey: req.ActionKey, ActionFingerprint: fingerprint, ExpectedReadiness: currentSnapshot}
	if run.TargetSchedulingAuthority == TargetExternalProvider {
		validator, ok := s.external.(transactionalTargetReadiness)
		if !ok {
			return nil, ErrReadinessConflict
		}
		expected := run.ReadinessSnapshot
		input.ValidateTargetReadiness = func(ctx context.Context, tx *sql.Tx) error {
			actual, err := validator.SchedulingTargetReadinessTx(ctx, tx, salonID, ownerUserID)
			if err != nil {
				return err
			}
			if !sameReadinessEvidence(expected, actual) {
				return ErrReadinessConflict
			}
			return nil
		}
	}
	committed, replayed, err := s.store.Commit(ctx, input)
	if err != nil {
		return nil, err
	}
	return &PreviewResponse{SwitchRun: committed, Replayed: replayed}, nil
}

func sameReadinessEvidence(expected scheduling.TargetReadiness, actual scheduling.TargetReadiness) bool {
	if !expected.Ready || !actual.Ready || expected.ReadinessEvidenceVersion <= 0 ||
		expected.ReadinessEvidenceVersion != actual.ReadinessEvidenceVersion ||
		len(expected.ReadinessEvidenceFingerprint) != sha256.Size*2 ||
		len(actual.ReadinessEvidenceFingerprint) != sha256.Size*2 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected.ReadinessEvidenceFingerprint), []byte(actual.ReadinessEvidenceFingerprint)) == 1
}

func (s *Service) Latest(ctx context.Context, salonID string, ownerUserID string) (*PreviewResponse, error) {
	if strings.TrimSpace(salonID) == "" || strings.TrimSpace(ownerUserID) == "" {
		return nil, ErrValidation
	}
	run, err := s.store.Latest(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	return &PreviewResponse{SwitchRun: run}, nil
}

func (s *Service) Get(ctx context.Context, salonID string, ownerUserID string, runID string) (*PreviewResponse, error) {
	if strings.TrimSpace(salonID) == "" || strings.TrimSpace(ownerUserID) == "" || strings.TrimSpace(runID) == "" {
		return nil, ErrValidation
	}
	run, err := s.store.Get(ctx, salonID, ownerUserID, strings.TrimSpace(runID))
	if err != nil {
		return nil, err
	}
	return &PreviewResponse{SwitchRun: run}, nil
}

func (s *Service) evaluateTarget(ctx context.Context, salonID string, ownerUserID string, target string, authorityVersion int64) (ReadinessSnapshot, []ReadinessBlocker, error) {
	snapshot := ReadinessSnapshot{TargetSchedulingAuthority: target, AuthorityVersion: authorityVersion, Checks: make([]ReadinessCheck, 0)}
	blockers := make([]ReadinessBlocker, 0)
	add := func(code string, ready bool, scope string, entityID string, message string) {
		snapshot.Checks = append(snapshot.Checks, ReadinessCheck{Code: code, Ready: ready, Scope: scope, EntityID: entityID})
		if !ready {
			blockers = append(blockers, ReadinessBlocker{Code: code, Scope: scope, EntityID: entityID, Message: message})
		}
	}
	switch target {
	case TargetOwnerManual:
		count, err := s.store.EligibleServiceCount(ctx, salonID, ownerUserID)
		if err != nil {
			return snapshot, nil, err
		}
		snapshot.EligibleServiceCount = count
		bookingMode, err := s.store.BookingMode(ctx, salonID, ownerUserID)
		if err != nil {
			return snapshot, nil, err
		}
		add("OWNER_MANUAL_EXECUTOR_REGISTERED", s.ownerManualRegistered, "executor", "", "The owner-review scheduling executor is not registered.")
		add("ELIGIBLE_SERVICE_REQUIRED", count > 0, "service", "", "Add at least one active, AI-bookable service with a positive duration.")
		add("BOOKING_MODE_COMPATIBLE", bookingMode == "pending_approval" || bookingMode == "disabled", "settings", "", "Owner manual scheduling requires pending approval or disabled booking mode.")
	case TargetManleAICalendar:
		if s.calendar == nil {
			add("MANLEAI_CALENDAR_EXECUTOR_REGISTERED", false, "executor", "", "The internal calendar scheduling executor is not registered.")
			break
		}
		readiness, err := s.calendar.SchedulingTargetReadiness(ctx, salonID, ownerUserID)
		if err != nil {
			return snapshot, nil, err
		}
		snapshot = readiness
		blockers = append(blockers, readiness.Blockers...)
	case TargetExternalProvider:
		if s.external == nil {
			add("EXTERNAL_PROVIDER_ADAPTER_REGISTERED", false, "executor", "", "The external scheduling provider adapter is not registered.")
			break
		}
		readiness, err := s.external.SchedulingTargetReadiness(ctx, salonID, ownerUserID)
		if err != nil {
			return snapshot, nil, err
		}
		snapshot = readiness
		blockers = append(blockers, readiness.Blockers...)
	default:
		return snapshot, nil, ErrValidation
	}
	snapshot.TargetSchedulingAuthority = target
	snapshot.AuthorityVersion = authorityVersion
	snapshot.Blockers = nil
	snapshot.Ready = len(blockers) == 0
	return snapshot, blockers, nil
}

func normalizePreviewRequest(req PreviewRequest) PreviewRequest {
	req.OperationKey = strings.TrimSpace(req.OperationKey)
	req.SourceSchedulingAuthority = strings.TrimSpace(req.SourceSchedulingAuthority)
	req.TargetSchedulingAuthority = strings.TrimSpace(req.TargetSchedulingAuthority)
	req.RollbackOfSwitchRunID = strings.TrimSpace(req.RollbackOfSwitchRunID)
	return req
}

func commitFingerprint(runID string, req CommitRequest) (string, error) {
	payload, err := json.Marshal(struct {
		RunID     string `json:"run_id"`
		ActionKey string `json:"action_key"`
		EventType string `json:"event_type"`
	}{runID, req.ActionKey, "commit"})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func validPreviewRequest(salonID string, ownerUserID string, req PreviewRequest) bool {
	return strings.TrimSpace(salonID) != "" && strings.TrimSpace(ownerUserID) != "" && req.OperationKey != "" && len(req.OperationKey) <= maxOperationKeyBytes &&
		req.ExpectedSourceAuthorityVersion > 0 && validAuthority(req.SourceSchedulingAuthority) && validAuthority(req.TargetSchedulingAuthority) && req.SourceSchedulingAuthority != req.TargetSchedulingAuthority
}

func validAuthority(value string) bool {
	return value == TargetOwnerManual || value == TargetManleAICalendar || value == TargetExternalProvider
}

func previewFingerprint(req PreviewRequest) (string, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}
