package conversation

import (
	"context"
	"fmt"
	"github.com/manleai/ai-receptionist/modules/booking"
	"sort"
	"strings"
	"time"
)

func (s *Service) applyAvailabilityForRequestedTime(ctx context.Context, ownerUserID string, turn *TurnRecord, session *Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (bool, error) {
	if session == nil || session.RequestedStartTime == nil {
		return false, nil
	}
	preferredDate := preferredDateFromMessage("", session.RequestedStartTime, timezoneLocation(cfg.Timezone), s.now)
	if preferredDate == "" {
		return false, nil
	}
	result, err := s.availableSlotsWithLimit(ctx, turn.SalonID, ownerUserID, *session, preferredDate, exactAvailabilityLimit)
	if err != nil {
		return false, err
	}
	slots := []booking.AvailabilitySlot{}
	if result != nil {
		slots = result.Slots
	}
	matches := exactAvailabilityMatches(slots, *session)
	if len(matches) > 0 {
		selection, err := s.selectAvailabilitySlot(ctx, turn.SalonID, *session, matches, cfg)
		if err != nil {
			return false, err
		}
		turn.ToolMessage = availabilityToolMessage(len(slots))
		applyAssignmentSelectionMetadata(turn, selection)
		applySelectedOfferedSlot(session, offeredSlotFromAvailability(result, selection.Slot))
		syncTurnUpdate(turn, *session, services, staff, cfg)
		return true, nil
	}
	if handled, err := s.applySpecificStaffUnavailableOffer(ctx, ownerUserID, turn, session, services, staff, cfg, preferredDate, result); err != nil {
		return false, err
	} else if handled {
		return false, nil
	}
	if shouldOfferPartySplitAvailability(*session) {
		options, err := s.planPartySplitOptions(ctx, ownerUserID, turn.SalonID, *session, services, preferredDate, cfg)
		if err != nil {
			return false, err
		}
		if len(options) > 0 {
			applyPartySplitOffer(turn, session, services, staff, cfg, options, true)
			return false, nil
		}
	}
	applyAvailabilityOffer(turn, session, services, staff, cfg, result, true)
	return false, nil
}

func shouldCheckAvailabilityForRequestedTime(before Session, after Session, selectedOfferedSlot bool) bool {
	if selectedOfferedSlot || strings.TrimSpace(after.ServiceID) == "" || after.RequestedStartTime == nil {
		return false
	}
	return !sameAvailabilityRequest(before, after) || !hasStaffAssignment(before)
}

func sameAvailabilityRequest(left Session, right Session) bool {
	if strings.TrimSpace(left.ServiceID) != strings.TrimSpace(right.ServiceID) ||
		strings.TrimSpace(left.RequestedDate) != strings.TrimSpace(right.RequestedDate) ||
		!sameOptionalTime(left.RequestedStartTime, right.RequestedStartTime) {
		return false
	}
	leftMode := staffSelectionModeForAvailability(left)
	rightMode := staffSelectionModeForAvailability(right)
	if leftMode != rightMode {
		return false
	}
	leftSegments := availabilitySegmentsForSession(left, leftMode)
	rightSegments := availabilitySegmentsForSession(right, rightMode)
	if len(leftSegments) != len(rightSegments) {
		return false
	}
	for index := range leftSegments {
		if strings.TrimSpace(leftSegments[index].ServiceID) != strings.TrimSpace(rightSegments[index].ServiceID) ||
			strings.TrimSpace(leftSegments[index].StaffID) != strings.TrimSpace(rightSegments[index].StaffID) ||
			normalizeConversationStaffSelectionMode(leftSegments[index].StaffSelectionMode) != normalizeConversationStaffSelectionMode(rightSegments[index].StaffSelectionMode) {
			return false
		}
	}
	return len(leftSegments) > 0
}

func sameOptionalTime(left *time.Time, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func exactAvailabilityMatches(slots []booking.AvailabilitySlot, session Session) []booking.AvailabilitySlot {
	if session.RequestedStartTime == nil {
		return nil
	}
	matches := []booking.AvailabilitySlot{}
	for _, slot := range slots {
		if !slot.StartTime.Equal(*session.RequestedStartTime) {
			continue
		}
		if staffID := strings.TrimSpace(session.StaffID); staffID != "" && availabilitySlotStaffID(slot) != staffID {
			continue
		}
		matches = append(matches, slot)
	}
	return matches
}

func (s *Service) selectAvailabilitySlot(ctx context.Context, salonID string, session Session, matches []booking.AvailabilitySlot, cfg *RuntimeConfig) (availabilitySelection, error) {
	if len(matches) == 0 {
		return availabilitySelection{}, ErrNotFound
	}
	if staffSelectionModeForAvailability(session) == booking.StaffSelectionSpecific && strings.TrimSpace(session.StaffID) != "" {
		sort.SliceStable(matches, stableAvailabilitySlotLess(matches))
		slot := matches[0]
		return availabilitySelection{
			Slot:   slot,
			Policy: "customer_requested_staff",
			Candidates: []assignmentCandidate{{
				StaffID:   availabilitySlotStaffID(slot),
				StaffName: availabilitySlotStaffName(slot),
				Slot:      slot,
			}},
		}, nil
	}
	return s.selectFairAvailabilitySlot(ctx, salonID, session, matches, cfg)
}

func (s *Service) selectFairAvailabilitySlot(ctx context.Context, salonID string, session Session, matches []booking.AvailabilitySlot, cfg *RuntimeConfig) (availabilitySelection, error) {
	unique := uniqueAvailabilitySlotsByStaff(matches)
	if len(unique) == 0 {
		unique = append([]booking.AvailabilitySlot(nil), matches...)
	}
	staffIDs := make([]string, 0, len(unique))
	for _, slot := range unique {
		if staffID := availabilitySlotStaffID(slot); staffID != "" {
			staffIDs = append(staffIDs, staffID)
		}
	}
	from, to := assignmentStatsWindow(session.RequestedStartTime, timezoneLocation(timezoneFromConfig(cfg)))
	stats, err := s.store.ListStaffAssignmentStats(ctx, salonID, staffIDs, from, to)
	if err != nil {
		return availabilitySelection{}, err
	}
	candidates := make([]assignmentCandidate, 0, len(unique))
	for _, slot := range unique {
		staffID := availabilitySlotStaffID(slot)
		stat := stats[staffID]
		candidates = append(candidates, assignmentCandidate{
			StaffID:        staffID,
			StaffName:      availabilitySlotStaffName(slot),
			AssignedCount:  stat.AssignedCount,
			LastAssignedAt: stat.LastAssignedAt,
			Slot:           slot,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return assignmentCandidateLess(candidates[i], candidates[j])
	})
	return availabilitySelection{
		Slot:       candidates[0].Slot,
		Policy:     "fair_rotation",
		Candidates: candidates,
	}, nil
}

func stableAvailabilitySlotLess(slots []booking.AvailabilitySlot) func(i int, j int) bool {
	return func(i int, j int) bool {
		leftID := availabilitySlotStaffID(slots[i])
		rightID := availabilitySlotStaffID(slots[j])
		if leftID != rightID {
			return leftID < rightID
		}
		leftName := strings.ToLower(availabilitySlotStaffName(slots[i]))
		rightName := strings.ToLower(availabilitySlotStaffName(slots[j]))
		if leftName != rightName {
			return leftName < rightName
		}
		return slots[i].StartTime.Before(slots[j].StartTime)
	}
}

func uniqueAvailabilitySlotsByStaff(slots []booking.AvailabilitySlot) []booking.AvailabilitySlot {
	out := make([]booking.AvailabilitySlot, 0, len(slots))
	seen := map[string]bool{}
	for _, slot := range slots {
		staffID := availabilitySlotStaffID(slot)
		if staffID == "" {
			continue
		}
		if seen[staffID] {
			continue
		}
		seen[staffID] = true
		out = append(out, slot)
	}
	return out
}

func availabilitySlotStaffID(slot booking.AvailabilitySlot) string {
	if staffID := strings.TrimSpace(slot.StaffID); staffID != "" {
		return staffID
	}
	for _, segment := range slot.Segments {
		if staffID := strings.TrimSpace(segment.StaffID); staffID != "" {
			return staffID
		}
	}
	return ""
}

func availabilitySlotStaffName(slot booking.AvailabilitySlot) string {
	if name := strings.TrimSpace(slot.StaffName); name != "" {
		return name
	}
	for _, segment := range slot.Segments {
		if name := strings.TrimSpace(segment.StaffName); name != "" {
			return name
		}
	}
	return ""
}

func assignmentStatsWindow(requestedStartTime *time.Time, loc *time.Location) (time.Time, time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	if requestedStartTime == nil || requestedStartTime.IsZero() {
		now := time.Now().In(loc)
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		return start.UTC(), start.AddDate(0, 0, 1).UTC()
	}
	local := requestedStartTime.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return start.UTC(), start.AddDate(0, 0, 1).UTC()
}

func assignmentCandidateLess(left assignmentCandidate, right assignmentCandidate) bool {
	if left.AssignedCount != right.AssignedCount {
		return left.AssignedCount < right.AssignedCount
	}
	if left.LastAssignedAt == nil && right.LastAssignedAt != nil {
		return true
	}
	if left.LastAssignedAt != nil && right.LastAssignedAt == nil {
		return false
	}
	if left.LastAssignedAt != nil && right.LastAssignedAt != nil && !left.LastAssignedAt.Equal(*right.LastAssignedAt) {
		return left.LastAssignedAt.Before(*right.LastAssignedAt)
	}
	if left.StaffID != right.StaffID {
		return left.StaffID < right.StaffID
	}
	return strings.ToLower(left.StaffName) < strings.ToLower(right.StaffName)
}

func applyAssignmentSelectionMetadata(turn *TurnRecord, selection availabilitySelection) {
	if turn == nil {
		return
	}
	candidates := make([]map[string]any, 0, len(selection.Candidates))
	for _, item := range selection.Candidates {
		entry := map[string]any{
			"staff_id":       item.StaffID,
			"staff_name":     item.StaffName,
			"assigned_count": item.AssignedCount,
		}
		if item.LastAssignedAt != nil {
			entry["last_assigned_at"] = item.LastAssignedAt.Format(time.RFC3339)
		}
		candidates = append(candidates, entry)
	}
	turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, map[string]any{
		"assignment_policy":      selection.Policy,
		"selected_staff_id":      availabilitySlotStaffID(selection.Slot),
		"selected_staff_name":    availabilitySlotStaffName(selection.Slot),
		"assignment_candidates":  candidates,
		"assignment_candidate_n": len(candidates),
	})
}

func (s *Service) offerAvailableSlots(ctx context.Context, ownerUserID string, turn *TurnRecord, session *Session, services []ServiceOption, staff []StaffOption, preferredDate string, unavailableRequestedTime bool, cfg *RuntimeConfig) error {
	result, err := s.availableSlots(ctx, turn.SalonID, ownerUserID, *session, preferredDate)
	if err != nil {
		return err
	}
	offered := offeredSlotsFromAvailabilityForSession(result, *session, timezoneLocation(timezoneFromConfig(cfg)))
	if len(offered) == 0 && shouldOfferPartySplitAvailability(*session) {
		options, err := s.planPartySplitOptions(ctx, ownerUserID, turn.SalonID, *session, services, preferredDate, cfg)
		if err != nil {
			return err
		}
		if len(options) > 0 {
			applyPartySplitOffer(turn, session, services, staff, cfg, options, unavailableRequestedTime)
			return nil
		}
	}
	applyAvailabilityOffer(turn, session, services, staff, cfg, result, unavailableRequestedTime)
	return nil
}

func (s *Service) availableSlots(ctx context.Context, salonID string, ownerUserID string, session Session, preferredDate string) (*booking.AvailabilityResult, error) {
	limit := availabilityOfferLimit
	if _, ok := activeSlotTimePreference(session); ok {
		limit = exactAvailabilityLimit
	}
	return s.availableSlotsWithLimit(ctx, salonID, ownerUserID, session, preferredDate, limit)
}

func (s *Service) availableSlotsWithLimit(ctx context.Context, salonID string, ownerUserID string, session Session, preferredDate string, limit int) (*booking.AvailabilityResult, error) {
	if s.bookingTool == nil {
		return nil, fmt.Errorf("booking tool is unavailable")
	}
	staffSelectionMode := staffSelectionModeForAvailability(session)
	if limit <= 0 {
		limit = availabilityOfferLimit
	}
	req := booking.AvailabilityRequest{
		ServiceID:          session.ServiceID,
		StaffID:            staffIDForAvailability(session),
		StaffSelectionMode: staffSelectionMode,
		Segments:           availabilitySegmentsForSession(session, staffSelectionMode),
		PreferredDate:      preferredDate,
		Limit:              limit,
	}
	startedAt := time.Now()
	result, err := s.bookingTool.AvailableSlots(ctx, salonID, ownerUserID, req)
	recordTurnTiming(ctx, TurnTimingStageAvailabilityPOS, startedAt, turnTimingResult(err))
	if err != nil || result == nil {
		return result, err
	}
	if strings.TrimSpace(result.StaffSelectionMode) == "" {
		result.StaffSelectionMode = req.StaffSelectionMode
	}
	for i := range result.Slots {
		if strings.TrimSpace(result.Slots[i].StaffSelectionMode) == "" {
			result.Slots[i].StaffSelectionMode = req.StaffSelectionMode
		}
		for j := range result.Slots[i].Segments {
			if strings.TrimSpace(result.Slots[i].Segments[j].StaffSelectionMode) == "" {
				result.Slots[i].Segments[j].StaffSelectionMode = req.StaffSelectionMode
			}
		}
	}
	return result, nil
}

func (s *Service) applySpecificStaffUnavailableOffer(ctx context.Context, ownerUserID string, turn *TurnRecord, session *Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, preferredDate string, requestedStaffResult *booking.AvailabilityResult) (bool, error) {
	if session == nil || session.RequestedStartTime == nil || staffSelectionModeForAvailability(*session) != booking.StaffSelectionSpecific || strings.TrimSpace(session.StaffID) == "" {
		return false, nil
	}
	requestedStart := *session.RequestedStartTime
	anyoneSession := *session
	anyoneSession.StaffID = ""
	anyoneSession.StaffName = ""
	anyoneSession.StaffSelectionMode = booking.StaffSelectionAnyone
	clearBookingSegmentsStaffSelection(&anyoneSession)
	anyoneResult, err := s.availableSlotsWithLimit(ctx, turn.SalonID, ownerUserID, anyoneSession, preferredDate, exactAvailabilityLimit)
	if err != nil {
		return false, err
	}

	otherStaffMatches := exactAvailabilityMatches(availabilitySlots(anyoneResult), anyoneSession)
	filteredOtherStaffMatches := make([]booking.AvailabilitySlot, 0, len(otherStaffMatches))
	for _, slot := range otherStaffMatches {
		if availabilitySlotStaffID(slot) == strings.TrimSpace(session.StaffID) {
			continue
		}
		filteredOtherStaffMatches = append(filteredOtherStaffMatches, slot)
	}

	offered := []OfferedSlot{}
	if len(filteredOtherStaffMatches) > 0 {
		selection, err := s.selectFairAvailabilitySlot(ctx, turn.SalonID, anyoneSession, filteredOtherStaffMatches, cfg)
		if err != nil {
			return false, err
		}
		applyAssignmentSelectionMetadata(turn, selection)
		offered = append(offered, offeredSlotFromAvailability(anyoneResult, selection.Slot))
	}
	for _, slot := range offeredSlotsFromAvailability(requestedStaffResult) {
		if len(offered) >= availabilityOfferLimit {
			break
		}
		if offeredSlotAlreadyIncluded(offered, slot) {
			continue
		}
		offered = append(offered, slot)
	}

	session.RequestedStartTime = nil
	session.OfferedSlots = offered
	turn.ToolMessage = availabilityToolMessage(len(offered))
	turn.AIMessage = formatSpecificStaffUnavailableOffer(*session, staff, requestedStart, offered, timezoneLocation(timezoneFromConfig(cfg)))
	turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, map[string]any{
		"availability_policy":          "specific_staff_unavailable",
		"requested_staff_id":           strings.TrimSpace(session.StaffID),
		"requested_staff_name":         staffName(session.StaffID, staff, session.StaffName),
		"same_time_alternative_count":  len(filteredOtherStaffMatches),
		"requested_staff_option_count": len(offeredSlotsFromAvailability(requestedStaffResult)),
	})
	syncTurnUpdate(turn, *session, services, staff, cfg)
	return true, nil
}

func availabilitySlots(result *booking.AvailabilityResult) []booking.AvailabilitySlot {
	if result == nil {
		return nil
	}
	return result.Slots
}

func offeredSlotAlreadyIncluded(slots []OfferedSlot, candidate OfferedSlot) bool {
	for _, slot := range slots {
		if slot.StartTime.Equal(candidate.StartTime) && strings.TrimSpace(slot.StaffID) == strings.TrimSpace(candidate.StaffID) {
			return true
		}
	}
	return false
}

func applyAvailabilityOffer(turn *TurnRecord, session *Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, result *booking.AvailabilityResult, unavailableRequestedTime bool) {
	offered := offeredSlotsFromAvailabilityForSession(result, *session, timezoneLocation(timezoneFromConfig(cfg)))
	session.RequestedStartTime = nil
	session.OfferedSlots = offered
	turn.ToolMessage = availabilityToolMessage(len(offered))
	if len(offered) == 0 {
		turn.AIMessage = "I do not see open times for that day. What other day works?"
	} else {
		turn.AIMessage = formatSlotOfferForSession(offered, timezoneLocation(cfg.Timezone), unavailableRequestedTime, *session, services)
	}
	syncTurnUpdate(turn, *session, services, staff, cfg)
}

type partySplitCandidate struct {
	Segment   booking.BookingSegmentRequest
	StartTime time.Time
	EndTime   time.Time
	StaffID   string
	StaffName string
}

type partySplitSelection struct {
	Option               PartySplitOption
	DateConsentConfirmed bool
}

func shouldOfferPartySplitAvailability(session Session) bool {
	return activePartyPlan(session.PartyPlan) &&
		partyPlanComplete(session.PartyPlan) &&
		!partyPlanHasSelectedSplitOption(session.PartyPlan) &&
		strings.TrimSpace(session.RequestedDate) != "" &&
		len(session.BookingSegments) > 1
}

func (s *Service) planPartySplitOptions(ctx context.Context, ownerUserID string, salonID string, session Session, services []ServiceOption, preferredDate string, cfg *RuntimeConfig) ([]PartySplitOption, error) {
	segments := bookingSegmentsForCreate(session)
	if len(segments) < 2 {
		return nil, nil
	}
	candidateSets := make([][]partySplitCandidate, 0, len(segments))
	for _, segment := range segments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" {
			return nil, nil
		}
		segmentSession := session
		segmentSession.ServiceID = serviceID
		segmentSession.StaffID = strings.TrimSpace(segment.StaffID)
		segmentSession.StaffSelectionMode = firstNonEmpty(segment.StaffSelectionMode, session.StaffSelectionMode, booking.StaffSelectionAnyone)
		segmentSession.BookingSegments = []booking.BookingSegmentRequest{{
			ServiceID:          serviceID,
			StaffID:            strings.TrimSpace(segment.StaffID),
			StaffSelectionMode: firstNonEmpty(segment.StaffSelectionMode, session.StaffSelectionMode, booking.StaffSelectionAnyone),
		}}
		segmentSession.RequestedStartTime = nil
		segmentSession.OfferedSlots = nil
		segmentSession.PartyPlan = nil

		result, err := s.availableSlotsWithLimit(ctx, salonID, ownerUserID, segmentSession, preferredDate, splitAvailabilityLimit)
		if err != nil {
			return nil, err
		}
		candidates := partySplitCandidatesFromAvailability(segmentSession.BookingSegments[0], result)
		if len(candidates) == 0 {
			return nil, nil
		}
		candidateSets = append(candidateSets, candidates)
	}
	options := rankPartySplitOptions(partySplitOptionsFromCandidates(candidateSets, session.RequestedDate, timezoneLocation(timezoneFromConfig(cfg))), splitPartyOptionLimit)
	return options, nil
}

