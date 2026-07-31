package salon

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/manleai/ai-receptionist/modules/booking"
)

var (
	ErrValidation                 = errors.New("validation failed")
	ErrCreateOperationConflict    = errors.New("salon create operation conflicts with existing payload")
	ErrPublicCatalogNotReady      = errors.New("public catalog is not ready for the selected scheduling authority")
	ErrSchedulingAuthorityChanged = errors.New("scheduling authority changed while updating public catalog")
)

const (
	DefaultAITone               = "professional_warm"
	AIToneProfessionalWarm      = "professional_warm"
	AIToneNaturalHuman          = "natural_human"
	AIToneFriendlyYoung         = "friendly_young"
	AIToneConciseCalm           = "concise_calm"
	maxCreateOperationKeyLength = 256
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) List(ctx context.Context, ownerUserID string) ([]Salon, error) {
	return s.repo.ListForOwner(ctx, ownerUserID)
}

func (s *Service) Get(ctx context.Context, id string, ownerUserID string) (*Salon, error) {
	return s.repo.GetForOwner(ctx, id, ownerUserID)
}

func (s *Service) Create(ctx context.Context, ownerUserID string, req CreateSalonRequest) (*Salon, error) {
	req, fingerprint, err := prepareCreate(req)
	if err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, ownerUserID, req, fingerprint)
}

// CreateInTx lets an owning cross-aggregate workflow include the established
// salon onboarding primitive in one transaction. The caller owns commit and
// rollback; validation, idempotency, membership, settings, and defaults remain
// salon-domain responsibilities.
func (s *Service) CreateInTx(ctx context.Context, tx *sql.Tx, ownerUserID string, req CreateSalonRequest) (*Salon, error) {
	req, fingerprint, err := prepareCreate(req)
	if err != nil {
		return nil, err
	}
	return s.repo.CreateInTx(ctx, tx, ownerUserID, req, fingerprint)
}

func prepareCreate(req CreateSalonRequest) (CreateSalonRequest, string, error) {
	req = normalizeCreate(req)
	if req.Name == "" || req.Phone == "" || !validCreateOperationKey(req.OperationKey) || !validSchedulingAuthority(req.SchedulingAuthority) {
		return CreateSalonRequest{}, "", ErrValidation
	}
	fingerprint, err := createSalonPayloadFingerprint(req)
	if err != nil {
		return CreateSalonRequest{}, "", err
	}
	return req, fingerprint, nil
}

func (s *Service) Update(ctx context.Context, id string, ownerUserID string, req UpdateSalonRequest) (*Salon, error) {
	req = normalizeUpdate(req)
	if req.Name == "" || req.Phone == "" {
		return nil, ErrValidation
	}
	return s.repo.Update(ctx, id, ownerUserID, req)
}

func (s *Service) GetSettings(ctx context.Context, salonID string, ownerUserID string) (*Settings, error) {
	return s.repo.GetSettings(ctx, salonID, ownerUserID)
}

func (s *Service) UpdateSettings(ctx context.Context, salonID string, ownerUserID string, req UpdateSettingsRequest) (*Settings, error) {
	req.AIGreeting = strings.TrimSpace(req.AIGreeting)
	req.AIVoice = defaultString(strings.TrimSpace(req.AIVoice), "professional_female")
	req.AITone = defaultString(strings.TrimSpace(req.AITone), DefaultAITone)
	req.BookingMode = defaultString(strings.TrimSpace(req.BookingMode), "pending_approval")
	req.RecordingConsentMessage = strings.TrimSpace(req.RecordingConsentMessage)
	if req.AIGreeting == "" || req.RecordingConsentMessage == "" || req.ReminderHoursBefore <= 0 {
		return nil, ErrValidation
	}
	if !validAITone(req.AITone) {
		return nil, ErrValidation
	}
	if req.BookingMode != "confirmed_booking" && req.BookingMode != "pending_approval" && req.BookingMode != "disabled" {
		return nil, ErrValidation
	}
	if req.ConsultationEnabled {
		readyCount, err := s.repo.CountConsultationReadyServices(ctx, salonID, ownerUserID)
		if err != nil {
			return nil, err
		}
		if !consultationCanBeEnabled(req.ConsultationEnabled, readyCount) {
			return nil, ErrValidation
		}
	}
	return s.repo.UpdateSettings(ctx, salonID, ownerUserID, req)
}

func consultationCanBeEnabled(requested bool, readyServiceCount int) bool {
	return !requested || readyServiceCount > 0
}

func (s *Service) GetPublicCatalogSettings(ctx context.Context, salonID string, ownerUserID string) (*PublicCatalogSettings, error) {
	return s.repo.GetPublicCatalogSettings(ctx, salonID, ownerUserID)
}

func (s *Service) UpdatePublicCatalogSettings(ctx context.Context, salonID string, ownerUserID string, req UpdatePublicCatalogRequest) (*PublicCatalogSettings, error) {
	req.PublicSlug = normalizePublicSlug(req.PublicSlug)
	if req.PublicCatalogEnabled && req.PublicSlug == "" {
		return nil, ErrValidation
	}
	if req.ExpectedSchedulingAuthorityVersion < 0 {
		return nil, ErrValidation
	}
	return s.repo.UpdatePublicCatalogSettings(ctx, salonID, ownerUserID, req)
}

