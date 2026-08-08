package pos_square

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

const squareOAuthStateTTL = 10 * time.Minute
const squareSchedulingReadinessEvidenceVersion = 2

var (
	ErrValidation                = errors.New("square request validation failed")
	ErrReadinessGate             = errors.New("square booking readiness gate is not complete")
	ErrBookingServiceUnavailable = errors.New("booking service unavailable")
	ErrSquareConfigRequired      = errors.New("stored square configuration is required")
)

type Service struct {
	repo                  *pos.Repository
	adapter               *SquareAdapter
	stateSecret           string
	bookingService        bookingOperationService
	webhookRepo           SquareWebhookStore
	webhookOperationsRepo SquareWebhookOperationsStore
	webhookConfigStatus   squareWebhookConfigurationStatusResolver
	readinessLoader       func(context.Context, string, string) (*ReadinessStatus, error)
}

type squareWebhookConfigurationStatusResolver interface {
	SquareWebhookConfigured(context.Context, string) (bool, error)
}

type bookingOperationService interface {
	CurrentSchedulingAuthority(ctx context.Context, salonID string, ownerUserID string) (string, error)
	ResolveCreateSchedulingAuthority(ctx context.Context, salonID string, ownerUserID string, operationKey string, retryOfAttemptID string) (string, error)
	Create(ctx context.Context, salonID string, ownerUserID string, req booking.CreateBookingRequest) (*booking.BookingAttempt, error)
	ReplayCreate(ctx context.Context, salonID string, ownerUserID string, req booking.CreateBookingRequest) (*booking.BookingAttempt, bool, error)
	Cancel(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req booking.CancelRequest) (*booking.Appointment, *booking.BookingAttempt, error)
	ReplayCancel(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req booking.CancelRequest) (*booking.Appointment, *booking.BookingAttempt, bool, error)
	LatestTestBooking(ctx context.Context, salonID string, ownerUserID string) (*booking.TestBookingRecord, error)
}

func NewService(repo *pos.Repository, adapter *SquareAdapter, stateSecret string, bookingService bookingOperationService) *Service {
	return &Service{repo: repo, adapter: adapter, stateSecret: stateSecret, bookingService: bookingService}
}

func (s *Service) SetWebhookRepository(repo SquareWebhookStore) {
	s.webhookRepo = repo
	if operationsRepo, ok := repo.(SquareWebhookOperationsStore); ok {
		s.webhookOperationsRepo = operationsRepo
	}
}

func (s *Service) SetWebhookConfigurationStatusResolver(resolver squareWebhookConfigurationStatusResolver) {
	s.webhookConfigStatus = resolver
}

type ConnectURLResponse struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

type StatusResponse struct {
	Connection        *pos.Connection                 `json:"connection"`
	SyncLogs          []pos.SyncLog                   `json:"sync_logs"`
	Readiness         *ReadinessStatus                `json:"readiness"`
	InitialActivation InitialProviderActivationStatus `json:"initial_activation"`
}

type InitialProviderActivationStatus struct {
	ActiveProvider                      pos.ActiveProviderState `json:"active_provider"`
	CanActivate                         bool                    `json:"can_activate"`
	Checks                              []ReadinessCheck        `json:"checks"`
	BlockedReason                       string                  `json:"blocked_reason,omitempty"`
	ExpectedIntegrationConfigVersion    int64                   `json:"expected_integration_config_version"`
	ExpectedConnectionCapabilityVersion int64                   `json:"expected_connection_capability_version"`
}

type InitialProviderActivationRequest struct {
	ActionKey                           string `json:"action_key"`
	ExpectedVersion                     int64  `json:"expected_version"`
	ExpectedIntegrationConfigVersion    int64  `json:"expected_integration_config_version"`
	ExpectedConnectionCapabilityVersion int64  `json:"expected_connection_capability_version"`
}

type ReadinessStatus struct {
	AIEnabled                           bool                       `json:"ai_enabled"`
	AIRuntimeVersion                    int64                      `json:"ai_runtime_version"`
	SchedulingAuthority                 string                     `json:"scheduling_authority"`
	CanTestBooking                      bool                       `json:"can_test_booking"`
	CanCancelTestBooking                bool                       `json:"can_cancel_test_booking"`
	CanEnableAIBooking                  bool                       `json:"can_enable_ai_booking"`
	AutomaticSingleCreate               bool                       `json:"automatic_single_create"`
	AutomaticReschedule                 bool                       `json:"automatic_reschedule"`
	AutomaticPartyCreate                bool                       `json:"automatic_party_create"`
	ResourceCapacity                    bool                       `json:"resource_capacity"`
	WritePermissionMode                 string                     `json:"write_permission_mode"`
	ReconnectRequired                   bool                       `json:"reconnect_required"`
	EvidenceCurrent                     bool                       `json:"evidence_current"`
	EvidenceVerifiedAt                  *time.Time                 `json:"evidence_verified_at,omitempty"`
	EvidenceExpiresAt                   *time.Time                 `json:"evidence_expires_at,omitempty"`
	CapabilityBlockerCode               string                     `json:"blocker_code,omitempty"`
	ConnectionCapabilityVersion         int64                      `json:"connection_capability_version"`
	IntegrationConfigVersion            int64                      `json:"integration_config_version"`
	BookingWriteBlocked                 bool                       `json:"booking_write_blocked"`
	BookingWriteBlockedCode             string                     `json:"booking_write_blocked_code,omitempty"`
	BookingWriteBlockedReason           string                     `json:"booking_write_blocked_reason,omitempty"`
	BookingWriteBlockedAt               *time.Time                 `json:"booking_write_blocked_at,omitempty"`
	AppointmentChangeWriteBlocked       bool                       `json:"appointment_change_write_blocked"`
	AppointmentChangeWriteBlockedCode   string                     `json:"appointment_change_write_blocked_code,omitempty"`
	AppointmentChangeWriteBlockedReason string                     `json:"appointment_change_write_blocked_reason,omitempty"`
	AppointmentChangeWriteBlockedAt     *time.Time                 `json:"appointment_change_write_blocked_at,omitempty"`
	ServiceCount                        int                        `json:"service_count"`
	StaffCount                          int                        `json:"staff_count"`
	BusinessHourCount                   int                        `json:"business_hour_period_count"`
	LatestTestBooking                   *booking.TestBookingRecord `json:"latest_test_booking,omitempty"`
	Checks                              []ReadinessCheck           `json:"checks"`
	providerCanTestBooking              bool
}

type ReadinessCheck struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Complete bool   `json:"complete"`
	Message  string `json:"message,omitempty"`
}

// BusinessReadinessResponse is the tenant-safe scheduling projection. It
// intentionally omits connection IDs, merchant/location IDs, scopes, sync
// logs, provider diagnostics, and test-booking evidence.
type BusinessReadinessResponse struct {
	SchedulingAuthority     string `json:"scheduling_authority"`
	ReadyForExternalNewWork bool   `json:"ready_for_external_new_work"`
	ServiceCount            int    `json:"service_count"`
	StaffCount              int    `json:"staff_count"`
	BusinessHourPeriodCount int    `json:"business_hour_period_count"`
	BookingWriteBlocked     bool   `json:"booking_write_blocked"`
}

func businessReadinessResponse(readiness *ReadinessStatus) BusinessReadinessResponse {
	if readiness == nil {
		return BusinessReadinessResponse{}
	}
	return BusinessReadinessResponse{
		SchedulingAuthority:     readiness.SchedulingAuthority,
		ReadyForExternalNewWork: readiness.SchedulingAuthority == booking.SchedulingAuthorityExternalProvider && readiness.CanEnableAIBooking,
		ServiceCount:            readiness.ServiceCount, StaffCount: readiness.StaffCount,
		BusinessHourPeriodCount: readiness.BusinessHourCount,
		BookingWriteBlocked:     readiness.BookingWriteBlocked,
	}
}

type TestBookingRequest struct {
	OperationKey        string    `json:"operation_key"`
	RetryOfAttemptID    string    `json:"retry_of_attempt_id,omitempty"`
	SalonID             string    `json:"salon_id"`
	AvailabilityQuoteID string    `json:"availability_quote_id,omitempty"`
	SlotFingerprint     string    `json:"slot_fingerprint,omitempty"`
	CustomerName        string    `json:"customer_name"`
	CustomerPhone       string    `json:"customer_phone"`
	CustomerEmail       string    `json:"customer_email"`
	ServiceID           string    `json:"service_id"`
	StaffID             string    `json:"staff_id"`
	StartTime           time.Time `json:"start_time"`
	Notes               string    `json:"notes"`
}

type TestBookingResponse struct {
	BookingAttempt    *booking.BookingAttempt    `json:"booking_attempt,omitempty"`
	Appointment       *booking.Appointment       `json:"appointment,omitempty"`
	LatestTestBooking *booking.TestBookingRecord `json:"latest_test_booking,omitempty"`
	Readiness         *ReadinessStatus           `json:"readiness"`
}

