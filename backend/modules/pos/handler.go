package pos

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/respond"
)

type Handler struct {
	service *ServiceLayer
}

func NewHandler(service *ServiceLayer) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Services(c *fiber.Ctx) error {
	items, err := h.service.Services(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICES_FAILED", "Could not load services.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"services": items})
}

func (h *Handler) ServiceCategories(c *fiber.Ctx) error {
	items, err := h.service.ServiceCategories(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICE_CATEGORIES_FAILED", "Could not load service categories.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"service_categories": items})
}

func (h *Handler) CreateServiceCategory(c *fiber.Ctx) error {
	req, err := parseServiceCategoryWriteRequest(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body must include a valid service category.")
	}
	item, err := h.service.CreateServiceCategory(c.UserContext(), c.Params("id"), middleware.UserID(c), *req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Service category name must be valid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICE_CATEGORY_CREATE_FAILED", "Could not create service category.")
	}
	return respond.JSON(c, fiber.StatusCreated, fiber.Map{"service_category": item})
}

func (h *Handler) UpdateServiceCategory(c *fiber.Ctx) error {
	req, err := parseServiceCategoryWriteRequest(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body must include a valid service category.")
	}
	item, err := h.service.UpdateServiceCategory(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("category_id"), *req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Service category name must be valid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SERVICE_CATEGORY_NOT_FOUND", "Service category was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICE_CATEGORY_UPDATE_FAILED", "Could not update service category.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"service_category": item})
}

func (h *Handler) ArchiveServiceCategory(c *fiber.Ctx) error {
	item, err := h.service.ArchiveServiceCategory(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("category_id"))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Service category cannot be archived.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SERVICE_CATEGORY_NOT_FOUND", "Service category was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICE_CATEGORY_ARCHIVE_FAILED", "Could not archive service category.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"service_category": item})
}

func (h *Handler) RestoreServiceCategory(c *fiber.Ctx) error {
	item, err := h.service.RestoreServiceCategory(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("category_id"))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Service category cannot be restored.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SERVICE_CATEGORY_NOT_FOUND", "Service category was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICE_CATEGORY_RESTORE_FAILED", "Could not restore service category.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"service_category": item})
}

func (h *Handler) UpsertServiceCategoryAlias(c *fiber.Ctx) error {
	req, err := parseServiceCategoryAliasWriteRequest(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body must include a valid category alias.")
	}
	item, err := h.service.UpsertServiceCategoryAlias(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("category_id"), *req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Alias conflicts with an active service alias or is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SERVICE_CATEGORY_NOT_FOUND", "Service category was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICE_CATEGORY_ALIAS_SAVE_FAILED", "Could not save service category alias.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"service_category_alias": item})
}

func (h *Handler) ArchiveServiceCategoryAlias(c *fiber.Ctx) error {
	item, err := h.service.ArchiveServiceCategoryAlias(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("alias_id"))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Service category alias cannot be archived.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SERVICE_CATEGORY_ALIAS_NOT_FOUND", "Service category alias was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICE_CATEGORY_ALIAS_ARCHIVE_FAILED", "Could not archive service category alias.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"service_category_alias": item})
}

func (h *Handler) AssignServiceCategory(c *fiber.Ctx) error {
	req, err := parseServiceCategoryAssignRequest(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body must include service_category_id.")
	}
	item, err := h.service.AssignServiceCategory(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("service_id"), *req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Service category assignment is invalid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SERVICE_OR_CATEGORY_NOT_FOUND", "Service or category was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICE_CATEGORY_ASSIGN_FAILED", "Could not assign service category.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"service": item})
}

func (h *Handler) RefreshServiceCategorySuggestions(c *fiber.Ctx) error {
	result, err := h.service.RefreshServiceCategorySuggestions(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICE_CATEGORY_SUGGESTION_REFRESH_FAILED", "Could not refresh service category suggestions.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"refresh": result})
}

func (h *Handler) CreateService(c *fiber.Ctx) error {
	req, err := parseServiceWriteRequest(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body must include a valid service.")
	}
	item, err := h.service.CreateService(c.UserContext(), c.Params("id"), middleware.UserID(c), *req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Service name, duration, and price must be valid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICE_CREATE_FAILED", "Could not create service.")
	}
	return respond.JSON(c, fiber.StatusCreated, fiber.Map{"service": item})
}

