package conversation

import (
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

func TestSchedulingActionRequestHistoricalTargetAbsentFromGuidanceUsesStartOnlySegmentSnapshot(t *testing.T) {
	store := newFakeConversationStore()
	requestedStart := time.Date(2026, time.August, 15, 16, 30, 0, 0, time.UTC)
	session := store.session
	session.CustomerName = "Historical Customer"
	session.CustomerPhone = "+13125550101"
	session.TargetAppointmentID = "appointment-historical"
	session.ServiceID = "service-inactive-archived-not-in-guidance"
	session.StaffSelectionMode = booking.StaffSelectionAnyone
	session.RequestedStartTime = &requestedStart
	session.BookingSegments = []booking.BookingSegmentRequest{{
		ServiceID:          session.ServiceID,
		StaffSelectionMode: booking.StaffSelectionAnyone,
	}}

	for _, operation := range []scheduling.OperationKind{scheduling.OperationKindReschedule, scheduling.OperationKindCancel} {
		t.Run(string(operation), func(t *testing.T) {
			req, ok := schedulingActionRequest(session, nil, &store.cfg, operation)
			if !ok {
				t.Fatalf("historical %s request was rejected by builder", operation)
			}
			if req.TargetAppointmentID != session.TargetAppointmentID || !req.RequestedStartTime.Equal(requestedStart) || !req.RequestedEndTime.IsZero() {
				t.Fatalf("historical %s top-level timing = %#v", operation, req)
			}
			if len(req.Segments) != 1 || req.Segments[0].ServiceID != session.ServiceID ||
				!req.Segments[0].RequestedStartTime.Equal(requestedStart) || !req.Segments[0].RequestedEndTime.IsZero() {
				t.Fatalf("historical %s segment snapshot = %#v", operation, req.Segments)
			}
		})
	}
}
