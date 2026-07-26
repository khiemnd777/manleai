package conversation

import (
	"context"
	"testing"

	"github.com/manleai/ai-receptionist/modules/booking"
)

func TestAvailableSlotsCarriesTargetAppointmentOnlyForRescheduleOrigin(t *testing.T) {
	tests := []struct {
		name       string
		session    Session
		wantTarget string
	}{
		{
			name: "reschedule",
			session: Session{
				BookingAction:       BookingActionReschedule,
				TargetAppointmentID: "appointment-origin-1",
				ServiceID:           "service-1",
				StaffSelectionMode:  booking.StaffSelectionAnyone,
			},
			wantTarget: "appointment-origin-1",
		},
		{
			name: "new booking ignores stale target",
			session: Session{
				BookingAction:       BookingActionBook,
				TargetAppointmentID: "stale-appointment",
				ServiceID:           "service-1",
				StaffSelectionMode:  booking.StaffSelectionAnyone,
			},
			wantTarget: "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tool := &fakeBookingTool{}
			service := NewService(nil, tool)
			if _, err := service.availableSlotsWithLimit(context.Background(), "salon-1", "owner-1", test.session, "2026-08-05", 4, &RuntimeConfig{BookingMode: "confirmed_booking", SchedulingAuthority: booking.SchedulingAuthorityExternalProvider}); err != nil {
				t.Fatalf("availableSlotsWithLimit returned error: %v", err)
			}
			if tool.availabilityRequest.TargetAppointmentID != test.wantTarget {
				t.Fatalf("target appointment = %q, want %q", tool.availabilityRequest.TargetAppointmentID, test.wantTarget)
			}
		})
	}
}
