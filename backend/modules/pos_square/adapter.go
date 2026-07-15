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
	cfg            config.SquareConfig
	configResolver SquareConfigResolver
	repo           *pos.Repository
	cipher         *encryption.TokenCipher
	httpClient     *http.Client
}

type SquareConfigResolver interface {
	ResolveSquareConfig(ctx context.Context, salonID string) (config.SquareConfig, error)
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

func (a *SquareAdapter) SetConfigResolver(resolver SquareConfigResolver) {
	a.configResolver = resolver
}

func (a *SquareAdapter) Name() string {
	return pos.ProviderSquare
}

func (a *SquareAdapter) Capabilities() pos.ProviderCapabilities {
	return pos.ProviderCapabilities{}
}

func (a *SquareAdapter) OAuthURL(ctx context.Context, salonID string, state string) (string, error) {
	cfg, err := a.configFor(ctx, salonID)
	if err != nil {
		return "", err
	}
	if cfg.ClientID == "" || cfg.RedirectURL == "" {
		return "", errors.New("square oauth is not configured")
	}
	values := url.Values{}
	values.Set("client_id", cfg.ClientID)
	values.Set("scope", strings.Join(squareScopes(cfg), " "))
	if strings.EqualFold(strings.TrimSpace(cfg.Environment), "production") {
		values.Set("session", "false")
	}
	values.Set("state", state)
	values.Set("redirect_uri", cfg.RedirectURL)
	return a.oauthBaseURL(cfg) + "/oauth2/authorize?" + values.Encode(), nil
}

func (a *SquareAdapter) Connect(ctx context.Context, input pos.ConnectInput) (*pos.Connection, error) {
	cfg, err := a.configFor(ctx, input.SalonID)
	if err != nil {
		return nil, err
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("square oauth credentials are not configured")
	}

	payload := map[string]string{
		"client_id":     cfg.ClientID,
		"client_secret": cfg.ClientSecret,
		"code":          input.Code,
		"grant_type":    "authorization_code",
		"redirect_uri":  defaultString(input.RedirectURL, cfg.RedirectURL),
	}
	var tokenResponse squareTokenResponse
	if err := a.doJSON(ctx, cfg, http.MethodPost, a.apiBaseURL(cfg)+"/oauth2/token", "", payload, &tokenResponse); err != nil {
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

	return a.repo.UpsertConnection(ctx, pos.Connection{
		SalonID:               input.SalonID,
		Provider:              pos.ProviderSquare,
		Status:                pos.StatusConnected,
		AccessTokenEncrypted:  accessTokenEncrypted,
		RefreshTokenEncrypted: refreshTokenEncrypted,
		MerchantID:            tokenResponse.MerchantID,
		Scopes:                connectionScopes(tokenResponse.Scope, cfg),
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
	cfg, err := a.configFor(ctx, salonID)
	if err != nil {
		return nil, err
	}
	var response squareLocationsResponse
	if err := a.doJSON(ctx, cfg, http.MethodGet, a.apiBaseURL(cfg)+"/v2/locations", token, nil, &response); err != nil {
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
	token, locationID, err := a.accessTokenAndLocation(ctx, salonID)
	if err != nil {
		return nil, err
	}
	cfg, err := a.configFor(ctx, salonID)
	if err != nil {
		return nil, err
	}
	services, err := a.listServices(ctx, cfg, token, locationID)
	if err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "list_services",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, err
	}
	return services, nil
}

func mapCatalogServices(response squareCatalogResponse) []pos.Service {
	return mapCatalogServicesForLocation(response, "")
}

func (a *SquareAdapter) listServices(ctx context.Context, cfg config.SquareConfig, token string, locationID string) ([]pos.Service, error) {
	services := make([]pos.Service, 0)
	cursor := ""
	seenCursors := map[string]bool{}
	for {
		values := url.Values{}
		values.Set("types", "ITEM,ITEM_VARIATION")
		if cursor != "" {
			values.Set("cursor", cursor)
		}
		var response squareCatalogResponse
		if err := a.doJSON(ctx, cfg, http.MethodGet, a.apiBaseURL(cfg)+"/v2/catalog/list?"+values.Encode(), token, nil, &response); err != nil {
			return nil, err
		}
		services = append(services, mapCatalogServicesForLocation(response, locationID)...)
		next := strings.TrimSpace(response.Cursor)
		if next == "" {
			break
		}
		if seenCursors[next] {
			return nil, fmt.Errorf("square catalog pagination repeated cursor")
		}
		seenCursors[next] = true
		cursor = next
	}
	return services, nil
}

func mapCatalogServicesForLocation(response squareCatalogResponse, locationID string) []pos.Service {
	var services []pos.Service
	for _, object := range response.Objects {
		if object.Type != "ITEM" || object.IsDeleted || !catalogObjectPresentAtLocation(object, locationID) {
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
				AIBookable:        false,
				Active:            !object.IsDeleted,
			})
			continue
		}
		for _, variation := range item.Variations {
			if variation.IsDeleted || !catalogObjectPresentAtLocation(variation, locationID) {
				continue
			}
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
				AIBookable:        catalogVariationIsBookable(variation),
				Active:            !object.IsDeleted && !variation.IsDeleted,
			})
		}
	}
	return services
}