func partySplitCandidatesFromAvailability(segment booking.BookingSegmentRequest, result *booking.AvailabilityResult) []partySplitCandidate {
	if result == nil || len(result.Slots) == 0 {
		return nil
	}
	out := make([]partySplitCandidate, 0, len(result.Slots))
	for _, rawSlot := range result.Slots {
		if len(out) >= splitAvailabilityLimit {
			break
		}
		if rawSlot.StartTime.IsZero() || rawSlot.EndTime.IsZero() {
			continue
		}
		slot := offeredSlotFromAvailability(result, rawSlot)
		if slot.StartTime.IsZero() || slot.EndTime.IsZero() {
			continue
		}
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" {
			continue
		}
		staffID := strings.TrimSpace(segment.StaffID)
		staffName := ""
		mode := firstNonEmpty(segment.StaffSelectionMode, slot.StaffSelectionMode, booking.StaffSelectionAnyone)
		for _, item := range slot.Segments {
			if strings.TrimSpace(item.ServiceID) != serviceID {
				continue
			}
			staffID = firstNonEmpty(staffID, item.StaffID)
			staffName = firstNonEmpty(staffName, item.StaffName)
			mode = firstNonEmpty(item.StaffSelectionMode, mode)
			break
		}
		staffID = firstNonEmpty(staffID, slot.StaffID)
		staffName = firstNonEmpty(staffName, slot.StaffName)
		if staffID == "" {
			continue
		}
		if mode == "" || mode == booking.StaffSelectionAnyone {
			mode = booking.StaffSelectionSpecific
		}
		out = append(out, partySplitCandidate{
			Segment: booking.BookingSegmentRequest{
				ServiceID:          serviceID,
				StaffID:            staffID,
				StaffSelectionMode: mode,
			},
			StartTime: slot.StartTime,
			EndTime:   slot.EndTime,
			StaffID:   staffID,
			StaffName: staffName,
		})
	}
	return out
}

