package pos_square

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/pos"
)

const squareOAuthStateTTL = 10 * time.Minute

var (
	ErrValidation                = errors.New("square request validation failed")
	ErrReadinessGate             = errors.New("square booking readiness gate is not complete")
	ErrBookingServiceUnavailable = errors.New("booking service unavailable")
)

type Service struct {
	repo           *pos.Repository
	adapter        *SquareAdapter
	stateSecret    string
	bookingService *booking.Service
}

func NewService(repo *pos.Repository, adapter *SquareAdapter, stateSecret string, bookingService *booking.Service) *Service {
	return &Service{repo: repo, adapter: adapter, stateSecret: stateSecret, bookingService: bookingService}
}

type ConnectURLResponse struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

type StatusResponse struct {
	Connection *pos.Connection  `json:"connection"`
	SyncLogs   []pos.SyncLog    `json:"sync_logs"`
	Readiness  *ReadinessStatus `json:"readiness"`
}

type ReadinessStatus struct {
	AIEnabled            bool                       `json:"ai_enabled"`
	CanTestBooking       bool                       `json:"can_test_booking"`
	CanCancelTestBooking bool                       `json:"can_cancel_test_booking"`
	CanEnableAIBooking   bool                       `json:"can_enable_ai_booking"`
	ServiceCount         int                        `json:"service_count"`
	StaffCount           int                        `json:"staff_count"`
	LatestTestBooking    *booking.TestBookingRecord `json:"latest_test_booking,omitempty"`
	Checks               []ReadinessCheck           `json:"checks"`
}

type ReadinessCheck struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Complete bool   `json:"complete"`
	Message  string `json:"message,omitempty"`
}

type TestBookingRequest struct {
	SalonID       string    `json:"salon_id"`
	CustomerName  string    `json:"customer_name"`
	CustomerPhone string    `json:"customer_phone"`
	CustomerEmail string    `json:"customer_email"`
	ServiceID     string    `json:"service_id"`
	StaffID       string    `json:"staff_id"`
	StartTime     time.Time `json:"start_time"`
	Notes         string    `json:"notes"`
}

type TestBookingResponse struct {
	BookingAttempt    *booking.BookingAttempt    `json:"booking_attempt,omitempty"`
	Appointment       *booking.Appointment       `json:"appointment,omitempty"`
	LatestTestBooking *booking.TestBookingRecord `json:"latest_test_booking,omitempty"`
	Readiness         *ReadinessStatus           `json:"readiness"`
}

type CancelTestBookingRequest struct {
	SalonID       string `json:"salon_id"`
	AppointmentID string `json:"appointment_id"`
	Reason        string `json:"reason"`
}

type GateRequest struct {
	SalonID string `json:"salon_id"`
}

type GateResponse struct {
	Readiness *ReadinessStatus `json:"readiness"`
}