func catalogVariationIsBookable(variation squareCatalogObject) bool {
	return variation.ItemVariationData.AvailableForBooking != nil &&
		*variation.ItemVariationData.AvailableForBooking &&
		variation.ItemVariationData.ServiceDuration/60000 > 0
}

func catalogObjectPresentAtLocation(object squareCatalogObject, locationID string) bool {
	locationID = strings.TrimSpace(locationID)
	if locationID == "" {
		return true
	}
	for _, absentID := range object.AbsentAtLocationIDs {
		if strings.TrimSpace(absentID) == locationID {
			return false
		}
	}
	presentEverywhere := object.PresentAtAllLocations == nil || *object.PresentAtAllLocations
	if presentEverywhere {
		return true
	}
	for _, presentID := range object.PresentAtLocationIDs {
		if strings.TrimSpace(presentID) == locationID {
			return true
		}
	}
	return false
}

func (a *SquareAdapter) ListStaff(ctx context.Context, salonID string) ([]pos.StaffMember, error) {
	token, locationID, err := a.accessTokenAndLocation(ctx, salonID)
	if err != nil {
		return nil, err
	}
	cfg, err := a.configFor(ctx, salonID)
	if err != nil {
		return nil, err
	}
	staff, err := a.listStaff(ctx, cfg, token, locationID)
	if err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "list_staff",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, err
	}
	return staff, nil
}

func (a *SquareAdapter) listStaff(ctx context.Context, cfg config.SquareConfig, token string, locationID string) ([]pos.StaffMember, error) {
	members, err := a.listActiveTeamMembers(ctx, cfg, token, locationID)
	if err != nil {
		return nil, err
	}
	profiles, err := a.listBookableTeamMemberProfiles(ctx, cfg, token, locationID)
	if err != nil {
		return nil, err
	}
	profilesByTeamMemberID := make(map[string]squareTeamMemberBookingProfile, len(profiles))
	for _, profile := range profiles {
		teamMemberID := strings.TrimSpace(profile.TeamMemberID)
		if teamMemberID == "" {
			continue
		}
		if !profile.IsBookable {
			delete(profilesByTeamMemberID, teamMemberID)
			continue
		}
		profilesByTeamMemberID[teamMemberID] = profile
	}

	staff := make([]pos.StaffMember, 0, len(members))
	seenTeamMemberIDs := make(map[string]bool, len(members))
	for _, member := range members {
		teamMemberID := strings.TrimSpace(member.ID)
		if teamMemberID == "" || seenTeamMemberIDs[teamMemberID] {
			continue
		}
		profile, ok := profilesByTeamMemberID[teamMemberID]
		if !ok {
			continue
		}
		name := strings.TrimSpace(member.GivenName + " " + member.FamilyName)
		if name == "" {
			name = strings.TrimSpace(profile.DisplayName)
		}
		if name == "" {
			continue
		}
		seenTeamMemberIDs[teamMemberID] = true
		staff = append(staff, pos.StaffMember{
			POSProvider: pos.ProviderSquare,
			POSStaffID:  teamMemberID,
			Name:        name,
			Phone:       member.PhoneNumber,
			Email:       member.EmailAddress,
			AIBookable:  true,
			Active:      true,
		})
	}
	return staff, nil
}