func (h *Handler) UpdateService(c *fiber.Ctx) error {
	req, err := parseServiceWriteRequest(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body must include a valid service.")
	}
	item, err := h.service.UpdateService(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("service_id"), *req)
	if errors.Is(err, ErrProviderManagedFields) {
		return respond.Error(c, fiber.StatusConflict, "PROVIDER_MANAGED_FIELDS", "Service operational fields are managed by the active POS provider. Edit them there, then sync.")
	}
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Service name, duration, price, and archive state must be valid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SERVICE_NOT_FOUND", "Service was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICE_UPDATE_FAILED", "Could not update service.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"service": item})
}

func (h *Handler) UpdateServiceOwnerControls(c *fiber.Ctx) error {
	req, err := parseServiceOwnerControlsWriteRequest(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body must include valid ManleAI service controls.")
	}
	item, err := h.service.UpdateServiceOwnerControls(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("service_id"), *req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Service category and consultation controls must be valid for a non-archived service.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SERVICE_NOT_FOUND", "Service was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICE_OWNER_CONTROLS_UPDATE_FAILED", "Could not update ManleAI service controls.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"service": item})
}

func (h *Handler) ArchiveService(c *fiber.Ctx) error {
	item, err := h.service.ArchiveService(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("service_id"))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Service cannot be archived.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SERVICE_NOT_FOUND", "Service was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICE_ARCHIVE_FAILED", "Could not archive service.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"service": item})
}

func (h *Handler) UpdateServiceAIBookable(c *fiber.Ctx) error {
	req, err := parseAIBookableRequest(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body must include ai_bookable.")
	}
	item, err := h.service.UpdateServiceAIBookable(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("service_id"), *req.AIBookable)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Service cannot be enabled for AI booking.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SERVICE_NOT_FOUND", "Service was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "SERVICE_UPDATE_FAILED", "Could not update service.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"service": item})
}

func (h *Handler) Staff(c *fiber.Ctx) error {
	items, err := h.service.Staff(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "STAFF_FAILED", "Could not load staff.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"staff": items})
}

func (h *Handler) ProviderSwitchReadiness(c *fiber.Ctx) error {
	readiness, err := h.service.ProviderSwitchReadiness(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "PROVIDER_SWITCH_READINESS_FAILED", "Could not load provider switch readiness.")
	}
	return respond.JSON(c, fiber.StatusOK, readiness)
}

func (h *Handler) CreateProviderSwitchRun(c *fiber.Ctx) error {
	req, err := parseProviderSwitchRunRequest(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body must include a target POS provider.")
	}
	run, err := h.service.CreateProviderSwitchRun(c.UserContext(), c.Params("id"), middleware.UserID(c), *req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Target POS provider must be valid and different from the active provider.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "PROVIDER_SWITCH_RUN_CREATE_FAILED", "Could not create provider switch run.")
	}
	return respond.JSON(c, fiber.StatusCreated, fiber.Map{"run": run})
}

func (h *Handler) LatestProviderSwitchRun(c *fiber.Ctx) error {
	run, err := h.service.LatestProviderSwitchRun(c.UserContext(), c.Params("id"), middleware.UserID(c))
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "PROVIDER_SWITCH_RUN_FAILED", "Could not load provider switch run.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"run": run})
}

func (h *Handler) GetProviderSwitchRun(c *fiber.Ctx) error {
	run, err := h.service.GetProviderSwitchRun(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("run_id"))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Provider switch run id is required.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "PROVIDER_SWITCH_RUN_NOT_FOUND", "Provider switch run was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "PROVIDER_SWITCH_RUN_FAILED", "Could not load provider switch run.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"run": run})
}

func (h *Handler) ProviderSwitchDryRunReadiness(c *fiber.Ctx) error {
	readiness, err := h.service.ProviderSwitchDryRunReadiness(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("run_id"))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Provider switch run id is required.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "PROVIDER_SWITCH_RUN_NOT_FOUND", "Provider switch run was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "PROVIDER_SWITCH_DRY_RUN_READINESS_FAILED", "Could not load provider switch dry-run readiness.")
	}
	return respond.JSON(c, fiber.StatusOK, readiness)
}

func (h *Handler) UpdateProviderSwitchMatch(c *fiber.Ctx) error {
	req, err := parseProviderSwitchMatchUpdateRequest(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body must include a valid match status.")
	}
	run, err := h.service.UpdateProviderSwitchMatch(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("run_id"), c.Params("match_id"), *req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Provider switch match cannot be updated to that state.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "PROVIDER_SWITCH_MATCH_NOT_FOUND", "Provider switch match was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "PROVIDER_SWITCH_MATCH_UPDATE_FAILED", "Could not update provider switch match.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"run": run})
}