func (s *Service) GetBusinessHours(ctx context.Context, salonID string, ownerUserID string) ([]BusinessHourPeriod, error) {
	return s.repo.GetBusinessHourPeriods(ctx, salonID, ownerUserID)
}

func (s *Service) UpdateBusinessHours(ctx context.Context, salonID string, ownerUserID string, req UpdateBusinessHoursRequest) ([]BusinessHour, error) {
	if len(req.Hours) == 0 {
		return nil, ErrValidation
	}
	for _, hour := range req.Hours {
		if hour.DayOfWeek < 0 || hour.DayOfWeek > 6 {
			return nil, ErrValidation
		}
		if !hour.IsClosed && (strings.TrimSpace(hour.OpenTime) == "" || strings.TrimSpace(hour.CloseTime) == "") {
			return nil, ErrValidation
		}
	}
	return s.repo.UpdateBusinessHours(ctx, salonID, ownerUserID, req)
}

func normalizeCreate(req CreateSalonRequest) CreateSalonRequest {
	req.OperationKey = strings.TrimSpace(req.OperationKey)
	req.SchedulingAuthority = defaultString(strings.TrimSpace(req.SchedulingAuthority), booking.SchedulingAuthorityOwnerManual)
	req.Name = strings.TrimSpace(req.Name)
	req.Phone = strings.TrimSpace(req.Phone)
	req.Address = strings.TrimSpace(req.Address)
	req.City = strings.TrimSpace(req.City)
	req.State = strings.TrimSpace(req.State)
	req.ZipCode = strings.TrimSpace(req.ZipCode)
	req.Timezone = defaultString(strings.TrimSpace(req.Timezone), "America/Chicago")
	req.PrimaryLanguage = defaultString(strings.TrimSpace(req.PrimaryLanguage), "en")
	req.SecondaryLanguage = defaultString(strings.TrimSpace(req.SecondaryLanguage), "vi")
	req.HandoffPhone = strings.TrimSpace(req.HandoffPhone)
	return req
}

func validCreateOperationKey(value string) bool {
	length := utf8.RuneCountInString(value)
	return value == strings.TrimSpace(value) && length >= 1 && length <= maxCreateOperationKeyLength
}

func validSchedulingAuthority(value string) bool {
	// Tenant onboarding creates only the safe request-only authority. Platform
	// Operations owns every later authority preview and commit.
	return value == booking.SchedulingAuthorityOwnerManual
}

func createSalonPayloadFingerprint(req CreateSalonRequest) (string, error) {
	payload, err := json.Marshal(struct {
		SchedulingAuthority string `json:"scheduling_authority"`
		Name                string `json:"name"`
		Phone               string `json:"phone"`
		Address             string `json:"address"`
		City                string `json:"city"`
		State               string `json:"state"`
		ZipCode             string `json:"zip_code"`
		Timezone            string `json:"timezone"`
		PrimaryLanguage     string `json:"primary_language"`
		SecondaryLanguage   string `json:"secondary_language"`
		HandoffPhone        string `json:"handoff_phone"`
	}{
		SchedulingAuthority: req.SchedulingAuthority,
		Name:                req.Name,
		Phone:               req.Phone,
		Address:             req.Address,
		City:                req.City,
		State:               req.State,
		ZipCode:             req.ZipCode,
		Timezone:            req.Timezone,
		PrimaryLanguage:     req.PrimaryLanguage,
		SecondaryLanguage:   req.SecondaryLanguage,
		HandoffPhone:        req.HandoffPhone,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func normalizeUpdate(req UpdateSalonRequest) UpdateSalonRequest {
	req.Name = strings.TrimSpace(req.Name)
	req.Phone = strings.TrimSpace(req.Phone)
	req.Address = strings.TrimSpace(req.Address)
	req.City = strings.TrimSpace(req.City)
	req.State = strings.TrimSpace(req.State)
	req.ZipCode = strings.TrimSpace(req.ZipCode)
	req.Timezone = defaultString(strings.TrimSpace(req.Timezone), "America/Chicago")
	req.PrimaryLanguage = defaultString(strings.TrimSpace(req.PrimaryLanguage), "en")
	req.SecondaryLanguage = defaultString(strings.TrimSpace(req.SecondaryLanguage), "vi")
	req.HandoffPhone = strings.TrimSpace(req.HandoffPhone)
	return req
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func validAITone(value string) bool {
	switch strings.TrimSpace(value) {
	case AIToneProfessionalWarm, AIToneNaturalHuman, AIToneFriendlyYoung, AIToneConciseCalm:
		return true
	default:
		return false
	}
}

func normalizePublicSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var builder strings.Builder
	previousHyphen := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			previousHyphen = false
		case unicode.IsDigit(r):
			builder.WriteRune(r)
			previousHyphen = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if builder.Len() > 0 && !previousHyphen {
				builder.WriteByte('-')
				previousHyphen = true
			}
		}
	}
	normalized := strings.Trim(builder.String(), "-")
	if len(normalized) < 3 || len(normalized) > 64 {
		return ""
	}
	return normalized
}
