package business

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/access"
)

type Handler struct {
	service    *ServiceLayer
	surface    access.Surface
	normalized bool
}

func NewHandler(service *ServiceLayer, surface access.Surface) *Handler {
	return &Handler{service: service, surface: surface}
}

func (h *Handler) salonID(c *fiber.Ctx) string {
	if h.surface == access.SurfacePlatform {
		return c.Params("tenant_id")
	}
	return c.Params("id")
}

func (h *Handler) ListSalons(c *fiber.Ctx) error {
	result, err := h.service.ListSalons(c.UserContext(), middleware.Actor(c), h.surface)
	if err != nil {
		return h.handleError(c, err)
	}
	if h.surface == access.SurfaceTenant {
		preferred := strings.TrimSpace(c.Get("X-Tenant-Salon-ID"))
		for index := range result.Salons {
			if result.Salons[index].ID == preferred {
				result.Salons[0], result.Salons[index] = result.Salons[index], result.Salons[0]
				break
			}
		}
	}
	return h.read(c, result)
}
func (h *Handler) SalonProfile(c *fiber.Ctx) error {
	result, err := h.service.SalonProfile(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c))
	if err != nil {
		return h.handleError(c, err)
	}
	return h.read(c, result)
}

func (h *Handler) PlatformTenantContext(c *fiber.Ctx) error {
	result, err := h.service.PlatformTenantContext(c.UserContext(), middleware.Actor(c), h.salonID(c))
	if err != nil {
		return h.handleError(c, err)
	}
	return h.read(c, result)
}
func (h *Handler) UpdateSalonProfile(c *fiber.Ctx) error {
	var req SalonProfileMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.UpdateSalonProfile(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c), req)
	return h.mutation(c, result, err, fiber.StatusOK)
}
func (h *Handler) Services(c *fiber.Ctx) error {
	result, err := h.service.Services(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c))
	if err != nil {
		return h.handleError(c, err)
	}
	return h.read(c, result)
}
func (h *Handler) CreateService(c *fiber.Ctx) error {
	var req ServiceMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.CreateService(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c), req)
	return h.mutation(c, result, err, fiber.StatusCreated)
}
func (h *Handler) UpdateService(c *fiber.Ctx) error {
	var req ServiceMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.UpdateService(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c), c.Params("service_id"), req)
	return h.mutation(c, result, err, fiber.StatusOK)
}
func (h *Handler) ArchiveService(c *fiber.Ctx) error {
	var req MutationControl
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.ArchiveService(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c), c.Params("service_id"), req)
	return h.mutation(c, result, err, fiber.StatusOK)
}
func (h *Handler) ServiceCategories(c *fiber.Ctx) error {
	result, err := h.service.ServiceCategories(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c))
	if err != nil {
		return h.handleError(c, err)
	}
	return h.read(c, result)
}
func (h *Handler) CreateServiceCategory(c *fiber.Ctx) error {
	var req ServiceCategoryMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.CreateServiceCategory(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c), req)
	return h.mutation(c, result, err, fiber.StatusCreated)
}
func (h *Handler) UpdateServiceCategory(c *fiber.Ctx) error {
	var req ServiceCategoryMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.UpdateServiceCategory(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c), c.Params("category_id"), req)
	return h.mutation(c, result, err, fiber.StatusOK)
}
func (h *Handler) ArchiveServiceCategory(c *fiber.Ctx) error {
	var req MutationControl
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.ArchiveServiceCategory(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c), c.Params("category_id"), req)
	return h.mutation(c, result, err, fiber.StatusOK)
}
func (h *Handler) Staff(c *fiber.Ctx) error {
	result, err := h.service.Staff(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c))
	if err != nil {
		return h.handleError(c, err)
	}
	return h.read(c, result)
}
func (h *Handler) CreateStaff(c *fiber.Ctx) error {
	var req StaffMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.CreateStaff(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c), req)
	return h.mutation(c, result, err, fiber.StatusCreated)
}
func (h *Handler) UpdateStaff(c *fiber.Ctx) error {
	var req StaffMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.UpdateStaff(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c), c.Params("staff_id"), req)
	return h.mutation(c, result, err, fiber.StatusOK)
}
func (h *Handler) ArchiveStaff(c *fiber.Ctx) error {
	var req MutationControl
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.ArchiveStaff(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c), c.Params("staff_id"), req)
	return h.mutation(c, result, err, fiber.StatusOK)
}
func (h *Handler) ReplaceStaffServiceEligibility(c *fiber.Ctx) error {
	var req StaffServiceEligibilityMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	req.StaffID = c.Params("staff_id")
	result, err := h.service.ReplaceStaffServiceEligibility(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c), req)
	return h.mutation(c, result, err, fiber.StatusOK)
}
func (h *Handler) BusinessHours(c *fiber.Ctx) error {
	result, err := h.service.BusinessHours(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c))
	if err != nil {
		return h.handleError(c, err)
	}
	return h.read(c, result)
}
func (h *Handler) ReplaceBusinessHours(c *fiber.Ctx) error {
	var req BusinessHoursMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.ReplaceBusinessHours(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c), req)
	return h.mutation(c, result, err, fiber.StatusOK)
}
func (h *Handler) PublicCatalogSettings(c *fiber.Ctx) error {
	result, err := h.service.PublicCatalogSettings(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c))
	if err != nil {
		return h.handleError(c, err)
	}
	return h.read(c, result)
}
func (h *Handler) UpdatePublicCatalogSettings(c *fiber.Ctx) error {
	var req PublicCatalogMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.UpdatePublicCatalogSettings(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c), req)
	return h.mutation(c, result, err, fiber.StatusOK)
}
func (h *Handler) Customers(c *fiber.Ctx) error {
	limit, err := strconv.Atoi(defaultQuery(c.Query("limit"), "50"))
	if err != nil {
		return h.handleError(c, ErrValidation)
	}
	offset, err := strconv.Atoi(defaultQuery(c.Query("offset"), "0"))
	if err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.Customers(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c), limit, offset)
	if err != nil {
		return h.handleError(c, err)
	}
	return h.read(c, result)
}
func (h *Handler) CreateCustomer(c *fiber.Ctx) error {
	var req CustomerMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.CreateCustomer(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c), req)
	return h.mutation(c, result, err, fiber.StatusCreated)
}
func (h *Handler) UpdateCustomer(c *fiber.Ctx) error {
	var req CustomerMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.UpdateCustomer(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c), c.Params("customer_id"), req)
	return h.mutation(c, result, err, fiber.StatusOK)
}
func (h *Handler) ArchiveCustomer(c *fiber.Ctx) error {
	var req MutationControl
	if err := c.BodyParser(&req); err != nil {
		return h.handleError(c, ErrValidation)
	}
	result, err := h.service.ArchiveCustomer(c.UserContext(), middleware.Actor(c), h.surface, h.salonID(c), c.Params("customer_id"), req)
	return h.mutation(c, result, err, fiber.StatusOK)
}

