package pos_square

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/encryption"
	"github.com/manleai/ai-receptionist/modules/pos"
)

var (
	ErrNotConnected        = errors.New("square is not connected")
	ErrLocationNotSelected = errors.New("square location is not selected")
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

func (a *SquareAdapter) Capabilities() pos.ProviderCapabilities {
	return pos.ProviderCapabilities{}
}

func (a *SquareAdapter) OAuthURL(state string) (string, error) {
	if a.cfg.ClientID == "" || a.cfg.RedirectURL == "" {
		return "", errors.New("square oauth is not configured")
	}
	values := url.Values{}
	values.Set("client_id", a.cfg.ClientID)
	values.Set("scope", strings.Join(squareScopes(), " "))
	if strings.EqualFold(strings.TrimSpace(a.cfg.Environment), "production") {
		values.Set("session", "false")
	}
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

	return mapCatalogServices(response), nil
}

func mapCatalogServices(response squareCatalogResponse) []pos.Service {
	var services []pos.Service
	for _, object := range response.Objects {
		if object.Type != "ITEM" {
			continue
		}
		item := object.ItemData
		if len(item.Variations) == 0 {
			services = append(services, pos.Service{
				POSProvider:       pos.ProviderSquare,
				POSServiceID:      object.ID,
				POSServiceVersion: object.Version,
				Name:              item.Name,
				Description:       item.Description,
				AIDescription:     item.Description,
				DurationMinutes:   0,
				PriceDisplay:      "starting at",
				AIBookable:        true,
				Active:            !object.IsDeleted,
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
				POSProvider:       pos.ProviderSquare,
				POSServiceID:      variation.ID,
				POSServiceVersion: variation.Version,
				Name:              name,
				Description:       item.Description,
				AIDescription:     item.Description,
				DurationMinutes:   int(variation.ItemVariationData.ServiceDuration / 60000),
				PriceFrom:         price,
				PriceDisplay:      fmt.Sprintf("starting at $%.2f", price),
				AIBookable:        true,
				Active:            !object.IsDeleted && !variation.IsDeleted,
			})
		}
	}
	return services
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
	token, err := a.accessToken(ctx, salonID)
	if err != nil {
		return nil, err
	}
	request, err := buildSquareCustomerSearchRequest(phone)
	if err != nil {
		return nil, err
	}
	var response squareCustomerSearchResponse
	if err := a.doJSON(ctx, http.MethodPost, a.apiBaseURL()+"/v2/customers/search", token, request, &response); err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "search_customer",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, err
	}
	if len(response.Customers) == 0 {
		return nil, nil
	}
	customer := mapSquareCustomer(response.Customers[0])
	return &customer, nil
}

func (a *SquareAdapter) CreateCustomer(ctx context.Context, salonID string, input pos.CreateCustomerInput) (*pos.Customer, error) {
	token, err := a.accessToken(ctx, salonID)
	if err != nil {
		return nil, err
	}
	request, err := buildSquareCreateCustomerRequest(input)
	if err != nil {
		return nil, err
	}
	var response squareCustomerResponse
	if err := a.doJSON(ctx, http.MethodPost, a.apiBaseURL()+"/v2/customers", token, request, &response); err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "create_customer",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, err
	}
	customer := mapSquareCustomer(response.Customer)
	if customer.POSCustomerID == "" {
		return nil, fmt.Errorf("square customer id was not returned")
	}
	return &customer, nil
}

func (a *SquareAdapter) CheckAvailability(ctx context.Context, salonID string, input pos.AvailabilityInput) ([]pos.TimeSlot, error) {
	token, locationID, err := a.accessTokenAndLocation(ctx, salonID)
	if err != nil {
		return nil, err
	}
	request, err := buildSquareAvailabilityRequest(locationID, input)
	if err != nil {
		return nil, err
	}
	var response squareAvailabilityResponse
	if err := a.doJSON(ctx, http.MethodPost, a.apiBaseURL()+"/v2/bookings/availability/search", token, request, &response); err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "check_availability",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, err
	}
	return mapSquareAvailabilities(response, availabilityFallbackDuration(input)), nil
}

