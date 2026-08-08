package scheduling

import (
	"errors"
	"testing"

	"github.com/manleai/ai-receptionist/modules/booking"
)

func TestConversationBehaviorOwnsAuthorityBookingModeMatrix(t *testing.T) {
	tests := []struct {
		name      string
		authority string
		mode      BookingMode
		want      ConversationSchedulingBehavior
		wantErr   bool
	}{
		{name: "owner review", authority: booking.SchedulingAuthorityOwnerManual, mode: BookingModePendingApproval, want: ConversationSchedulingBehaviorOwnerReview},
		{name: "owner disabled", authority: booking.SchedulingAuthorityOwnerManual, mode: BookingModeDisabled, want: ConversationSchedulingBehaviorDisabled},
		{name: "owner automatic invalid", authority: booking.SchedulingAuthorityOwnerManual, mode: BookingModeConfirmedBooking, wantErr: true},
		{name: "internal review", authority: booking.SchedulingAuthorityManleAICalendar, mode: BookingModePendingApproval, want: ConversationSchedulingBehaviorOwnerReview},
		{name: "internal automatic", authority: booking.SchedulingAuthorityManleAICalendar, mode: BookingModeConfirmedBooking, want: ConversationSchedulingBehaviorAutomaticInternalCommit},
		{name: "external review", authority: booking.SchedulingAuthorityExternalProvider, mode: BookingModePendingApproval, want: ConversationSchedulingBehaviorOwnerReview},
		{name: "external automatic", authority: booking.SchedulingAuthorityExternalProvider, mode: BookingModeConfirmedBooking, want: ConversationSchedulingBehaviorAutomaticExternalBooking},
		{name: "external disabled", authority: booking.SchedulingAuthorityExternalProvider, mode: BookingModeDisabled, want: ConversationSchedulingBehaviorDisabled},
		{name: "unknown mode", authority: booking.SchedulingAuthorityManleAICalendar, mode: BookingMode("other"), wantErr: true},
		{name: "unknown authority", authority: "other", mode: BookingModePendingApproval, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ConversationBehavior(ConversationPolicyFence{BookingMode: test.mode, SchedulingAuthority: test.authority})
			if test.wantErr {
				var notReady *AuthorityNotReadyError
				if !errors.As(err, &notReady) {
					t.Fatalf("error=%v, want AuthorityNotReadyError", err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("behavior=%q error=%v, want %q", got, err, test.want)
			}
		})
	}
}

func TestAllowedConversationBookingModesAreAuthoritySpecific(t *testing.T) {
	ownerModes, err := AllowedConversationBookingModes(booking.SchedulingAuthorityOwnerManual)
	if err != nil || len(ownerModes) != 2 || ownerModes[0] != BookingModePendingApproval || ownerModes[1] != BookingModeDisabled {
		t.Fatalf("owner modes=%v error=%v", ownerModes, err)
	}
	internalModes, err := AllowedConversationBookingModes(booking.SchedulingAuthorityManleAICalendar)
	if err != nil || len(internalModes) != 3 || internalModes[1] != BookingModeConfirmedBooking {
		t.Fatalf("internal modes=%v error=%v", internalModes, err)
	}
}