func (h *Handler) mutation(c *fiber.Ctx, result any, err error, status int) error {
	if err != nil {
		return h.handleError(c, err)
	}
	replayed := false
	switch value := result.(type) {
	case *MutationResponse[SalonProfile]:
		replayed = value.Replayed
	case *MutationResponse[Service]:
		replayed = value.Replayed
	case *MutationResponse[ServiceCategory]:
		replayed = value.Replayed
	case *MutationResponse[StaffMember]:
		replayed = value.Replayed
	case *MutationResponse[BusinessHours]:
		replayed = value.Replayed
	case *MutationResponse[PublicCatalogSettings]:
		replayed = value.Replayed
	case *MutationResponse[Customer]:
		replayed = value.Replayed
	}
	c.Set("X-Idempotent-Replay", strconv.FormatBool(replayed))
	if h.normalized {
		data, version := normalizedMutationData(result)
		return respond.JSON(c, status, fiber.Map{"data": data, "meta": h.normalizedMeta(c, version, replayed, nil)})
	}
	return respond.JSON(c, status, result)
}

func (h *Handler) read(c *fiber.Ctx, result any) error {
	if !h.normalized {
		return respond.JSON(c, fiber.StatusOK, result)
	}
	data := result
	version := int64(0)
	var page fiber.Map
	switch value := result.(type) {
	case *SalonProfile:
		version = value.Version
	case *ServicesResponse:
		data = value.Services
		version = maxServiceVersion(value.Services)
	case *ServiceCategoriesResponse:
		data = value.Categories
		version = maxCategoryVersion(value.Categories)
	case *StaffResponse:
		data = value.Staff
		version = maxStaffVersion(value.Staff)
	case *BusinessHours:
		version = value.Version
	case *PublicCatalogSettings:
		version = value.Version
	case *CustomersResponse:
		data = value.Customers
		version = maxCustomerVersion(value.Customers)
		limit, _ := strconv.Atoi(defaultQuery(c.Query("limit"), "50"))
		offset, _ := strconv.Atoi(defaultQuery(c.Query("offset"), "0"))
		page = fiber.Map{"limit": limit, "offset": offset, "has_more": len(value.Customers) == limit}
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"data": data, "meta": h.normalizedMeta(c, version, false, page)})
}

