package scheduling

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

func TestAuthorityActionJSONContractsDoNotCrossPopulateEvidence(t *testing.T) {
	now := time.Date(2026, time.July, 24, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		result    ActionResult
		wantKeys  []string
		forbidden []string
	}{
		{
			name: "owner manual contains request evidence only",
			result: ActionResult{
				Kind: ActionKindPendingOwnerReview, OperationType: OperationKindBook,
				SchedulingAuthority: booking.SchedulingAuthorityOwnerManual,
				PendingOwnerReview: &PendingOwnerReviewResult{
					SchedulingRequestID: "request-1", Status: string(SchedulingRequestStatusPending), Version: 1,
				},
			},
			wantKeys:  []string{`"scheduling_authority":"owner_manual"`, `"pending_owner_review"`, `"scheduling_request_id":"request-1"`},
			forbidden: providerShapedJSONKeys(),
		},
		{
			name: "internal confirmation contains durable root lifecycle evidence only",
			result: ActionResult{
				Kind: ActionKindConfirmedAppointment, OperationType: OperationKindReschedule,
				SchedulingAuthority:               booking.SchedulingAuthorityManleAICalendar,
				TargetAuthorityAppointmentVersion: 3, AuthorityAppointmentVersion: 4,
				ConfirmedAppointment: &ConfirmedAppointmentResult{
					AppointmentID: "internal-root-1", BookingAttemptID: "internal-attempt-1",
					AppointmentStatus: booking.StatusRescheduled, ActiveChildCount: 1,
					Children: []ConfirmedAppointmentSegment{{
						AppointmentServiceID: "child-1", ServiceID: "service-1", StaffID: "staff-1",
						StaffSelectionMode: booking.StaffSelectionSpecific, Quantity: 1,
						ScheduledStartTime: now, ScheduledEndTime: now.Add(time.Hour),
						OccupiedStartTime: now, OccupiedEndTime: now.Add(time.Hour),
					}},
				},
			},
			wantKeys: []string{
				`"scheduling_authority":"manleai_calendar"`, `"appointment_id":"internal-root-1"`,
				`"booking_attempt_id":"internal-attempt-1"`, `"authority_appointment_version":4`, `"children"`,
			},
			forbidden: providerShapedJSONKeys(),
		},
		{
			name: "external confirmation retains canonical and compatibility evidence",
			result: ActionResult{
				Kind: ActionKindConfirmedAppointment, OperationType: OperationKindBook,
				SchedulingAuthority: booking.SchedulingAuthorityExternalProvider,
				ConfirmedAppointment: &ConfirmedAppointmentResult{
					AppointmentID: "appointment-1", ExternalAttemptID: "attempt-1",
					AppointmentStatus: booking.StatusConfirmed, ActiveChildCount: 1,
					ExternalAttempt: &booking.BookingAttempt{
						ID: "attempt-1", SchedulingAuthority: booking.SchedulingAuthorityExternalProvider,
						AuthorityProvider: "square", AuthorityAppointmentID: "provider-booking-1", AuthorityAppointmentVersion: 7,
						POSProvider: "square", POSBookingID: "provider-booking-1", POSBookingVersion: 7,
						Status: booking.StatusConfirmed, ProviderOutcome: booking.ProviderOutcomeSucceeded,
						RetryPolicy: booking.RetryPolicyNone, Reconciliation: booking.ReconciliationNotRequired,
					},
				},
			},
			wantKeys: []string{
				`"scheduling_authority":"external_provider"`, `"authority_provider":"square"`,
				`"authority_appointment_id":"provider-booking-1"`, `"pos_booking_id":"provider-booking-1"`,
				`"provider_outcome":"succeeded"`, `"reconciliation_status":"not_required"`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload, err := json.Marshal(test.result)
			if err != nil {
				t.Fatalf("marshal action result: %v", err)
			}
			text := string(payload)
			for _, want := range test.wantKeys {
				if !strings.Contains(text, want) {
					t.Fatalf("response missing %s: %s", want, text)
				}
			}
			for _, forbidden := range test.forbidden {
				if strings.Contains(text, forbidden) {
					t.Fatalf("response contains forbidden provider-shaped field %s: %s", forbidden, text)
				}
			}
		})
	}
}