func partySplitOptionsFromCandidates(candidateSets [][]partySplitCandidate, requestedDate string, loc *time.Location) []PartySplitOption {
	if len(candidateSets) == 0 {
		return nil
	}
	options := []PartySplitOption{}
	current := make([]partySplitCandidate, 0, len(candidateSets))
	var walk func(int)
	walk = func(index int) {
		if len(options) >= splitCombinationLimit {
			return
		}
		if index >= len(candidateSets) {
			options = append(options, partySplitOptionFromCandidates(current, requestedDate, loc))
			return
		}
		for _, candidate := range candidateSets[index] {
			if partySplitCandidateConflicts(current, candidate) {
				continue
			}
			current = append(current, candidate)
			walk(index + 1)
			current = current[:len(current)-1]
			if len(options) >= splitCombinationLimit {
				return
			}
		}
	}
	walk(0)
	return dedupePartySplitOptions(options)
}

func partySplitCandidateConflicts(existing []partySplitCandidate, candidate partySplitCandidate) bool {
	for _, item := range existing {
		if strings.TrimSpace(item.StaffID) == "" || strings.TrimSpace(candidate.StaffID) == "" {
			continue
		}
		if strings.TrimSpace(item.StaffID) != strings.TrimSpace(candidate.StaffID) {
			continue
		}
		if candidate.StartTime.Before(item.EndTime) && item.StartTime.Before(candidate.EndTime) {
			return true
		}
	}
	return false
}

