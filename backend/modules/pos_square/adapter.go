package pos_square

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/encryption"
	"github.com/manleai/ai-receptionist/modules/pos"
)

var (
	ErrNotConnected     = errors.New("square is not connected")
	ErrBookingMilestone = errors.New("square booking operations are implemented in milestone 3")
)

type SquareAdapter struct {
	cfg        config.SquareConfig
	repo       *pos.Repository
	cipher     *encryption.TokenCipher
	httpClient *http.Client
}

func NewSquareAdapter(cfg config.SquareConfig, repo *pos.Repository, cipher *encryption.TokenCipher) *SquareAdapter {
	return &SquareAdapter{
		cfg:    cfg,
		repo:   repo,
		cipher: cipher,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (a *SquareAdapter) Name() string {
	return pos.ProviderSquare
}

func (a *SquareAdapter) OAuthURL(state string) (string, error) {
	if a.cfg.ClientID == "" || a.cfg.RedirectURL == "" {
		return "", errors.New("square oauth is not configured")
	}
	values := url.Values{}
	values.Set("client_id", a.cfg.ClientID)
	values.Set("scope", strings.Join(squareScopes(), " "))
	values.Set("session", "false")
	values.Set("state", state)
	values.Set("redirect_uri", a.cfg.RedirectURL)
	return a.oauthBaseURL() + "/oauth2/authorize?" + values.Encode(), nil
}

func (a *SquareAdapter) Connect(ctx context.Context, input pos.ConnectInput) (*pos.Connection, error) {
	if a.cfg.ClientID == "" || a.cfg.ClientSecret == "" {
		return nil, errors.New("square oauth credentials are not configured")
	}

	payload := map[string]string{
		"client_id":     a.cfg.ClientID,
		"client_secret": a.cfg.ClientSecret,
		"code":          input.Code,
		"grant_type":    "authorization_code",
		"redirect_uri":  input.RedirectURL,
	}
	var tokenResponse squareTokenResponse
	if err := a.doJSON(ctx, http.MethodPost, a.apiBaseURL()+"/oauth2/token", "", payload, &tokenResponse); err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      input.SalonID,
			Provider:     pos.ProviderSquare,
			Operation:    "oauth_token_exchange",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, err
	}

	accessTokenEncrypted, err := a.cipher.Encrypt(tokenResponse.AccessToken)
	if err != nil {
		return nil, err
	}
	refreshTokenEncrypted, err := a.cipher.Encrypt(tokenResponse.RefreshToken)
	if err != nil {
		return nil, err
	}

	scopes := strings.Fields(tokenResponse.Scope)
	if len(scopes) == 0 {
		scopes = squareScopes()
	}

	return a.repo.UpsertConnection(ctx, pos.Connection{
		SalonID:               input.SalonID,
		Provider:              pos.ProviderSquare,
		Status:                pos.StatusConnected,
		AccessTokenEncrypted:  accessTokenEncrypted,
		RefreshTokenEncrypted: refreshTokenEncrypted,
		MerchantID:            tokenResponse.MerchantID,
		Scopes:                scopes,
	})
}

func (a *SquareAdapter) HealthCheck(ctx context.Context, salonID string) error {
	_, err := a.ListLocations(ctx, salonID)
	return err
}

func (a *SquareAdapter) ListLocations(ctx context.Context, salonID string) ([]pos.Location, error) {
	token, err := a.accessToken(ctx, salonID)
	if err != nil {
		return nil, err
	}
	var response squareLocationsResponse
	if err := a.doJSON(ctx, http.MethodGet, a.apiBaseURL()+"/v2/locations", token, nil, &response); err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "list_locations",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, err
	}
	locations := make([]pos.Location, 0, len(response.Locations))
	for _, item := range response.Locations {
		locations = append(locations, pos.Location{
			ID:       item.ID,
			Name:     item.Name,
			Timezone: item.Timezone,
			Address:  item.Address.String(),
			Status:   item.Status,
		})
	}
	return locations, nil
}