func (a *SquareAdapter) CreateAppointment(ctx context.Context, salonID string, input pos.CreateAppointmentInput) (*pos.Appointment, error) {
	token, locationID, err := a.accessTokenAndLocation(ctx, salonID)
	if err != nil {
		return nil, err
	}
	request, err := buildSquareCreateBookingRequest(locationID, input)
	if err != nil {
		return nil, err
	}
	var response squareBookingResponse
	if err := a.doJSON(ctx, http.MethodPost, a.apiBaseURL()+"/v2/bookings", token, request, &response); err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "create_booking",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, err
	}
	if response.Booking.Version < 0 && strings.TrimSpace(response.Booking.ID) != "" {
		response.Booking, err = a.retrieveBooking(ctx, token, response.Booking.ID)
		if err != nil {
			_ = a.repo.LogError(ctx, pos.POSError{
				SalonID:      salonID,
				Provider:     pos.ProviderSquare,
				Operation:    "retrieve_created_booking",
				ErrorCode:    normalizeSquareError(err),
				ErrorMessage: err.Error(),
			})
			return nil, err
		}
	}
	appointment, err := mapSquareBooking(response.Booking, appointmentFallbackDuration(input.Segments, input.DurationMinutes))
	if err != nil {
		return nil, err
	}
	return appointment, nil
}

func (a *SquareAdapter) RescheduleAppointment(ctx context.Context, salonID string, appointmentID string, input pos.RescheduleInput) (*pos.Appointment, error) {
	token, locationID, err := a.accessTokenAndLocation(ctx, salonID)
	if err != nil {
		return nil, err
	}
	request, err := buildSquareUpdateBookingRequest(locationID, input)
	if err != nil {
		return nil, err
	}
	var response squareBookingResponse
	if err := a.doJSON(ctx, http.MethodPut, a.apiBaseURL()+"/v2/bookings/"+url.PathEscape(appointmentID), token, request, &response); err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "reschedule_booking",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, err
	}
	return mapSquareBooking(response.Booking, appointmentFallbackDuration(input.Segments, input.DurationMinutes))
}

func (a *SquareAdapter) CancelAppointment(ctx context.Context, salonID string, appointmentID string, input pos.CancelInput) (*pos.Appointment, error) {
	token, err := a.accessToken(ctx, salonID)
	if err != nil {
		return nil, err
	}
	request, err := buildSquareCancelBookingRequest(input)
	if err != nil {
		return nil, err
	}
	var response squareBookingResponse
	if err := a.doJSON(ctx, http.MethodPost, a.apiBaseURL()+"/v2/bookings/"+url.PathEscape(appointmentID)+"/cancel", token, request, &response); err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "cancel_booking",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, err
	}
	return mapSquareBooking(response.Booking, 0)
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

func (a *SquareAdapter) retrieveBooking(ctx context.Context, token string, bookingID string) (squareBooking, error) {
	var response squareBookingResponse
	if err := a.doJSON(ctx, http.MethodGet, a.apiBaseURL()+"/v2/bookings/"+url.PathEscape(bookingID), token, nil, &response); err != nil {
		return squareBooking{}, err
	}
	return response.Booking, nil
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

func (a *SquareAdapter) accessTokenAndLocation(ctx context.Context, salonID string) (string, string, error) {
	connection, err := a.repo.GetConnection(ctx, salonID, pos.ProviderSquare)
	if err != nil {
		return "", "", ErrNotConnected
	}
	if connection.AccessTokenEncrypted == "" {
		return "", "", ErrNotConnected
	}
	if strings.TrimSpace(connection.LocationID) == "" {
		return "", "", ErrLocationNotSelected
	}
	token, err := a.cipher.Decrypt(connection.AccessTokenEncrypted)
	if err != nil {
		return "", "", err
	}
	return token, connection.LocationID, nil
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
	if a.cfg.APIVersion != "" {
		req.Header.Set("Square-Version", a.cfg.APIVersion)
	}
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
	if strings.TrimSpace(a.cfg.APIBaseURL) != "" {
		return strings.TrimRight(a.cfg.APIBaseURL, "/")
	}
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
		"ITEMS_WRITE",
		"MERCHANT_PROFILE_READ",
		"EMPLOYEES_READ",
		"EMPLOYEES_WRITE",
	}
}

func normalizeSquareError(err error) string {
	if err == nil {
		return pos.ErrorUnknown
	}
	if errors.Is(err, ErrLocationNotSelected) {
		return pos.ErrorLocationNotSelected
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
	case strings.Contains(msg, "location"):
		return pos.ErrorLocationNotSelected
	case strings.Contains(msg, "conflict"), strings.Contains(msg, "overlap"):
		return pos.ErrorBookingConflict
	case strings.Contains(msg, "availability"):
		return pos.ErrorAvailabilityFailed
	default:
		return pos.ErrorUnknown
	}
}

