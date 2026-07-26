package scheduling_manleai_calendar

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

var ErrAvailabilityConflictState = fmt.Errorf("%w: manleai calendar external conflict evidence is incomplete", booking.ErrAvailabilityQuoteStale)

type StaffConflict struct {
	StaffID  string
	StartsAt time.Time
	EndsAt   time.Time
}

type AvailabilitySnapshot struct {
	Aggregate                  *Aggregate
	Conflicts                  []StaffConflict
	ResourceConflicts          []ResourceConflict
	UnresolvedExternalConflict bool
	TargetOriginAuthorized     bool
}

type ResourceConflict struct {
	ResourcePoolID string
	UnitsAllocated int
	StartsAt       time.Time
	EndsAt         time.Time
}

type normalizedAvailabilitySegment struct {
	ServiceID          string
	StaffID            string
	StaffSelectionMode string
	GuestReference     string
	Quantity           int
}

type normalizedAvailabilityRequest struct {
	ServiceID          string
	StaffID            string
	StaffSelectionMode string
	PartySize          int
	Segments           []normalizedAvailabilitySegment
	PreferredDate      string
	Limit              int
}

type InternalResourceAllocation struct {
	ResourcePoolID string
	ResourceName   string
	UnitsAllocated int
}

type InternalAvailabilitySegment struct {
	ServiceID           string
	ServiceName         string
	StaffID             string
	StaffName           string
	StaffSelectionMode  string
	GuestReference      string
	Quantity            int
	StartTime           time.Time
	EndTime             time.Time
	OccupiedStartTime   time.Time
	OccupiedEndTime     time.Time
	BufferBeforeMinutes int
	BufferAfterMinutes  int
	ResourceAllocations []InternalResourceAllocation
}

type InternalAvailabilitySlot struct {
	Fingerprint         string
	ServiceID           string
	ServiceName         string
	StaffID             string
	StaffName           string
	StartTime           time.Time
	EndTime             time.Time
	OccupiedStartTime   time.Time
	OccupiedEndTime     time.Time
	BufferBeforeMinutes int
	BufferAfterMinutes  int
	Segments            []InternalAvailabilitySegment
}

type minuteRange struct {
	start int
	end   int
}

func planStaffOnlyAvailability(snapshot AvailabilitySnapshot, req normalizedAvailabilityRequest, now time.Time) ([]InternalAvailabilitySlot, error) {
	if len(req.Segments) == 0 {
		req.PartySize = 1
		req.Segments = []normalizedAvailabilitySegment{{
			ServiceID: req.ServiceID, StaffID: req.StaffID, StaffSelectionMode: req.StaffSelectionMode, Quantity: 1,
		}}
	}
	return planAggregateAvailability(snapshot, req, now)
}