func (a *SquareAdapter) listActiveTeamMembers(ctx context.Context, cfg config.SquareConfig, token string, locationID string) ([]squareTeamMember, error) {
	members := make([]squareTeamMember, 0)
	cursor := ""
	seenCursors := map[string]bool{}
	for {
		request := squareTeamMembersSearchRequest{
			Query: squareTeamMembersSearchQuery{Filter: squareTeamMembersSearchFilter{
				LocationIDs: []string{strings.TrimSpace(locationID)},
				Status:      "ACTIVE",
			}},
			Limit:  200,
			Cursor: cursor,
		}
		var response squareTeamMembersResponse
		if err := a.doJSON(ctx, cfg, http.MethodPost, a.apiBaseURL(cfg)+"/v2/team-members/search", token, request, &response); err != nil {
			return nil, err
		}
		for _, member := range response.TeamMembers {
			status := strings.ToUpper(strings.TrimSpace(member.Status))
			if status != "ACTIVE" {
				continue
			}
			members = append(members, member)
		}
		next := strings.TrimSpace(response.Cursor)
		if next == "" {
			break
		}
		if seenCursors[next] {
			return nil, fmt.Errorf("square team member pagination repeated cursor")
		}
		seenCursors[next] = true
		cursor = next
	}
	return members, nil
}

func (a *SquareAdapter) listBookableTeamMemberProfiles(ctx context.Context, cfg config.SquareConfig, token string, locationID string) ([]squareTeamMemberBookingProfile, error) {
	profiles := make([]squareTeamMemberBookingProfile, 0)
	cursor := ""
	seenCursors := map[string]bool{}
	for {
		values := url.Values{}
		values.Set("bookable_only", "true")
		values.Set("location_id", strings.TrimSpace(locationID))
		values.Set("limit", "100")
		if cursor != "" {
			values.Set("cursor", cursor)
		}
		var response squareTeamMemberBookingProfilesResponse
		if err := a.doJSON(ctx, cfg, http.MethodGet, a.apiBaseURL(cfg)+"/v2/bookings/team-member-booking-profiles?"+values.Encode(), token, nil, &response); err != nil {
			return nil, err
		}
		profiles = append(profiles, response.TeamMemberBookingProfiles...)
		next := strings.TrimSpace(response.Cursor)
		if next == "" {
			break
		}
		if seenCursors[next] {
			return nil, fmt.Errorf("square team member booking profile pagination repeated cursor")
		}
		seenCursors[next] = true
		cursor = next
	}
	return profiles, nil
}

func (a *SquareAdapter) ListBusinessHourPeriods(ctx context.Context, salonID string) ([]pos.BusinessHourPeriod, error) {
	token, locationID, err := a.accessTokenAndLocation(ctx, salonID)
	if err != nil {
		return nil, err
	}
	cfg, err := a.configFor(ctx, salonID)
	if err != nil {
		return nil, err
	}
	periods, err := a.listBusinessHourPeriods(ctx, cfg, token, locationID)
	if err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "retrieve_location",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, err
	}
	return periods, nil
}

func (a *SquareAdapter) listBusinessHourPeriods(ctx context.Context, cfg config.SquareConfig, token string, locationID string) ([]pos.BusinessHourPeriod, error) {
	var response squareLocationResponse
	if err := a.doJSON(ctx, cfg, http.MethodGet, a.apiBaseURL(cfg)+"/v2/locations/"+url.PathEscape(locationID), token, nil, &response); err != nil {
		return nil, err
	}
	return mapSquareBusinessHourPeriods(response.Location.BusinessHours.Periods), nil
}

func (a *SquareAdapter) ListCustomers(ctx context.Context, salonID string) ([]pos.Customer, error) {
	token, err := a.accessToken(ctx, salonID)
	if err != nil {
		return nil, err
	}
	cfg, err := a.configFor(ctx, salonID)
	if err != nil {
		return nil, err
	}

	customers, err := a.listCustomers(ctx, cfg, token)
	if err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "list_customers",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, err
	}
	return customers, nil
}

func (a *SquareAdapter) listCustomers(ctx context.Context, cfg config.SquareConfig, token string) ([]pos.Customer, error) {
	customers := make([]pos.Customer, 0)
	cursor := ""
	seenCursors := map[string]bool{}
	for {
		request := squareCustomerSearchRequest{
			Limit:  100,
			Cursor: cursor,
		}
		var response squareCustomerSearchResponse
		if err := a.doJSON(ctx, cfg, http.MethodPost, a.apiBaseURL(cfg)+"/v2/customers/search", token, request, &response); err != nil {
			return nil, err
		}
		for _, item := range response.Customers {
			customer := mapSquareCustomer(item)
			if strings.TrimSpace(customer.POSCustomerID) != "" {
				customers = append(customers, customer)
			}
		}
		next := strings.TrimSpace(response.Cursor)
		if next == "" {
			break
		}
		if seenCursors[next] {
			return nil, fmt.Errorf("square customer pagination repeated cursor")
		}
		seenCursors[next] = true
		cursor = next
	}
	return customers, nil
}