func (s *Service) ConnectURL(ctx context.Context, salonID string, ownerUserID string) (*ConnectURLResponse, error) {
	if err := s.repo.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
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

func (s *Service) HandleCallback(ctx context.Context, code string, state string, _ string) (*pos.Connection, error) {
	salonID, nonceHash, err := decodeState(state, s.stateSecret, time.Now().UTC())
	if err != nil {
		return nil, err
	}
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
	return &StatusResponse{Connection: connection, SyncLogs: logs, Readiness: readiness}, nil
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

func (s *Service) Readiness(ctx context.Context, salonID string, ownerUserID string) (*ReadinessStatus, error) {
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
	latest, err := s.latestTestBooking(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	return buildReadiness(aiEnabled, connection, services, staff, latest), nil
}

func (s *Service) CreateTestBooking(ctx context.Context, salonID string, ownerUserID string, req TestBookingRequest) (*TestBookingResponse, error) {
	if s.bookingService == nil {
		return nil, ErrBookingServiceUnavailable
	}
	req = normalizeTestBookingRequest(salonID, req)
	if req.SalonID == "" || req.ServiceID == "" || req.StaffID == "" || req.StartTime.IsZero() {
		return nil, ErrValidation
	}
	readiness, err := s.Readiness(ctx, req.SalonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if !readiness.CanTestBooking {
		return nil, ErrReadinessGate
	}
	attempt, err := s.bookingService.Create(ctx, req.SalonID, ownerUserID, booking.CreateBookingRequest{
		Source:        booking.SourceSquareTestBooking,
		CustomerName:  req.CustomerName,
		CustomerPhone: req.CustomerPhone,
		CustomerEmail: req.CustomerEmail,
		ServiceID:     req.ServiceID,
		StaffID:       req.StaffID,
		StartTime:     req.StartTime,
		Notes:         req.Notes,
	})
	if err != nil {
		return nil, err
	}
	if attempt == nil {
		return nil, fmt.Errorf("booking service did not return a booking attempt")
	}
	readiness, err = s.Readiness(ctx, req.SalonID, ownerUserID)
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

func (s *Service) CancelTestBooking(ctx context.Context, salonID string, ownerUserID string, req CancelTestBookingRequest) (*TestBookingResponse, error) {
	if s.bookingService == nil {
		return nil, ErrBookingServiceUnavailable
	}
	req = normalizeCancelTestBookingRequest(salonID, req)
	if req.SalonID == "" {
		return nil, ErrValidation
	}
	latest, err := s.latestTestBooking(ctx, req.SalonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	appointmentID := strings.TrimSpace(req.AppointmentID)
	if appointmentID == "" && latest != nil {
		appointmentID = latest.AppointmentID
	}
	if appointmentID == "" || latest == nil || latest.AppointmentStatus == booking.StatusCancelled {
		return nil, ErrReadinessGate
	}
	appointment, fallback, err := s.bookingService.Cancel(ctx, req.SalonID, ownerUserID, appointmentID, booking.CancelRequest{
		Reason: req.Reason,
		Source: booking.SourceSquareTestBooking,
	})
	if err != nil {
		return nil, err
	}
	readiness, err := s.Readiness(ctx, req.SalonID, ownerUserID)
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

func (s *Service) EnableAIBooking(ctx context.Context, salonID string, ownerUserID string) (*GateResponse, error) {
	readiness, err := s.Readiness(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
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

func buildReadiness(aiEnabled bool, connection *pos.Connection, services []pos.Service, staff []pos.StaffMember, latest *booking.TestBookingRecord) *ReadinessStatus {
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
	servicesReady := synced && serviceCount > 0
	staffReady := synced && staffCount > 0
	canTest := connected && locationSelected && synced && servicesReady && staffReady
	canCancel := latest != nil &&
		latest.AppointmentID != "" &&
		latest.POSBookingID != "" &&
		latest.AppointmentStatus != booking.StatusCancelled
	testCancelled := latest != nil &&
		latest.Status == booking.StatusCancelled &&
		latest.AppointmentStatus == booking.StatusCancelled
	canEnable := canTest && testCancelled

	checks := []ReadinessCheck{
		{Key: "connect_square", Label: "Connect Square", Complete: connected, Message: incompleteMessage(connected, "Square Appointments is not connected.")},
		{Key: "select_location", Label: "Select location", Complete: locationSelected, Message: incompleteMessage(locationSelected, "Choose the Square location for this salon.")},
		{Key: "sync_services", Label: "Sync services", Complete: servicesReady, Message: incompleteMessage(servicesReady, "Sync at least one active AI-bookable service.")},
		{Key: "sync_staff", Label: "Sync staff", Complete: staffReady, Message: incompleteMessage(staffReady, "Sync at least one active AI-bookable staff member.")},
		{Key: "create_test_booking", Label: "Create test booking", Complete: latest != nil && latest.POSBookingID != "" && latest.Status != booking.StatusFallbackPending, Message: incompleteMessage(latest != nil && latest.POSBookingID != "" && latest.Status != booking.StatusFallbackPending, "Create a real Square test booking.")},
		{Key: "cancel_test_booking", Label: "Cancel test booking", Complete: testCancelled, Message: incompleteMessage(testCancelled, "Cancel the latest Square test booking before enabling AI booking.")},
		{Key: "enable_ai_booking", Label: "Enable AI booking", Complete: aiEnabled, Message: incompleteMessage(aiEnabled, "AI booking is disabled until all safety checks pass.")},
	}

	return &ReadinessStatus{
		AIEnabled:            aiEnabled,
		CanTestBooking:       canTest,
		CanCancelTestBooking: canCancel,
		CanEnableAIBooking:   canEnable,
		ServiceCount:         serviceCount,
		StaffCount:           staffCount,
		LatestTestBooking:    latest,
		Checks:               checks,
	}
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
	req.SalonID = defaultString(strings.TrimSpace(req.SalonID), strings.TrimSpace(salonID))
	req.CustomerName = defaultString(strings.TrimSpace(req.CustomerName), "ManleAI Test Customer")
	req.CustomerPhone = defaultString(strings.TrimSpace(req.CustomerPhone), "+13125550199")
	req.CustomerEmail = strings.TrimSpace(req.CustomerEmail)
	req.ServiceID = strings.TrimSpace(req.ServiceID)
	req.StaffID = strings.TrimSpace(req.StaffID)
	req.Notes = defaultString(strings.TrimSpace(req.Notes), "AI booking readiness test. Cancel after verifying Square booking creation.")
	return req
}

func normalizeCancelTestBookingRequest(salonID string, req CancelTestBookingRequest) CancelTestBookingRequest {
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