func (h *Handler) CreateStaff(c *fiber.Ctx) error {
	req, err := parseStaffWriteRequest(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body must include a valid staff member.")
	}
	item, err := h.service.CreateStaff(c.UserContext(), c.Params("id"), middleware.UserID(c), *req)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Staff name and contact fields must be valid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "SALON_NOT_FOUND", "Salon not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "STAFF_CREATE_FAILED", "Could not create staff member.")
	}
	return respond.JSON(c, fiber.StatusCreated, fiber.Map{"staff_member": item})
}

func (h *Handler) UpdateStaff(c *fiber.Ctx) error {
	req, err := parseStaffWriteRequest(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body must include a valid staff member.")
	}
	item, err := h.service.UpdateStaff(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("staff_id"), *req)
	if errors.Is(err, ErrProviderManagedFields) {
		return respond.Error(c, fiber.StatusConflict, "PROVIDER_MANAGED_FIELDS", "Staff operational fields are managed by the active POS provider. Edit them there, then sync.")
	}
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Staff name, contact fields, active state, and archive state must be valid.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "STAFF_NOT_FOUND", "Staff member was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "STAFF_UPDATE_FAILED", "Could not update staff member.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"staff_member": item})
}

func (h *Handler) ArchiveStaff(c *fiber.Ctx) error {
	item, err := h.service.ArchiveStaff(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("staff_id"))
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Staff member cannot be archived.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "STAFF_NOT_FOUND", "Staff member was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "STAFF_ARCHIVE_FAILED", "Could not archive staff member.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"staff_member": item})
}

func (h *Handler) UpdateStaffAIBookable(c *fiber.Ctx) error {
	req, err := parseAIBookableRequest(c)
	if err != nil {
		return respond.Error(c, fiber.StatusBadRequest, "INVALID_REQUEST", "Request body must include ai_bookable.")
	}
	item, err := h.service.UpdateStaffAIBookable(c.UserContext(), c.Params("id"), middleware.UserID(c), c.Params("staff_id"), *req.AIBookable)
	if errors.Is(err, ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "Staff member cannot be enabled for AI booking.")
	}
	if errors.Is(err, ErrNotFound) {
		return respond.Error(c, fiber.StatusNotFound, "STAFF_NOT_FOUND", "Staff member was not found.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "STAFF_UPDATE_FAILED", "Could not update staff member.")
	}
	return respond.JSON(c, fiber.StatusOK, fiber.Map{"staff_member": item})
}

type updateAIBookableRequest struct {
	AIBookable *bool `json:"ai_bookable"`
}

func parseServiceWriteRequest(c *fiber.Ctx) (*ServiceWriteRequest, error) {
	var req ServiceWriteRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

func parseServiceOwnerControlsWriteRequest(c *fiber.Ctx) (*ServiceOwnerControlsWriteRequest, error) {
	var req ServiceOwnerControlsWriteRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

func parseServiceCategoryWriteRequest(c *fiber.Ctx) (*ServiceCategoryWriteRequest, error) {
	var req ServiceCategoryWriteRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

func parseServiceCategoryAliasWriteRequest(c *fiber.Ctx) (*ServiceCategoryAliasWriteRequest, error) {
	var req ServiceCategoryAliasWriteRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

func parseServiceCategoryAssignRequest(c *fiber.Ctx) (*ServiceCategoryAssignRequest, error) {
	var req ServiceCategoryAssignRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

func parseStaffWriteRequest(c *fiber.Ctx) (*StaffWriteRequest, error) {
	var req StaffWriteRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

func parseProviderSwitchRunRequest(c *fiber.Ctx) (*ProviderSwitchRunRequest, error) {
	var req ProviderSwitchRunRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, err
	}
	if req.ToProvider == "" {
		return nil, ErrValidation
	}
	return &req, nil
}

func parseProviderSwitchMatchUpdateRequest(c *fiber.Ctx) (*ProviderSwitchMatchUpdateRequest, error) {
	var req ProviderSwitchMatchUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, err
	}
	if req.MatchStatus == "" {
		return nil, ErrValidation
	}
	return &req, nil
}

func parseAIBookableRequest(c *fiber.Ctx) (*updateAIBookableRequest, error) {
	var req updateAIBookableRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, err
	}
	if req.AIBookable == nil {
		return nil, ErrValidation
	}
	return &req, nil
}