func partySplitOptionFromCandidates(candidates []partySplitCandidate, requestedDate string, loc *time.Location) PartySplitOption {
	ordered := append([]partySplitCandidate(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].StartTime.Equal(ordered[j].StartTime) {
			return ordered[i].StartTime.Before(ordered[j].StartTime)
		}
		return strings.TrimSpace(ordered[i].Segment.ServiceID) < strings.TrimSpace(ordered[j].Segment.ServiceID)
	})
	blocks := []PartySplitBlock{}
	blockIndexes := map[string]int{}
	var firstStart time.Time
	var lastStart time.Time
	var lastEnd time.Time
	for _, candidate := range ordered {
		if firstStart.IsZero() || candidate.StartTime.Before(firstStart) {
			firstStart = candidate.StartTime
		}
		if lastStart.IsZero() || candidate.StartTime.After(lastStart) {
			lastStart = candidate.StartTime
		}
		if lastEnd.IsZero() || candidate.EndTime.After(lastEnd) {
			lastEnd = candidate.EndTime
		}
		key := candidate.StartTime.Format(time.RFC3339)
		index, ok := blockIndexes[key]
		if !ok {
			index = len(blocks)
			blockIndexes[key] = index
			blocks = append(blocks, PartySplitBlock{
				StartTime: candidate.StartTime,
				EndTime:   candidate.EndTime,
				Segments:  []booking.BookingSegmentRequest{},
			})
		}
		if candidate.EndTime.After(blocks[index].EndTime) {
			blocks[index].EndTime = candidate.EndTime
		}
		blocks[index].Segments = append(blocks[index].Segments, candidate.Segment)
	}
	sort.SliceStable(blocks, func(i, j int) bool {
		return blocks[i].StartTime.Before(blocks[j].StartTime)
	})
	option := PartySplitOption{
		ID:                  partySplitOptionID(blocks),
		Blocks:              blocks,
		SpanMinutes:         int(lastEnd.Sub(firstStart).Minutes()),
		FinishSpreadMinutes: int(lastEnd.Sub(lastStart).Minutes()),
	}
	option = applyPartySplitDatePolicy(option, requestedDate, loc)
	return option
}

func applyPartySplitDatePolicy(option PartySplitOption, requestedDate string, loc *time.Location) PartySplitOption {
	policy := partySplitDatePolicy(option, requestedDate, loc)
	option.DatePolicy = policy
	option.RequiresDateConsent = policy != partySplitDatePolicyRequestedDate
	option.DateConsentConfirmed = !option.RequiresDateConsent
	return option
}

func partySplitDatePolicy(option PartySplitOption, requestedDate string, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	requestedDate = strings.TrimSpace(requestedDate)
	dates := partySplitOptionLocalDates(option, loc)
	if len(dates) == 0 {
		return partySplitDatePolicyMultiDay
	}
	if len(dates) == 1 {
		for date := range dates {
			if requestedDate != "" && date == requestedDate {
				return partySplitDatePolicyRequestedDate
			}
			return partySplitDatePolicyAlternateDate
		}
	}
	return partySplitDatePolicyMultiDay
}

func partySplitOptionLocalDates(option PartySplitOption, loc *time.Location) map[string]bool {
	if loc == nil {
		loc = time.UTC
	}
	dates := map[string]bool{}
	for _, block := range option.Blocks {
		if block.StartTime.IsZero() {
			continue
		}
		dates[block.StartTime.In(loc).Format("2006-01-02")] = true
	}
	return dates
}

func partySplitOptionID(blocks []PartySplitBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		serviceIDs := make([]string, 0, len(block.Segments))
		for _, segment := range block.Segments {
			serviceIDs = append(serviceIDs, strings.TrimSpace(segment.ServiceID)+"@"+strings.TrimSpace(segment.StaffID))
		}
		sort.Strings(serviceIDs)
		parts = append(parts, block.StartTime.UTC().Format("20060102T150405Z")+"["+strings.Join(serviceIDs, ",")+"]")
	}
	return strings.Join(parts, "|")
}

func dedupePartySplitOptions(options []PartySplitOption) []PartySplitOption {
	out := make([]PartySplitOption, 0, len(options))
	seen := map[string]bool{}
	for _, option := range options {
		key := partySplitOptionID(option.Blocks)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		option.ID = key
		out = append(out, option)
	}
	return out
}

func rankPartySplitOptions(options []PartySplitOption, limit int) []PartySplitOption {
	if len(options) == 0 {
		return nil
	}
	sort.SliceStable(options, func(i, j int) bool {
		left := options[i]
		right := options[j]
		if partySplitDatePolicyRank(left.DatePolicy) != partySplitDatePolicyRank(right.DatePolicy) {
			return partySplitDatePolicyRank(left.DatePolicy) < partySplitDatePolicyRank(right.DatePolicy)
		}
		if left.SpanMinutes != right.SpanMinutes {
			return left.SpanMinutes < right.SpanMinutes
		}
		if left.FinishSpreadMinutes != right.FinishSpreadMinutes {
			return left.FinishSpreadMinutes < right.FinishSpreadMinutes
		}
		if len(left.Blocks) != len(right.Blocks) {
			return len(left.Blocks) < len(right.Blocks)
		}
		leftStart := partySplitFirstStart(left)
		rightStart := partySplitFirstStart(right)
		if !leftStart.Equal(rightStart) {
			return leftStart.Before(rightStart)
		}
		return left.ID < right.ID
	})
	if limit <= 0 || limit > len(options) {
		limit = len(options)
	}
	return options[:limit]
}

func partySplitDatePolicyRank(policy string) int {
	switch strings.TrimSpace(policy) {
	case partySplitDatePolicyRequestedDate:
		return 0
	case partySplitDatePolicyAlternateDate:
		return 1
	case partySplitDatePolicyMultiDay:
		return 2
	default:
		return 3
	}
}

func partySplitFirstStart(option PartySplitOption) time.Time {
	var first time.Time
	for _, block := range option.Blocks {
		if first.IsZero() || block.StartTime.Before(first) {
			first = block.StartTime
		}
	}
	return first
}

func applyPartySplitOffer(turn *TurnRecord, session *Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, options []PartySplitOption, unavailableRequestedTime bool) {
	if turn == nil || session == nil {
		return
	}
	plan := clonePartyPlan(session.PartyPlan)
	if plan == nil {
		plan = &PartyPlan{}
	}
	plan.SplitOptions = append([]PartySplitOption(nil), options...)
	plan.SelectedSplitOptionID = ""
	plan.SplitBookingAttemptIDs = nil
	plan.SplitAppointmentIDs = nil
	session.PartyPlan = plan
	session.RequestedStartTime = nil
	session.OfferedSlots = nil
	turn.ToolMessage = fmt.Sprintf("Availability check returned no full-party slots; split availability returned %d option(s).", len(options))
	turn.AIMessage = partySplitOfferMessage(*session, services, cfg)
	if unavailableRequestedTime {
		turn.AIMessage = "That time is not available. " + turn.AIMessage
	}
	turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, map[string]any{
		"availability_policy":         "party_split_offer",
		"split_option_count":          len(options),
		"split_requires_date_consent": partySplitOptionsRequireDateConsent(options),
		"split_date_policy_summary":   partySplitDatePolicySummary(options),
		"full_party_slot_count":       0,
		"requested_time_unavailable":  unavailableRequestedTime,
	})
	syncTurnUpdate(turn, *session, services, staff, cfg)
}

func partyPlanHasSelectedSplitOption(plan *PartyPlan) bool {
	_, ok := selectedPartySplitOption(plan)
	return ok
}

func selectedPartySplitOption(plan *PartyPlan) (PartySplitOption, bool) {
	if plan == nil || strings.TrimSpace(plan.SelectedSplitOptionID) == "" {
		return PartySplitOption{}, false
	}
	for _, option := range plan.SplitOptions {
		if strings.TrimSpace(option.ID) == strings.TrimSpace(plan.SelectedSplitOptionID) {
			return option, true
		}
	}
	return PartySplitOption{}, false
}

func partyPlanSelectedSplitRequiresDateConsent(plan *PartyPlan) bool {
	option, ok := selectedPartySplitOption(plan)
	return ok && option.RequiresDateConsent && !option.DateConsentConfirmed
}

