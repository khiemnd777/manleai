package pos

import "testing"

func TestNormalizeAppointmentStatus(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  AppointmentStatus
	}{
		{name: "accepted", input: "ACCEPTED", want: AppointmentStatusAccepted},
		{name: "legacy confirmed", input: "confirmed", want: AppointmentStatusAccepted},
		{name: "pending", input: " pending ", want: AppointmentStatusPending},
		{name: "provider cancellation variant", input: "CANCELED_BY_CUSTOMER", want: AppointmentStatusCancelled},
		{name: "declined alias", input: "rejected", want: AppointmentStatusDeclined},
		{name: "no show spacing", input: "no show", want: AppointmentStatusNoShow},
		{name: "unrecognized fails closed", input: "tentative", want: AppointmentStatusUnknown},
		{name: "blank fails closed", input: "", want: AppointmentStatusUnknown},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := NormalizeAppointmentStatus(test.input); got != test.want {
				t.Fatalf("NormalizeAppointmentStatus(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}
