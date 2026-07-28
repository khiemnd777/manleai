package auth

import "time"

type User struct {
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	PasswordHash   string    `json:"-"`
	FullName       string    `json:"full_name"`
	Phone          string    `json:"phone,omitempty"`
	Status         string    `json:"status"`
	PrincipalScope string    `json:"principal_scope"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type BootstrapOwnerRequest struct {
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Password string `json:"password"`
}

type BootstrapStatusResponse struct {
	Available bool `json:"available"`
}

type LoginResponse struct {
	AccessToken    string    `json:"access_token"`
	RefreshToken   string    `json:"-"`
	ExpiresAt      time.Time `json:"expires_at"`
	User           User      `json:"user"`
	Roles          []string  `json:"roles"`
	SalonID        string    `json:"salon_id,omitempty"`
	PrincipalScope string    `json:"principal_scope"`
}

type MeResponse struct {
	User           User     `json:"user"`
	Roles          []string `json:"roles"`
	SalonID        string   `json:"salon_id,omitempty"`
	PrincipalScope string   `json:"principal_scope"`
}