func (a *SquareAdapter) SearchCustomerByPhone(ctx context.Context, salonID string, phone string) (*pos.Customer, error) {
	token, err := a.accessToken(ctx, salonID)
	if err != nil {
		return nil, err
	}
	cfg, err := a.configFor(ctx, salonID)
	if err != nil {
		return nil, err
	}
	request, err := buildSquareCustomerSearchRequest(phone)
	if err != nil {
		return nil, err
	}
	var response squareCustomerSearchResponse
	if err := a.doJSON(ctx, cfg, http.MethodPost, a.apiBaseURL(cfg)+"/v2/customers/search", token, request, &response); err != nil {
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
	cfg, err := a.configFor(ctx, salonID)
	if err != nil {
		return nil, err
	}
	request, err := buildSquareCreateCustomerRequest(input)
	if err != nil {
		return nil, err
	}
	var response squareCustomerResponse
	if err := a.doJSON(ctx, cfg, http.MethodPost, a.apiBaseURL(cfg)+"/v2/customers", token, request, &response); err != nil {
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
	token, locationID, err := a.accessTokenAndProviderFence(ctx, salonID, input.ProviderFence)
	if err != nil {
		return nil, err
	}
	cfg, err := a.configFor(ctx, salonID)
	if err != nil {
		return nil, err
	}
	request, err := buildSquareAvailabilityRequest(locationID, input)
	if err != nil {
		return nil, err
	}
	var response squareAvailabilityResponse
	if err := a.doJSON(ctx, cfg, http.MethodPost, a.apiBaseURL(cfg)+"/v2/bookings/availability/search", token, request, &response); err != nil {
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
	token, locationID, err := a.accessTokenAndProviderFence(ctx, salonID, input.ProviderFence)
	if err != nil {
		return nil, pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhasePrepare, err)
	}
	cfg, err := a.configFor(ctx, salonID)
	if err != nil {
		return nil, pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhasePrepare, err)
	}
	request, err := buildSquareCreateBookingRequest(locationID, input)
	if err != nil {
		return nil, pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhasePrepare, err)
	}
	var response squareBookingResponse
	if err := a.doJSON(ctx, cfg, http.MethodPost, a.apiBaseURL(cfg)+"/v2/bookings", token, request, &response); err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "create_booking",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, squareWriteError(err, pos.WritePhaseDispatch)
	}
	if response.Booking.Version < 0 && strings.TrimSpace(response.Booking.ID) != "" {
		response.Booking, err = a.retrieveBooking(ctx, cfg, token, response.Booking.ID)
		if err != nil {
			_ = a.repo.LogError(ctx, pos.POSError{
				SalonID:      salonID,
				Provider:     pos.ProviderSquare,
				Operation:    "retrieve_created_booking",
				ErrorCode:    normalizeSquareError(err),
				ErrorMessage: err.Error(),
			})
			return nil, pos.NewWriteError(pos.WriteOutcomeUnknown, pos.WritePhasePostWriteRead, err)
		}
	}
	appointment, err := mapSquareBooking(response.Booking, appointmentFallbackDuration(input.Segments, input.DurationMinutes))
	if err != nil {
		return nil, pos.NewWriteError(pos.WriteOutcomeUnknown, pos.WritePhaseResponse, err)
	}
	return appointment, nil
}

func (a *SquareAdapter) RescheduleAppointment(ctx context.Context, salonID string, appointmentID string, input pos.RescheduleInput) (*pos.Appointment, error) {
	token, locationID, err := a.accessTokenAndProviderFence(ctx, salonID, input.ProviderFence)
	if err != nil {
		return nil, pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhasePrepare, err)
	}
	cfg, err := a.configFor(ctx, salonID)
	if err != nil {
		return nil, pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhasePrepare, err)
	}
	request, err := buildSquareUpdateBookingRequest(locationID, input)
	if err != nil {
		return nil, pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhasePrepare, err)
	}
	var response squareBookingResponse
	if err := a.doJSON(ctx, cfg, http.MethodPut, a.apiBaseURL(cfg)+"/v2/bookings/"+url.PathEscape(appointmentID), token, request, &response); err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "reschedule_booking",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, squareWriteError(err, pos.WritePhaseDispatch)
	}
	appointment, err := mapSquareBooking(response.Booking, appointmentFallbackDuration(input.Segments, input.DurationMinutes))
	if err != nil {
		return nil, pos.NewWriteError(pos.WriteOutcomeUnknown, pos.WritePhaseResponse, err)
	}
	return appointment, nil
}