func selectPartySplitOption(message string, plan *PartyPlan, loc *time.Location) (partySplitSelection, bool) {
	if plan == nil || len(plan.SplitOptions) == 0 {
		return partySplitSelection{}, false
	}
	if option, ok := selectedPartySplitOption(plan); ok && option.RequiresDateConsent && !option.DateConsentConfirmed && isAffirmativeSlotReply(message) {
		return partySplitSelection{Option: option, DateConsentConfirmed: true}, true
	}
	if index, ok := selectedSlotIndex(message); ok && index >= 0 && index < len(plan.SplitOptions) {
		option := plan.SplitOptions[index]
		return partySplitSelection{Option: option, DateConsentConfirmed: partySplitSelectionConfirmsDateConsent(message, option, loc, true)}, true
	}
	if len(plan.SplitOptions) == 1 && isAffirmativeSlotReply(message) {
		option := plan.SplitOptions[0]
		return partySplitSelection{Option: option, DateConsentConfirmed: partySplitSelectionConfirmsDateConsent(message, option, loc, true)}, true
	}
	matches := partySplitOptionsForClockCandidates(plan.SplitOptions, clockCandidatesFromText(message), loc)
	if len(matches) == 1 {
		option := matches[0]
		return partySplitSelection{Option: option, DateConsentConfirmed: partySplitSelectionConfirmsDateConsent(message, option, loc, false)}, true
	}
	return partySplitSelection{}, false
}

func partySplitSelectionConfirmsDateConsent(message string, option PartySplitOption, loc *time.Location, explicitOptionSelection bool) bool {
	if !option.RequiresDateConsent {
		return true
	}
	if explicitOptionSelection {
		return true
	}
	return messageMentionsPartySplitDateConsent(message, option, loc)
}

func messageMentionsPartySplitDateConsent(message string, option PartySplitOption, loc *time.Location) bool {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	dateChangeSignals := []string{
		"different day",
		"different days",
		"another day",
		"other day",
		"split day",
		"split days",
		"split across days",
		"across days",
		"separate day",
		"separate days",
		"next day",
	}
	for _, signal := range dateChangeSignals {
		if strings.Contains(normalized, signal) {
			return true
		}
	}
	if loc == nil {
		loc = time.UTC
	}
	dates := partySplitOptionLocalDates(option, loc)
	for date := range dates {
		if messageMentionsISODateOrWeekday(message, date, loc) {
			return true
		}
	}
	return false
}

func messageMentionsISODateOrWeekday(message string, isoDate string, loc *time.Location) bool {
	if strings.TrimSpace(isoDate) == "" {
		return false
	}
	if strings.Contains(message, isoDate) {
		return true
	}
	parsed, err := time.Parse("2006-01-02", isoDate)
	if err != nil {
		return false
	}
	weekday := strings.ToLower(parsed.Weekday().String())
	return strings.Contains(normalizeLooseText(message), weekday)
}

func partySplitOptionsForClockCandidates(options []PartySplitOption, candidates []int, loc *time.Location) []PartySplitOption {
	if len(options) == 0 || len(candidates) == 0 {
		return nil
	}
	if loc == nil {
		loc = time.UTC
	}
	out := []PartySplitOption{}
	seen := map[string]bool{}
	for _, option := range options {
		if partySplitOptionHasClockCandidate(option, candidates, loc) && !seen[option.ID] {
			out = append(out, option)
			seen[option.ID] = true
		}
	}
	return out
}

func partySplitOptionHasClockCandidate(option PartySplitOption, candidates []int, loc *time.Location) bool {
	for _, block := range option.Blocks {
		minutes := block.StartTime.In(loc).Hour()*60 + block.StartTime.In(loc).Minute()
		for _, candidate := range candidates {
			if minutes == candidate {
				return true
			}
		}
	}
	return false
}

func applySelectedPartySplitOption(session *Session, option PartySplitOption, dateConsentConfirmed bool) {
	if session == nil {
		return
	}
	plan := clonePartyPlan(session.PartyPlan)
	if plan == nil {
		plan = &PartyPlan{}
	}
	option.DateConsentConfirmed = dateConsentConfirmed || !option.RequiresDateConsent
	plan.SelectedSplitOptionID = option.ID
	for i := range plan.SplitOptions {
		if strings.TrimSpace(plan.SplitOptions[i].ID) == strings.TrimSpace(option.ID) {
			plan.SplitOptions[i].DateConsentConfirmed = option.DateConsentConfirmed
			plan.SplitOptions[i].RequiresDateConsent = option.RequiresDateConsent
			plan.SplitOptions[i].DatePolicy = option.DatePolicy
			break
		}
	}
	session.PartyPlan = plan
	session.RequestedStartTime = nil
	session.OfferedSlots = nil
	session.BookingSegments = partySplitOptionSegments(option)
	if strings.TrimSpace(session.ServiceID) == "" && len(session.BookingSegments) > 0 {
		session.ServiceID = strings.TrimSpace(session.BookingSegments[0].ServiceID)
	}
	if strings.TrimSpace(session.RequestedDate) == "" {
		if first := partySplitFirstStart(option); !first.IsZero() {
			session.RequestedDate = first.Format("2006-01-02")
		}
	}
	if bookingSegmentsHaveStaff(session.BookingSegments) {
		session.StaffID = ""
		session.StaffName = ""
		session.StaffSelectionMode = booking.StaffSelectionSpecific
	}
}

func partySplitOptionSegments(option PartySplitOption) []booking.BookingSegmentRequest {
	segments := []booking.BookingSegmentRequest{}
	for _, block := range option.Blocks {
		segments = append(segments, block.Segments...)
	}
	return segments
}

func partySplitOfferMessage(session Session, services []ServiceOption, cfg *RuntimeConfig) string {
	plan := session.PartyPlan
	if plan == nil || len(plan.SplitOptions) == 0 {
		return "I do not see one opening that fits the whole group together. What other time or day works?"
	}
	loc := timezoneLocation(timezoneFromConfig(cfg))
	includeDate := !partySplitOptionsAllRequestedDate(plan.SplitOptions)
	intro := partySplitOfferIntro(session, plan.SplitOptions, loc)
	parts := []string{intro}
	for i, option := range plan.SplitOptions {
		parts = append(parts, fmt.Sprintf("Option %d: %s.", i+1, partySplitOptionSpeech(option, services, loc, includeDate || partySplitOptionHasMultipleDates(option, loc))))
	}
	parts = append(parts, partySplitOfferQuestion(plan.SplitOptions))
	return strings.Join(parts, " ")
}

func partySplitOfferIntro(session Session, options []PartySplitOption, loc *time.Location) string {
	requested := requestedDateLabel(session.RequestedDate, loc)
	base := "I do not see one opening that fits the whole group together"
	if requested != "" {
		base += " on " + requested
	}
	if partySplitOptionsAllRequestedDate(options) {
		return base + ", but I can still make it work by staggering the start times."
	}
	if date, ok := partySplitOptionsSharedSingleDate(options, loc); ok {
		return base + ". I found an option on " + date + " instead, so I need your okay before booking."
	}
	return base + ". I found an option that splits the services across days, so I need your okay before booking."
}

func partySplitOfferQuestion(options []PartySplitOption) string {
	if partySplitOptionsRequireDateConsent(options) {
		if len(options) == 1 {
			return "Does that work for your group, or should I keep looking for one day?"
		}
		return "Which option works for your group, or should I keep looking for one day?"
	}
	if len(options) == 1 {
		return "Does that work for your group?"
	}
	return "Which option works better?"
}

func partySplitDateConsentPrompt(session Session, services []ServiceOption, cfg *RuntimeConfig) string {
	option, ok := selectedPartySplitOption(session.PartyPlan)
	if !ok {
		return "Is it okay to split the group across different days?"
	}
	loc := timezoneLocation(timezoneFromConfig(cfg))
	requested := requestedDateLabel(session.RequestedDate, loc)
	details := partySplitOptionSpeech(option, services, loc, true)
	if strings.TrimSpace(details) == "" {
		details = "the available times"
	}
	if option.DatePolicy == partySplitDatePolicyAlternateDate {
		if date, ok := partySplitOptionSingleDate(option, loc); ok {
			if requested != "" {
				return "Just to confirm, I found this on " + date + " instead of " + requested + ": " + details + ". Should I book that, or keep looking for " + requested + "?"
			}
			return "Just to confirm, I found this on " + date + ": " + details + ". Should I book that?"
		}
	}
	return "Just to confirm, this splits the group across different days: " + details + ". Is that okay, or should I keep looking for one day?"
}

