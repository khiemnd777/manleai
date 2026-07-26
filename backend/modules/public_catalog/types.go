package public_catalog

type Catalog struct {
	Salon                      PublicSalon                `json:"salon"`
	SchedulingAuthority        string                     `json:"scheduling_authority"`
	SchedulingAuthorityVersion int64                      `json:"scheduling_authority_version"`
	Services                   []PublicService            `json:"services"`
	Staff                      []PublicStaffMember        `json:"staff"`
	Hours                      []PublicBusinessHourPeriod `json:"hours"`
	BookingNote                string                     `json:"booking_note"`
}

type PublicSalon struct {
	Slug              string `json:"slug"`
	Name              string `json:"name"`
	Phone             string `json:"phone"`
	Address           string `json:"address,omitempty"`
	City              string `json:"city,omitempty"`
	State             string `json:"state,omitempty"`
	ZipCode           string `json:"zip_code,omitempty"`
	Timezone          string `json:"timezone"`
	PrimaryLanguage   string `json:"primary_language"`
	SecondaryLanguage string `json:"secondary_language,omitempty"`
}

type PublicService struct {
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	AIDescription   string   `json:"ai_description,omitempty"`
	DurationMinutes int      `json:"duration_minutes"`
	PriceFrom       *float64 `json:"price_from,omitempty"`
	PriceDisplay    string   `json:"price_display,omitempty"`
}

type PublicStaffMember struct {
	Name string `json:"name"`
}

type PublicBusinessHourPeriod struct {
	DayOfWeek      int    `json:"day_of_week"`
	StartLocalTime string `json:"start_local_time"`
	EndLocalTime   string `json:"end_local_time"`
	Source         string `json:"source"`
}
