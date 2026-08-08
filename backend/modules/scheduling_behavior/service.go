package scheduling_behavior

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/manleai/ai-receptionist/modules/scheduling"
)

const maxActionKeyBytes = 256

var (
	ErrValidation       = errors.New("scheduling behavior validation failed")
	ErrNotFound         = errors.New("scheduling behavior not found")
	ErrVersionConflict  = errors.New("scheduling behavior version conflict")
	ErrActionConflict   = errors.New("scheduling behavior action conflict")
	ErrIncompatibleMode = errors.New("booking mode is incompatible with scheduling authority")
)

type Store interface {
	Get(context.Context, string) (PersistedState, error)
	UpdateBookingMode(context.Context, UpdateBookingModeCommand) (BookingModeMutationResult, bool, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (service *Service) Get(ctx context.Context, salonID string) (State, error) {
	salonID = strings.TrimSpace(salonID)
	if salonID == "" || service == nil || service.store == nil {
		return State{}, ErrValidation
	}
	persisted, err := service.store.Get(ctx, salonID)
	if err != nil {
		return State{}, err
	}
	return schedulingBehaviorState(persisted)
}

func (service *Service) UpdateBookingMode(ctx context.Context, salonID string, actorUserID string, request UpdateBookingModeRequest) (BookingModeMutationResult, bool, error) {
	salonID = strings.TrimSpace(salonID)
	actorUserID = strings.TrimSpace(actorUserID)
	request.ActionKey = strings.TrimSpace(request.ActionKey)
	if salonID == "" || actorUserID == "" || request.ActionKey == "" || len(request.ActionKey) > maxActionKeyBytes || request.ExpectedVersion < 0 || service == nil || service.store == nil {
		return BookingModeMutationResult{}, false, ErrValidation
	}
	if request.BookingMode != scheduling.BookingModePendingApproval && request.BookingMode != scheduling.BookingModeConfirmedBooking && request.BookingMode != scheduling.BookingModeDisabled {
		return BookingModeMutationResult{}, false, ErrValidation
	}
	payload, err := json.Marshal(struct {
		BookingMode scheduling.BookingMode `json:"booking_mode"`
	}{BookingMode: request.BookingMode})
	if err != nil {
		return BookingModeMutationResult{}, false, err
	}
	fingerprint := sha256.Sum256(payload)
	return service.store.UpdateBookingMode(ctx, UpdateBookingModeCommand{
		SalonID:            salonID,
		ActorUserID:        actorUserID,
		BookingMode:        request.BookingMode,
		ExpectedVersion:    request.ExpectedVersion,
		ActionKey:          request.ActionKey,
		RequestFingerprint: hex.EncodeToString(fingerprint[:]),
	})
}

func schedulingBehaviorState(persisted PersistedState) (State, error) {
	policy := scheduling.ConversationPolicyFence{
		BookingMode:         persisted.BookingMode,
		SchedulingAuthority: persisted.SchedulingAuthority,
	}
	allowed, err := scheduling.AllowedConversationBookingModes(policy.SchedulingAuthority)
	if err != nil {
		return State{}, err
	}
	behavior, err := scheduling.ConversationBehavior(policy)
	if err != nil {
		return State{}, ErrIncompatibleMode
	}
	return State{
		SchedulingAuthority: persisted.SchedulingAuthority,
		AuthorityVersion:    persisted.AuthorityVersion,
		BookingMode:         persisted.BookingMode,
		PolicyVersion:       persisted.PolicyVersion,
		AllowedBookingModes: allowed,
		EffectiveBehavior:   behavior,
	}, nil
}