func buildSquareCustomerSearchRequest(phone string) (squareCustomerSearchRequest, error) {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return squareCustomerSearchRequest{}, fmt.Errorf("phone is required")
	}
	return squareCustomerSearchRequest{
		Query: squareCustomerQuery{
			Filter: squareCustomerFilter{
				PhoneNumber: &squareCustomerTextFilter{Exact: phone},
			},
		},
		Limit: 1,
	}, nil
}

func buildSquareCreateCustomerRequest(input pos.CreateCustomerInput) (squareCreateCustomerRequest, error) {
	name := strings.TrimSpace(input.Name)
	phone := strings.TrimSpace(input.Phone)
	email := strings.TrimSpace(input.Email)
	if name == "" && phone == "" && email == "" {
		return squareCreateCustomerRequest{}, fmt.Errorf("customer name, phone, or email is required")
	}
	givenName, familyName := splitCustomerName(name)
	return squareCreateCustomerRequest{
		IdempotencyKey: uuid.NewString(),
		GivenName:      givenName,
		FamilyName:     familyName,
		EmailAddress:   email,
		PhoneNumber:    phone,
	}, nil
}

func buildSquareAvailabilityRequest(locationID string, input pos.AvailabilityInput) (squareAvailabilityRequest, error) {
	if strings.TrimSpace(locationID) == "" {
		return squareAvailabilityRequest{}, ErrLocationNotSelected
	}
	startAt, endAt, err := availabilityRange(input.PreferredDate)
	if err != nil {
		return squareAvailabilityRequest{}, err
	}
	segments, err := availabilitySegments(input)
	if err != nil {
		return squareAvailabilityRequest{}, err
	}
	filters := make([]squareSegmentFilter, 0, len(segments))
	for _, segment := range segments {
		filter := squareSegmentFilter{
			ServiceVariationID: strings.TrimSpace(segment.ServiceID),
		}
		if strings.TrimSpace(segment.StaffID) != "" {
			filter.TeamMemberIDFilter = &squareTeamMemberIDFilter{Any: []string{strings.TrimSpace(segment.StaffID)}}
		}
		filters = append(filters, filter)
	}
	return squareAvailabilityRequest{
		Query: squareAvailabilityQuery{
			Filter: squareAvailabilityFilter{
				StartAtRange: squareStartAtRange{
					StartAt: startAt.Format(time.RFC3339),
					EndAt:   endAt.Format(time.RFC3339),
				},
				LocationID:     strings.TrimSpace(locationID),
				SegmentFilters: filters,
			},
		},
	}, nil
}

func buildSquareCreateBookingRequest(locationID string, input pos.CreateAppointmentInput) (squareCreateBookingRequest, error) {
	if strings.TrimSpace(locationID) == "" {
		return squareCreateBookingRequest{}, ErrLocationNotSelected
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return squareCreateBookingRequest{}, fmt.Errorf("idempotency key is required")
	}
	if strings.TrimSpace(input.CustomerID) == "" || input.StartTime.IsZero() {
		return squareCreateBookingRequest{}, fmt.Errorf("customer and start time are required")
	}
	segments, err := appointmentSegments(input.Segments, input.ServiceID, input.ServiceVersion, input.StaffID, input.DurationMinutes)
	if err != nil {
		return squareCreateBookingRequest{}, err
	}
	return squareCreateBookingRequest{
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey),
		Booking: squareBooking{
			CustomerID:          strings.TrimSpace(input.CustomerID),
			StartAt:             input.StartTime.UTC().Format(time.RFC3339),
			LocationID:          strings.TrimSpace(locationID),
			CustomerNote:        strings.TrimSpace(input.Notes),
			AppointmentSegments: segments,
		},
	}, nil
}

