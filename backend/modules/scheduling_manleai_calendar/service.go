package scheduling_manleai_calendar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

type Store interface {
	GetAggregate(ctx context.Context, salonID string, ownerUserID string) (*Aggregate, error)
	PutConfig(ctx context.Context, salonID string, ownerUserID string, req CalendarConfigInput, fingerprint string) (*Aggregate, bool, error)
	PutHours(ctx context.Context, salonID string, ownerUserID string, req ReplaceBusinessHoursInput, fingerprint string) (*Aggregate, bool, error)
	PutStaffProfile(ctx context.Context, salonID string, ownerUserID string, staffID string, req StaffProfileInput, fingerprint string) (*Aggregate, bool, error)
	PutServicePolicy(ctx context.Context, salonID string, ownerUserID string, serviceID string, req ServicePolicyInput, fingerprint string) (*Aggregate, bool, error)
	CreateResource(ctx context.Context, salonID string, ownerUserID string, req ResourcePoolInput, fingerprint string) (*Aggregate, bool, error)
	UpdateResource(ctx context.Context, salonID string, ownerUserID string, resourceID string, req ResourcePoolInput, fingerprint string) (*Aggregate, bool, error)
	ArchiveResource(ctx context.Context, salonID string, ownerUserID string, resourceID string, req MutationMeta, fingerprint string) (*Aggregate, bool, error)
	CreateException(ctx context.Context, salonID string, ownerUserID string, req ExceptionInput, fingerprint string) (*Aggregate, bool, error)
	CancelException(ctx context.Context, salonID string, ownerUserID string, exceptionID string, req MutationMeta, fingerprint string) (*Aggregate, bool, error)
	Activate(ctx context.Context, salonID string, ownerUserID string, req MutationMeta, fingerprint string) (*Aggregate, bool, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) GetAggregate(ctx context.Context, salonID string, ownerUserID string) (*AggregateResponse, error) {
	if err := validateIdentity(salonID, ownerUserID); err != nil {
		return nil, err
	}
	aggregate, err := s.store.GetAggregate(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	return &AggregateResponse{ManleaiCalendar: aggregate}, nil
}

// SchedulingTargetReadiness evaluates the current persisted configuration and
// activation as if manleai_calendar were selected. It does not write or alter
// the aggregate and lets authority-switch preview use granular capabilities.
func (s *Service) SchedulingTargetReadiness(ctx context.Context, salonID string, ownerUserID string) (scheduling.TargetReadiness, error) {
	if err := validateIdentity(salonID, ownerUserID); err != nil {
		return scheduling.TargetReadiness{}, err
	}
	aggregate, err := s.store.GetAggregate(ctx, salonID, ownerUserID)
	if err != nil {
		return scheduling.TargetReadiness{}, err
	}
	preview := *aggregate
	preview.SchedulingAuthority = booking.SchedulingAuthorityManleAICalendar
	readiness := EvaluateReadiness(&preview)
	result := scheduling.TargetReadiness{
		TargetSchedulingAuthority: booking.SchedulingAuthorityManleAICalendar,
		AuthorityVersion:          readiness.AuthorityVersion,
		ConfigVersion:             readiness.ConfigVersion,
		Checks:                    make([]scheduling.TargetReadinessCheck, 0, 3),
		Blockers:                  make([]scheduling.TargetReadinessBlocker, 0),
		AvailabilityBlockers:      make([]scheduling.TargetReadinessBlocker, 0),
		ExecutionBlockers:         make([]scheduling.TargetReadinessBlocker, 0),
	}
	add := func(code string, ready bool, message string) {
		result.Checks = append(result.Checks, scheduling.TargetReadinessCheck{Code: code, Ready: ready, Scope: "calendar"})
		if !ready {
			result.Blockers = append(result.Blockers, scheduling.TargetReadinessBlocker{Code: code, Scope: "calendar", Message: message})
		}
	}
	add("CONFIGURATION_READY", readiness.ConfigurationReady, "Resolve the internal calendar configuration blockers.")
	add("STAFF_ONLY_AVAILABILITY_READY", readiness.Capabilities.StaffOnlyAvailability, "Activate a current staff-only calendar configuration that supports availability.")
	add("STAFF_ONLY_CREATE_READY", readiness.Capabilities.StaffOnlyCreate, "Activate a current staff-only calendar configuration that supports appointment creation.")
	for _, blocker := range readiness.Blockers {
		if blocker.Dimension != ReadinessDimensionConfiguration && blocker.Code != BlockerConfigNotActivated {
			continue
		}
		result.Blockers = appendCalendarTargetBlocker(result.Blockers, scheduling.TargetReadinessBlocker{
			Code: blocker.Code, Scope: blocker.Scope, EntityID: blocker.EntityID,
			Message: "Resolve the internal calendar readiness blocker " + blocker.Code + ".",
		})
	}
	result.AvailabilityReady = readiness.Capabilities.StaffOnlyAvailability || readiness.Capabilities.PooledCapacity
	result.ExecutionReady = readiness.Capabilities.StaffOnlyCreate || readiness.Capabilities.PooledCapacity
	if !result.AvailabilityReady {
		result.AvailabilityBlockers = calendarCapabilityBlockers(result.Blockers, scheduling.TargetReadinessBlocker{
			Code: "MANLEAI_CALENDAR_AVAILABILITY_UNAVAILABLE", Scope: "calendar",
			Message: "Activate a current internal calendar configuration with an available staff-only or pooled service.",
		})
	}
	if !result.ExecutionReady {
		result.ExecutionBlockers = calendarCapabilityBlockers(result.Blockers, scheduling.TargetReadinessBlocker{
			Code: "MANLEAI_CALENDAR_CREATE_UNAVAILABLE", Scope: "calendar",
			Message: "Activate a current internal calendar configuration that can create an appointment atomically.",
		})
	}
	result.Ready = len(result.Blockers) == 0
	return result, nil
}

func calendarCapabilityBlockers(base []scheduling.TargetReadinessBlocker, capability scheduling.TargetReadinessBlocker) []scheduling.TargetReadinessBlocker {
	result := make([]scheduling.TargetReadinessBlocker, 0, len(base)+1)
	for _, item := range base {
		if item.Code == "STAFF_ONLY_AVAILABILITY_READY" || item.Code == "STAFF_ONLY_CREATE_READY" {
			continue
		}
		result = appendCalendarTargetBlocker(result, item)
	}
	return appendCalendarTargetBlocker(result, capability)
}

func appendCalendarTargetBlocker(items []scheduling.TargetReadinessBlocker, candidate scheduling.TargetReadinessBlocker) []scheduling.TargetReadinessBlocker {
	for _, item := range items {
		if item.Code == candidate.Code && item.Scope == candidate.Scope && item.EntityID == candidate.EntityID {
			return items
		}
	}
	return append(items, candidate)
}

func (s *Service) GetStaffProfile(ctx context.Context, salonID string, ownerUserID string, staffID string) (*StaffProfileResponse, error) {
	if err := validateIdentity(salonID, ownerUserID, staffID); err != nil {
		return nil, err
	}
	aggregate, err := s.store.GetAggregate(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	for i := range aggregate.StaffProfiles {
		if aggregate.StaffProfiles[i].Staff.ID == staffID {
			return &StaffProfileResponse{StaffProfile: &aggregate.StaffProfiles[i], ConfigVersion: aggregate.ConfigVersion, Readiness: aggregate.Readiness}, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Service) GetServicePolicy(ctx context.Context, salonID string, ownerUserID string, serviceID string) (*ServicePolicyResponse, error) {
	if err := validateIdentity(salonID, ownerUserID, serviceID); err != nil {
		return nil, err
	}
	aggregate, err := s.store.GetAggregate(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	for i := range aggregate.ServicePolicies {
		if aggregate.ServicePolicies[i].Service.ID == serviceID {
			return &ServicePolicyResponse{ServicePolicy: &aggregate.ServicePolicies[i], ConfigVersion: aggregate.ConfigVersion, Readiness: aggregate.Readiness}, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Service) ListResources(ctx context.Context, salonID string, ownerUserID string) (*ResourceListResponse, error) {
	if err := validateIdentity(salonID, ownerUserID); err != nil {
		return nil, err
	}
	aggregate, err := s.store.GetAggregate(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	return &ResourceListResponse{Resources: aggregate.Resources, ConfigVersion: aggregate.ConfigVersion, Readiness: aggregate.Readiness}, nil
}

func (s *Service) PutConfig(ctx context.Context, salonID string, ownerUserID string, req CalendarConfigInput) (*MutationResponse, error) {
	if err := validateIdentity(salonID, ownerUserID); err != nil {
		return nil, err
	}
	if err := normalizeAndValidateConfig(&req); err != nil {
		return nil, err
	}
	return mutationResponse(s.store.PutConfig(ctx, salonID, ownerUserID, req, requestFingerprint(EventConfigUpdated, "", req)))
}

func (s *Service) PutHours(ctx context.Context, salonID string, ownerUserID string, req ReplaceBusinessHoursInput) (*MutationResponse, error) {
	if err := validateIdentity(salonID, ownerUserID); err != nil {
		return nil, err
	}
	if err := normalizeAndValidateHours(&req); err != nil {
		return nil, err
	}
	return mutationResponse(s.store.PutHours(ctx, salonID, ownerUserID, req, requestFingerprint(EventSalonHoursReplaced, "", req)))
}

func (s *Service) PutStaffProfile(ctx context.Context, salonID string, ownerUserID string, staffID string, req StaffProfileInput) (*MutationResponse, error) {
	if err := validateIdentity(salonID, ownerUserID, staffID); err != nil {
		return nil, err
	}
	if err := normalizeAndValidateStaffProfile(&req); err != nil {
		return nil, err
	}
	return mutationResponse(s.store.PutStaffProfile(ctx, salonID, ownerUserID, staffID, req, requestFingerprint(EventStaffScheduleReplaced, staffID, req)))
}

func (s *Service) PutServicePolicy(ctx context.Context, salonID string, ownerUserID string, serviceID string, req ServicePolicyInput) (*MutationResponse, error) {
	if err := validateIdentity(salonID, ownerUserID, serviceID); err != nil {
		return nil, err
	}
	if err := normalizeAndValidateServicePolicy(&req); err != nil {
		return nil, err
	}
	return mutationResponse(s.store.PutServicePolicy(ctx, salonID, ownerUserID, serviceID, req, requestFingerprint(EventServicePolicyUpdated, serviceID, req)))
}

func (s *Service) CreateResource(ctx context.Context, salonID string, ownerUserID string, req ResourcePoolInput) (*MutationResponse, error) {
	if err := validateIdentity(salonID, ownerUserID); err != nil {
		return nil, err
	}
	if err := normalizeAndValidateResource(&req); err != nil {
		return nil, err
	}
	return mutationResponse(s.store.CreateResource(ctx, salonID, ownerUserID, req, requestFingerprint(EventResourcePoolCreated, "", req)))
}

func (s *Service) UpdateResource(ctx context.Context, salonID string, ownerUserID string, resourceID string, req ResourcePoolInput) (*MutationResponse, error) {
	if err := validateIdentity(salonID, ownerUserID, resourceID); err != nil {
		return nil, err
	}
	if err := normalizeAndValidateResource(&req); err != nil {
		return nil, err
	}
	return mutationResponse(s.store.UpdateResource(ctx, salonID, ownerUserID, resourceID, req, requestFingerprint(EventResourcePoolUpdated, resourceID, req)))
}

func (s *Service) ArchiveResource(ctx context.Context, salonID string, ownerUserID string, resourceID string, req MutationMeta) (*MutationResponse, error) {
	if err := validateIdentity(salonID, ownerUserID, resourceID); err != nil {
		return nil, err
	}
	if err := normalizeAndValidateMeta(&req); err != nil {
		return nil, err
	}
	return mutationResponse(s.store.ArchiveResource(ctx, salonID, ownerUserID, resourceID, req, requestFingerprint(EventResourcePoolArchived, resourceID, req)))
}

func (s *Service) CreateException(ctx context.Context, salonID string, ownerUserID string, req ExceptionInput) (*MutationResponse, error) {
	if err := validateIdentity(salonID, ownerUserID); err != nil {
		return nil, err
	}
	if err := normalizeAndValidateException(&req); err != nil {
		return nil, err
	}
	return mutationResponse(s.store.CreateException(ctx, salonID, ownerUserID, req, requestFingerprint(EventExceptionCreated, "", req)))
}

func (s *Service) CancelException(ctx context.Context, salonID string, ownerUserID string, exceptionID string, req MutationMeta) (*MutationResponse, error) {
	if err := validateIdentity(salonID, ownerUserID, exceptionID); err != nil {
		return nil, err
	}
	if err := normalizeAndValidateMeta(&req); err != nil {
		return nil, err
	}
	return mutationResponse(s.store.CancelException(ctx, salonID, ownerUserID, exceptionID, req, requestFingerprint(EventExceptionCancelled, exceptionID, req)))
}

func (s *Service) Activate(ctx context.Context, salonID string, ownerUserID string, req MutationMeta) (*MutationResponse, error) {
	if err := validateIdentity(salonID, ownerUserID); err != nil {
		return nil, err
	}
	if err := normalizeAndValidateMeta(&req); err != nil {
		return nil, err
	}
	return mutationResponse(s.store.Activate(ctx, salonID, ownerUserID, req, requestFingerprint(EventConfigActivated, "", req)))
}

func mutationResponse(aggregate *Aggregate, replayed bool, err error) (*MutationResponse, error) {
	if err != nil {
		return nil, err
	}
	return &MutationResponse{ManleaiCalendar: aggregate, Replayed: replayed}, nil
}

func normalizeAndValidateConfig(req *CalendarConfigInput) error {
	if req == nil || normalizeAndValidateMeta(&req.MutationMeta) != nil {
		return ErrValidation
	}
	if req.SlotStepMinutes < 1 || req.SlotStepMinutes > 1440 || 1440%req.SlotStepMinutes != 0 ||
		req.MinimumBookingNoticeMinutes < 0 || req.MinimumBookingNoticeMinutes > 525600 ||
		req.BookingHorizonDays < 1 || req.BookingHorizonDays > 366 ||
		req.MaxPartySize < 1 || req.MaxPartySize > 100 ||
		req.DefaultBufferBeforeMinutes < 0 || req.DefaultBufferBeforeMinutes > 1440 ||
		req.DefaultBufferAfterMinutes < 0 || req.DefaultBufferAfterMinutes > 1440 ||
		!validOptionalRange(req.RescheduleCutoffMinutes, 0, 525600) ||
		!validOptionalRange(req.CancellationCutoffMinutes, 0, 525600) {
		return ErrValidation
	}
	return nil
}

func normalizeAndValidateHours(req *ReplaceBusinessHoursInput) error {
	if req == nil || normalizeAndValidateMeta(&req.MutationMeta) != nil {
		return ErrValidation
	}
	sort.Slice(req.Periods, func(i, j int) bool {
		if req.Periods[i].DayOfWeek != req.Periods[j].DayOfWeek {
			return req.Periods[i].DayOfWeek < req.Periods[j].DayOfWeek
		}
		if req.Periods[i].StartMinute != req.Periods[j].StartMinute {
			return req.Periods[i].StartMinute < req.Periods[j].StartMinute
		}
		return req.Periods[i].EndMinute < req.Periods[j].EndMinute
	})
	return validatePeriods(req.Periods, func(p BusinessHourPeriodInput) (int, int, int) { return p.DayOfWeek, p.StartMinute, p.EndMinute })
}

func normalizeAndValidateStaffProfile(req *StaffProfileInput) error {
	if req == nil || normalizeAndValidateMeta(&req.MutationMeta) != nil {
		return ErrValidation
	}
	sort.Slice(req.WeeklyPeriods, func(i, j int) bool {
		if req.WeeklyPeriods[i].DayOfWeek != req.WeeklyPeriods[j].DayOfWeek {
			return req.WeeklyPeriods[i].DayOfWeek < req.WeeklyPeriods[j].DayOfWeek
		}
		if req.WeeklyPeriods[i].StartMinute != req.WeeklyPeriods[j].StartMinute {
			return req.WeeklyPeriods[i].StartMinute < req.WeeklyPeriods[j].StartMinute
		}
		return req.WeeklyPeriods[i].EndMinute < req.WeeklyPeriods[j].EndMinute
	})
	if err := validatePeriods(req.WeeklyPeriods, func(p WeeklyPeriodInput) (int, int, int) { return p.DayOfWeek, p.StartMinute, p.EndMinute }); err != nil {
		return err
	}
	ids, err := normalizeUUIDSet(req.EligibleServiceIDs)
	if err != nil {
		return err
	}
	req.EligibleServiceIDs = ids
	return nil
}

func normalizeAndValidateServicePolicy(req *ServicePolicyInput) error {
	if req == nil || normalizeAndValidateMeta(&req.MutationMeta) != nil {
		return ErrValidation
	}
	if req.CapacityMode != nil {
		value := strings.TrimSpace(*req.CapacityMode)
		req.CapacityMode = &value
		if value != CapacityModeStaffOnly && value != CapacityModePooled {
			return ErrValidation
		}
	}
	if req.Enabled && req.CapacityMode == nil {
		return ErrValidation
	}
	if !validOptionalRange(req.BufferBeforeMinutesOverride, 0, 1440) || !validOptionalRange(req.BufferAfterMinutesOverride, 0, 1440) {
		return ErrValidation
	}
	staffIDs, err := normalizeUUIDSet(req.EligibleStaffIDs)
	if err != nil {
		return err
	}
	req.EligibleStaffIDs = staffIDs
	seenPools := make(map[string]struct{}, len(req.ResourceRequirements))
	for i := range req.ResourceRequirements {
		req.ResourceRequirements[i].ResourcePoolID = strings.TrimSpace(req.ResourceRequirements[i].ResourcePoolID)
		if _, err := uuid.Parse(req.ResourceRequirements[i].ResourcePoolID); err != nil || req.ResourceRequirements[i].UnitsRequired < 1 || req.ResourceRequirements[i].UnitsRequired > 1000 {
			return ErrValidation
		}
		if _, exists := seenPools[req.ResourceRequirements[i].ResourcePoolID]; exists {
			return ErrValidation
		}
		seenPools[req.ResourceRequirements[i].ResourcePoolID] = struct{}{}
	}
	sort.Slice(req.ResourceRequirements, func(i, j int) bool {
		return req.ResourceRequirements[i].ResourcePoolID < req.ResourceRequirements[j].ResourcePoolID
	})
	if req.CapacityMode != nil && *req.CapacityMode == CapacityModeStaffOnly && len(req.ResourceRequirements) != 0 {
		return ErrValidation
	}
	return nil
}

func normalizeAndValidateResource(req *ResourcePoolInput) error {
	if req == nil || normalizeAndValidateMeta(&req.MutationMeta) != nil {
		return ErrValidation
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || !utf8.ValidString(req.Name) || utf8.RuneCountInString(req.Name) > 200 || req.Capacity < 1 || req.Capacity > 1000 {
		return ErrValidation
	}
	return nil
}

func normalizeAndValidateException(req *ExceptionInput) error {
	if req == nil || normalizeAndValidateMeta(&req.MutationMeta) != nil {
		return ErrValidation
	}
	req.ScopeType = strings.TrimSpace(req.ScopeType)
	req.StaffID = strings.TrimSpace(req.StaffID)
	req.ResourcePoolID = strings.TrimSpace(req.ResourcePoolID)
	req.Effect = strings.TrimSpace(req.Effect)
	req.Reason = strings.TrimSpace(req.Reason)
	if !req.StartsAt.Before(req.EndsAt) || len([]byte(req.Reason)) > 2000 {
		return ErrValidation
	}
	switch req.ScopeType {
	case ExceptionScopeSalon:
		if req.StaffID != "" || req.ResourcePoolID != "" {
			return ErrValidation
		}
	case ExceptionScopeStaff:
		if req.ResourcePoolID != "" || validateUUID(req.StaffID) != nil {
			return ErrValidation
		}
	case ExceptionScopeResource:
		if req.StaffID != "" || validateUUID(req.ResourcePoolID) != nil {
			return ErrValidation
		}
	default:
		return ErrValidation
	}
	switch req.ScopeType {
	case ExceptionScopeSalon, ExceptionScopeStaff:
		if req.Effect != ExceptionEffectAvailable && req.Effect != ExceptionEffectUnavailable || req.CapacityOverride != nil {
			return ErrValidation
		}
	case ExceptionScopeResource:
		if req.Effect != ExceptionEffectCapacityOverride || req.CapacityOverride == nil || !validOptionalRange(req.CapacityOverride, 0, 1000) {
			return ErrValidation
		}
	}
	req.StartsAt = req.StartsAt.UTC()
	req.EndsAt = req.EndsAt.UTC()
	return nil
}

func normalizeAndValidateMeta(meta *MutationMeta) error {
	if meta == nil {
		return ErrValidation
	}
	meta.ActionKey = strings.TrimSpace(meta.ActionKey)
	if meta.ActionKey == "" || len([]byte(meta.ActionKey)) > 256 || meta.ExpectedConfigVersion < 0 {
		return ErrValidation
	}
	return nil
}

func validateIdentity(values ...string) error {
	for _, value := range values {
		if validateUUID(strings.TrimSpace(value)) != nil {
			return ErrValidation
		}
	}
	return nil
}

func validateUUID(value string) error {
	_, err := uuid.Parse(value)
	return err
}

func normalizeUUIDSet(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if validateUUID(value) != nil {
			return nil, ErrValidation
		}
		if _, exists := seen[value]; exists {
			return nil, ErrValidation
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func validatePeriods[T any](periods []T, extract func(T) (int, int, int)) error {
	previousDay := -1
	previousEnd := -1
	for _, period := range periods {
		day, start, end := extract(period)
		if day < 0 || day > 6 || start < 0 || start >= end || end > 1440 {
			return ErrValidation
		}
		if day == previousDay && start < previousEnd {
			return ErrValidation
		}
		previousDay = day
		previousEnd = end
	}
	return nil
}

func validOptionalRange(value *int, minimum int, maximum int) bool {
	return value == nil || (*value >= minimum && *value <= maximum)
}

func requestFingerprint(eventType string, targetID string, request any) string {
	payload, _ := json.Marshal(struct {
		EventType string `json:"event_type"`
		TargetID  string `json:"target_id,omitempty"`
		Request   any    `json:"request"`
	}{EventType: eventType, TargetID: targetID, Request: request})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func EvaluateReadiness(aggregate *Aggregate) Readiness {
	readiness := Readiness{ExecutionReady: false, Blockers: make([]ReadinessBlocker, 0)}
	if aggregate == nil {
		readiness.Blockers = append(readiness.Blockers, blocker(BlockerConfigRequired, ReadinessDimensionConfiguration, "calendar", "", "Create the internal calendar configuration."))
		readiness.Blockers = append(readiness.Blockers, blocker(BlockerPartyCreateEngineUnavailable, ReadinessDimensionExecution, "calendar", "", "Party appointment creation is not available in this phase."))
		return readiness
	}
	readiness.AuthorityVersion = aggregate.AuthorityVersion
	readiness.ConfigVersion = aggregate.ConfigVersion
	if aggregate.Config == nil {
		readiness.Blockers = append(readiness.Blockers, blocker(BlockerConfigRequired, ReadinessDimensionConfiguration, "calendar", "", "Create the internal calendar configuration."))
	}
	if len(aggregate.Hours) == 0 {
		readiness.Blockers = append(readiness.Blockers, blocker(BlockerLocalHoursRequired, ReadinessDimensionConfiguration, "calendar", "", "Add at least one local business-hours period."))
	}

	staffByID := make(map[string]StaffProfile, len(aggregate.StaffProfiles))
	for _, profile := range aggregate.StaffProfiles {
		staffByID[profile.Staff.ID] = profile
	}
	eligibleServices := 0
	enabledServices := 0
	pooledEnabled := false
	for _, policy := range aggregate.ServicePolicies {
		serviceEligible := eligibleService(policy.Service)
		if serviceEligible {
			eligibleServices++
			if !policy.Configured {
				readiness.Blockers = append(readiness.Blockers, blocker(BlockerServicePolicyRequired, ReadinessDimensionConfiguration, "service", policy.Service.ID, "Choose whether this eligible service is enabled for the internal calendar."))
				continue
			}
		}
		if !policy.Configured || !policy.Enabled {
			continue
		}
		enabledServices++
		if !serviceEligible {
			readiness.Blockers = append(readiness.Blockers, blocker(BlockerServiceIneligible, ReadinessDimensionConfiguration, "service", policy.Service.ID, "The enabled service is inactive, archived, not AI-bookable, or has no duration."))
		}
		if policy.CapacityMode == nil || (*policy.CapacityMode != CapacityModeStaffOnly && *policy.CapacityMode != CapacityModePooled) {
			readiness.Blockers = append(readiness.Blockers, blocker(BlockerServiceCapacityModeRequired, ReadinessDimensionConfiguration, "service", policy.Service.ID, "Select a supported capacity mode for this enabled service."))
		}
		if len(policy.EligibleStaff) == 0 {
			readiness.Blockers = append(readiness.Blockers, blocker(BlockerServiceStaffRequired, ReadinessDimensionConfiguration, "service", policy.Service.ID, "Assign at least one eligible staff member to this enabled service."))
		}
		for _, staff := range policy.EligibleStaff {
			profile, exists := staffByID[staff.ID]
			if !exists || !eligibleStaff(staff) {
				readiness.Blockers = append(readiness.Blockers, blocker(BlockerStaffIneligible, ReadinessDimensionConfiguration, "staff", staff.ID, "Assigned staff must be active, AI-bookable, and not archived."))
				continue
			}
			if len(profile.WeeklyPeriods) == 0 {
				readiness.Blockers = append(readiness.Blockers, blocker(BlockerStaffScheduleRequired, ReadinessDimensionConfiguration, "staff", staff.ID, "Assigned staff must have at least one weekly schedule period."))
			}
		}
		if policy.CapacityMode != nil && *policy.CapacityMode == CapacityModePooled {
			pooledEnabled = true
			if len(policy.ResourceRequirements) == 0 {
				readiness.Blockers = append(readiness.Blockers, blocker(BlockerPooledResourceRequired, ReadinessDimensionConfiguration, "service", policy.Service.ID, "A pooled-capacity service must require at least one resource pool."))
			}
		} else if len(policy.ResourceRequirements) != 0 {
			readiness.Blockers = append(readiness.Blockers, blocker(BlockerStaffOnlyResourceNotAllowed, ReadinessDimensionConfiguration, "service", policy.Service.ID, "A staff-only service cannot require pooled resources."))
		}
		for _, requirement := range policy.ResourceRequirements {
			if requirement.PoolArchivedAt != nil {
				readiness.Blockers = append(readiness.Blockers, blocker(BlockerResourceArchived, ReadinessDimensionConfiguration, "resource", requirement.ResourcePoolID, "An enabled service references an archived resource pool."))
			}
			if requirement.UnitsRequired > requirement.PoolCapacity {
				readiness.Blockers = append(readiness.Blockers, blocker(BlockerResourceCapacityExceeded, ReadinessDimensionConfiguration, "resource", requirement.ResourcePoolID, "Required resource units exceed the pool capacity."))
			}
		}
	}
	if eligibleServices == 0 {
		readiness.Blockers = append(readiness.Blockers, blocker(BlockerEligibleServicesRequired, ReadinessDimensionConfiguration, "calendar", "", "At least one active, AI-bookable service with a duration is required."))
	} else if enabledServices == 0 {
		readiness.Blockers = append(readiness.Blockers, blocker(BlockerEnabledServiceRequired, ReadinessDimensionConfiguration, "calendar", "", "Enable at least one eligible service for the internal calendar."))
	}

	readiness.ConfigurationReady = true
	for _, item := range readiness.Blockers {
		if item.Dimension == ReadinessDimensionConfiguration {
			readiness.ConfigurationReady = false
			break
		}
	}
	if aggregate.Config == nil || aggregate.Config.ActivatedAt == nil || aggregate.Config.ActivatedVersion == nil || *aggregate.Config.ActivatedVersion != aggregate.Config.Version {
		readiness.Blockers = append(readiness.Blockers, blocker(BlockerConfigNotActivated, ReadinessDimensionExecution, "calendar", "", "Activate the configuration after all configuration blockers are resolved."))
	}
	activationCurrent := aggregate.Config != nil && aggregate.Config.ActivatedAt != nil && aggregate.Config.ActivatedVersion != nil && *aggregate.Config.ActivatedVersion == aggregate.Config.Version
	staffOnlyConfigured := false
	for _, policy := range aggregate.ServicePolicies {
		if policy.Configured && policy.Enabled && policy.CapacityMode != nil && *policy.CapacityMode == CapacityModeStaffOnly &&
			eligibleService(policy.Service) && len(policy.EligibleStaff) > 0 && len(policy.ResourceRequirements) == 0 {
			staffOnlyConfigured = true
			break
		}
	}
	staffOnlyReady := readiness.ConfigurationReady && activationCurrent && aggregate.SchedulingAuthority == booking.SchedulingAuthorityManleAICalendar && staffOnlyConfigured
	readiness.Capabilities.StaffOnlyAvailability = staffOnlyReady
	readiness.Capabilities.StaffOnlyCreate = staffOnlyReady
	engineReady := readiness.ConfigurationReady && activationCurrent && aggregate.SchedulingAuthority == booking.SchedulingAuthorityManleAICalendar
	readiness.Capabilities.PooledCapacity = engineReady && pooledEnabled
	readiness.Capabilities.PartyCreate = engineReady && aggregate.Config != nil && aggregate.Config.MaxPartySize > 1 && enabledServices > 0
	readiness.Capabilities.Reschedule = engineReady && enabledServices > 0
	readiness.Capabilities.Cancel = engineReady && enabledServices > 0
	readiness.ExecutionReady = readiness.Capabilities.StaffOnlyAvailability &&
		readiness.Capabilities.StaffOnlyCreate &&
		readiness.Capabilities.PooledCapacity &&
		readiness.Capabilities.PartyCreate &&
		readiness.Capabilities.Reschedule &&
		readiness.Capabilities.Cancel
	return readiness
}

func eligibleService(service ServiceRef) bool {
	return service.Active && service.AIBookable && service.ArchivedAt == nil && service.DurationMinutes > 0
}

func eligibleStaff(staff StaffRef) bool {
	return staff.Active && staff.AIBookable && staff.ArchivedAt == nil
}

func blocker(code string, dimension string, scope string, entityID string, message string) ReadinessBlocker {
	return ReadinessBlocker{Code: code, Dimension: dimension, Scope: scope, EntityID: entityID, Message: message}
}

func describeVersionConflict(expected int64, actual int64) error {
	return fmt.Errorf("%w: expected %d, actual %d", ErrVersionConflict, expected, actual)
}