func partySplitOptionsAllRequestedDate(options []PartySplitOption) bool {
	if len(options) == 0 {
		return false
	}
	for _, option := range options {
		if option.DatePolicy != partySplitDatePolicyRequestedDate {
			return false
		}
	}
	return true
}

func partySplitOptionsRequireDateConsent(options []PartySplitOption) bool {
	for _, option := range options {
		if option.RequiresDateConsent {
			return true
		}
	}
	return false
}

func partySplitDatePolicySummary(options []PartySplitOption) string {
	if len(options) == 0 {
		return ""
	}
	seen := map[string]bool{}
	ordered := []string{}
	for _, option := range options {
		policy := strings.TrimSpace(option.DatePolicy)
		if policy == "" {
			policy = "unknown"
		}
		if seen[policy] {
			continue
		}
		seen[policy] = true
		ordered = append(ordered, policy)
	}
	return strings.Join(ordered, ",")
}

func partySplitOptionsSharedSingleDate(options []PartySplitOption, loc *time.Location) (string, bool) {
	shared := ""
	for _, option := range options {
		date, ok := partySplitOptionSingleDate(option, loc)
		if !ok {
			return "", false
		}
		if shared == "" {
			shared = date
			continue
		}
		if shared != date {
			return "", false
		}
	}
	return shared, shared != ""
}

func partySplitDateLabel(option PartySplitOption, requestedDate string, loc *time.Location) string {
	if first := partySplitFirstStart(option); !first.IsZero() {
		return first.In(loc).Format("Monday, January 2")
	}
	return requestedDateLabel(requestedDate, loc)
}

func partySplitOptionsSharedDate(options []PartySplitOption, requestedDate string, loc *time.Location) string {
	if len(options) == 0 {
		return requestedDateLabel(requestedDate, loc)
	}
	shared := ""
	for _, option := range options {
		date, ok := partySplitOptionSingleDate(option, loc)
		if !ok {
			return ""
		}
		if shared == "" {
			shared = date
			continue
		}
		if shared != date {
			return ""
		}
	}
	if shared != "" {
		return shared
	}
	return requestedDateLabel(requestedDate, loc)
}

func partySplitOptionSingleDate(option PartySplitOption, loc *time.Location) (string, bool) {
	date := ""
	for _, block := range option.Blocks {
		if block.StartTime.IsZero() {
			continue
		}
		current := block.StartTime.In(loc).Format("Monday, January 2")
		if date == "" {
			date = current
			continue
		}
		if date != current {
			return "", false
		}
	}
	return date, date != ""
}

func partySplitOptionHasMultipleDates(option PartySplitOption, loc *time.Location) bool {
	_, ok := partySplitOptionSingleDate(option, loc)
	return !ok
}

func partySplitOptionSpeech(option PartySplitOption, services []ServiceOption, loc *time.Location, includeDate bool) string {
	blockParts := make([]string, 0, len(option.Blocks))
	for _, block := range option.Blocks {
		blockParts = append(blockParts, partySplitBlockSpeech(block, services, loc, includeDate))
	}
	summary := joinHumanList(blockParts)
	if option.SpanMinutes > 0 {
		summary += " so the group finishes within about " + roundedMinutePhrase(option.SpanMinutes)
	}
	return summary
}

func partySplitBlockSpeech(block PartySplitBlock, services []ServiceOption, loc *time.Location, includeDate bool) string {
	whenFormat := "3:04 PM"
	if includeDate {
		whenFormat = "Monday, January 2 at 3:04 PM"
	}
	when := block.StartTime.In(loc).Format(whenFormat)
	return serviceCountSummaryForSegments(block.Segments, services) + " at " + when
}

func serviceCountSummaryForSegments(segments []booking.BookingSegmentRequest, services []ServiceOption) string {
	session := Session{BookingSegments: segments}
	if summary := selectedServiceCountSummary(session, services); summary != "" {
		return summary
	}
	return "the services"
}

func roundedMinutePhrase(minutes int) string {
	if minutes <= 0 {
		return "the same time"
	}
	if minutes < 60 {
		return fmt.Sprintf("%d minutes", minutes)
	}
	hours := minutes / 60
	remainder := minutes % 60
	if remainder == 0 {
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}
	return fmt.Sprintf("%d hour %d minutes", hours, remainder)
}

func offeredSlotsFromAvailability(result *booking.AvailabilityResult) []OfferedSlot {
	return offeredSlotsFromAvailabilityLimit(result, availabilityOfferLimit)
}

func offeredSlotsFromAvailabilityLimit(result *booking.AvailabilityResult, limit int) []OfferedSlot {
	if result == nil || len(result.Slots) == 0 {
		return nil
	}
	if limit <= 0 || limit > len(result.Slots) {
		limit = len(result.Slots)
	}
	out := make([]OfferedSlot, 0, limit)
	for _, slot := range result.Slots {
		if len(out) >= limit {
			break
		}
		if slot.StartTime.IsZero() || slot.EndTime.IsZero() || strings.TrimSpace(slot.StaffID) == "" {
			continue
		}
		out = append(out, offeredSlotFromAvailability(result, slot))
	}
	return out
}

func offeredSlotsFromAvailabilityForSession(result *booking.AvailabilityResult, session Session, loc *time.Location) []OfferedSlot {
	limit := availabilityOfferLimit
	if _, ok := activeSlotTimePreference(session); ok {
		limit = exactAvailabilityLimit
	}
	offered := offeredSlotsFromAvailabilityLimit(result, limit)
	if len(offered) == 0 {
		return nil
	}
	if preference, ok := activeSlotTimePreference(session); ok {
		offered = filterOfferedSlotsByPreference(offered, preference, loc)
	}
	if len(offered) > availabilityOfferLimit {
		offered = offered[:availabilityOfferLimit]
	}
	return offered
}

func offeredSlotFromAvailability(result *booking.AvailabilityResult, slot booking.AvailabilitySlot) OfferedSlot {
	offered := OfferedSlot{
		StartTime:          slot.StartTime,
		EndTime:            slot.EndTime,
		StaffID:            slot.StaffID,
		StaffName:          slot.StaffName,
		StaffSelectionMode: firstNonEmpty(slot.StaffSelectionMode, result.StaffSelectionMode),
		Segments:           offeredSlotSegments(result, slot),
	}
	if offered.StaffSelectionMode == "" {
		offered.StaffSelectionMode = booking.StaffSelectionSpecific
	}
	if offered.StaffID == "" && len(offered.Segments) > 0 {
		offered.StaffID = offered.Segments[0].StaffID
		offered.StaffName = offered.Segments[0].StaffName
	}
	return offered
}

func offeredSlotMatchesServiceSelection(slot OfferedSlot, session Session) bool {
	mode := staffSelectionModeForAvailability(session)
	want := availabilitySegmentsForSession(session, mode)
	if len(want) == 0 {
		return true
	}
	if len(slot.Segments) == 0 {
		if len(want) != 1 {
			return false
		}
		return segmentStaffMatchesOfferedSlot(want[0], slot.StaffID)
	}
	if len(want) != len(slot.Segments) {
		return false
	}
	for index := range want {
		if strings.TrimSpace(want[index].ServiceID) != strings.TrimSpace(slot.Segments[index].ServiceID) ||
			!segmentStaffMatchesOfferedSlot(want[index], slot.Segments[index].StaffID) {
			return false
		}
	}
	return true
}

func segmentStaffMatchesOfferedSlot(segment booking.BookingSegmentRequest, offeredStaffID string) bool {
	mode := normalizeConversationStaffSelectionMode(segment.StaffSelectionMode)
	if mode == booking.StaffSelectionAnyone {
		return strings.TrimSpace(offeredStaffID) != ""
	}
	wantStaffID := strings.TrimSpace(segment.StaffID)
	return wantStaffID != "" && wantStaffID == strings.TrimSpace(offeredStaffID)
}

func offeredSlotServiceIDs(slot OfferedSlot) []string {
	if len(slot.Segments) == 0 {
		return nil
	}
	out := make([]string, 0, len(slot.Segments))
	for _, segment := range slot.Segments {
		if serviceID := strings.TrimSpace(segment.ServiceID); serviceID != "" {
			out = append(out, serviceID)
		}
	}
	return out
}