func planAggregateAvailability(snapshot AvailabilitySnapshot, req normalizedAvailabilityRequest, now time.Time) ([]InternalAvailabilitySlot, error) {
	if snapshot.UnresolvedExternalConflict {
		return nil, ErrAvailabilityConflictState
	}
	if snapshot.Aggregate == nil || snapshot.Aggregate.Config == nil || strings.TrimSpace(req.PreferredDate) == "" || len(req.Segments) == 0 {
		return nil, booking.ErrValidation
	}
	aggregate := snapshot.Aggregate
	config := aggregate.Config
	if aggregate.SchedulingAuthority != booking.SchedulingAuthorityManleAICalendar && !snapshot.TargetOriginAuthorized ||
		config.ActivatedAt == nil || config.ActivatedVersion == nil || *config.ActivatedVersion != config.Version ||
		config.SlotStepMinutes <= 0 || config.BookingHorizonDays <= 0 {
		return nil, booking.ErrSchedulingAuthorityNotReady
	}
	location, err := time.LoadLocation(strings.TrimSpace(aggregate.Timezone))
	if err != nil {
		return nil, booking.ErrValidation
	}
	localDate, err := time.Parse("2006-01-02", req.PreferredDate)
	if err != nil {
		return nil, booking.ErrValidation
	}
	localNow := now.In(location)
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.UTC)
	requestedDay := time.Date(localDate.Year(), localDate.Month(), localDate.Day(), 0, 0, 0, 0, time.UTC)
	if requestedDay.Before(today) || requestedDay.After(today.AddDate(0, 0, config.BookingHorizonDays)) {
		return []InternalAvailabilitySlot{}, nil
	}

	if req.PartySize <= 0 {
		req.PartySize = 1
	}
	if config.MaxPartySize > 0 && req.PartySize > config.MaxPartySize {
		return nil, booking.ErrValidation
	}
	policiesByID := make(map[string]*ServicePolicy, len(aggregate.ServicePolicies))
	for index := range aggregate.ServicePolicies {
		candidate := &aggregate.ServicePolicies[index]
		policiesByID[candidate.Service.ID] = candidate
	}
	profilesByID := make(map[string]StaffProfile, len(aggregate.StaffProfiles))
	for _, profile := range aggregate.StaffProfiles {
		profilesByID[profile.Staff.ID] = profile
	}
	for _, segment := range req.Segments {
		policy := policiesByID[segment.ServiceID]
		if policy == nil || !policy.Configured || !policy.Enabled || policy.CapacityMode == nil || !eligibleService(policy.Service) ||
			(*policy.CapacityMode != CapacityModeStaffOnly && *policy.CapacityMode != CapacityModePooled) ||
			(*policy.CapacityMode == CapacityModeStaffOnly && len(policy.ResourceRequirements) != 0) ||
			(*policy.CapacityMode == CapacityModePooled && len(policy.ResourceRequirements) == 0) || segment.Quantity != 1 ||
			(segment.StaffSelectionMode != booking.StaffSelectionSpecific && segment.StaffSelectionMode != booking.StaffSelectionAnyone) ||
			(segment.StaffSelectionMode == booking.StaffSelectionSpecific && strings.TrimSpace(segment.StaffID) == "") ||
			(segment.StaffSelectionMode == booking.StaffSelectionAnyone && strings.TrimSpace(segment.StaffID) != "") {
			return nil, booking.ErrValidation
		}
	}
	dayOfWeek := int(time.Date(localDate.Year(), localDate.Month(), localDate.Day(), 12, 0, 0, 0, location).Weekday())
	salonBase := periodRanges(aggregate.Hours, dayOfWeek)
	minimumStart := now.UTC().Add(time.Duration(config.MinimumBookingNoticeMinutes) * time.Minute)
	limit := req.Limit
	exactScan := limit == -1
	if limit == -1 {
		// Internal create-time revalidation must scan the complete day rather than
		// only the bounded list returned by the public availability API.
		limit = 1440
	} else if limit <= 0 {
		limit = 10
	}
	if !exactScan && limit > 50 {
		limit = 50
	}
	slots := make([]InternalAvailabilitySlot, 0, limit)
	for startMinute := 0; startMinute < 1440 && len(slots) < limit; startMinute += config.SlotStepMinutes {
		startTime, ok := strictLocalMinute(localDate.Year(), localDate.Month(), localDate.Day(), startMinute, location)
		if !ok || startTime.Before(minimumStart) {
			continue
		}
		plannedSegments, valid := assignAggregateSegments(snapshot, req.Segments, 0, startMinute, localDate, location,
			dayOfWeek, salonBase, policiesByID, profilesByID, nil, map[string]int{})
		if !valid {
			continue
		}
		rootEnd := plannedSegments[0].EndTime
		for _, segment := range plannedSegments[1:] {
			if segment.EndTime.After(rootEnd) {
				rootEnd = segment.EndTime
			}
		}
		first := plannedSegments[0]
		slots = append(slots, InternalAvailabilitySlot{
			ServiceID: first.ServiceID, ServiceName: first.ServiceName, StaffID: first.StaffID, StaffName: first.StaffName,
			StartTime: startTime, EndTime: rootEnd, OccupiedStartTime: first.OccupiedStartTime, OccupiedEndTime: first.OccupiedEndTime,
			BufferBeforeMinutes: first.BufferBeforeMinutes, BufferAfterMinutes: first.BufferAfterMinutes, Segments: plannedSegments,
		})
	}
	return slots, nil
}