func buildSquareUpdateBookingRequest(locationID string, input pos.RescheduleInput) (squareUpdateBookingRequest, error) {
	if strings.TrimSpace(locationID) == "" {
		return squareUpdateBookingRequest{}, ErrLocationNotSelected
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return squareUpdateBookingRequest{}, fmt.Errorf("idempotency key is required")
	}
	if input.BookingVersion < 0 {
		return squareUpdateBookingRequest{}, fmt.Errorf("square booking version is required")
	}
	if input.StartTime.IsZero() {
		return squareUpdateBookingRequest{}, fmt.Errorf("start time is required")
	}
	segments, err := appointmentSegments(input.Segments, input.ServiceID, input.ServiceVersion, input.StaffID, input.DurationMinutes)
	if err != nil {
		return squareUpdateBookingRequest{}, err
	}
	return squareUpdateBookingRequest{
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey),
		Booking: squareBooking{
			Version:             input.BookingVersion,
			StartAt:             input.StartTime.UTC().Format(time.RFC3339),
			LocationID:          strings.TrimSpace(locationID),
			CustomerNote:        strings.TrimSpace(input.Notes),
			AppointmentSegments: segments,
		},
	}, nil
}

func availabilitySegments(input pos.AvailabilityInput) ([]pos.AvailabilitySegmentInput, error) {
	segments := input.Segments
	if len(segments) == 0 {
		segments = []pos.AvailabilitySegmentInput{
			{
				ServiceID:       input.ServiceID,
				StaffID:         input.StaffID,
				DurationMinutes: input.DurationMinutes,
			},
		}
	}
	normalized := make([]pos.AvailabilitySegmentInput, 0, len(segments))
	for _, segment := range segments {
		segment.ServiceID = strings.TrimSpace(segment.ServiceID)
		segment.StaffID = strings.TrimSpace(segment.StaffID)
		if segment.ServiceID == "" {
			return nil, fmt.Errorf("service id is required")
		}
		normalized = append(normalized, segment)
	}
	return normalized, nil
}

func availabilityFallbackDuration(input pos.AvailabilityInput) int {
	if len(input.Segments) == 0 {
		return input.DurationMinutes
	}
	total := 0
	for _, segment := range input.Segments {
		if segment.DurationMinutes > 0 {
			total += segment.DurationMinutes
		}
	}
	if total > 0 {
		return total
	}
	return input.DurationMinutes
}

func appointmentSegments(input []pos.AppointmentSegmentInput, serviceID string, serviceVersion int64, staffID string, durationMinutes int) ([]squareAppointmentSegment, error) {
	segments := input
	if len(segments) == 0 {
		segments = []pos.AppointmentSegmentInput{
			{
				ServiceID:       serviceID,
				ServiceVersion:  serviceVersion,
				StaffID:         staffID,
				DurationMinutes: durationMinutes,
			},
		}
	}
	mapped := make([]squareAppointmentSegment, 0, len(segments))
	for _, segment := range segments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		staffID := strings.TrimSpace(segment.StaffID)
		if serviceID == "" || staffID == "" {
			return nil, fmt.Errorf("service and staff are required")
		}
		if segment.ServiceVersion <= 0 {
			return nil, fmt.Errorf("square service variation version is required")
		}
		if segment.DurationMinutes <= 0 {
			return nil, fmt.Errorf("duration minutes is required")
		}
		mapped = append(mapped, squareAppointmentSegment{
			DurationMinutes:         segment.DurationMinutes,
			TeamMemberID:            staffID,
			ServiceVariationID:      serviceID,
			ServiceVariationVersion: segment.ServiceVersion,
		})
	}
	return mapped, nil
}

func appointmentFallbackDuration(segments []pos.AppointmentSegmentInput, fallbackDuration int) int {
	total := 0
	for _, segment := range segments {
		if segment.DurationMinutes > 0 {
			total += segment.DurationMinutes
		}
	}
	if total > 0 {
		return total
	}
	return fallbackDuration
}

func buildSquareCancelBookingRequest(input pos.CancelInput) (squareCancelBookingRequest, error) {
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return squareCancelBookingRequest{}, fmt.Errorf("idempotency key is required")
	}
	if input.BookingVersion < 0 {
		return squareCancelBookingRequest{}, fmt.Errorf("square booking version is required")
	}
	return squareCancelBookingRequest{
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey),
		BookingVersion: input.BookingVersion,
	}, nil
}

func mapSquareCustomer(customer squareCustomer) pos.Customer {
	name := strings.TrimSpace(strings.TrimSpace(customer.GivenName + " " + customer.FamilyName))
	if name == "" {
		name = strings.TrimSpace(customer.CompanyName)
	}
	return pos.Customer{
		POSCustomerID: customer.ID,
		Name:          name,
		Phone:         customer.PhoneNumber,
		Email:         customer.EmailAddress,
	}
}