type CancelTestBookingRequest struct {
	OperationKey     string `json:"operation_key"`
	RetryOfAttemptID string `json:"retry_of_attempt_id,omitempty"`
	SalonID          string `json:"salon_id"`
	AppointmentID    string `json:"appointment_id"`
	Reason           string `json:"reason"`
}

type GateRequest struct {
	SalonID string `json:"salon_id"`
}

type GateResponse struct {
	AIRuntime pos.AIRuntimeState `json:"ai_runtime"`
	Readiness *ReadinessStatus   `json:"readiness"`
}

type ReevaluateSchedulingCapabilityRequest struct {
	ActionKey                           string `json:"action_key"`
	ExpectedConnectionCapabilityVersion int64  `json:"expected_connection_capability_version"`
	ExpectedIntegrationConfigVersion    int64  `json:"expected_integration_config_version"`
}

func (s *Service) ReevaluateSchedulingCapabilityForPlatform(ctx context.Context, salonID, actorUserID string, req ReevaluateSchedulingCapabilityRequest) (pos.SchedulingCapabilityEvaluation, bool, error) {
	salonID = strings.TrimSpace(salonID)
	actorUserID = strings.TrimSpace(actorUserID)
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	if salonID == "" || actorUserID == "" || req.ActionKey == "" || len(req.ActionKey) > 256 ||
		req.ExpectedConnectionCapabilityVersion <= 0 || req.ExpectedIntegrationConfigVersion <= 0 {
		return pos.SchedulingCapabilityEvaluation{}, false, ErrValidation
	}
	payload, err := json.Marshal(struct {
		ExpectedConnectionCapabilityVersion int64 `json:"expected_connection_capability_version"`
		ExpectedIntegrationConfigVersion    int64 `json:"expected_integration_config_version"`
	}{req.ExpectedConnectionCapabilityVersion, req.ExpectedIntegrationConfigVersion})
	if err != nil {
		return pos.SchedulingCapabilityEvaluation{}, false, err
	}
	digest := sha256.Sum256(payload)
	return s.repo.ReevaluateSquareSchedulingCapability(ctx, pos.SchedulingCapabilityEvaluationInput{
		SalonID: salonID, ActorUserID: actorUserID, ActionKey: req.ActionKey,
		RequestFingerprint:                  hex.EncodeToString(digest[:]),
		ExpectedConnectionCapabilityVersion: req.ExpectedConnectionCapabilityVersion,
		ExpectedIntegrationConfigVersion:    req.ExpectedIntegrationConfigVersion,
	})
}

func (s *Service) ConnectURL(ctx context.Context, salonID string, ownerUserID string) (*ConnectURLResponse, error) {
	if err := s.repo.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	return s.connectURLForSalon(ctx, salonID)
}

func (s *Service) ConnectURLForPlatform(ctx context.Context, salonID string) (*ConnectURLResponse, error) {
	if err := s.repo.EnsureSalonExists(ctx, salonID); err != nil {
		return nil, err
	}
	return s.connectURLForSalon(ctx, salonID)
}