func assignAggregateSegments(
	snapshot AvailabilitySnapshot,
	requestedSegments []normalizedAvailabilitySegment,
	index int,
	rootStartMinute int,
	localDate time.Time,
	location *time.Location,
	dayOfWeek int,
	salonBase []minuteRange,
	policies map[string]*ServicePolicy,
	profiles map[string]StaffProfile,
	planned []InternalAvailabilitySegment,
	guestNext map[string]int,
) ([]InternalAvailabilitySegment, bool) {
	if index == len(requestedSegments) {
		return planned, true
	}
	requested := requestedSegments[index]
	policy := policies[requested.ServiceID]
	segmentStartMinute := rootStartMinute
	guestKey := strings.TrimSpace(requested.GuestReference)
	if guestKey == "" {
		guestKey = "__single_guest"
	}
	if next, exists := guestNext[guestKey]; exists {
		segmentStartMinute = next
	}
	config := snapshot.Aggregate.Config
	bufferBefore := config.DefaultBufferBeforeMinutes
	if policy.BufferBeforeMinutesOverride != nil {
		bufferBefore = *policy.BufferBeforeMinutesOverride
	}
	bufferAfter := config.DefaultBufferAfterMinutes
	if policy.BufferAfterMinutesOverride != nil {
		bufferAfter = *policy.BufferAfterMinutesOverride
	}
	segmentEndMinute := segmentStartMinute + policy.Service.DurationMinutes
	occupiedStartMinute := segmentStartMinute - bufferBefore
	occupiedEndMinute := segmentEndMinute + bufferAfter
	if occupiedStartMinute < 0 || occupiedEndMinute > 1440 || segmentEndMinute > 1440 {
		return nil, false
	}
	segmentStart, startOK := strictLocalMinute(localDate.Year(), localDate.Month(), localDate.Day(), segmentStartMinute, location)
	segmentEnd, endOK := strictLocalMinute(localDate.Year(), localDate.Month(), localDate.Day(), segmentEndMinute, location)
	occupiedStart, occupiedStartOK := strictLocalMinute(localDate.Year(), localDate.Month(), localDate.Day(), occupiedStartMinute, location)
	occupiedEnd, occupiedEndOK := strictLocalMinute(localDate.Year(), localDate.Month(), localDate.Day(), occupiedEndMinute, location)
	if !startOK || !endOK || !occupiedStartOK || !occupiedEndOK || !segmentEnd.After(segmentStart) || !occupiedEnd.After(occupiedStart) ||
		!scopeAllowsRange(salonBase, occupiedStartMinute, occupiedEndMinute, occupiedStart, occupiedEnd, snapshot.Aggregate.Exceptions, ExceptionScopeSalon, "") {
		return nil, false
	}
	allocations := make([]InternalResourceAllocation, 0, len(policy.ResourceRequirements))
	for _, requirement := range policy.ResourceRequirements {
		allocations = append(allocations, InternalResourceAllocation{
			ResourcePoolID: requirement.ResourcePoolID, ResourceName: requirement.ResourceName, UnitsAllocated: requirement.UnitsRequired,
		})
	}
	sort.Slice(allocations, func(i, j int) bool { return allocations[i].ResourcePoolID < allocations[j].ResourcePoolID })
	for _, staffID := range eligibleStaffIDs(policy, profiles, requested) {
		profile := profiles[staffID]
		staffBase := weeklyPeriodRanges(profile.WeeklyPeriods, dayOfWeek)
		if !scopeAllowsRange(staffBase, occupiedStartMinute, occupiedEndMinute, occupiedStart, occupiedEnd, snapshot.Aggregate.Exceptions, ExceptionScopeStaff, staffID) ||
			hasStaffConflict(snapshot.Conflicts, staffID, occupiedStart, occupiedEnd) || hasPlannedStaffConflict(planned, staffID, occupiedStart, occupiedEnd) {
			continue
		}
		candidate := InternalAvailabilitySegment{
			ServiceID: policy.Service.ID, ServiceName: policy.Service.Name, StaffID: staffID, StaffName: profile.Staff.Name,
			StaffSelectionMode: requested.StaffSelectionMode, GuestReference: requested.GuestReference, Quantity: 1,
			StartTime: segmentStart, EndTime: segmentEnd, OccupiedStartTime: occupiedStart, OccupiedEndTime: occupiedEnd,
			BufferBeforeMinutes: bufferBefore, BufferAfterMinutes: bufferAfter, ResourceAllocations: allocations,
		}
		nextPlanned := append(append([]InternalAvailabilitySegment{}, planned...), candidate)
		if !resourcesFit(snapshot, nextPlanned) {
			continue
		}
		nextGuest := make(map[string]int, len(guestNext)+1)
		for key, value := range guestNext {
			nextGuest[key] = value
		}
		nextGuest[guestKey] = segmentEndMinute
		if result, ok := assignAggregateSegments(snapshot, requestedSegments, index+1, rootStartMinute, localDate, location,
			dayOfWeek, salonBase, policies, profiles, nextPlanned, nextGuest); ok {
			return result, true
		}
	}
	return nil, false
}