func mapSquareAvailabilities(response squareAvailabilityResponse, fallbackDuration int) []pos.TimeSlot {
	slots := make([]pos.TimeSlot, 0, len(response.Availabilities))
	for _, item := range response.Availabilities {
		startAt, err := time.Parse(time.RFC3339, item.StartAt)
		if err != nil {
			continue
		}
		duration := fallbackDuration
		staffID := ""
		if len(item.AppointmentSegments) > 0 {
			staffID = item.AppointmentSegments[0].TeamMemberID
			if segmentDuration := squareSegmentDuration(item.AppointmentSegments); segmentDuration > 0 {
				duration = segmentDuration
			}
		}
		if duration <= 0 {
			continue
		}
		slots = append(slots, pos.TimeSlot{
			StartTime: startAt,
			EndTime:   startAt.Add(time.Duration(duration) * time.Minute),
			StaffID:   staffID,
			Segments:  mapSquareTimeSlotSegments(item.AppointmentSegments),
		})
	}
	return slots
}

func mapSquareTimeSlotSegments(segments []squareAppointmentSegment) []pos.TimeSlotSegment {
	items := make([]pos.TimeSlotSegment, 0, len(segments))
	for _, segment := range segments {
		items = append(items, pos.TimeSlotSegment{
			ServiceID:       segment.ServiceVariationID,
			StaffID:         segment.TeamMemberID,
			DurationMinutes: segment.DurationMinutes,
		})
	}
	return items
}

func mapSquareBooking(booking squareBooking, fallbackDuration int) (*pos.Appointment, error) {
	if strings.TrimSpace(booking.ID) == "" {
		return nil, fmt.Errorf("square booking id was not returned")
	}
	var startAt time.Time
	if strings.TrimSpace(booking.StartAt) != "" {
		parsed, err := time.Parse(time.RFC3339, booking.StartAt)
		if err != nil {
			return nil, err
		}
		startAt = parsed
	} else if fallbackDuration > 0 {
		return nil, fmt.Errorf("square booking start time was not returned")
	}
	duration := fallbackDuration
	if segmentDuration := squareSegmentDuration(booking.AppointmentSegments); segmentDuration > 0 {
		duration = segmentDuration
	}
	if fallbackDuration > 0 && duration <= 0 {
		return nil, fmt.Errorf("square booking duration was not returned")
	}
	var endTime time.Time
	if !startAt.IsZero() && duration > 0 {
		endTime = startAt.Add(time.Duration(duration) * time.Minute)
	}
	return &pos.Appointment{
		POSAppointmentID:      booking.ID,
		POSAppointmentVersion: booking.Version,
		StartTime:             startAt,
		EndTime:               endTime,
		Status:                strings.ToLower(booking.Status),
	}, nil
}

func squareSegmentDuration(segments []squareAppointmentSegment) int {
	total := 0
	for _, segment := range segments {
		if segment.DurationMinutes > 0 {
			total += segment.DurationMinutes
		}
	}
	return total
}

func availabilityRange(preferredDate string) (time.Time, time.Time, error) {
	value := strings.TrimSpace(preferredDate)
	if value == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("preferred date is required")
	}
	if day, err := time.Parse("2006-01-02", value); err == nil {
		start := day.UTC()
		return start, start.Add(24 * time.Hour), nil
	}
	start, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start = start.UTC()
	return start, start.Add(24 * time.Hour), nil
}

func splitCustomerName(name string) (string, string) {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], strings.Join(parts[1:], " ")
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
	Version   int64  `json:"version"`
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

type squareCustomerSearchRequest struct {
	Query squareCustomerQuery `json:"query"`
	Limit int                 `json:"limit,omitempty"`
}

type squareCustomerQuery struct {
	Filter squareCustomerFilter `json:"filter"`
}

type squareCustomerFilter struct {
	PhoneNumber *squareCustomerTextFilter `json:"phone_number,omitempty"`
}

type squareCustomerTextFilter struct {
	Exact string `json:"exact,omitempty"`
	Fuzzy string `json:"fuzzy,omitempty"`
}

type squareCustomerSearchResponse struct {
	Customers []squareCustomer `json:"customers"`
}

type squareCustomerResponse struct {
	Customer squareCustomer `json:"customer"`
}