func (s *Service) connectURLForSalon(ctx context.Context, salonID string) (*ConnectURLResponse, error) {
	state, nonceHash, expiresAt, err := encodeState(salonID, s.stateSecret, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	url, err := s.adapter.OAuthURL(ctx, salonID, state)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateOAuthState(ctx, pos.OAuthState{
		SalonID:   salonID,
		Provider:  pos.ProviderSquare,
		StateHash: hashValue(state),
		NonceHash: nonceHash,
		ExpiresAt: expiresAt,
	}); err != nil {
		return nil, err
	}
	return &ConnectURLResponse{URL: url, State: state}, nil
}

func (s *Service) StatusForPlatform(ctx context.Context, salonID string) (*StatusResponse, error) {
	if err := s.repo.EnsureSalonExists(ctx, salonID); err != nil {
		return nil, err
	}
	connection, err := s.repo.GetConnection(ctx, salonID, pos.ProviderSquare)
	if errors.Is(err, pos.ErrNotFound) {
		connection = &pos.Connection{SalonID: salonID, Provider: pos.ProviderSquare, Status: pos.StatusNotConnected, Scopes: []string{}}
	} else if err != nil {
		return nil, err
	}
	logs, err := s.repo.RecentSyncLogs(ctx, salonID, pos.ProviderSquare, 10)
	if err != nil {
		return nil, err
	}
	readiness, err := s.ReadinessForPlatform(ctx, salonID)
	if err != nil {
		return nil, err
	}
	activation, err := s.initialProviderActivationStatus(ctx, salonID)
	if err != nil {
		return nil, err
	}
	return &StatusResponse{Connection: connection, SyncLogs: logs, Readiness: readiness, InitialActivation: activation}, nil
}

func (s *Service) initialProviderActivationStatus(ctx context.Context, salonID string) (InitialProviderActivationStatus, error) {
	evidence, err := s.repo.GetInitialProviderActivationEvidence(ctx, salonID)
	if err != nil {
		return InitialProviderActivationStatus{}, err
	}
	configReady := false
	if s.adapter != nil {
		_, configErr := s.adapter.configFor(ctx, salonID)
		configReady = configErr == nil
	}
	connectionReady := evidence.ConnectionPresent && evidence.ConnectionStatus == pos.StatusActive &&
		strings.TrimSpace(evidence.MerchantID) != "" && strings.TrimSpace(evidence.LocationID) != "" &&
		evidence.SnapshotGeneration > 0 && evidence.LastSyncAt != nil && evidence.ConnectionCapabilityVersion > 0
	providerUnselected := strings.TrimSpace(evidence.ActiveProvider) == ""
	status := InitialProviderActivationStatus{
		ActiveProvider:                      pos.ActiveProviderState{Provider: strings.TrimSpace(evidence.ActiveProvider), Version: evidence.ActiveProviderVersion},
		ExpectedIntegrationConfigVersion:    evidence.IntegrationConfigVersion,
		ExpectedConnectionCapabilityVersion: evidence.ConnectionCapabilityVersion,
		Checks: []ReadinessCheck{
			{Key: "provider_unselected", Label: "No active POS provider selected", Complete: providerUnselected},
			{Key: "tenant_config", Label: "Stored tenant Square configuration", Complete: configReady},
			{Key: "tenant_connection", Label: "Tenant Square connection and location synced", Complete: connectionReady},
		},
	}
	status.CanActivate = providerUnselected && configReady && connectionReady && evidence.IntegrationConfigVersion > 0
	if !providerUnselected {
		status.BlockedReason = "An active POS provider is already selected for this salon."
	} else if !configReady {
		status.BlockedReason = "Save a complete enabled Square configuration for this salon."
	} else if !connectionReady {
		status.BlockedReason = "Connect Square, select a location, and complete a successful sync for this salon."
	}
	return status, nil
}

func (s *Service) ActivateInitialProviderForPlatform(ctx context.Context, salonID, actorUserID string, req InitialProviderActivationRequest) (pos.ActiveProviderState, bool, error) {
	salonID = strings.TrimSpace(salonID)
	actorUserID = strings.TrimSpace(actorUserID)
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	if salonID == "" || actorUserID == "" || req.ActionKey == "" || len(req.ActionKey) > 256 || req.ExpectedVersion < 0 ||
		req.ExpectedIntegrationConfigVersion <= 0 || req.ExpectedConnectionCapabilityVersion <= 0 {
		return pos.ActiveProviderState{}, false, ErrValidation
	}
	if s.adapter == nil {
		return pos.ActiveProviderState{}, false, ErrSquareConfigRequired
	}
	if _, err := s.adapter.configFor(ctx, salonID); err != nil {
		return pos.ActiveProviderState{}, false, ErrSquareConfigRequired
	}
	payload, err := json.Marshal(struct {
		Provider                            string `json:"provider"`
		ExpectedVersion                     int64  `json:"expected_version"`
		ExpectedIntegrationConfigVersion    int64  `json:"expected_integration_config_version"`
		ExpectedConnectionCapabilityVersion int64  `json:"expected_connection_capability_version"`
	}{pos.ProviderSquare, req.ExpectedVersion, req.ExpectedIntegrationConfigVersion, req.ExpectedConnectionCapabilityVersion})
	if err != nil {
		return pos.ActiveProviderState{}, false, err
	}
	fingerprint := sha256.Sum256(payload)
	return s.repo.ActivateInitialProviderForPlatform(ctx, pos.InitialProviderActivationMutation{
		SalonID: salonID, ActorUserID: actorUserID, Provider: pos.ProviderSquare, ActionKey: req.ActionKey,
		RequestFingerprint: hex.EncodeToString(fingerprint[:]), ExpectedVersion: req.ExpectedVersion,
		ExpectedIntegrationConfigVersion:    req.ExpectedIntegrationConfigVersion,
		ExpectedConnectionCapabilityVersion: req.ExpectedConnectionCapabilityVersion,
	})
}

func (s *Service) ReadinessForPlatform(ctx context.Context, salonID string) (*ReadinessStatus, error) {
	aiRuntime, err := s.repo.GetSalonAIRuntimeForPlatform(ctx, salonID)
	if err != nil {
		return nil, err
	}
	authority, err := s.repo.GetSchedulingAuthorityForPlatform(ctx, salonID)
	if err != nil {
		return nil, err
	}
	connection, err := s.repo.GetConnection(ctx, salonID, pos.ProviderSquare)
	if errors.Is(err, pos.ErrNotFound) {
		connection = &pos.Connection{SalonID: salonID, Provider: pos.ProviderSquare, Status: pos.StatusNotConnected, Scopes: []string{}}
	} else if err != nil {
		return nil, err
	}
	services, err := s.repo.ListServices(ctx, salonID, pos.ProviderSquare)
	if err != nil {
		return nil, err
	}
	staff, err := s.repo.ListStaff(ctx, salonID, pos.ProviderSquare)
	if err != nil {
		return nil, err
	}
	periods, err := s.repo.ListBusinessHourPeriods(ctx, salonID)
	if err != nil {
		return nil, err
	}
	bookingWriteError, err := s.repo.LatestErrorForOperations(ctx, salonID, pos.ProviderSquare, []string{"create_booking"})
	if errors.Is(err, pos.ErrNotFound) {
		bookingWriteError = nil
	} else if err != nil {
		return nil, err
	}
	appointmentChangeError, err := s.repo.LatestErrorForOperations(ctx, salonID, pos.ProviderSquare, []string{"reschedule_booking", "cancel_booking"})
	if errors.Is(err, pos.ErrNotFound) {
		appointmentChangeError = nil
	} else if err != nil {
		return nil, err
	}
	capability, err := s.currentSchedulingCapability(ctx, salonID)
	if err != nil {
		return nil, err
	}
	readiness := buildReadiness(aiRuntime.Enabled, authority, connection, services, staff, periods, nil, bookingWriteError, appointmentChangeError, capability)
	readiness.AIRuntimeVersion = aiRuntime.Version
	activeEvidence, err := s.repo.GetInitialProviderActivationEvidence(ctx, salonID)
	if err != nil {
		return nil, err
	}
	applyActiveProviderReadiness(readiness, activeEvidence.ActiveProvider)
	return readiness, nil
}

func (s *Service) SetAIBookingForPlatform(ctx context.Context, salonID, actorUserID string, enabled bool, actionKey string, expectedVersion int64) (*GateResponse, bool, error) {
	salonID = strings.TrimSpace(salonID)
	actorUserID = strings.TrimSpace(actorUserID)
	actionKey = strings.TrimSpace(actionKey)
	if salonID == "" || actorUserID == "" || actionKey == "" || len(actionKey) > 256 || expectedVersion < 0 {
		return nil, false, ErrValidation
	}
	if enabled {
		readiness, err := s.ReadinessForPlatform(ctx, salonID)
		if err != nil {
			return nil, false, err
		}
		if !readiness.CanEnableAIBooking {
			return nil, false, ErrReadinessGate
		}
	}
	payload, err := json.Marshal(struct {
		Enabled bool `json:"enabled"`
	}{Enabled: enabled})
	if err != nil {
		return nil, false, err
	}
	fingerprint := sha256.Sum256(payload)
	state, replayed, err := s.repo.SetSalonAIRuntimeForPlatform(ctx, pos.AIRuntimeMutation{
		SalonID: salonID, ActorUserID: actorUserID, ActionKey: actionKey,
		RequestFingerprint: hex.EncodeToString(fingerprint[:]), ExpectedVersion: expectedVersion, Enabled: enabled,
	})
	if err != nil {
		return nil, false, err
	}
	readiness, err := s.ReadinessForPlatform(ctx, salonID)
	if err != nil {
		return nil, false, err
	}
	return &GateResponse{AIRuntime: state, Readiness: readiness}, replayed, nil
}

func (s *Service) HandleCallback(ctx context.Context, code string, state string) (*pos.Connection, error) {
	salonID, nonceHash, err := decodeState(state, s.stateSecret, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	ctx = databasecontext.WithSystemSalon(ctx, databasecontext.ScopeProvider, salonID)
	if err := s.repo.ConsumeOAuthState(ctx, pos.ProviderSquare, salonID, hashValue(state), nonceHash); err != nil {
		return nil, fmt.Errorf("invalid or expired square state")
	}
	return s.adapter.Connect(ctx, pos.ConnectInput{
		SalonID: salonID,
		Code:    code,
		State:   state,
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
	readiness, err := s.Readiness(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	activation, err := s.initialProviderActivationStatus(ctx, salonID)
	if err != nil {
		return nil, err
	}
	return &StatusResponse{Connection: connection, SyncLogs: logs, Readiness: readiness, InitialActivation: activation}, nil
}

func (s *Service) Locations(ctx context.Context, salonID string, ownerUserID string) ([]pos.Location, error) {
	if err := s.repo.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	return s.adapter.ListLocations(ctx, salonID)
}

func (s *Service) LocationsForPlatform(ctx context.Context, salonID string) ([]pos.Location, error) {
	if err := s.repo.EnsureSalonExists(ctx, salonID); err != nil {
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

func (s *Service) SelectLocationForPlatform(ctx context.Context, salonID, locationID string) (*pos.Connection, error) {
	if err := s.repo.EnsureSalonExists(ctx, salonID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(locationID) == "" {
		return nil, ErrValidation
	}
	return s.repo.UpdateLocation(ctx, salonID, pos.ProviderSquare, locationID)
}

func (s *Service) Sync(ctx context.Context, salonID string, ownerUserID string) (*pos.SyncSummary, error) {
	if err := s.repo.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	return s.syncForSalon(ctx, salonID)
}

func (s *Service) syncForSalon(ctx context.Context, salonID string) (*pos.SyncSummary, error) {
	logID, err := s.repo.CreateSyncLog(ctx, salonID, pos.ProviderSquare, "full_import")
	if err != nil {
		return nil, err
	}
	summary, err := s.adapter.SyncWithSummary(ctx, salonID)
	if err != nil {
		errorCode := normalizeSquareError(err)
		safeMessage := pos.SafeErrorMessage(errorCode)
		_ = s.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "sync",
			ErrorCode:    errorCode,
			ErrorMessage: safeMessage,
		})
		_ = s.repo.CompleteSyncLog(ctx, logID, "failed", safeMessage)
		if generation, ok := providerSnapshotGenerationFromError(err); ok {
			_ = s.repo.MarkSyncCompleteForGeneration(ctx, salonID, pos.ProviderSquare, generation, pos.StatusError, safeMessage)
		}
		return nil, err
	}
	if err := s.repo.CompleteSyncLog(ctx, logID, "succeeded", syncSummaryMessage(summary)); err != nil {
		return nil, err
	}
	return summary, s.repo.MarkSyncCompleteForGeneration(ctx, salonID, pos.ProviderSquare, summary.SnapshotGeneration, pos.StatusActive, "")
}

func (s *Service) SyncForPlatform(ctx context.Context, salonID string) (*pos.SyncSummary, error) {
	if err := s.repo.EnsureSalonExists(ctx, salonID); err != nil {
		return nil, err
	}
	return s.syncForSalon(ctx, salonID)
}

func (s *Service) Readiness(ctx context.Context, salonID string, ownerUserID string) (*ReadinessStatus, error) {
	schedulingAuthority, err := s.currentSchedulingAuthority(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	aiEnabled, err := s.repo.GetSalonAIEnabled(ctx, salonID, ownerUserID)
	if err != nil {
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
	services, err := s.repo.ListServices(ctx, salonID, pos.ProviderSquare)
	if err != nil {
		return nil, err
	}
	staff, err := s.repo.ListStaff(ctx, salonID, pos.ProviderSquare)
	if err != nil {
		return nil, err
	}
	periods, err := s.repo.ListBusinessHourPeriods(ctx, salonID)
	if err != nil {
		return nil, err
	}
	latest, err := s.latestTestBooking(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	bookingWriteError, err := s.repo.LatestErrorForOperations(ctx, salonID, pos.ProviderSquare, []string{"create_booking"})
	if errors.Is(err, pos.ErrNotFound) {
		bookingWriteError = nil
	} else if err != nil {
		return nil, err
	}
	appointmentChangeError, err := s.repo.LatestErrorForOperations(ctx, salonID, pos.ProviderSquare, []string{"reschedule_booking", "cancel_booking"})
	if errors.Is(err, pos.ErrNotFound) {
		appointmentChangeError = nil
	} else if err != nil {
		return nil, err
	}
	capability, err := s.currentSchedulingCapability(ctx, salonID)
	if err != nil {
		return nil, err
	}
	readiness := buildReadiness(aiEnabled, schedulingAuthority, connection, services, staff, periods, latest, bookingWriteError, appointmentChangeError, capability)
	activeEvidence, err := s.repo.GetInitialProviderActivationEvidence(ctx, salonID)
	if err != nil {
		return nil, err
	}
	applyActiveProviderReadiness(readiness, activeEvidence.ActiveProvider)
	return readiness, nil
}

func applyActiveProviderReadiness(readiness *ReadinessStatus, activeProvider string) {
	if readiness == nil {
		return
	}
	selected := strings.TrimSpace(activeProvider) == pos.ProviderSquare
	readiness.Checks = append(readiness.Checks, ReadinessCheck{
		Key: "active_provider", Label: "Square selected as active POS provider", Complete: selected,
		Message: incompleteMessage(selected, "Select Square as this salon's active POS provider."),
	})
	if selected {
		return
	}
	readiness.CanTestBooking = false
	readiness.CanEnableAIBooking = false
	readiness.AutomaticSingleCreate = false
	readiness.providerCanTestBooking = false
	readiness.BookingWriteBlocked = true
	readiness.BookingWriteBlockedCode = "POS_ACTIVE_PROVIDER_NOT_CONFIGURED"
	readiness.BookingWriteBlockedReason = "Square is not selected as this salon's active POS provider."
}

// SchedulingTargetReadiness evaluates the existing Square connection,
// location, snapshot, and booking-write gates without requiring the salon's
// current scheduling authority to already be external_provider.
func (s *Service) SchedulingTargetReadiness(ctx context.Context, salonID string, ownerUserID string) (scheduling.TargetReadiness, error) {
	var result scheduling.TargetReadiness
	err := s.repo.WithSchedulingFenceTx(ctx, salonID, func(tx *sql.Tx) error {
		var err error
		result, err = s.SchedulingTargetReadinessTx(ctx, tx, salonID, ownerUserID)
		return err
	})
	return result, err
}

// SchedulingTargetReadinessTx recomputes the provider-owned target proof in
// the caller's transaction. Authority-switch commit calls this only after it
// owns the shared scheduling fence, so the proof and authority write are one
// atomic decision.
func (s *Service) SchedulingTargetReadinessTx(ctx context.Context, tx *sql.Tx, salonID string, ownerUserID string) (scheduling.TargetReadiness, error) {
	evidence, err := loadSquareSchedulingTargetEvidenceTx(ctx, tx, salonID, ownerUserID)
	if err != nil {
		return scheduling.TargetReadiness{}, err
	}
	readiness := evidence.readinessStatus()
	result := buildSchedulingTargetReadiness(evidence.ActiveProvider, readiness)
	result.AuthorityVersion = evidence.AuthorityVersion
	result.ReadinessEvidenceVersion = squareSchedulingReadinessEvidenceVersion
	result.ReadinessEvidenceFingerprint, err = evidence.fingerprint()
	if err != nil {
		return scheduling.TargetReadiness{}, err
	}
	return result, nil
}

type squareSchedulingTargetEvidence struct {
	ActiveProvider   string                              `json:"active_provider"`
	AuthorityVersion int64                               `json:"authority_version"`
	Connection       squareSchedulingConnectionEvidence  `json:"connection"`
	Services         []squareSchedulingServiceEvidence   `json:"services"`
	Staff            []squareSchedulingStaffEvidence     `json:"staff"`
	BusinessHours    []squareSchedulingHourEvidence      `json:"business_hours"`
	LatestTest       *squareSchedulingTestEvidence       `json:"latest_test,omitempty"`
	LatestWriteError *squareSchedulingWriteErrorEvidence `json:"latest_write_error,omitempty"`
	Capability       pos.SchedulingCapabilityEvaluation  `json:"capability"`
}

type squareSchedulingConnectionEvidence struct {
	Present            bool   `json:"present"`
	Status             string `json:"status"`
	LocationID         string `json:"location_id"`
	SnapshotGeneration int64  `json:"snapshot_generation"`
	LastSyncAt         string `json:"last_sync_at"`
}

type squareSchedulingServiceEvidence struct {
	ID                  string `json:"id"`
	ProviderEntityID    string `json:"provider_entity_id"`
	ProviderVersion     int64  `json:"provider_version"`
	LinkProviderVersion int64  `json:"link_provider_version"`
	Active              bool   `json:"active"`
	AIBookable          bool   `json:"ai_bookable"`
	Archived            bool   `json:"archived"`
	DurationMinutes     int    `json:"duration_minutes"`
	SyncStatus          string `json:"sync_status"`
	LinkSyncStatus      string `json:"link_sync_status"`
}

func (e squareSchedulingServiceEvidence) eligible() bool {
	return e.Active && e.AIBookable && !e.Archived && e.DurationMinutes > 0 &&
		e.SyncStatus == pos.SyncStatusSynced && e.LinkSyncStatus == pos.SyncStatusSynced &&
		strings.TrimSpace(e.ProviderEntityID) != "" && e.ProviderVersion > 0
}

type squareSchedulingStaffEvidence struct {
	ID               string `json:"id"`
	ProviderEntityID string `json:"provider_entity_id"`
	Active           bool   `json:"active"`
	AIBookable       bool   `json:"ai_bookable"`
	Archived         bool   `json:"archived"`
	SyncStatus       string `json:"sync_status"`
	LinkSyncStatus   string `json:"link_sync_status"`
}

func (e squareSchedulingStaffEvidence) eligible() bool {
	return e.Active && e.AIBookable && !e.Archived && e.SyncStatus == pos.SyncStatusSynced &&
		e.LinkSyncStatus == pos.SyncStatusSynced && strings.TrimSpace(e.ProviderEntityID) != ""
}

type squareSchedulingHourEvidence struct {
	ID                 string `json:"id"`
	ProviderLocationID string `json:"provider_location_id"`
	DayOfWeek          int    `json:"day_of_week"`
	StartLocalTime     string `json:"start_local_time"`
	EndLocalTime       string `json:"end_local_time"`
}

type squareSchedulingTestEvidence struct {
	Status          string `json:"status"`
	ProviderOutcome string `json:"provider_outcome"`
	RetryPolicy     string `json:"retry_policy"`
	Reconciliation  string `json:"reconciliation"`
	POSBookingID    string `json:"pos_booking_id"`
	ErrorCode       string `json:"error_code"`
	CreatedAt       string `json:"created_at"`
}

func (e squareSchedulingTestEvidence) blocked() bool {
	return e.Status == booking.StatusPOSPending || e.ProviderOutcome == booking.ProviderOutcomeInFlight ||
		e.ProviderOutcome == booking.ProviderOutcomeUnknown || e.RetryPolicy == booking.RetryPolicyBlocked ||
		e.Reconciliation == booking.ReconciliationRequired
}

type squareSchedulingWriteErrorEvidence struct {
	ErrorCode string `json:"error_code"`
	CreatedAt string `json:"created_at"`
}

func (e squareSchedulingTargetEvidence) fingerprint() (string, error) {
	payload, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func (e squareSchedulingTargetEvidence) readinessStatus() *ReadinessStatus {
	connected := e.Connection.Present && e.Connection.Status != pos.StatusNotConnected &&
		e.Connection.Status != pos.StatusError && e.Connection.Status != pos.StatusExpiredToken &&
		e.Connection.Status != pos.StatusDisabled
	locationSelected := connected && strings.TrimSpace(e.Connection.LocationID) != ""
	synced := connected && e.Connection.LastSyncAt != ""
	serviceCount := 0
	for _, item := range e.Services {
		if item.eligible() {
			serviceCount++
		}
	}
	staffCount := 0
	for _, item := range e.Staff {
		if item.eligible() {
			staffCount++
		}
	}
	hourCount := len(e.BusinessHours)
	servicesReady := synced && serviceCount > 0
	staffReady := synced && staffCount > 0
	hoursReady := synced && hourCount > 0
	testBlocked := e.LatestTest != nil && e.LatestTest.blocked()
	writeBlocked := e.bookingWriteBlocked()
	bookingReady := connected && locationSelected && servicesReady && staffReady && hoursReady
	atomicSlotCommitReady := e.Capability.EvidenceCurrent && e.Capability.AutomaticSingleCreate
	return &ReadinessStatus{
		ServiceCount:      serviceCount,
		StaffCount:        staffCount,
		BusinessHourCount: hourCount,
		Checks: []ReadinessCheck{
			{Key: "connect_square", Complete: connected},
			{Key: "select_location", Complete: locationSelected},
			{Key: "sync_services", Complete: servicesReady},
			{Key: "sync_staff", Complete: staffReady},
			{Key: "sync_business_hours", Complete: hoursReady},
			{Key: "booking_writes", Complete: !writeBlocked},
			{Key: "atomic_slot_commit", Complete: atomicSlotCommitReady, Message: incompleteMessage(atomicSlotCommitReady, "Square single-create requires current buyer-write evidence; seller-write, reschedule, party, and resource capacity remain request-only.")},
		},
		AutomaticSingleCreate:       e.Capability.AutomaticSingleCreate,
		AutomaticReschedule:         false,
		AutomaticPartyCreate:        false,
		ResourceCapacity:            false,
		WritePermissionMode:         e.Capability.WritePermissionMode,
		ReconnectRequired:           e.Capability.ReconnectRequired,
		EvidenceCurrent:             e.Capability.EvidenceCurrent,
		EvidenceVerifiedAt:          e.Capability.EvidenceVerifiedAt,
		EvidenceExpiresAt:           e.Capability.EvidenceExpiresAt,
		CapabilityBlockerCode:       e.Capability.BlockerCode,
		ConnectionCapabilityVersion: e.Capability.ConnectionCapabilityVersion,
		IntegrationConfigVersion:    e.Capability.IntegrationConfigVersion,
		providerCanTestBooking:      bookingReady && !testBlocked && atomicSlotCommitReady,
	}
}

func (e squareSchedulingTargetEvidence) bookingWriteBlocked() bool {
	if e.LatestWriteError == nil || e.LatestWriteError.ErrorCode != pos.ErrorPermissionDenied {
		return false
	}
	if e.LatestTest == nil || strings.TrimSpace(e.LatestTest.POSBookingID) == "" || strings.TrimSpace(e.LatestTest.ErrorCode) != "" {
		return true
	}
	testAt, testErr := time.Parse(time.RFC3339Nano, e.LatestTest.CreatedAt)
	errorAt, errorErr := time.Parse(time.RFC3339Nano, e.LatestWriteError.CreatedAt)
	return testErr != nil || errorErr != nil || !testAt.After(errorAt)
}

func loadSquareSchedulingTargetEvidenceTx(ctx context.Context, tx *sql.Tx, salonID string, actorUserID string) (squareSchedulingTargetEvidence, error) {
	evidence := squareSchedulingTargetEvidence{Services: []squareSchedulingServiceEvidence{}, Staff: []squareSchedulingStaffEvidence{}, BusinessHours: []squareSchedulingHourEvidence{}}
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(BTRIM(salon.active_pos_provider), ''), settings.scheduling_authority_version
		FROM salons salon
		JOIN salon_settings settings ON settings.salon_id = salon.id
		WHERE salon.id::text = $1
		  AND (
		      public.app_rls_system_salon_allowed(salon.id)
		      OR public.has_active_tenant_membership(salon.id, $2::uuid)
		      OR public.has_platform_salon_capability(salon.id, $2::uuid, 'technical.read')
		      OR public.app_actor_feature_access($2::uuid, salon.id, 'calls.read', 'calls')
		      OR public.app_actor_feature_access($2::uuid, salon.id, 'calls.simulate', 'calls')
		  )
		FOR SHARE OF salon, settings
	`, salonID, actorUserID).Scan(&evidence.ActiveProvider, &evidence.AuthorityVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return evidence, pos.ErrNotFound
		}
		return evidence, err
	}
	var lastSync sql.NullTime
	err := tx.QueryRowContext(ctx, `
		SELECT status, COALESCE(location_id, ''), snapshot_generation, last_sync_at
		FROM pos_connections
		WHERE salon_id::text = $1 AND provider = $2
		FOR SHARE
	`, salonID, pos.ProviderSquare).Scan(&evidence.Connection.Status, &evidence.Connection.LocationID, &evidence.Connection.SnapshotGeneration, &lastSync)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return evidence, err
	}
	if err == nil {
		evidence.Connection.Present = true
		if lastSync.Valid {
			evidence.Connection.LastSyncAt = lastSync.Time.UTC().Format(time.RFC3339Nano)
		}
	}
	var capabilityVerifiedAt time.Time
	var capabilityExpiresAt time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT capability.id::text,capability.atomic_create_no_overlap,
		       capability.write_permission_mode,capability.reconnect_required,
		       COALESCE(capability.blocker_code,''),capability.verified_at,capability.expires_at,
		       connection.booking_write_capability_version,version.version
		FROM external_provider_scheduling_capability_evidence capability
		JOIN pos_connections connection
		  ON connection.salon_id=capability.salon_id
		 AND connection.provider=capability.provider
		 AND connection.id=capability.connection_id
		 AND connection.booking_write_capability_version=capability.connection_capability_version
		 AND connection.status='active'
		 AND connection.last_sync_at IS NOT NULL
		 AND connection.location_id=capability.provider_location_id
		JOIN salon_integration_configs config
		  ON config.salon_id=capability.salon_id
		 AND config.id=capability.integration_config_id
		 AND config.provider=capability.provider
		 AND config.enabled=true
		 AND config.settings->>'api_version'=capability.provider_api_version
		JOIN technical_resource_versions version
		  ON version.salon_id=config.salon_id
		 AND version.resource_type='integration_config'
		 AND version.resource_id=config.provider
		 AND version.version=capability.config_version
		WHERE capability.salon_id::text=$1
		  AND capability.provider=$2
		  AND capability.verification_contract_version='square-buyer-single-create-v1'
		  AND capability.write_permission_mode='buyer_write'
		  AND capability.oauth_scope_fingerprint=public.square_oauth_scope_fingerprint(connection.scopes)
		  AND capability.verified_at <= now() AND capability.expires_at > now()
		ORDER BY capability.verified_at DESC,capability.id DESC
		LIMIT 1
		FOR SHARE OF capability,connection,config,version
	`, salonID, pos.ProviderSquare).Scan(
		&evidence.Capability.EvidenceID,
		&evidence.Capability.AutomaticSingleCreate,
		&evidence.Capability.WritePermissionMode,
		&evidence.Capability.ReconnectRequired,
		&evidence.Capability.BlockerCode,
		&capabilityVerifiedAt,
		&capabilityExpiresAt,
		&evidence.Capability.ConnectionCapabilityVersion,
		&evidence.Capability.IntegrationConfigVersion,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return evidence, err
	}
	if err == nil {
		evidence.Capability.EvidenceCurrent = true
		evidence.Capability.EvidenceVerifiedAt = &capabilityVerifiedAt
		evidence.Capability.EvidenceExpiresAt = &capabilityExpiresAt
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT service.id::text, COALESCE(link.provider_entity_id, ''), COALESCE(service.pos_service_version, 0),
		       COALESCE(link.provider_version, 0), service.active, service.ai_bookable,
		       service.archived_at IS NOT NULL, service.duration_minutes, service.sync_status,
		       COALESCE(link.sync_status, '')
		FROM services service
		LEFT JOIN pos_entity_links link
		  ON link.salon_id=service.salon_id AND link.entity_type='service' AND link.entity_id=service.id AND link.provider=$2
		WHERE service.salon_id::text=$1 AND service.pos_provider=$2
		ORDER BY service.id
		FOR SHARE OF service
	`, salonID, pos.ProviderSquare)
	if err != nil {
		return evidence, err
	}
	for rows.Next() {
		var item squareSchedulingServiceEvidence
		if err := rows.Scan(&item.ID, &item.ProviderEntityID, &item.ProviderVersion, &item.LinkProviderVersion, &item.Active, &item.AIBookable, &item.Archived, &item.DurationMinutes, &item.SyncStatus, &item.LinkSyncStatus); err != nil {
			rows.Close()
			return evidence, err
		}
		evidence.Services = append(evidence.Services, item)
	}
	if err := rows.Close(); err != nil {
		return evidence, err
	}
	if err := rows.Err(); err != nil {
		return evidence, err
	}

	rows, err = tx.QueryContext(ctx, `
		SELECT member.id::text, COALESCE(link.provider_entity_id, ''), member.active, member.ai_bookable,
		       member.archived_at IS NOT NULL, member.sync_status, COALESCE(link.sync_status, '')
		FROM staff member
		LEFT JOIN pos_entity_links link
		  ON link.salon_id=member.salon_id AND link.entity_type='staff' AND link.entity_id=member.id AND link.provider=$2
		WHERE member.salon_id::text=$1 AND member.pos_provider=$2
		ORDER BY member.id
		FOR SHARE OF member
	`, salonID, pos.ProviderSquare)
	if err != nil {
		return evidence, err
	}
	for rows.Next() {
		var item squareSchedulingStaffEvidence
		if err := rows.Scan(&item.ID, &item.ProviderEntityID, &item.Active, &item.AIBookable, &item.Archived, &item.SyncStatus, &item.LinkSyncStatus); err != nil {
			rows.Close()
			return evidence, err
		}
		evidence.Staff = append(evidence.Staff, item)
	}
	if err := rows.Close(); err != nil {
		return evidence, err
	}
	if err := rows.Err(); err != nil {
		return evidence, err
	}

	rows, err = tx.QueryContext(ctx, `
		SELECT id::text, COALESCE(provider_location_id, ''), day_of_week,
		       start_local_time::text, end_local_time::text
		FROM salon_business_hour_periods
		WHERE salon_id::text=$1 AND source='imported' AND provider=$2
		ORDER BY id
		FOR SHARE
	`, salonID, pos.ProviderSquare)
	if err != nil {
		return evidence, err
	}
	for rows.Next() {
		var item squareSchedulingHourEvidence
		if err := rows.Scan(&item.ID, &item.ProviderLocationID, &item.DayOfWeek, &item.StartLocalTime, &item.EndLocalTime); err != nil {
			rows.Close()
			return evidence, err
		}
		evidence.BusinessHours = append(evidence.BusinessHours, item)
	}
	if err := rows.Close(); err != nil {
		return evidence, err
	}
	if err := rows.Err(); err != nil {
		return evidence, err
	}

	var test squareSchedulingTestEvidence
	var posBookingID, errorCode sql.NullString
	var testCreatedAt time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT status, provider_outcome, retry_policy, reconciliation_status,
		       pos_booking_id, error_code, created_at
		FROM booking_attempts
		WHERE salon_id::text=$1 AND source='square_test_booking'
		ORDER BY created_at DESC, id DESC
		LIMIT 1
		FOR SHARE
	`, salonID).Scan(&test.Status, &test.ProviderOutcome, &test.RetryPolicy, &test.Reconciliation, &posBookingID, &errorCode, &testCreatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return evidence, err
	}
	if err == nil {
		test.POSBookingID = strings.TrimSpace(posBookingID.String)
		test.ErrorCode = strings.TrimSpace(errorCode.String)
		test.CreatedAt = testCreatedAt.UTC().Format(time.RFC3339Nano)
		evidence.LatestTest = &test
	}

	var writeError squareSchedulingWriteErrorEvidence
	var writeErrorCreatedAt time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT error_code, created_at
		FROM pos_errors
		WHERE salon_id::text=$1 AND provider=$2 AND operation='create_booking'
		ORDER BY created_at DESC, id DESC
		LIMIT 1
		FOR SHARE
	`, salonID, pos.ProviderSquare).Scan(&writeError.ErrorCode, &writeErrorCreatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return evidence, err
	}
	if err == nil {
		writeError.CreatedAt = writeErrorCreatedAt.UTC().Format(time.RFC3339Nano)
		evidence.LatestWriteError = &writeError
	}
	return evidence, nil
}

func buildSchedulingTargetReadiness(activeProvider string, readiness *ReadinessStatus) scheduling.TargetReadiness {
	result := scheduling.TargetReadiness{
		TargetSchedulingAuthority: booking.SchedulingAuthorityExternalProvider,
		ServiceCount:              readiness.ServiceCount,
		StaffCount:                readiness.StaffCount,
		BusinessHourPeriodCount:   readiness.BusinessHourCount,
		Checks:                    make([]scheduling.TargetReadinessCheck, 0, len(readiness.Checks)+2),
		Blockers:                  make([]scheduling.TargetReadinessBlocker, 0),
		AvailabilityBlockers:      make([]scheduling.TargetReadinessBlocker, 0),
		ExecutionBlockers:         make([]scheduling.TargetReadinessBlocker, 0),
	}
	add := func(key string, ready bool, message string, requiredForAvailability bool, requiredForExecution bool) {
		code := "EXTERNAL_PROVIDER_" + strings.ToUpper(key)
		result.Checks = append(result.Checks, scheduling.TargetReadinessCheck{Code: code, Ready: ready, Scope: "provider"})
		if !ready {
			blocker := scheduling.TargetReadinessBlocker{Code: code, Scope: "provider", Message: message}
			result.Blockers = append(result.Blockers, blocker)
			if requiredForAvailability {
				result.AvailabilityBlockers = append(result.AvailabilityBlockers, blocker)
			}
			if requiredForExecution {
				result.ExecutionBlockers = append(result.ExecutionBlockers, blocker)
			}
		}
	}
	selectedAdapter := activeProvider == pos.ProviderSquare
	add("select_pos_adapter", selectedAdapter, "Select the Square adapter for this salon.", true, true)
	for _, check := range readiness.Checks {
		if check.Key == "enable_ai_booking" {
			continue
		}
		availabilityCheck := check.Key != "booking_writes" && check.Key != "atomic_slot_commit"
		add(check.Key, check.Complete, externalTargetBlockerMessage(check.Key), availabilityCheck, true)
	}
	testSafetyReady := readiness.canTestExternalProviderBooking()
	add("booking_test_safety", testSafetyReady, "Resolve the current Square booking test before switching scheduling authority.", false, true)
	result.AvailabilityReady = len(result.AvailabilityBlockers) == 0
	result.ExecutionReady = len(result.ExecutionBlockers) == 0
	result.Ready = len(result.Blockers) == 0
	return result
}

func externalTargetBlockerMessage(key string) string {
	switch key {
	case "connect_square":
		return "Connect Square Appointments."
	case "select_location":
		return "Select a Square location."
	case "sync_services":
		return "Sync at least one active, AI-bookable service."
	case "sync_staff":
		return "Sync at least one active, AI-bookable staff member."
	case "sync_business_hours":
		return "Sync at least one Square business-hours period."
	case "booking_writes":
		return "Resolve the Square booking-write readiness blocker."
	case "atomic_slot_commit":
		return "Square auto-confirmation is blocked until no-overlap semantics are verified for the exact provider write path."
	default:
		return "Resolve the external provider readiness blocker."
	}
}

func (s *Service) currentSchedulingAuthority(ctx context.Context, salonID string, ownerUserID string) (string, error) {
	if s.bookingService == nil {
		return "", ErrBookingServiceUnavailable
	}
	return s.bookingService.CurrentSchedulingAuthority(ctx, salonID, ownerUserID)
}

func (s *Service) loadReadiness(ctx context.Context, salonID string, ownerUserID string) (*ReadinessStatus, error) {
	if s.readinessLoader != nil {
		return s.readinessLoader(ctx, salonID, ownerUserID)
	}
	return s.Readiness(ctx, salonID, ownerUserID)
}

func (s *Service) CreateTestBooking(ctx context.Context, salonID string, ownerUserID string, req TestBookingRequest) (*TestBookingResponse, error) {
	if s.bookingService == nil {
		return nil, ErrBookingServiceUnavailable
	}
	req = normalizeTestBookingRequest(salonID, req)
	if req.OperationKey == "" {
		return nil, ErrValidation
	}
	createRequest := testBookingCreateRequest(req)
	replayed, found, err := s.bookingService.ReplayCreate(ctx, req.SalonID, ownerUserID, createRequest)
	if err != nil {
		return nil, err
	}
	if found {
		if replayed == nil {
			return nil, fmt.Errorf("booking service returned an empty create replay")
		}
		readiness, err := s.loadReadiness(ctx, req.SalonID, ownerUserID)
		if err != nil {
			return nil, err
		}
		return &TestBookingResponse{
			BookingAttempt:    replayed,
			Appointment:       replayed.Appointment,
			LatestTestBooking: readiness.LatestTestBooking,
			Readiness:         readiness,
		}, nil
	}
	schedulingAuthority, err := s.bookingService.ResolveCreateSchedulingAuthority(ctx, req.SalonID, ownerUserID, req.OperationKey, req.RetryOfAttemptID)
	if err != nil {
		return nil, err
	}
	if schedulingAuthority != booking.SchedulingAuthorityExternalProvider {
		return nil, booking.ErrSchedulingAuthorityNotReady
	}
	if req.SalonID == "" || req.ServiceID == "" || req.StaffID == "" || req.StartTime.IsZero() {
		return nil, ErrValidation
	}
	if req.AvailabilityQuoteID == "" || req.SlotFingerprint == "" {
		return nil, booking.ErrAvailabilityQuoteRequired
	}
	readiness, err := s.loadReadiness(ctx, req.SalonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if !readiness.canTestExternalProviderBooking() {
		return nil, ErrReadinessGate
	}
	attempt, err := s.bookingService.Create(ctx, req.SalonID, ownerUserID, createRequest)
	if err != nil {
		return nil, err
	}
	if attempt == nil {
		return nil, fmt.Errorf("booking service did not return a booking attempt")
	}
	readiness, err = s.loadReadiness(ctx, req.SalonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	return &TestBookingResponse{
		BookingAttempt:    attempt,
		Appointment:       attempt.Appointment,
		LatestTestBooking: readiness.LatestTestBooking,
		Readiness:         readiness,
	}, nil
}

func testBookingCreateRequest(req TestBookingRequest) booking.CreateBookingRequest {
	return booking.CreateBookingRequest{
		OperationKey:        req.OperationKey,
		RetryOfAttemptID:    req.RetryOfAttemptID,
		AvailabilityQuoteID: req.AvailabilityQuoteID,
		SlotFingerprint:     req.SlotFingerprint,
		Source:              booking.SourceSquareTestBooking,
		CustomerName:        req.CustomerName,
		CustomerPhone:       req.CustomerPhone,
		CustomerEmail:       req.CustomerEmail,
		ServiceID:           req.ServiceID,
		StaffID:             req.StaffID,
		StartTime:           req.StartTime,
		Notes:               req.Notes,
	}
}

func (s *Service) CancelTestBooking(ctx context.Context, salonID string, ownerUserID string, req CancelTestBookingRequest) (*TestBookingResponse, error) {
	if s.bookingService == nil {
		return nil, ErrBookingServiceUnavailable
	}
	req = normalizeCancelTestBookingRequest(salonID, req)
	if req.SalonID == "" || req.OperationKey == "" {
		return nil, ErrValidation
	}
	appointmentID := strings.TrimSpace(req.AppointmentID)
	if appointmentID != "" {
		response, found, err := s.replayTestBookingCancellation(ctx, ownerUserID, req, appointmentID)
		if err != nil || found {
			return response, err
		}
	}
	latest, err := s.latestTestBooking(ctx, req.SalonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if appointmentID == "" && latest != nil {
		appointmentID = latest.AppointmentID
		if appointmentID != "" {
			response, found, err := s.replayTestBookingCancellation(ctx, ownerUserID, req, appointmentID)
			if err != nil || found {
				return response, err
			}
		}
	}
	if appointmentID == "" || latest == nil || latest.AppointmentStatus == booking.StatusCancelled {
		return nil, ErrReadinessGate
	}
	appointment, fallback, err := s.bookingService.Cancel(ctx, req.SalonID, ownerUserID, appointmentID, testBookingCancelRequest(req))
	if err != nil {
		return nil, err
	}
	readiness, err := s.loadReadiness(ctx, req.SalonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	return &TestBookingResponse{
		BookingAttempt:    fallback,
		Appointment:       appointment,
		LatestTestBooking: readiness.LatestTestBooking,
		Readiness:         readiness,
	}, nil
}

func (s *Service) replayTestBookingCancellation(ctx context.Context, ownerUserID string, req CancelTestBookingRequest, appointmentID string) (*TestBookingResponse, bool, error) {
	appointment, fallback, found, err := s.bookingService.ReplayCancel(ctx, req.SalonID, ownerUserID, appointmentID, testBookingCancelRequest(req))
	if err != nil || !found {
		return nil, found, err
	}
	if appointment == nil && fallback == nil {
		return nil, false, fmt.Errorf("booking service returned an empty cancel replay")
	}
	readiness, err := s.loadReadiness(ctx, req.SalonID, ownerUserID)
	if err != nil {
		return nil, false, err
	}
	return &TestBookingResponse{
		BookingAttempt:    fallback,
		Appointment:       appointment,
		LatestTestBooking: readiness.LatestTestBooking,
		Readiness:         readiness,
	}, true, nil
}

func testBookingCancelRequest(req CancelTestBookingRequest) booking.CancelRequest {
	return booking.CancelRequest{
		OperationKey:     req.OperationKey,
		RetryOfAttemptID: req.RetryOfAttemptID,
		Reason:           req.Reason,
		Source:           booking.SourceSquareTestBooking,
	}
}

func (s *Service) EnableAIBooking(ctx context.Context, salonID string, ownerUserID string) (*GateResponse, error) {
	readiness, err := s.loadReadiness(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if readiness.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider {
		return nil, booking.ErrSchedulingAuthorityNotReady
	}
	if !readiness.CanEnableAIBooking {
		return nil, ErrReadinessGate
	}
	if err := s.repo.SetSalonAIEnabled(ctx, salonID, ownerUserID, true); err != nil {
		return nil, err
	}
	readiness, err = s.Readiness(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	return &GateResponse{Readiness: readiness}, nil
}

func (s *Service) DisableAIBooking(ctx context.Context, salonID string, ownerUserID string) (*GateResponse, error) {
	if err := s.repo.SetSalonAIEnabled(ctx, salonID, ownerUserID, false); err != nil {
		return nil, err
	}
	readiness, err := s.Readiness(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	return &GateResponse{Readiness: readiness}, nil
}

func (s *Service) latestTestBooking(ctx context.Context, salonID string, ownerUserID string) (*booking.TestBookingRecord, error) {
	if s.bookingService == nil {
		return nil, nil
	}
	latest, err := s.bookingService.LatestTestBooking(ctx, salonID, ownerUserID)
	if errors.Is(err, pos.ErrNotFound) {
		return nil, nil
	}
	return latest, err
}

func buildReadiness(aiEnabled bool, schedulingAuthority string, connection *pos.Connection, services []pos.Service, staff []pos.StaffMember, periods []pos.BusinessHourPeriod, latest *booking.TestBookingRecord, bookingWriteError *pos.POSErrorRecord, appointmentChangeError *pos.POSErrorRecord, capabilities ...pos.SchedulingCapabilityEvaluation) *ReadinessStatus {
	capability := pos.SchedulingCapabilityEvaluation{}
	if len(capabilities) > 0 {
		capability = capabilities[0]
	}
	connected := connection != nil &&
		connection.ID != "" &&
		connection.Status != pos.StatusNotConnected &&
		connection.Status != pos.StatusError &&
		connection.Status != pos.StatusExpiredToken &&
		connection.Status != pos.StatusDisabled
	locationSelected := connected && strings.TrimSpace(connection.LocationID) != ""
	synced := connected && connection.LastSyncAt != nil
	serviceCount := countBookableServices(services)
	staffCount := countBookableStaff(staff)
	businessHourCount := countImportedBusinessHourPeriods(periods, pos.ProviderSquare)
	servicesReady := synced && serviceCount > 0
	staffReady := synced && staffCount > 0
	businessHoursReady := synced && businessHourCount > 0
	testWriteBlocked := testBookingWriteBlocked(latest)
	bookingReady := connected && locationSelected && synced && servicesReady && staffReady && businessHoursReady
	atomicSlotCommitReady := capability.EvidenceCurrent && capability.AutomaticSingleCreate
	externalProviderSelected := schedulingAuthority == booking.SchedulingAuthorityExternalProvider
	providerCanTestBooking := bookingReady && !testWriteBlocked && atomicSlotCommitReady
	canTest := externalProviderSelected && providerCanTestBooking
	canCancel := latest != nil &&
		latest.AppointmentID != "" &&
		latest.POSBookingID != "" &&
		latest.AppointmentStatus != booking.StatusCancelled &&
		!testWriteBlocked
	bookingWriteBlocker := bookingWriteBlockerFromError(bookingWriteError, latest)
	canEnable := externalProviderSelected && bookingReady && !bookingWriteBlocker.Blocked && atomicSlotCommitReady

	checks := []ReadinessCheck{
		{Key: "connect_square", Label: "Connect Square", Complete: connected, Message: incompleteMessage(connected, "Square Appointments is not connected.")},
		{Key: "select_location", Label: "Select location", Complete: locationSelected, Message: incompleteMessage(locationSelected, "Choose the Square location for this salon.")},
		{Key: "sync_services", Label: "Sync services", Complete: servicesReady, Message: incompleteMessage(servicesReady, "Sync at least one active AI-bookable service.")},
		{Key: "sync_staff", Label: "Sync staff", Complete: staffReady, Message: incompleteMessage(staffReady, "Sync at least one active AI-bookable staff member.")},
		{Key: "sync_business_hours", Label: "Sync business hours", Complete: businessHoursReady, Message: incompleteMessage(businessHoursReady, "Sync at least one Square business hour period.")},
		{Key: "booking_writes", Label: "Square booking writes", Complete: !bookingWriteBlocker.Blocked, Message: incompleteMessage(!bookingWriteBlocker.Blocked, bookingWriteBlocker.Message())},
		{Key: "atomic_slot_commit", Label: "Atomic slot commit", Complete: atomicSlotCommitReady, Message: incompleteMessage(atomicSlotCommitReady, "Square single-create requires current buyer-write safety evidence. Seller-write, reschedule, party, and resource-capacity automation remain blocked.")},
		{Key: "enable_ai_booking", Label: "Enable AI booking", Complete: aiEnabled, Message: incompleteMessage(aiEnabled, "AI booking is disabled until all safety checks pass.")},
	}
	appointmentChangeBlocker := appointmentChangeWriteBlockerFromError(appointmentChangeError)

	return &ReadinessStatus{
		AIEnabled:                           aiEnabled,
		SchedulingAuthority:                 schedulingAuthority,
		CanTestBooking:                      canTest,
		CanCancelTestBooking:                canCancel,
		CanEnableAIBooking:                  canEnable,
		AutomaticSingleCreate:               capability.AutomaticSingleCreate,
		AutomaticReschedule:                 false,
		AutomaticPartyCreate:                false,
		ResourceCapacity:                    false,
		WritePermissionMode:                 capability.WritePermissionMode,
		ReconnectRequired:                   capability.ReconnectRequired,
		EvidenceCurrent:                     capability.EvidenceCurrent,
		EvidenceVerifiedAt:                  capability.EvidenceVerifiedAt,
		EvidenceExpiresAt:                   capability.EvidenceExpiresAt,
		CapabilityBlockerCode:               capability.BlockerCode,
		ConnectionCapabilityVersion:         capability.ConnectionCapabilityVersion,
		IntegrationConfigVersion:            capability.IntegrationConfigVersion,
		BookingWriteBlocked:                 bookingWriteBlocker.Blocked,
		BookingWriteBlockedCode:             bookingWriteBlocker.ErrorCode,
		BookingWriteBlockedReason:           bookingWriteBlocker.Reason,
		BookingWriteBlockedAt:               bookingWriteBlocker.LastSeenAt,
		AppointmentChangeWriteBlocked:       appointmentChangeBlocker.Blocked,
		AppointmentChangeWriteBlockedCode:   appointmentChangeBlocker.ErrorCode,
		AppointmentChangeWriteBlockedReason: appointmentChangeBlocker.Reason,
		AppointmentChangeWriteBlockedAt:     appointmentChangeBlocker.LastSeenAt,
		ServiceCount:                        serviceCount,
		StaffCount:                          staffCount,
		BusinessHourCount:                   businessHourCount,
		LatestTestBooking:                   latest,
		Checks:                              checks,
		providerCanTestBooking:              providerCanTestBooking,
	}
}

func (s *Service) currentSchedulingCapability(ctx context.Context, salonID string) (pos.SchedulingCapabilityEvaluation, error) {
	capability, err := s.repo.GetSquareSchedulingCapabilityEvaluation(ctx, salonID)
	if errors.Is(err, pos.ErrNotFound) {
		return pos.SchedulingCapabilityEvaluation{WritePermissionMode: pos.SchedulingWriteModeUnsupported, ReconnectRequired: true, BlockerCode: "SQUARE_CAPABILITY_EVIDENCE_REQUIRED"}, nil
	}
	return capability, err
}

func (r *ReadinessStatus) canTestExternalProviderBooking() bool {
	return r != nil && (r.providerCanTestBooking || r.CanTestBooking)
}

func testBookingWriteBlocked(latest *booking.TestBookingRecord) bool {
	if latest == nil {
		return false
	}
	return latest.Status == booking.StatusPOSPending ||
		latest.ProviderOutcome == booking.ProviderOutcomeInFlight ||
		latest.ProviderOutcome == booking.ProviderOutcomeUnknown ||
		latest.RetryPolicy == booking.RetryPolicyBlocked ||
		latest.Reconciliation == booking.ReconciliationRequired
}

type bookingWriteBlocker struct {
	Blocked    bool
	ErrorCode  string
	Reason     string
	LastSeenAt *time.Time
}

func (b bookingWriteBlocker) Message() string {
	if strings.TrimSpace(b.Reason) != "" {
		return b.Reason
	}
	return "Square Appointments rejected booking writes. Reconnect Square with booking write permissions or run the Square test booking after updating the seller account."
}

func bookingWriteBlockerFromError(item *pos.POSErrorRecord, latest *booking.TestBookingRecord) bookingWriteBlocker {
	if item == nil || (item.ErrorCode != pos.ErrorPermissionDenied && item.ErrorCode != pos.ErrorWriteUnsupported) {
		return bookingWriteBlocker{}
	}
	if latest != nil &&
		strings.TrimSpace(latest.POSBookingID) != "" &&
		latest.ErrorCode == "" &&
		latest.CreatedAt.After(item.CreatedAt) {
		return bookingWriteBlocker{}
	}
	createdAt := item.CreatedAt
	return bookingWriteBlocker{
		Blocked:    true,
		ErrorCode:  item.ErrorCode,
		Reason:     pos.SafeErrorMessage(item.ErrorCode),
		LastSeenAt: &createdAt,
	}
}

type appointmentChangeWriteBlocker struct {
	Blocked    bool
	ErrorCode  string
	Reason     string
	LastSeenAt *time.Time
}

func appointmentChangeWriteBlockerFromError(item *pos.POSErrorRecord) appointmentChangeWriteBlocker {
	if item == nil {
		return appointmentChangeWriteBlocker{}
	}
	legacyUnsupported := item.ErrorCode == pos.ErrorPermissionDenied &&
		strings.Contains(strings.ToLower(item.ErrorMessage), "merchant subscription does not support write operations")
	if item.ErrorCode != pos.ErrorWriteUnsupported && !legacyUnsupported {
		return appointmentChangeWriteBlocker{}
	}
	createdAt := item.CreatedAt
	return appointmentChangeWriteBlocker{
		Blocked:    true,
		ErrorCode:  pos.ErrorWriteUnsupported,
		Reason:     pos.SafeErrorMessage(pos.ErrorWriteUnsupported),
		LastSeenAt: &createdAt,
	}
}

func syncSummaryMessage(summary *pos.SyncSummary) string {
	if summary == nil {
		return "Square sync completed."
	}
	return fmt.Sprintf(
		"Synced %d services, %d staff, %d business hour periods, and %d customers from Square.",
		summary.ServicesSynced,
		summary.StaffSynced,
		summary.BusinessHourPeriodsSynced,
		summary.CustomersSynced,
	)
}

func countBookableServices(services []pos.Service) int {
	count := 0
	for _, service := range services {
		if service.Active &&
			service.AIBookable &&
			service.SyncStatus == pos.SyncStatusSynced &&
			service.POSLinked &&
			strings.TrimSpace(service.POSServiceID) != "" &&
			service.POSServiceVersion > 0 &&
			service.DurationMinutes > 0 {
			count++
		}
	}
	return count
}

func countBookableStaff(staff []pos.StaffMember) int {
	count := 0
	for _, member := range staff {
		if member.Active &&
			member.AIBookable &&
			member.ArchivedAt == nil &&
			member.SyncStatus == pos.SyncStatusSynced &&
			member.POSLinked &&
			strings.TrimSpace(member.POSStaffID) != "" {
			count++
		}
	}
	return count
}

func countImportedBusinessHourPeriods(periods []pos.BusinessHourPeriod, provider string) int {
	count := 0
	for _, period := range periods {
		if period.Source == pos.BusinessHourSourceImported && period.Provider == provider {
			count++
		}
	}
	return count
}

func incompleteMessage(complete bool, message string) string {
	if complete {
		return ""
	}
	return message
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func normalizeTestBookingRequest(salonID string, req TestBookingRequest) TestBookingRequest {
	req.OperationKey = strings.TrimSpace(req.OperationKey)
	req.RetryOfAttemptID = strings.TrimSpace(req.RetryOfAttemptID)
	req.SalonID = defaultString(strings.TrimSpace(req.SalonID), strings.TrimSpace(salonID))
	req.AvailabilityQuoteID = strings.TrimSpace(req.AvailabilityQuoteID)
	req.SlotFingerprint = strings.TrimSpace(req.SlotFingerprint)
	req.CustomerName = defaultString(strings.TrimSpace(req.CustomerName), "ManleAI Test Customer")
	req.CustomerPhone = defaultString(strings.TrimSpace(req.CustomerPhone), "+13125550199")
	req.CustomerEmail = strings.TrimSpace(req.CustomerEmail)
	req.ServiceID = strings.TrimSpace(req.ServiceID)
	req.StaffID = strings.TrimSpace(req.StaffID)
	req.Notes = defaultString(strings.TrimSpace(req.Notes), "AI booking readiness test. Cancel after verifying Square booking creation.")
	return req
}

func normalizeCancelTestBookingRequest(salonID string, req CancelTestBookingRequest) CancelTestBookingRequest {
	req.OperationKey = strings.TrimSpace(req.OperationKey)
	req.RetryOfAttemptID = strings.TrimSpace(req.RetryOfAttemptID)
	req.SalonID = defaultString(strings.TrimSpace(req.SalonID), strings.TrimSpace(salonID))
	req.AppointmentID = strings.TrimSpace(req.AppointmentID)
	req.Reason = defaultString(strings.TrimSpace(req.Reason), "AI booking readiness test cleanup")
	return req
}

func encodeState(salonID string, secret string, now time.Time) (string, string, time.Time, error) {
	if strings.TrimSpace(salonID) == "" {
		return "", "", time.Time{}, fmt.Errorf("salon id is required")
	}
	if secret == "" {
		return "", "", time.Time{}, fmt.Errorf("square state secret is not configured")
	}
	nonce, err := randomNonce()
	if err != nil {
		return "", "", time.Time{}, err
	}
	expiresAt := now.Add(squareOAuthStateTTL)
	body := strings.Join([]string{
		pos.ProviderSquare,
		salonID,
		nonce,
		fmt.Sprintf("%d", expiresAt.Unix()),
	}, ":")
	signed := body + ":" + signState(body, secret)
	return base64.RawURLEncoding.EncodeToString([]byte(signed)), hashValue(nonce), expiresAt, nil
}

func decodeState(state string, secret string, now time.Time) (string, string, error) {
	if secret == "" {
		return "", "", fmt.Errorf("square state secret is not configured")
	}
	raw, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil {
		return "", "", err
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 5 || parts[0] != pos.ProviderSquare {
		return "", "", fmt.Errorf("invalid square state")
	}
	salonID := parts[1]
	nonce := parts[2]
	expiresUnix, err := parseUnix(parts[3])
	if err != nil || salonID == "" || nonce == "" {
		return "", "", fmt.Errorf("invalid square state")
	}
	expiresAt := time.Unix(expiresUnix, 0)
	if !expiresAt.After(now) {
		return "", "", fmt.Errorf("expired square state")
	}
	body := strings.Join(parts[:4], ":")
	expected := signState(body, secret)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(parts[4])) != 1 {
		return "", "", fmt.Errorf("invalid square state")
	}
	return salonID, hashValue(nonce), nil
}

func randomNonce() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func signState(value string, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func hashValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func parseUnix(value string) (int64, error) {
	return strconv.ParseInt(value, 10, 64)
}