func (a *SquareAdapter) ListServices(ctx context.Context, salonID string) ([]pos.Service, error) {
	token, err := a.accessToken(ctx, salonID)
	if err != nil {
		return nil, err
	}
	var response squareCatalogResponse
	if err := a.doJSON(ctx, http.MethodGet, a.apiBaseURL()+"/v2/catalog/list?types=ITEM,ITEM_VARIATION", token, nil, &response); err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "list_services",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, err
	}

	var services []pos.Service
	for _, object := range response.Objects {
		if object.Type != "ITEM" {
			continue
		}
		item := object.ItemData
		if len(item.Variations) == 0 {
			services = append(services, pos.Service{
				POSProvider:     pos.ProviderSquare,
				POSServiceID:    object.ID,
				Name:            item.Name,
				Description:     item.Description,
				AIDescription:   item.Description,
				DurationMinutes: 0,
				PriceDisplay:    "starting at",
				AIBookable:      true,
				Active:          !object.IsDeleted,
			})
			continue
		}
		for _, variation := range item.Variations {
			price := float64(variation.ItemVariationData.PriceMoney.Amount) / 100
			name := item.Name
			if variation.ItemVariationData.Name != "" && variation.ItemVariationData.Name != "Regular" {
				name = item.Name + " - " + variation.ItemVariationData.Name
			}
			services = append(services, pos.Service{
				POSProvider:     pos.ProviderSquare,
				POSServiceID:    variation.ID,
				Name:            name,
				Description:     item.Description,
				AIDescription:   item.Description,
				DurationMinutes: int(variation.ItemVariationData.ServiceDuration / 60000),
				PriceFrom:       price,
				PriceDisplay:    fmt.Sprintf("starting at $%.2f", price),
				AIBookable:      true,
				Active:          !object.IsDeleted && !variation.IsDeleted,
			})
		}
	}
	return services, nil
}

func (a *SquareAdapter) ListStaff(ctx context.Context, salonID string) ([]pos.StaffMember, error) {
	token, err := a.accessToken(ctx, salonID)
	if err != nil {
		return nil, err
	}
	var response squareTeamMembersResponse
	if err := a.doJSON(ctx, http.MethodPost, a.apiBaseURL()+"/v2/team-members/search", token, map[string]any{}, &response); err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "list_staff",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, err
	}

	staff := make([]pos.StaffMember, 0, len(response.TeamMembers))
	for _, member := range response.TeamMembers {
		staff = append(staff, pos.StaffMember{
			POSProvider: pos.ProviderSquare,
			POSStaffID:  member.ID,
			Name:        strings.TrimSpace(member.GivenName + " " + member.FamilyName),
			Phone:       member.PhoneNumber,
			Email:       member.EmailAddress,
			AIBookable:  true,
			Active:      member.Status == "" || member.Status == "ACTIVE",
		})
	}
	return staff, nil
}

func (a *SquareAdapter) SearchCustomerByPhone(ctx context.Context, salonID string, phone string) (*pos.Customer, error) {
	return nil, ErrBookingMilestone
}

func (a *SquareAdapter) CreateCustomer(ctx context.Context, salonID string, input pos.CreateCustomerInput) (*pos.Customer, error) {
	return nil, ErrBookingMilestone
}

func (a *SquareAdapter) CheckAvailability(ctx context.Context, salonID string, input pos.AvailabilityInput) ([]pos.TimeSlot, error) {
	return nil, ErrBookingMilestone
}

func (a *SquareAdapter) CreateAppointment(ctx context.Context, salonID string, input pos.CreateAppointmentInput) (*pos.Appointment, error) {
	return nil, ErrBookingMilestone
}

func (a *SquareAdapter) RescheduleAppointment(ctx context.Context, salonID string, appointmentID string, input pos.RescheduleInput) error {
	return ErrBookingMilestone
}

func (a *SquareAdapter) CancelAppointment(ctx context.Context, salonID string, appointmentID string, reason string) error {
	return ErrBookingMilestone
}

func (a *SquareAdapter) Sync(ctx context.Context, salonID string) error {
	services, err := a.ListServices(ctx, salonID)
	if err != nil {
		return err
	}
	if err := a.repo.UpsertServices(ctx, salonID, services); err != nil {
		return err
	}
	staff, err := a.ListStaff(ctx, salonID)
	if err != nil {
		return err
	}
	return a.repo.UpsertStaff(ctx, salonID, staff)
}