func (a *SquareAdapter) CancelAppointment(ctx context.Context, salonID string, appointmentID string, input pos.CancelInput) (*pos.Appointment, error) {
	token, _, err := a.accessTokenAndProviderFence(ctx, salonID, input.ProviderFence)
	if err != nil {
		return nil, pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhasePrepare, err)
	}
	cfg, err := a.configFor(ctx, salonID)
	if err != nil {
		return nil, pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhasePrepare, err)
	}
	request, err := buildSquareCancelBookingRequest(input)
	if err != nil {
		return nil, pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhasePrepare, err)
	}
	var response squareBookingResponse
	if err := a.doJSON(ctx, cfg, http.MethodPost, a.apiBaseURL(cfg)+"/v2/bookings/"+url.PathEscape(appointmentID)+"/cancel", token, request, &response); err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "cancel_booking",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, squareWriteError(err, pos.WritePhaseDispatch)
	}
	appointment, err := mapSquareBooking(response.Booking, 0)
	if err != nil {
		return nil, pos.NewWriteError(pos.WriteOutcomeUnknown, pos.WritePhaseResponse, err)
	}
	return appointment, nil
}

func (a *SquareAdapter) ListAppointments(ctx context.Context, salonID string, input pos.AppointmentListInput) (*pos.AppointmentListResult, error) {
	token, locationID, err := a.accessTokenAndProviderFence(ctx, salonID, input.ProviderFence)
	if err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "list_bookings",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, err
	}
	cfg, err := a.configFor(ctx, salonID)
	if err != nil {
		return nil, err
	}
	values := url.Values{}
	values.Set("location_id", locationID)
	if !input.StartTime.IsZero() {
		values.Set("start_at_min", input.StartTime.UTC().Format(time.RFC3339))
	}
	if !input.EndTime.IsZero() {
		values.Set("start_at_max", input.EndTime.UTC().Format(time.RFC3339))
	}
	limit := input.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	values.Set("limit", strconv.Itoa(limit))
	if strings.TrimSpace(input.Cursor) != "" {
		values.Set("cursor", strings.TrimSpace(input.Cursor))
	}
	var response squareBookingListResponse
	if err := a.doJSON(ctx, cfg, http.MethodGet, a.apiBaseURL(cfg)+"/v2/bookings?"+values.Encode(), token, nil, &response); err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "list_bookings",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, err
	}
	if err := validateSquareBookingLocations(response.Bookings, input.ProviderFence.LocationID); err != nil {
		_ = a.repo.LogError(ctx, pos.POSError{
			SalonID:      salonID,
			Provider:     pos.ProviderSquare,
			Operation:    "list_bookings",
			ErrorCode:    normalizeSquareError(err),
			ErrorMessage: err.Error(),
		})
		return nil, err
	}
	items := make([]pos.ListedAppointment, 0, len(response.Bookings))
	for _, booking := range response.Bookings {
		item, ok := mapSquareListedBooking(booking)
		if ok {
			items = append(items, item)
		}
	}
	return &pos.AppointmentListResult{Appointments: items, Cursor: response.Cursor}, nil
}

func validateSquareBookingLocations(bookings []squareBooking, expectedLocationID string) error {
	expectedLocationID = strings.TrimSpace(expectedLocationID)
	if expectedLocationID == "" {
		return pos.ErrStaleProviderFence
	}
	for _, booking := range bookings {
		locationID := strings.TrimSpace(booking.LocationID)
		if locationID != "" && locationID != expectedLocationID {
			return pos.ErrStaleProviderFence
		}
	}
	return nil
}

func (a *SquareAdapter) Sync(ctx context.Context, salonID string) error {
	_, err := a.SyncWithSummary(ctx, salonID)
	return err
}

type providerSnapshotSyncError struct {
	Generation int64
	Err        error
}

func (e *providerSnapshotSyncError) Error() string {
	if e == nil || e.Err == nil {
		return "provider snapshot sync failed"
	}
	return e.Err.Error()
}

