package salon

import "time"

type Salon struct {
	ID                   string    `json:"id"`
	Name                 string    `json:"name"`
	Phone                string    `json:"phone"`
	Address              string    `json:"address,omitempty"`
	City                 string    `json:"city,omitempty"`
	State                string    `json:"state,omitempty"`
	ZipCode              string    `json:"zip_code,omitempty"`
	Timezone             string    `json:"timezone"`
	OwnerUserID          string    `json:"owner_user_id"`
	PrimaryLanguage      string    `json:"primary_language"`
	SecondaryLanguage    string    `json:"secondary_language"`
	HandoffPhone         string    `json:"handoff_phone,omitempty"`
	AIEnabled            bool      `json:"ai_enabled"`
	ActivePOSProvider    string    `json:"active_pos_provider"`
	PublicSlug           string    `json:"public_slug,omitempty"`
	PublicCatalogEnabled bool      `json:"public_catalog_enabled"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type CreateSalonRequest struct {
	Name              string `json:"name"`
	Phone             string `json:"phone"`
	Address           string `json:"address"`
	City              string `json:"city"`
	State             string `json:"state"`
	ZipCode           string `json:"zip_code"`
	Timezone          string `json:"timezone"`
	PrimaryLanguage   string `json:"primary_language"`
	SecondaryLanguage string `json:"secondary_language"`
	HandoffPhone      string `json:"handoff_phone"`
}

type UpdateSalonRequest struct {
	Name              string `json:"name"`
	Phone             string `json:"phone"`
	Address           string `json:"address"`
	City              string `json:"city"`
	State             string `json:"state"`
	ZipCode           string `json:"zip_code"`
	Timezone          string `json:"timezone"`
	PrimaryLanguage   string `json:"primary_language"`
	SecondaryLanguage string `json:"secondary_language"`
	HandoffPhone      string `json:"handoff_phone"`
	AIEnabled         bool   `json:"ai_enabled"`
}

type Settings struct {
	ID                      string    `json:"id"`
	SalonID                 string    `json:"salon_id"`
	AIGreeting              string    `json:"ai_greeting"`
	AIVoice                 string    `json:"ai_voice"`
	BookingMode             string    `json:"booking_mode"`
	RecordingEnabled        bool      `json:"recording_enabled"`
	RecordingConsentMessage string    `json:"recording_consent_message"`
	SMSConfirmationEnabled  bool      `json:"sms_confirmation_enabled"`
	SMSReminderEnabled      bool      `json:"sms_reminder_enabled"`
	ReminderHoursBefore     int       `json:"reminder_hours_before"`
	HandoffEnabled          bool      `json:"handoff_enabled"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type UpdateSettingsRequest struct {
	AIGreeting              string `json:"ai_greeting"`
	AIVoice                 string `json:"ai_voice"`
	BookingMode             string `json:"booking_mode"`
	RecordingEnabled        bool   `json:"recording_enabled"`
	RecordingConsentMessage string `json:"recording_consent_message"`
	SMSConfirmationEnabled  bool   `json:"sms_confirmation_enabled"`
	SMSReminderEnabled      bool   `json:"sms_reminder_enabled"`
	ReminderHoursBefore     int    `json:"reminder_hours_before"`
	HandoffEnabled          bool   `json:"handoff_enabled"`
}

type PublicCatalogSettings struct {
	SalonID              string    `json:"salon_id"`
	PublicSlug           string    `json:"public_slug,omitempty"`
	PublicCatalogEnabled bool      `json:"public_catalog_enabled"`
	PublicPath           string    `json:"public_path,omitempty"`
	BookableServiceCount int       `json:"bookable_service_count"`
	BookableStaffCount   int       `json:"bookable_staff_count"`
	CanPublish           bool      `json:"can_publish"`
	BlockedReason        string    `json:"blocked_reason,omitempty"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type UpdatePublicCatalogRequest struct {
	PublicSlug           string `json:"public_slug"`
	PublicCatalogEnabled bool   `json:"public_catalog_enabled"`
}

type BusinessHour struct {
	ID        string    `json:"id"`
	SalonID   string    `json:"salon_id"`
	DayOfWeek int       `json:"day_of_week"`
	OpenTime  string    `json:"open_time,omitempty"`
	CloseTime string    `json:"close_time,omitempty"`
	IsClosed  bool      `json:"is_closed"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type BusinessHourPeriod struct {
	ID                  string     `json:"id,omitempty"`
	SalonID             string     `json:"salon_id,omitempty"`
	DayOfWeek           int        `json:"day_of_week"`
	StartLocalTime      string     `json:"start_local_time"`
	EndLocalTime        string     `json:"end_local_time"`
	Source              string     `json:"source"`
	Provider            string     `json:"provider,omitempty"`
	ProviderLocationID  string     `json:"provider_location_id,omitempty"`
	ProviderPeriodIndex int        `json:"provider_period_index"`
	LastSyncedAt        *time.Time `json:"last_synced_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type UpdateBusinessHoursRequest struct {
	Hours []BusinessHourInput `json:"hours"`
}

type BusinessHourInput struct {
	DayOfWeek int    `json:"day_of_week"`
	OpenTime  string `json:"open_time"`
	CloseTime string `json:"close_time"`
	IsClosed  bool   `json:"is_closed"`
}