func offeredSlotSegments(result *booking.AvailabilityResult, slot booking.AvailabilitySlot) []OfferedSlotSegment {
	source := slot.Segments
	if len(source) == 0 {
		source = result.Segments
	}
	if len(source) == 0 {
		serviceID := firstNonEmpty(result.ServiceID)
		if serviceID == "" {
			return nil
		}
		source = []booking.AvailabilitySegment{{
			ServiceID:          serviceID,
			ServiceName:        result.ServiceName,
			StaffID:            firstNonEmpty(slot.StaffID, result.StaffID),
			StaffName:          firstNonEmpty(slot.StaffName, result.StaffName),
			StaffSelectionMode: firstNonEmpty(slot.StaffSelectionMode, result.StaffSelectionMode),
			DurationMinutes:    result.DurationMinutes,
		}}
	}
	out := make([]OfferedSlotSegment, 0, len(source))
	for _, segment := range source {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" {
			continue
		}
		mode := firstNonEmpty(segment.StaffSelectionMode, slot.StaffSelectionMode, result.StaffSelectionMode)
		if mode == "" {
			mode = booking.StaffSelectionSpecific
		}
		out = append(out, OfferedSlotSegment{
			ServiceID:          serviceID,
			ServiceName:        segment.ServiceName,
			StaffID:            firstNonEmpty(segment.StaffID, slot.StaffID, result.StaffID),
			StaffName:          firstNonEmpty(segment.StaffName, slot.StaffName, result.StaffName),
			StaffSelectionMode: mode,
			DurationMinutes:    segment.DurationMinutes,
		})
	}
	return out
}

func availabilityToolMessage(slotCount int) string {
	if slotCount == 0 {
		return "Availability check returned no bookable slots."
	}
	return fmt.Sprintf("Availability check returned %d bookable slot(s).", slotCount)
}

func formatSlotOffer(slots []OfferedSlot, loc *time.Location, unavailableRequestedTime bool) string {
	prefix := "I have openings"
	if unavailableRequestedTime {
		prefix = "That time is not available. I have openings"
	}
	return prefix + " " + formatAvailabilitySlotPhrase(slots, loc) + ". Which works best?"
}

func formatSlotOfferForSession(slots []OfferedSlot, loc *time.Location, unavailableRequestedTime bool, session Session, services []ServiceOption) string {
	service := strings.TrimSpace(serviceSummary(session, services))
	if service == "" {
		return formatSlotOffer(slots, loc, unavailableRequestedTime)
	}
	prefix := serviceOfferPrefix(service) + ", I have openings"
	if unavailableRequestedTime {
		prefix = "That time is not available. " + prefix
	}
	return prefix + " " + formatAvailabilitySlotPhrase(slots, loc) + ". Which works best?"
}

func formatRescheduleSlotOfferForSession(slots []OfferedSlot, loc *time.Location, unavailableRequestedTime bool, session Session, services []ServiceOption) string {
	service := strings.TrimSpace(serviceSummary(session, services))
	prefix := "I found openings"
	if service != "" {
		prefix += " for your " + service
	}
	if unavailableRequestedTime {
		prefix = "That time is not available. " + prefix
	}
	return prefix + " " + formatAvailabilitySlotPhrase(slots, loc) + ". Which time works?"
}

func serviceOfferPrefix(service string) string {
	if service == "" {
		return ""
	}
	first := []rune(strings.TrimSpace(service))
	if len(first) > 0 && first[0] >= '0' && first[0] <= '9' {
		return "For " + service
	}
	return "For your " + service
}

func formatAvailabilitySlotPhrase(slots []OfferedSlot, loc *time.Location) string {
	if len(slots) == 0 {
		return ""
	}
	if loc == nil {
		loc = time.UTC
	}
	groups := make([][]OfferedSlot, 0)
	groupIndex := map[string]int{}
	for _, slot := range slots {
		local := slot.StartTime.In(loc)
		key := local.Format("2006-01-02")
		index, ok := groupIndex[key]
		if !ok {
			index = len(groups)
			groupIndex[key] = index
			groups = append(groups, []OfferedSlot{})
		}
		groups[index] = append(groups[index], slot)
	}
	parts := make([]string, 0, len(groups))
	for _, group := range groups {
		parts = append(parts, formatAvailabilityDayGroup(group, loc))
	}
	return joinChoiceList(parts)
}

func formatAvailabilityDayGroup(slots []OfferedSlot, loc *time.Location) string {
	if len(slots) == 0 {
		return ""
	}
	day := slots[0].StartTime.In(loc).Format("Monday, January 2")
	staffPhrase := slotOfferStaffPhrase(slots[0])
	sameStaffPhrase := true
	for _, slot := range slots[1:] {
		if slotOfferStaffPhrase(slot) != staffPhrase {
			sameStaffPhrase = false
			break
		}
	}
	if sameStaffPhrase {
		times := make([]string, 0, len(slots))
		for _, slot := range slots {
			times = append(times, slot.StartTime.In(loc).Format("3:04 PM"))
		}
		return "on " + day + " at " + joinChoiceList(times) + staffPhrase
	}
	parts := make([]string, 0, len(slots))
	for _, slot := range slots {
		parts = append(parts, slot.StartTime.In(loc).Format("3:04 PM")+slotOfferStaffPhrase(slot))
	}
	return "on " + day + " at " + joinChoiceList(parts)
}

func offeredSlotSelectionRetryReply(message string, session Session, services []ServiceOption, loc *time.Location) string {
	reply := formatSlotOfferForSession(session.OfferedSlots, loc, false, session, services)
	if looksLikeUnclearOClockTime(message) {
		return "I heard a time but not clearly. " + reply
	}
	return reply
}

func looksLikeUnclearOClockTime(message string) bool {
	if len(clockCandidatesFromText(message)) > 0 {
		return false
	}
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	return strings.Contains(normalized, "o clock") ||
		strings.Contains(normalized, "oclock") ||
		strings.Contains(normalized, "clock am") ||
		strings.Contains(normalized, "clock pm")
}

func formatSpecificStaffUnavailableOffer(session Session, staff []StaffOption, requestedStart time.Time, slots []OfferedSlot, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	requestedStaff := staffName(session.StaffID, staff, session.StaffName)
	if requestedStaff == "" {
		requestedStaff = "That technician"
	}
	when := requestedStart.In(loc).Format("3:04 PM Monday")
	prefix := requestedStaff + " is not available at " + when + ". "
	if len(slots) == 0 {
		return prefix + "What other time works with " + requestedStaff + ", or should I use anyone available?"
	}
	return prefix + "I found openings " + formatAvailabilitySlotPhrase(slots, loc) + ". Which works best?"
}

func formatSlotOptions(slots []OfferedSlot, loc *time.Location) string {
	parts := make([]string, 0, len(slots))
	for i, slot := range slots {
		label := ordinalSpeechLabel(i + 1)
		when := slot.StartTime.In(loc).Format("Monday, January 2 at 3:04 PM")
		when += slotStaffPhrase(slot)
		parts = append(parts, label+" "+when)
	}
	return strings.Join(parts, "; ")
}

func slotStaffPhrase(slot OfferedSlot) string {
	if slotUsesAnyone(slot) {
		return availableTechnicianPhrase(slot)
	}
	if assigned := slotAssignedStaffLabel(slot); assigned != "" {
		return " with " + assigned
	}
	return ""
}

func slotOfferStaffPhrase(slot OfferedSlot) string {
	if slotUsesAnyone(slot) {
		return availableTechnicianPhrase(slot)
	}
	if assigned := slotAssignedStaffLabel(slot); assigned != "" {
		return " with " + assigned
	}
	return ""
}

func selectedRequestedTimeReply(session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, missing string) string {
	prompt := promptForMissingField(missing)
	if session.RequestedStartTime == nil {
		return prompt
	}
	loc := timezoneLocation(timezoneFromConfig(cfg))
	when := session.RequestedStartTime.In(loc).Format("Monday, January 2 at 3:04 PM")
	sentence := when + " is available"
	if service := strings.TrimSpace(serviceSummary(session, services)); service != "" {
		sentence += " for your " + service
	}
	if sessionUsesAnyone(session) {
		sentence += availableTechnicianPhraseForSegments(session.BookingSegments)
	} else if assigned := sessionAssignedStaffLabel(session, staff); assigned != "" {
		sentence += " with " + assigned
	}
	return sentence + ". " + prompt
}