func eligibleStaffIDs(policy *ServicePolicy, profiles map[string]StaffProfile, requested normalizedAvailabilitySegment) []string {
	staffIDs := make([]string, 0, len(policy.EligibleStaff))
	for _, staff := range policy.EligibleStaff {
		profile, ok := profiles[staff.ID]
		if !ok || !eligibleStaff(staff) || len(profile.WeeklyPeriods) == 0 ||
			(requested.StaffSelectionMode == booking.StaffSelectionSpecific && staff.ID != requested.StaffID) {
			continue
		}
		staffIDs = append(staffIDs, staff.ID)
	}
	sort.Strings(staffIDs)
	return staffIDs
}

func hasPlannedStaffConflict(segments []InternalAvailabilitySegment, staffID string, startsAt time.Time, endsAt time.Time) bool {
	for _, segment := range segments {
		if segment.StaffID == staffID && rangesOverlap(startsAt, endsAt, segment.OccupiedStartTime, segment.OccupiedEndTime) {
			return true
		}
	}
	return false
}

func resourcesFit(snapshot AvailabilitySnapshot, segments []InternalAvailabilitySegment) bool {
	if snapshot.Aggregate == nil {
		return false
	}
	baseCapacity := make(map[string]int, len(snapshot.Aggregate.Resources))
	for _, pool := range snapshot.Aggregate.Resources {
		if pool.ArchivedAt == nil {
			baseCapacity[pool.ID] = pool.Capacity
		}
	}
	poolIDs := map[string]struct{}{}
	for _, segment := range segments {
		for _, allocation := range segment.ResourceAllocations {
			poolIDs[allocation.ResourcePoolID] = struct{}{}
		}
	}
	for poolID := range poolIDs {
		capacity, exists := baseCapacity[poolID]
		if !exists || capacity < 1 {
			return false
		}
		boundaries := []time.Time{}
		for _, segment := range segments {
			if allocationUnits(segment.ResourceAllocations, poolID) > 0 {
				boundaries = append(boundaries, segment.OccupiedStartTime, segment.OccupiedEndTime)
			}
		}
		for _, existing := range snapshot.ResourceConflicts {
			if existing.ResourcePoolID == poolID {
				boundaries = append(boundaries, existing.StartsAt, existing.EndsAt)
			}
		}
		for _, exception := range snapshot.Aggregate.Exceptions {
			if exception.CancelledAt == nil && exception.ScopeType == ExceptionScopeResource && exception.ResourcePoolID == poolID && exception.Effect == ExceptionEffectCapacityOverride {
				boundaries = append(boundaries, exception.StartsAt, exception.EndsAt)
			}
		}
		sort.Slice(boundaries, func(i, j int) bool { return boundaries[i].Before(boundaries[j]) })
		for index := 0; index+1 < len(boundaries); index++ {
			intervalStart, intervalEnd := boundaries[index], boundaries[index+1]
			if !intervalEnd.After(intervalStart) {
				continue
			}
			effective := capacity
			for _, exception := range snapshot.Aggregate.Exceptions {
				if exception.CancelledAt == nil && exception.ScopeType == ExceptionScopeResource && exception.ResourcePoolID == poolID && exception.Effect == ExceptionEffectCapacityOverride &&
					exception.CapacityOverride != nil && !intervalStart.Before(exception.StartsAt) && intervalStart.Before(exception.EndsAt) {
					effective = *exception.CapacityOverride
					break
				}
			}
			used := 0
			for _, existing := range snapshot.ResourceConflicts {
				if existing.ResourcePoolID == poolID && rangesOverlap(intervalStart, intervalEnd, existing.StartsAt, existing.EndsAt) {
					used += existing.UnitsAllocated
				}
			}
			for _, segment := range segments {
				if rangesOverlap(intervalStart, intervalEnd, segment.OccupiedStartTime, segment.OccupiedEndTime) {
					used += allocationUnits(segment.ResourceAllocations, poolID)
				}
			}
			if used > effective {
				return false
			}
		}
	}
	return true
}