func TestInternalAppointmentJSONOmitsNullableProviderCompatibilityFields(t *testing.T) {
	payload, err := json.Marshal(booking.Appointment{
		ID: "internal-root-1", SalonID: "salon-1", BookingAttemptID: "internal-attempt-1",
		SchedulingAuthority:    booking.SchedulingAuthorityManleAICalendar,
		AuthorityAppointmentID: "internal-root-1", AuthorityAppointmentVersion: 1,
		Status: booking.StatusConfirmed,
	})
	if err != nil {
		t.Fatalf("marshal internal appointment: %v", err)
	}
	text := string(payload)
	for _, forbidden := range providerShapedJSONKeys() {
		if strings.Contains(text, forbidden) {
			t.Fatalf("internal appointment contains provider-shaped field %s: %s", forbidden, text)
		}
	}
}

func TestExternalCanonicalVersionZeroRemainsVisible(t *testing.T) {
	payload, err := json.Marshal(booking.BookingAttempt{
		ID: "attempt-1", SchedulingAuthority: booking.SchedulingAuthorityExternalProvider,
		AuthorityProvider: "square", AuthorityAppointmentID: "provider-booking-1",
		AuthorityAppointmentVersion: 0, Status: booking.StatusConfirmed,
	})
	if err != nil {
		t.Fatalf("marshal external attempt: %v", err)
	}
	if !strings.Contains(string(payload), `"authority_appointment_version":0`) {
		t.Fatalf("canonical zero version was omitted: %s", payload)
	}
}

func TestHistoricalExternalOperationUsesPersistedOriginAfterCurrentSwitch(t *testing.T) {
	resolver := &fakeAuthorityResolver{
		authority:            booking.SchedulingAuthorityOwnerManual,
		operationAuthorities: map[string]string{"historical-external": booking.SchedulingAuthorityExternalProvider},
	}
	external := &fakeNeutralExecutor{
		authority: booking.SchedulingAuthorityExternalProvider,
		actionResult: &ActionResult{
			Kind: ActionKindExternalFallbackPending, OperationType: OperationKindBook,
			SchedulingAuthority:     booking.SchedulingAuthorityExternalProvider,
			ExternalFallbackPending: &ExternalFallbackPendingResult{ExternalAttemptID: "external-attempt-1"},
		},
	}
	service := NewService(resolver, nil, external)
	result, err := service.ExecuteAction(context.Background(), "salon-1", "owner-1", ActionRequest{
		OperationType: OperationKindBook, OperationKey: "historical-external",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider || external.actionCalls != 1 {
		t.Fatalf("historical external dispatch = %#v, calls = %d", result, external.actionCalls)
	}
}

func TestUnknownPersistedAuthorityFailsClosedBeforeDispatch(t *testing.T) {
	resolver := &fakeAuthorityResolver{
		authority:            booking.SchedulingAuthorityOwnerManual,
		operationAuthorities: map[string]string{"unknown-origin": "future_authority"},
	}
	owner := &fakeNeutralExecutor{authority: booking.SchedulingAuthorityOwnerManual}
	service := NewService(resolver, nil, owner)
	result, err := service.ExecuteAction(context.Background(), "salon-1", "owner-1", ActionRequest{
		OperationType: OperationKindBook, OperationKey: "unknown-origin",
	})
	if result != nil || !errors.Is(err, booking.ErrSchedulingAuthorityNotReady) || owner.actionCalls != 0 {
		t.Fatalf("result = %#v, error = %v, owner calls = %d", result, err, owner.actionCalls)
	}
}

func providerShapedJSONKeys() []string {
	return []string{
		`"pos_provider"`, `"pos_booking_id"`, `"pos_booking_version"`,
		`"pos_appointment_id"`, `"pos_appointment_version"`, `"pos_sync_status"`,
		`"pos_sync_error"`, `"provider_outcome"`, `"reconciliation_status"`,
		`"error_code"`, `"error_message"`,
	}
}