func ordinalLabel(index int) string {
	switch index {
	case 1:
		return "first:"
	case 2:
		return "second:"
	case 3:
		return "third:"
	default:
		return fmt.Sprintf("%d:", index)
	}
}

func ordinalSpeechLabel(index int) string {
	switch index {
	case 1:
		return "First,"
	case 2:
		return "Second,"
	case 3:
		return "Third,"
	default:
		return fmt.Sprintf("%d.", index)
	}
}

func syncTurnUpdate(turn *TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) {
	turn.Update.BookingAction = bookingActionForSession(session)
	turn.Update.TargetAppointmentID = session.TargetAppointmentID
	turn.Update.RescheduleCandidates = session.RescheduleCandidates
	turn.Update.CustomerName = session.CustomerName
	turn.Update.CustomerPhone = session.CustomerPhone
	turn.Update.CustomerEmail = session.CustomerEmail
	turn.Update.ServiceID = session.ServiceID
	turn.Update.StaffID = session.StaffID
	turn.Update.StaffSelectionMode = staffSelectionModeForSession(session)
	turn.Update.RequestedDate = session.RequestedDate
	turn.Update.RequestedStartTime = session.RequestedStartTime
	turn.Update.OfferedSlots = session.OfferedSlots
	turn.Update.BookingSegments = session.BookingSegments
	turn.Update.PartyPlan = clonePartyPlan(session.PartyPlan)
	turn.Update.DialogState = cloneDialogState(session.DialogState)
	turn.Update.Summary = summaryFor(session, services, staff, cfg)
}

func cloneSessionForTurn(session Session) Session {
	cloned := session
	cloned.BookingSegments = append([]booking.BookingSegmentRequest(nil), session.BookingSegments...)
	cloned.RescheduleCandidates = append([]RescheduleCandidate(nil), session.RescheduleCandidates...)
	cloned.PartyPlan = clonePartyPlan(session.PartyPlan)
	cloned.DialogState = cloneDialogState(session.DialogState)
	if session.OfferedSlots != nil {
		cloned.OfferedSlots = make([]OfferedSlot, len(session.OfferedSlots))
		for index := range session.OfferedSlots {
			cloned.OfferedSlots[index] = session.OfferedSlots[index]
			cloned.OfferedSlots[index].Segments = append([]OfferedSlotSegment(nil), session.OfferedSlots[index].Segments...)
		}
	}
	return cloned
}

func newTurnRecord(salonID string, ownerUserID string, before Session, after Session, message string, eventKey string, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) TurnRecord {
	return TurnRecord{
		SalonID:         salonID,
		OwnerUserID:     ownerUserID,
		Session:         before,
		CustomerMessage: message,
		EventKey:        eventKey,
		Update: SessionUpdate{
			Status:               StatusActive,
			Intent:               after.Intent,
			Outcome:              OutcomeCollecting,
			BookingAction:        bookingActionForSession(after),
			TargetAppointmentID:  after.TargetAppointmentID,
			RescheduleCandidates: after.RescheduleCandidates,
			CustomerName:         after.CustomerName,
			CustomerPhone:        after.CustomerPhone,
			CustomerEmail:        after.CustomerEmail,
			ServiceID:            after.ServiceID,
			StaffID:              after.StaffID,
			StaffSelectionMode:   staffSelectionModeForSession(after),
			RequestedDate:        after.RequestedDate,
			RequestedStartTime:   after.RequestedStartTime,
			OfferedSlots:         after.OfferedSlots,
			BookingSegments:      after.BookingSegments,
			PartyPlan:            clonePartyPlan(after.PartyPlan),
			DialogState:          cloneDialogState(after.DialogState),
			Summary:              summaryFor(after, services, staff, cfg),
		},
	}
}

func finalizeTurnMetadata(turn *TurnRecord, before Session, after Session, missing string, nextRequired string, replySource string) {
	if turn == nil {
		return
	}
	customer := map[string]any{
		"slots_before": bookingSlotSnapshot(before),
		"slots_after":  bookingSlotSnapshot(after),
	}
	if turn.EventKey != "" {
		customer["event_key"] = turn.EventKey
	}
	if missing != "" {
		customer["missing_booking_field"] = missing
	}
	if nextRequired != "" {
		customer["next_required_field"] = nextRequired
	}
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, customer)
	ai := map[string]any{
		"turn_path":            replySource,
		"known_booking_fields": knownBookingFields(after),
	}
	if nextRequired != "" {
		ai["next_required_field"] = nextRequired
	}
	if missing != "" {
		ai["missing_booking_field"] = missing
	}
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, ai)
}

func applyServiceUnderstandingMetadata(turn *TurnRecord, result serviceUnderstandingResult) {
	if turn == nil || (result.Status == serviceUnderstandingStatusUnknown && strings.TrimSpace(result.NormalizedInput) == "") {
		return
	}
	candidateIDs := make([]string, 0, len(result.Candidates))
	candidateNames := make([]string, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		candidateIDs = append(candidateIDs, strings.TrimSpace(candidate.ID))
		candidateNames = append(candidateNames, strings.TrimSpace(candidate.Name))
	}
	selectedID := ""
	selectedName := ""
	if result.Selected != nil {
		selectedID = strings.TrimSpace(result.Selected.ID)
		selectedName = strings.TrimSpace(result.Selected.Name)
	}
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
		"service_understanding_status":        string(result.Status),
		"service_understanding_reason":        result.Reason,
		"service_understanding_confidence":    result.Confidence,
		"service_understanding_token":         result.MatchedToken,
		"service_understanding_source":        result.MatchedSource,
		"service_understanding_alias_id":      result.MatchedAliasID,
		"service_understanding_alias":         result.MatchedAlias,
		"service_understanding_category_id":   result.MatchedCategoryID,
		"service_understanding_category":      result.MatchedCategoryName,
		"service_understanding_normalized":    result.NormalizedInput,
		"service_understanding_candidate_ids": candidateIDs,
		"service_understanding_candidates":    candidateNames,
		"service_understanding_selected_id":   selectedID,
		"service_understanding_selected":      selectedName,
	})
}

func mergeMetadata(base map[string]any, updates map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range base {
		out[key] = value
	}
	for key, value := range updates {
		out[key] = value
	}
	return out
}

func bookingSlotSnapshot(session Session) map[string]any {
	out := map[string]any{
		"intent":                 session.Intent,
		"service_id":             strings.TrimSpace(session.ServiceID),
		"staff_id":               strings.TrimSpace(session.StaffID),
		"staff_selection_mode":   staffSelectionModeForSession(session),
		"requested_date":         strings.TrimSpace(session.RequestedDate),
		"offered_slot_count":     len(session.OfferedSlots),
		"booking_segment_count":  len(session.BookingSegments),
		"customer_name_present":  strings.TrimSpace(session.CustomerName) != "",
		"customer_phone_present": strings.TrimSpace(session.CustomerPhone) != "",
	}
	if session.RequestedStartTime != nil {
		out["requested_start_time"] = session.RequestedStartTime.Format(time.RFC3339)
	}
	return out
}

func knownBookingFields(session Session) []string {
	fields := []string{}
	if strings.TrimSpace(session.ServiceID) != "" {
		fields = append(fields, "service")
	}
	if strings.TrimSpace(session.RequestedDate) != "" || session.RequestedStartTime != nil {
		fields = append(fields, "requested_date")
	}
	if session.RequestedStartTime != nil {
		fields = append(fields, "requested_start_time", "requested_time")
	}
	if strings.TrimSpace(session.CustomerName) != "" {
		fields = append(fields, "customer_name")
	}
	if strings.TrimSpace(session.CustomerPhone) != "" {
		fields = append(fields, "customer_phone")
	}
	if hasStaffAssignment(session) {
		fields = append(fields, "staff")
	}
	return fields
}
