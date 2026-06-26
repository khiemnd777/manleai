package pos_square

import (
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/pos"
)

func TestSquareStateRoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	state, nonceHash, expiresAt, err := encodeState("salon-123", "secret", now)
	if err != nil {
		t.Fatalf("encode state failed: %v", err)
	}
	if !expiresAt.Equal(now.Add(squareOAuthStateTTL)) {
		t.Fatalf("unexpected expiry: %s", expiresAt)
	}
	salonID, decodedNonceHash, err := decodeState(state, "secret", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("decode state failed: %v", err)
	}
	if salonID != "salon-123" {
		t.Fatalf("unexpected salon id: %s", salonID)
	}
	if decodedNonceHash != nonceHash {
		t.Fatalf("unexpected nonce hash")
	}
}

func TestSquareStateRejectsWrongProvider(t *testing.T) {
	_, _, err := decodeState("bm90LXNxdWFyZToxMjM", "secret", time.Now().UTC())
	if err == nil {
		t.Fatalf("expected invalid state")
	}
}

func TestSquareStateRejectsTampering(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	state, _, _, err := encodeState("salon-123", "secret", now)
	if err != nil {
		t.Fatalf("encode state failed: %v", err)
	}
	_, _, err = decodeState(state+"x", "secret", now.Add(time.Minute))
	if err == nil {
		t.Fatalf("expected tampered state to fail")
	}
}

func TestSquareStateRejectsExpiredState(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	state, _, _, err := encodeState("salon-123", "secret", now)
	if err != nil {
		t.Fatalf("encode state failed: %v", err)
	}
	_, _, err = decodeState(state, "secret", now.Add(squareOAuthStateTTL+time.Second))
	if err == nil {
		t.Fatalf("expected expired state to fail")
	}
}

func TestBuildReadinessAllowsEnableWhenSquareIsBookingReady(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	connection := &pos.Connection{
		ID:         "connection_1",
		Status:     pos.StatusActive,
		LocationID: "loc_1",
		LastSyncAt: &now,
	}
	services := []pos.Service{
		{
			POSServiceID:      "svc_1",
			POSServiceVersion: 123,
			DurationMinutes:   45,
			AIBookable:        true,
			Active:            true,
			SyncStatus:        pos.SyncStatusSynced,
			POSLinked:         true,
		},
	}
	staff := []pos.StaffMember{
		{POSStaffID: "staff_1", AIBookable: true, Active: true, SyncStatus: pos.SyncStatusSynced, POSLinked: true},
	}
	periods := []pos.BusinessHourPeriod{
		{DayOfWeek: 1, StartLocalTime: "09:00:00", EndLocalTime: "17:00:00", Source: pos.BusinessHourSourceImported, Provider: pos.ProviderSquare},
	}

	confirmed := buildReadiness(false, connection, services, staff, periods, &booking.TestBookingRecord{
		AppointmentID:     "appointment_1",
		Status:            booking.StatusConfirmed,
		AppointmentStatus: booking.StatusConfirmed,
		POSBookingID:      "booking_1",
	})
	if !confirmed.CanTestBooking {
		t.Fatalf("expected test booking to be allowed")
	}
	if !confirmed.CanCancelTestBooking {
		t.Fatalf("expected cancel test booking to be allowed")
	}
	if !confirmed.CanEnableAIBooking {
		t.Fatalf("enable should be allowed when Square is connected, synced, and booking-ready")
	}

	cancelled := buildReadiness(false, connection, services, staff, periods, &booking.TestBookingRecord{
		AppointmentID:     "appointment_1",
		Status:            booking.StatusCancelled,
		AppointmentStatus: booking.StatusCancelled,
		POSBookingID:      "booking_1",
	})
	if cancelled.CanCancelTestBooking {
		t.Fatalf("cancel should not be allowed after test booking is cancelled")
	}
	if !cancelled.CanEnableAIBooking {
		t.Fatalf("enable should remain allowed after cancelled test booking")
	}

	withoutTest := buildReadiness(false, connection, services, staff, periods, nil)
	if !withoutTest.CanEnableAIBooking {
		t.Fatalf("enable should not require an optional Square test booking")
	}
}

func TestBuildReadinessBlocksTestBookingWithoutBookableRecords(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	readiness := buildReadiness(false, &pos.Connection{
		ID:         "connection_1",
		Status:     pos.StatusActive,
		LocationID: "loc_1",
		LastSyncAt: &now,
	}, []pos.Service{}, []pos.StaffMember{}, nil, nil)
	if readiness.CanTestBooking {
		t.Fatalf("test booking should be blocked without bookable services and staff")
	}
	if readiness.ServiceCount != 0 || readiness.StaffCount != 0 {
		t.Fatalf("unexpected counts: services=%d staff=%d", readiness.ServiceCount, readiness.StaffCount)
	}
}
