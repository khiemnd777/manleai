package ai_runtime_control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/manleai/ai-receptionist/modules/pos"
)

const maxActionKeyBytes = 256

var ErrValidation = errors.New("ai runtime control validation failed")

type Store interface {
	GetSalonAIRuntimeForPlatform(context.Context, string) (pos.AIRuntimeState, error)
	SetSalonAIRuntimeForPlatform(context.Context, pos.AIRuntimeMutation) (pos.AIRuntimeState, bool, error)
}

type UpdateRequest struct {
	Enabled         bool   `json:"enabled"`
	ActionKey       string `json:"action_key"`
	ExpectedVersion int64  `json:"expected_version"`
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (service *Service) Get(ctx context.Context, salonID string) (pos.AIRuntimeState, error) {
	salonID = strings.TrimSpace(salonID)
	if salonID == "" || service == nil || service.store == nil {
		return pos.AIRuntimeState{}, ErrValidation
	}
	return service.store.GetSalonAIRuntimeForPlatform(ctx, salonID)
}

// Update owns the salon-wide AI runtime command. It intentionally does not
// inspect a POS provider or scheduling authority: the scheduling boundary
// resolves each later operation from persisted authority and origin evidence.
func (service *Service) Update(ctx context.Context, salonID string, actorUserID string, request UpdateRequest) (pos.AIRuntimeState, bool, error) {
	salonID = strings.TrimSpace(salonID)
	actorUserID = strings.TrimSpace(actorUserID)
	request.ActionKey = strings.TrimSpace(request.ActionKey)
	if salonID == "" || actorUserID == "" || request.ActionKey == "" || len(request.ActionKey) > maxActionKeyBytes || request.ExpectedVersion < 0 || service == nil || service.store == nil {
		return pos.AIRuntimeState{}, false, ErrValidation
	}
	payload, err := json.Marshal(struct {
		Enabled bool `json:"enabled"`
	}{Enabled: request.Enabled})
	if err != nil {
		return pos.AIRuntimeState{}, false, err
	}
	fingerprint := sha256.Sum256(payload)
	return service.store.SetSalonAIRuntimeForPlatform(ctx, pos.AIRuntimeMutation{
		SalonID:            salonID,
		ActorUserID:        actorUserID,
		ActionKey:          request.ActionKey,
		RequestFingerprint: hex.EncodeToString(fingerprint[:]),
		ExpectedVersion:    request.ExpectedVersion,
		Enabled:            request.Enabled,
	})
}