func (a *SquareAdapter) accessToken(ctx context.Context, salonID string) (string, error) {
	connection, err := a.repo.GetConnection(ctx, salonID, pos.ProviderSquare)
	if err != nil {
		return "", ErrNotConnected
	}
	if connection.AccessTokenEncrypted == "" {
		return "", ErrNotConnected
	}
	return a.cipher.Decrypt(connection.AccessTokenEncrypted)
}

func (a *SquareAdapter) doJSON(ctx context.Context, method string, endpoint string, bearerToken string, input any, output any) error {
	var body *bytes.Reader
	if input == nil {
		body = bytes.NewReader(nil)
	} else {
		payload, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	res, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var squareErr squareErrorResponse
		_ = json.NewDecoder(res.Body).Decode(&squareErr)
		if len(squareErr.Errors) > 0 {
			return fmt.Errorf("square %s: %s", squareErr.Errors[0].Code, squareErr.Errors[0].Detail)
		}
		return fmt.Errorf("square returned status %d", res.StatusCode)
	}
	if output == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(output)
}

func (a *SquareAdapter) apiBaseURL() string {
	if a.cfg.Environment == "production" {
		return "https://connect.squareup.com"
	}
	return "https://connect.squareupsandbox.com"
}

func (a *SquareAdapter) oauthBaseURL() string {
	return a.apiBaseURL()
}

func squareScopes() []string {
	return []string{
		"APPOINTMENTS_READ",
		"APPOINTMENTS_WRITE",
		"APPOINTMENTS_BUSINESS_SETTINGS_READ",
		"CUSTOMERS_READ",
		"CUSTOMERS_WRITE",
		"ITEMS_READ",
		"MERCHANT_PROFILE_READ",
		"EMPLOYEES_READ",
	}
}

func normalizeSquareError(err error) string {
	if err == nil {
		return pos.ErrorUnknown
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "unauthorized"), strings.Contains(msg, "expired"):
		return pos.ErrorTokenExpired
	case strings.Contains(msg, "forbidden"), strings.Contains(msg, "permission"):
		return pos.ErrorPermissionDenied
	case strings.Contains(msg, "rate"):
		return pos.ErrorRateLimited
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return pos.ErrorTimeout
	default:
		return pos.ErrorUnknown
	}
}

type squareTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	MerchantID   string `json:"merchant_id"`
	Scope        string `json:"scope"`
}

type squareErrorResponse struct {
	Errors []struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	} `json:"errors"`
}

type squareLocationsResponse struct {
	Locations []struct {
		ID       string        `json:"id"`
		Name     string        `json:"name"`
		Status   string        `json:"status"`
		Timezone string        `json:"timezone"`
		Address  squareAddress `json:"address"`
	} `json:"locations"`
}

type squareAddress struct {
	AddressLine1                 string `json:"address_line_1"`
	Locality                     string `json:"locality"`
	AdministrativeDistrictLevel1 string `json:"administrative_district_level_1"`
	PostalCode                   string `json:"postal_code"`
}

func (a squareAddress) String() string {
	parts := []string{a.AddressLine1, a.Locality, a.AdministrativeDistrictLevel1, a.PostalCode}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, strings.TrimSpace(part))
		}
	}
	return strings.Join(out, ", ")
}

type squareCatalogResponse struct {
	Objects []squareCatalogObject `json:"objects"`
}

type squareCatalogObject struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	IsDeleted bool   `json:"is_deleted"`
	ItemData  struct {
		Name        string                `json:"name"`
		Description string                `json:"description"`
		Variations  []squareCatalogObject `json:"variations"`
	} `json:"item_data"`
	ItemVariationData struct {
		Name            string `json:"name"`
		ServiceDuration int64  `json:"service_duration"`
		PriceMoney      struct {
			Amount   int64  `json:"amount"`
			Currency string `json:"currency"`
		} `json:"price_money"`
	} `json:"item_variation_data"`
}

type squareTeamMembersResponse struct {
	TeamMembers []struct {
		ID           string `json:"id"`
		GivenName    string `json:"given_name"`
		FamilyName   string `json:"family_name"`
		EmailAddress string `json:"email_address"`
		PhoneNumber  string `json:"phone_number"`
		Status       string `json:"status"`
	} `json:"team_members"`
}
