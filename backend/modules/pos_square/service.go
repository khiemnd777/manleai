package pos_square

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/manleai/ai-receptionist/modules/pos"
)

type Service struct {
	repo    *pos.Repository
	adapter *SquareAdapter
}

func NewService(repo *pos.Repository, adapter *SquareAdapter) *Service {
	return &Service{repo: repo, adapter: adapter}
}

type ConnectURLResponse struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

type StatusResponse struct {
	Connection *pos.Connection `json:"connection"`
	SyncLogs   []pos.SyncLog   `json:"sync_logs"`
}

func (s *Service) ConnectURL(ctx context.Context, salonID string, ownerUserID string) (*ConnectURLResponse, error) {
	if err := s.repo.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	state := encodeState(salonID)
	url, err := s.adapter.OAuthURL(state)
	if err != nil {
		return nil, err
	}
	return &ConnectURLResponse{URL: url, State: state}, nil
}

func (s *Service) HandleCallback(ctx context.Context, code string, state string, redirectURL string) (*pos.Connection, error) {
	salonID, err := decodeState(state)
	if err != nil {
		return nil, err
	}
	return s.adapter.Connect(ctx, pos.ConnectInput{
		SalonID:     salonID,
		Code:        code,
		RedirectURL: redirectURL,
		State:       state,
	})
}

func (s *Service) Status(ctx context.Context, salonID string, ownerUserID string) (*StatusResponse, error) {
	if err := s.repo.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	connection, err := s.repo.GetConnection(ctx, salonID, pos.ProviderSquare)
	if errors.Is(err, pos.ErrNotFound) {
		connection = &pos.Connection{
			SalonID:  salonID,
			Provider: pos.ProviderSquare,
			Status:   pos.StatusNotConnected,
			Scopes:   []string{},
		}
	} else if err != nil {
		return nil, err
	}
	logs, err := s.repo.RecentSyncLogs(ctx, salonID, pos.ProviderSquare, 10)
	if err != nil {
		return nil, err
	}
	return &StatusResponse{Connection: connection, SyncLogs: logs}, nil
}

func (s *Service) Locations(ctx context.Context, salonID string, ownerUserID string) ([]pos.Location, error) {
	if err := s.repo.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	return s.adapter.ListLocations(ctx, salonID)
}

func (s *Service) SelectLocation(ctx context.Context, salonID string, ownerUserID string, locationID string) (*pos.Connection, error) {
	if err := s.repo.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(locationID) == "" {
		return nil, fmt.Errorf("location id is required")
	}
	return s.repo.UpdateLocation(ctx, salonID, pos.ProviderSquare, locationID)
}

func (s *Service) Sync(ctx context.Context, salonID string, ownerUserID string) error {
	if err := s.repo.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return err
	}
	logID, err := s.repo.CreateSyncLog(ctx, salonID, pos.ProviderSquare, "services_and_staff")
	if err != nil {
		return err
	}
	if err := s.repo.MarkSyncing(ctx, salonID, pos.ProviderSquare); err != nil {
		return err
	}
	if err := s.adapter.Sync(ctx, salonID); err != nil {
		_ = s.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "sync",
			ErrorCode:    pos.ErrorUnknown,
			ErrorMessage: err.Error(),
		})
		_ = s.repo.CompleteSyncLog(ctx, logID, "failed", err.Error())
		_ = s.repo.MarkSyncComplete(ctx, salonID, pos.ProviderSquare, pos.StatusError, err.Error())
		return err
	}
	if err := s.repo.CompleteSyncLog(ctx, logID, "succeeded", "Services and staff synced from Square."); err != nil {
		return err
	}
	return s.repo.MarkSyncComplete(ctx, salonID, pos.ProviderSquare, pos.StatusActive, "")
}

func encodeState(salonID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte("square:" + salonID))
}

func decodeState(state string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil {
		return "", err
	}
	value := string(raw)
	if !strings.HasPrefix(value, "square:") {
		return "", fmt.Errorf("invalid square state")
	}
	salonID := strings.TrimPrefix(value, "square:")
	if salonID == "" {
		return "", fmt.Errorf("invalid square state")
	}
	return salonID, nil
}
