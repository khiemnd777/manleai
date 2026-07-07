package conversation

import (
	"fmt"
	"github.com/manleai/ai-receptionist/modules/booking"
	"strconv"
	"strings"
	"time"
)

func parseDateAndClock(date string, hourRaw string, minuteRaw string, meridiem string, loc *time.Location) (time.Time, error) {
	if hourRaw == "" {
		return time.Time{}, fmt.Errorf("hour is required")
	}
	hour, err := strconv.Atoi(hourRaw)
	if err != nil {
		return time.Time{}, err
	}
	minute := 0
	if minuteRaw != "" {
		minute, err = strconv.Atoi(minuteRaw)
		if err != nil {
			return time.Time{}, err
		}
	}
	meridiem = strings.ToLower(strings.TrimSpace(meridiem))
	meridiem = strings.NewReplacer(".", "", " ", "").Replace(meridiem)
	if meridiem == "pm" && hour < 12 {
		hour += 12
	}
	if meridiem == "am" && hour == 12 {
		hour = 0
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return time.Time{}, fmt.Errorf("invalid clock")
	}
	parsedDate, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(parsedDate.Year(), parsedDate.Month(), parsedDate.Day(), hour, minute, 0, 0, loc), nil
}

func timezoneLocation(name string) *time.Location {
	if strings.TrimSpace(name) == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

func summaryFor(session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) string {
	parts := []string{}
	if cfg != nil && cfg.SalonName != "" {
		parts = append(parts, cfg.SalonName)
	}
	switch bookingActionForSession(session) {
	case BookingActionReschedule:
		parts = append(parts, "reschedule request")
	case BookingActionCancel:
		parts = append(parts, "cancellation request")
	}
	if session.CustomerName != "" {
		parts = append(parts, "customer "+session.CustomerName)
	}
	if session.CustomerPhone != "" {
		parts = append(parts, session.CustomerPhone)
	}
	if name := serviceSummary(session, services); name != "" {
		parts = append(parts, name)
	}
	if name := sessionAssignedStaffLabel(session, staff); name != "" {
		if sessionUsesAnyone(session) {
			parts = append(parts, "assigned "+name)
		} else {
			parts = append(parts, "with "+name)
		}
	}
	if session.RequestedStartTime != nil {
		parts = append(parts, "requested "+session.RequestedStartTime.Format(time.RFC3339))
	} else if session.RequestedDate != "" {
		parts = append(parts, "requested date "+session.RequestedDate)
	}
	if len(parts) == 0 {
		return "Conversation needs owner review."
	}
	return strings.Join(parts, " · ")
}

func confirmedMessage(session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) string {
	service := serviceSummary(session, services)
	member := sessionAssignedStaffLabel(session, staff)
	salon := salonName(cfg)
	when := ""
	if session.RequestedStartTime != nil {
		loc := timezoneLocation("")
		if cfg != nil {
			loc = timezoneLocation(cfg.Timezone)
		}
		when = session.RequestedStartTime.In(loc).Format("Monday, January 2 at 3:04 PM")
	}
	parts := []string{}
	if service != "" {
		parts = append(parts, "for your "+service)
	}
	if when != "" {
		parts = append(parts, "on "+when)
	}
	if sessionUsesAnyone(session) {
		if phrase := strings.TrimSpace(availableTechnicianPhraseForSegments(session.BookingSegments)); phrase != "" {
			parts = append(parts, phrase)
		}
	} else if member != "" {
		parts = append(parts, "with "+member)
	}
	prefix := "You're confirmed"
	if salon != "" {
		prefix += " with " + salon
	}
	message := strings.TrimSpace(prefix + " " + strings.Join(parts, " "))
	if message == prefix {
		message = prefix
	}
	message += "."
	if name := strings.TrimSpace(session.CustomerName); name != "" {
		message += " The appointment is under " + name + "."
	}
	message += " Thank you, goodbye."
	return message
}

func cancelledMessage(session Session, cfg *RuntimeConfig) string {
	loc := timezoneLocation("")
	if cfg != nil {
		loc = timezoneLocation(cfg.Timezone)
	}
	parts := []string{}
	if service := strings.TrimSpace(session.ServiceName); service != "" {
		parts = append(parts, "for "+service)
	}
	if session.RequestedStartTime != nil {
		parts = append(parts, "on "+session.RequestedStartTime.In(loc).Format("Monday, January 2 at 3:04 PM"))
	}
	message := "Your appointment has been cancelled"
	if len(parts) > 0 {
		message += " " + strings.Join(parts, " ")
	}
	message += ". Thank you, goodbye."
	return message
}

func slotAssignedStaffLabel(slot OfferedSlot) string {
	names := []string{}
	seen := map[string]bool{}
	addStaffName(&names, seen, slot.StaffName)
	for _, segment := range slot.Segments {
		addStaffName(&names, seen, segment.StaffName)
	}
	return joinHumanList(names)
}

func availableTechnicianPhrase(slot OfferedSlot) string {
	return availableTechnicianPhraseForSegmentCount(len(slot.Segments))
}

func availableTechnicianPhraseForSegments(segments []booking.BookingSegmentRequest) string {
	return availableTechnicianPhraseForSegmentCount(len(segments))
}

func availableTechnicianPhraseForSegmentCount(count int) string {
	return ""
}

func sessionAssignedStaffLabel(session Session, staff []StaffOption) string {
	names := []string{}
	seen := map[string]bool{}
	addStaffName(&names, seen, staffName(session.StaffID, staff, session.StaffName))
	for _, segment := range session.BookingSegments {
		addStaffName(&names, seen, staffName(segment.StaffID, staff, ""))
	}
	return joinHumanList(names)
}

func addStaffName(names *[]string, seen map[string]bool, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	key := strings.ToLower(name)
	if seen[key] {
		return
	}
	seen[key] = true
	*names = append(*names, name)
}

func joinHumanList(values []string) string {
	switch len(values) {
	case 0:
		return ""
	case 1:
		return values[0]
	case 2:
		return values[0] + " and " + values[1]
	default:
		return strings.Join(values[:len(values)-1], ", ") + ", and " + values[len(values)-1]
	}
}

func joinChoiceList(values []string) string {
	switch len(values) {
	case 0:
		return ""
	case 1:
		return values[0]
	case 2:
		return values[0] + " or " + values[1]
	default:
		return strings.Join(values[:len(values)-1], ", ") + ", or " + values[len(values)-1]
	}
}

func serviceSummary(session Session, services []ServiceOption) string {
	if activePartyPlan(session.PartyPlan) || hasRepeatedBookingService(session.BookingSegments) {
		if summary := selectedServiceCountSummary(session, services); summary != "" {
			return summary
		}
	}
	names := selectedServiceNames(session, services)
	if len(names) > 0 {
		return joinHumanList(names)
	}
	return ""
}

func hasRepeatedBookingService(segments []booking.BookingSegmentRequest) bool {
	seen := map[string]bool{}
	for _, segment := range segments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" {
			continue
		}
		if seen[serviceID] {
			return true
		}
		seen[serviceID] = true
	}
	return false
}