func allocationUnits(allocations []InternalResourceAllocation, poolID string) int {
	for _, allocation := range allocations {
		if allocation.ResourcePoolID == poolID {
			return allocation.UnitsAllocated
		}
	}
	return 0
}

func strictLocalMinute(year int, month time.Month, day int, minute int, location *time.Location) (time.Time, bool) {
	if minute < 0 || minute > 1440 {
		return time.Time{}, false
	}
	if minute == 1440 {
		base := time.Date(year, month, day, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
		return strictLocalInstant(base.Year(), base.Month(), base.Day(), 0, 0, location)
	}
	return strictLocalInstant(year, month, day, minute/60, minute%60, location)
}

func strictLocalInstant(year int, month time.Month, day int, hour int, minute int, location *time.Location) (time.Time, bool) {
	if location == nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return time.Time{}, false
	}
	wall := time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
	offsets := map[int]struct{}{}
	for sample := wall.Add(-36 * time.Hour); !sample.After(wall.Add(36 * time.Hour)); sample = sample.Add(30 * time.Minute) {
		_, offset := sample.In(location).Zone()
		offsets[offset] = struct{}{}
	}
	matches := make([]time.Time, 0, 2)
	for offset := range offsets {
		candidate := wall.Add(-time.Duration(offset) * time.Second)
		local := candidate.In(location)
		if local.Year() == year && local.Month() == month && local.Day() == day && local.Hour() == hour && local.Minute() == minute {
			matches = append(matches, candidate.UTC())
		}
	}
	if len(matches) != 1 {
		return time.Time{}, false
	}
	return matches[0], true
}

func periodRanges(periods []BusinessHourPeriod, dayOfWeek int) []minuteRange {
	result := make([]minuteRange, 0, len(periods))
	for _, period := range periods {
		if period.DayOfWeek == dayOfWeek {
			result = append(result, minuteRange{start: period.StartMinute, end: period.EndMinute})
		}
	}
	return result
}

func weeklyPeriodRanges(periods []WeeklyPeriod, dayOfWeek int) []minuteRange {
	result := make([]minuteRange, 0, len(periods))
	for _, period := range periods {
		if period.DayOfWeek == dayOfWeek {
			result = append(result, minuteRange{start: period.StartMinute, end: period.EndMinute})
		}
	}
	return result
}

func scopeAllowsRange(base []minuteRange, startMinute int, endMinute int, startsAt time.Time, endsAt time.Time, exceptions []CalendarException, scope string, entityID string) bool {
	allowed := false
	for _, period := range base {
		if startMinute >= period.start && endMinute <= period.end {
			allowed = true
			break
		}
	}
	for _, exception := range exceptions {
		if exception.CancelledAt != nil || exception.ScopeType != scope || (scope == ExceptionScopeStaff && exception.StaffID != entityID) {
			continue
		}
		if exception.Effect == ExceptionEffectUnavailable && rangesOverlap(startsAt, endsAt, exception.StartsAt, exception.EndsAt) {
			return false
		}
		if exception.Effect == ExceptionEffectAvailable && !startsAt.Before(exception.StartsAt) && !endsAt.After(exception.EndsAt) {
			allowed = true
		}
	}
	return allowed
}

func hasStaffConflict(conflicts []StaffConflict, staffID string, startsAt time.Time, endsAt time.Time) bool {
	for _, conflict := range conflicts {
		if conflict.StaffID == staffID && rangesOverlap(startsAt, endsAt, conflict.StartsAt, conflict.EndsAt) {
			return true
		}
	}
	return false
}

func rangesOverlap(leftStart time.Time, leftEnd time.Time, rightStart time.Time, rightEnd time.Time) bool {
	return leftStart.Before(rightEnd) && rightStart.Before(leftEnd)
}