func (e *providerSnapshotSyncError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func wrapProviderSnapshotSyncError(generation int64, err error) error {
	if err == nil {
		return nil
	}
	return &providerSnapshotSyncError{Generation: generation, Err: err}
}

func providerSnapshotGenerationFromError(err error) (int64, bool) {
	var syncErr *providerSnapshotSyncError
	if !errors.As(err, &syncErr) || syncErr.Generation <= 0 {
		return 0, false
	}
	return syncErr.Generation, true
}

func (a *SquareAdapter) SyncWithSummary(ctx context.Context, salonID string) (*pos.SyncSummary, error) {
	token, locationID, err := a.accessTokenAndLocation(ctx, salonID)
	if err != nil {
		return nil, err
	}
	generation, err := a.repo.BeginProviderSnapshot(ctx, salonID, pos.ProviderSquare, locationID)
	if err != nil {
		return nil, err
	}
	cfg, err := a.configFor(ctx, salonID)
	if err != nil {
		return nil, wrapProviderSnapshotSyncError(generation, err)
	}
	services, err := a.listServices(ctx, cfg, token, locationID)
	if err != nil {
		return nil, wrapProviderSnapshotSyncError(generation, err)
	}
	staff, err := a.listStaff(ctx, cfg, token, locationID)
	if err != nil {
		return nil, wrapProviderSnapshotSyncError(generation, err)
	}
	periods, err := a.listBusinessHourPeriods(ctx, cfg, token, locationID)
	if err != nil {
		return nil, wrapProviderSnapshotSyncError(generation, err)
	}
	customers, err := a.listCustomers(ctx, cfg, token)
	if err != nil {
		return nil, wrapProviderSnapshotSyncError(generation, err)
	}
	summary, err := a.repo.ApplyProviderSnapshot(ctx, salonID, pos.ProviderSnapshot{
		Provider:            pos.ProviderSquare,
		LocationID:          locationID,
		Generation:          generation,
		Services:            services,
		Staff:               staff,
		BusinessHourPeriods: periods,
		Customers:           customers,
	})
	if err != nil {
		return nil, wrapProviderSnapshotSyncError(generation, err)
	}
	return summary, nil
}

func (a *SquareAdapter) retrieveBooking(ctx context.Context, cfg config.SquareConfig, token string, bookingID string) (squareBooking, error) {
	var response squareBookingResponse
	if err := a.doJSON(ctx, cfg, http.MethodGet, a.apiBaseURL(cfg)+"/v2/bookings/"+url.PathEscape(bookingID), token, nil, &response); err != nil {
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

func (a *SquareAdapter) accessTokenAndProviderFence(ctx context.Context, salonID string, expected pos.ProviderFence) (string, string, error) {
	connection, err := a.repo.GetConnection(ctx, salonID, pos.ProviderSquare)
	if err != nil {
		return "", "", ErrNotConnected
	}
	if !connectionMatchesProviderFence(connection, expected) {
		return "", "", pos.ErrStaleProviderFence
	}
	if connection.AccessTokenEncrypted == "" {
		return "", "", ErrNotConnected
	}
	token, err := a.cipher.Decrypt(connection.AccessTokenEncrypted)
	if err != nil {
		return "", "", err
	}
	return token, strings.TrimSpace(expected.LocationID), nil
}

func connectionMatchesProviderFence(connection *pos.Connection, expected pos.ProviderFence) bool {
	return connection != nil &&
		connection.Status == pos.StatusActive &&
		connection.LastSyncAt != nil &&
		strings.TrimSpace(expected.LocationID) != "" &&
		expected.SnapshotGeneration > 0 &&
		strings.TrimSpace(connection.LocationID) == strings.TrimSpace(expected.LocationID) &&
		connection.SnapshotGeneration == expected.SnapshotGeneration
}

func (a *SquareAdapter) doJSON(ctx context.Context, cfg config.SquareConfig, method string, endpoint string, bearerToken string, input any, output any) error {
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
	if cfg.APIVersion != "" {
		req.Header.Set("Square-Version", cfg.APIVersion)
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
			return &squareHTTPError{StatusCode: res.StatusCode, Code: squareErr.Errors[0].Code, Detail: squareErr.Errors[0].Detail}
		}
		return &squareHTTPError{StatusCode: res.StatusCode}
	}
	if output == nil {
		return nil
	}
	if err := json.NewDecoder(res.Body).Decode(output); err != nil {
		return &squareResponseError{Err: err}
	}
	return nil
}

type squareResponseError struct {
	Err error
}

func (e *squareResponseError) Error() string {
	if e == nil || e.Err == nil {
		return "square response could not be decoded"
	}
	return e.Err.Error()
}

func (e *squareResponseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type squareHTTPError struct {
	StatusCode int
	Code       string
	Detail     string
}

func (e *squareHTTPError) Error() string {
	if e == nil {
		return "square request failed"
	}
	if strings.TrimSpace(e.Code) != "" {
		return fmt.Sprintf("square %s: %s", e.Code, e.Detail)
	}
	return fmt.Sprintf("square returned status %d", e.StatusCode)
}

func squareWriteError(err error, phase string) error {
	var httpErr *squareHTTPError
	if errors.As(err, &httpErr) && definitiveSquareWriteRejection(httpErr.StatusCode) {
		return pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhaseResponse, err)
	}
	var responseErr *squareResponseError
	if errors.As(err, &responseErr) || errors.As(err, &httpErr) {
		phase = pos.WritePhaseResponse
	}
	return pos.NewWriteError(pos.WriteOutcomeUnknown, phase, err)
}

func definitiveSquareWriteRejection(statusCode int) bool {
	switch statusCode {
	case http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusConflict,
		http.StatusUnprocessableEntity,
		http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}

func (a *SquareAdapter) configFor(ctx context.Context, salonID string) (config.SquareConfig, error) {
	if a.configResolver == nil || strings.TrimSpace(salonID) == "" {
		return a.cfg, nil
	}
	cfg, err := a.configResolver.ResolveSquareConfig(ctx, salonID)
	if err != nil {
		return config.SquareConfig{}, err
	}
	return cfg, nil
}

func (a *SquareAdapter) apiBaseURL(cfg config.SquareConfig) string {
	if strings.TrimSpace(cfg.APIBaseURL) != "" {
		return strings.TrimRight(cfg.APIBaseURL, "/")
	}
	if cfg.Environment == "production" {
		return "https://connect.squareup.com"
	}
	return "https://connect.squareupsandbox.com"
}

func (a *SquareAdapter) oauthBaseURL(cfg config.SquareConfig) string {
	return a.apiBaseURL(cfg)
}

func squareScopes(cfg config.SquareConfig) []string {
	scopes := []string{
		"APPOINTMENTS_READ",
		"APPOINTMENTS_ALL_READ",
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
	if strings.EqualFold(strings.TrimSpace(cfg.Environment), "production") {
		scopes = append(scopes[:3], append([]string{"APPOINTMENTS_ALL_WRITE"}, scopes[3:]...)...)
	}
	return scopes
}

func connectionScopes(tokenScope string, cfg config.SquareConfig) []string {
	scopes := strings.Fields(tokenScope)
	if len(scopes) > 0 {
		return scopes
	}
	return squareScopes(cfg)
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
		Query: &squareCustomerQuery{
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
	startAt, endAt, err := availabilityRange(input.PreferredDate, input.Timezone)
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

func mapSquareBusinessHourPeriods(periods []squareBusinessHourPeriod) []pos.BusinessHourPeriod {
	items := make([]pos.BusinessHourPeriod, 0, len(periods))
	dayIndexes := make(map[int]int)
	for _, period := range periods {
		dayOfWeek, ok := squareDayOfWeek(period.DayOfWeek)
		if !ok {
			continue
		}
		startLocalTime := strings.TrimSpace(period.StartLocalTime)
		endLocalTime := strings.TrimSpace(period.EndLocalTime)
		if startLocalTime == "" || endLocalTime == "" {
			continue
		}
		periodIndex := dayIndexes[dayOfWeek]
		dayIndexes[dayOfWeek] = periodIndex + 1
		items = append(items, pos.BusinessHourPeriod{
			DayOfWeek:           dayOfWeek,
			StartLocalTime:      startLocalTime,
			EndLocalTime:        endLocalTime,
			Source:              pos.BusinessHourSourceImported,
			Provider:            pos.ProviderSquare,
			ProviderPeriodIndex: periodIndex,
		})
	}
	return items
}

func squareDayOfWeek(value string) (int, bool) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "SUN", "SUNDAY":
		return 0, true
	case "MON", "MONDAY":
		return 1, true
	case "TUE", "TUESDAY":
		return 2, true
	case "WED", "WEDNESDAY":
		return 3, true
	case "THU", "THURSDAY":
		return 4, true
	case "FRI", "FRIDAY":
		return 5, true
	case "SAT", "SATURDAY":
		return 6, true
	default:
		return 0, false
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

func mapSquareListedBooking(booking squareBooking) (pos.ListedAppointment, bool) {
	appointment, err := mapSquareBooking(booking, 0)
	if err != nil || appointment == nil || strings.TrimSpace(appointment.POSAppointmentID) == "" || appointment.StartTime.IsZero() {
		return pos.ListedAppointment{}, false
	}
	segments := make([]pos.ListedAppointmentSegment, 0, len(booking.AppointmentSegments))
	for _, segment := range booking.AppointmentSegments {
		segments = append(segments, pos.ListedAppointmentSegment{
			POSServiceID:      strings.TrimSpace(segment.ServiceVariationID),
			POSServiceVersion: segment.ServiceVariationVersion,
			POSStaffID:        strings.TrimSpace(segment.TeamMemberID),
			DurationMinutes:   segment.DurationMinutes,
		})
	}
	return pos.ListedAppointment{
		POSAppointmentID:      appointment.POSAppointmentID,
		POSAppointmentVersion: appointment.POSAppointmentVersion,
		Status:                appointment.Status,
		POSCustomerID:         strings.TrimSpace(booking.CustomerID),
		CustomerName:          "Square customer",
		StartTime:             appointment.StartTime,
		EndTime:               appointment.EndTime,
		Notes:                 firstNonEmpty(booking.CustomerNote, booking.SellerNote),
		Segments:              segments,
	}, true
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func availabilityRange(preferredDate string, timezone string) (time.Time, time.Time, error) {
	value := strings.TrimSpace(preferredDate)
	if value == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("preferred date is required")
	}
	locationName := strings.TrimSpace(timezone)
	if locationName == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("salon timezone is required")
	}
	loc, err := time.LoadLocation(locationName)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if day, err := time.ParseInLocation("2006-01-02", value, loc); err == nil {
		return day.UTC(), day.AddDate(0, 0, 1).UTC(), nil
	}
	instant, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	local := instant.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return start.UTC(), start.AddDate(0, 0, 1).UTC(), nil
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
	Locations []squareLocation `json:"locations"`
}

type squareLocationResponse struct {
	Location squareLocation `json:"location"`
}

type squareLocation struct {
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Status        string              `json:"status"`
	Timezone      string              `json:"timezone"`
	Address       squareAddress       `json:"address"`
	BusinessHours squareBusinessHours `json:"business_hours"`
}

type squareBusinessHours struct {
	Periods []squareBusinessHourPeriod `json:"periods"`
}

type squareBusinessHourPeriod struct {
	DayOfWeek      string `json:"day_of_week"`
	StartLocalTime string `json:"start_local_time"`
	EndLocalTime   string `json:"end_local_time"`
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
	Cursor  string                `json:"cursor"`
}

type squareCatalogObject struct {
	ID                    string   `json:"id"`
	Type                  string   `json:"type"`
	Version               int64    `json:"version"`
	IsDeleted             bool     `json:"is_deleted"`
	PresentAtAllLocations *bool    `json:"present_at_all_locations"`
	PresentAtLocationIDs  []string `json:"present_at_location_ids"`
	AbsentAtLocationIDs   []string `json:"absent_at_location_ids"`
	ItemData              struct {
		Name        string                `json:"name"`
		Description string                `json:"description"`
		Variations  []squareCatalogObject `json:"variations"`
	} `json:"item_data"`
	ItemVariationData struct {
		Name                string `json:"name"`
		ServiceDuration     int64  `json:"service_duration"`
		AvailableForBooking *bool  `json:"available_for_booking"`
		PriceMoney          struct {
			Amount   int64  `json:"amount"`
			Currency string `json:"currency"`
		} `json:"price_money"`
	} `json:"item_variation_data"`
}

type squareTeamMembersResponse struct {
	TeamMembers []squareTeamMember `json:"team_members"`
	Cursor      string             `json:"cursor"`
}

type squareTeamMember struct {
	ID           string `json:"id"`
	GivenName    string `json:"given_name"`
	FamilyName   string `json:"family_name"`
	EmailAddress string `json:"email_address"`
	PhoneNumber  string `json:"phone_number"`
	Status       string `json:"status"`
}

type squareTeamMemberBookingProfilesResponse struct {
	TeamMemberBookingProfiles []squareTeamMemberBookingProfile `json:"team_member_booking_profiles"`
	Cursor                    string                           `json:"cursor"`
}

type squareTeamMemberBookingProfile struct {
	TeamMemberID string `json:"team_member_id"`
	DisplayName  string `json:"display_name"`
	IsBookable   bool   `json:"is_bookable"`
}

type squareTeamMembersSearchRequest struct {
	Query  squareTeamMembersSearchQuery `json:"query"`
	Limit  int                          `json:"limit,omitempty"`
	Cursor string                       `json:"cursor,omitempty"`
}

type squareTeamMembersSearchQuery struct {
	Filter squareTeamMembersSearchFilter `json:"filter"`
}

type squareTeamMembersSearchFilter struct {
	LocationIDs []string `json:"location_ids,omitempty"`
	Status      string   `json:"status,omitempty"`
}

type squareCustomerSearchRequest struct {
	Query  *squareCustomerQuery `json:"query,omitempty"`
	Limit  int                  `json:"limit,omitempty"`
	Cursor string               `json:"cursor,omitempty"`
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
	Cursor    string           `json:"cursor"`
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

type squareBookingListResponse struct {
	Bookings []squareBooking `json:"bookings"`
	Cursor   string          `json:"cursor"`
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