func (h *Handler) normalizedMeta(c *fiber.Ctx, version int64, replayed bool, page fiber.Map) fiber.Map {
	requestID := ""
	if value := c.Locals("requestid"); value != nil {
		requestID = fmt.Sprint(value)
	}
	meta := fiber.Map{
		"request_id":       requestID,
		"replayed":         replayed,
		"resource_version": version,
		"permissions":      fiber.Map{"can_read": true, "allowed_actions": []string{}},
	}
	if page != nil {
		meta["page"] = page
	}
	return meta
}

func normalizedMutationData(result any) (any, int64) {
	switch value := result.(type) {
	case *MutationResponse[SalonProfile]:
		return value.Data, value.Data.Version
	case *MutationResponse[Service]:
		return value.Data, value.Data.Version
	case *MutationResponse[ServiceCategory]:
		return value.Data, value.Data.Version
	case *MutationResponse[StaffMember]:
		return value.Data, value.Data.Version
	case *MutationResponse[BusinessHours]:
		return value.Data, value.Data.Version
	case *MutationResponse[PublicCatalogSettings]:
		return value.Data, value.Data.Version
	case *MutationResponse[Customer]:
		return value.Data, value.Data.Version
	default:
		return result, 0
	}
}

func maxServiceVersion(items []Service) int64 {
	var version int64
	for _, item := range items {
		if item.Version > version {
			version = item.Version
		}
	}
	return version
}
func maxCategoryVersion(items []ServiceCategory) int64 {
	var version int64
	for _, item := range items {
		if item.Version > version {
			version = item.Version
		}
	}
	return version
}
func maxStaffVersion(items []StaffMember) int64 {
	var version int64
	for _, item := range items {
		if item.Version > version {
			version = item.Version
		}
		if item.EligibilityVersion > version {
			version = item.EligibilityVersion
		}
	}
	return version
}
func maxCustomerVersion(items []Customer) int64 {
	var version int64
	for _, item := range items {
		if item.Version > version {
			version = item.Version
		}
	}
	return version
}

func (h *Handler) handleError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, ErrValidation):
		return respond.Error(c, fiber.StatusBadRequest, "BUSINESS_INVALID", "Business request is invalid.")
	case errors.Is(err, ErrForbidden):
		return respond.Error(c, fiber.StatusForbidden, "BUSINESS_FORBIDDEN", "This business action is not permitted.")
	case errors.Is(err, ErrNotFound):
		return respond.Error(c, fiber.StatusNotFound, "BUSINESS_NOT_FOUND", "The requested business record was not found.")
	case errors.Is(err, ErrVersionConflict):
		return respond.Error(c, fiber.StatusConflict, "BUSINESS_VERSION_CONFLICT", "Business data changed. Reload and retry with the current version.")
	case errors.Is(err, ErrActionConflict):
		return respond.Error(c, fiber.StatusConflict, "BUSINESS_ACTION_CONFLICT", "The action key was already used with different input.")
	case errors.Is(err, ErrProviderReadOnly):
		return respond.Error(c, fiber.StatusConflict, "BUSINESS_PROVIDER_READ_ONLY", "This field is managed by the connected provider and is read-only here.")
	case errors.Is(err, ErrDuplicate):
		return respond.Error(c, fiber.StatusConflict, "BUSINESS_DUPLICATE", "A business record with the same identity already exists.")
	case errors.Is(err, ErrPublicationBlocked):
		return respond.Error(c, fiber.StatusConflict, "PUBLIC_CATALOG_NOT_READY", "Complete the business catalog prerequisites before publishing.")
	default:
		return respond.Error(c, fiber.StatusInternalServerError, "BUSINESS_OPERATION_FAILED", "Could not complete the business operation.")
	}
}

func defaultQuery(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