func selectedServiceCountSummary(session Session, services []ServiceOption) string {
	type serviceCount struct {
		ServiceID string
		Name      string
		Count     int
	}
	counts := make([]serviceCount, 0)
	indexes := map[string]int{}
	for _, segment := range session.BookingSegments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" {
			continue
		}
		index, ok := indexes[serviceID]
		if !ok {
			name := serviceName(serviceID, services, "")
			if name == "" {
				continue
			}
			index = len(counts)
			indexes[serviceID] = index
			counts = append(counts, serviceCount{ServiceID: serviceID, Name: name})
		}
		counts[index].Count++
	}
	if len(counts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(counts))
	for _, item := range counts {
		if item.Count <= 1 {
			parts = append(parts, "1 "+item.Name)
			continue
		}
		parts = append(parts, fmt.Sprintf("%d %s", item.Count, pluralDisplayName(item.Name)))
	}
	return joinHumanList(parts)
}

func selectedServiceNames(session Session, services []ServiceOption) []string {
	names := make([]string, 0, len(session.BookingSegments))
	seen := map[string]bool{}
	for _, segment := range session.BookingSegments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" || seen[serviceID] {
			continue
		}
		if name := serviceName(serviceID, services, ""); name != "" {
			names = append(names, name)
			seen[serviceID] = true
		}
	}
	if len(names) == 0 {
		if name := serviceName(session.ServiceID, services, session.ServiceName); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && value != "with" {
			out = append(out, value)
		}
	}
	return out
}

func serviceName(id string, services []ServiceOption, fallback string) string {
	for _, item := range services {
		if item.ID == id {
			return item.Name
		}
	}
	return fallback
}

func staffName(id string, staff []StaffOption, fallback string) string {
	for _, item := range staff {
		if item.ID == id {
			return item.Name
		}
	}
	return fallback
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultSessionListLimit
	}
	if limit > maxSessionListLimit {
		return maxSessionListLimit
	}
	return limit
}

func clampOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

func clampRetentionLimit(limit int) int {
	if limit <= 0 {
		return defaultRetentionLimit
	}
	if limit > maxRetentionLimit {
		return maxRetentionLimit
	}
	return limit
}

func clampWebhookLimit(limit int) int {
	if limit <= 0 {
		return defaultWebhookLimit
	}
	if limit > maxWebhookLimit {
		return maxWebhookLimit
	}
	return limit
}

func normalizeLifecycleStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "", LifecycleActive:
		return LifecycleActive
	case LifecycleArchived:
		return LifecycleArchived
	case LifecycleRedacted:
		return LifecycleRedacted
	default:
		return ""
	}
}

func allowedPartyRequestStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case PartyRequestStatusPending, PartyRequestStatusContacted, PartyRequestStatusResolved, PartyRequestStatusDismissed:
		return true
	default:
		return false
	}
}
