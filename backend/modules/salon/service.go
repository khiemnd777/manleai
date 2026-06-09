package salon

import (
	"context"
	"errors"
	"strings"
)

var ErrValidation = errors.New("validation failed")

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
	req = normalizeCreate(req)
	if req.Name == "" || req.Phone == "" {
		return nil, ErrValidation
	}
	return s.repo.Create(ctx, ownerUserID, req)
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
	req.BookingMode = defaultString(strings.TrimSpace(req.BookingMode), "pending_approval")
	req.RecordingConsentMessage = strings.TrimSpace(req.RecordingConsentMessage)
	if req.AIGreeting == "" || req.RecordingConsentMessage == "" || req.ReminderHoursBefore <= 0 {
		return nil, ErrValidation
	}
	if req.BookingMode != "confirmed_booking" && req.BookingMode != "pending_approval" && req.BookingMode != "disabled" {
		return nil, ErrValidation
	}
	return s.repo.UpdateSettings(ctx, salonID, ownerUserID, req)
}

func (s *Service) GetBusinessHours(ctx context.Context, salonID string, ownerUserID string) ([]BusinessHour, error) {
	return s.repo.GetBusinessHours(ctx, salonID, ownerUserID)
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