type squareCustomer struct {
	ID           string `json:"id"`
	GivenName    string `json:"given_name"`
	FamilyName   string `json:"family_name"`
	CompanyName  string `json:"company_name"`
	EmailAddress string `json:"email_address"`
	PhoneNumber  string `json:"phone_number"`
}

type squareCreateCustomerRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	GivenName      string `json:"given_name,omitempty"`
	FamilyName     string `json:"family_name,omitempty"`
	EmailAddress   string `json:"email_address,omitempty"`
	PhoneNumber    string `json:"phone_number,omitempty"`
}

type squareAvailabilityRequest struct {
	Query squareAvailabilityQuery `json:"query"`
}

type squareAvailabilityQuery struct {
	Filter squareAvailabilityFilter `json:"filter"`
}

type squareAvailabilityFilter struct {
	StartAtRange   squareStartAtRange    `json:"start_at_range"`
	LocationID     string                `json:"location_id"`
	SegmentFilters []squareSegmentFilter `json:"segment_filters"`
}

type squareStartAtRange struct {
	StartAt string `json:"start_at"`
	EndAt   string `json:"end_at"`
}

type squareSegmentFilter struct {
	ServiceVariationID string                    `json:"service_variation_id"`
	TeamMemberIDFilter *squareTeamMemberIDFilter `json:"team_member_id_filter,omitempty"`
}

type squareTeamMemberIDFilter struct {
	Any []string `json:"any,omitempty"`
}

type squareAvailabilityResponse struct {
	Availabilities []squareAvailability `json:"availabilities"`
}

type squareAvailability struct {
	StartAt             string                     `json:"start_at"`
	LocationID          string                     `json:"location_id"`
	AppointmentSegments []squareAppointmentSegment `json:"appointment_segments"`
}

type squareBookingResponse struct {
	Booking squareBooking `json:"booking"`
}

type squareCreateBookingRequest struct {
	IdempotencyKey string        `json:"idempotency_key"`
	Booking        squareBooking `json:"booking"`
}

type squareUpdateBookingRequest struct {
	IdempotencyKey string        `json:"idempotency_key"`
	Booking        squareBooking `json:"booking"`
}

type squareCancelBookingRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	BookingVersion int    `json:"booking_version"`
}

type squareBooking struct {
	ID                  string                     `json:"id,omitempty"`
	Version             int                        `json:"version,omitempty"`
	Status              string                     `json:"status,omitempty"`
	CustomerID          string                     `json:"customer_id,omitempty"`
	CustomerNote        string                     `json:"customer_note,omitempty"`
	SellerNote          string                     `json:"seller_note,omitempty"`
	StartAt             string                     `json:"start_at,omitempty"`
	LocationID          string                     `json:"location_id,omitempty"`
	AppointmentSegments []squareAppointmentSegment `json:"appointment_segments,omitempty"`
}

func (b *squareBooking) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID                  string                     `json:"id"`
		Version             json.RawMessage            `json:"version"`
		Status              string                     `json:"status"`
		CustomerID          string                     `json:"customer_id"`
		CustomerNote        string                     `json:"customer_note"`
		SellerNote          string                     `json:"seller_note"`
		StartAt             string                     `json:"start_at"`
		LocationID          string                     `json:"location_id"`
		AppointmentSegments []squareAppointmentSegment `json:"appointment_segments"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	b.ID = raw.ID
	b.Version = -1
	b.Status = raw.Status
	b.CustomerID = raw.CustomerID
	b.CustomerNote = raw.CustomerNote
	b.SellerNote = raw.SellerNote
	b.StartAt = raw.StartAt
	b.LocationID = raw.LocationID
	b.AppointmentSegments = raw.AppointmentSegments
	if len(raw.Version) == 0 || string(raw.Version) == "null" {
		return nil
	}
	var version int
	if err := json.Unmarshal(raw.Version, &version); err == nil {
		b.Version = version
		return nil
	}
	var versionString string
	if err := json.Unmarshal(raw.Version, &versionString); err != nil {
		return err
	}
	versionString = strings.TrimSpace(versionString)
	if versionString == "" {
		return nil
	}
	parsed, err := strconv.Atoi(versionString)
	if err != nil {
		return err
	}
	b.Version = parsed
	return nil
}

type squareAppointmentSegment struct {
	DurationMinutes         int    `json:"duration_minutes"`
	TeamMemberID            string `json:"team_member_id"`
	ServiceVariationID      string `json:"service_variation_id"`
	ServiceVariationVersion int64  `json:"service_variation_version"`
}
