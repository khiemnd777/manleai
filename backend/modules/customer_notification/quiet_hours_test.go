package customernotification

import (
	"testing"
	"time"
)

func TestQuietHoursEndCrossesMidnight(t *testing.T) {
	now := time.Date(2026, 7, 24, 4, 0, 0, 0, time.UTC) // 11 PM in Chicago.
	end, quiet, err := quietHoursEnd(now, "America/Chicago", "21:00", "08:00")
	if err != nil || !quiet {
		t.Fatalf("quiet end: quiet=%v err=%v", quiet, err)
	}
	local := end.In(mustLocation(t, "America/Chicago"))
	if local.Hour() != 8 || local.Minute() != 0 || !end.After(now) {
		t.Fatalf("quiet end=%v local=%v", end, local)
	}
}

func TestQuietHoursEndAdvancesAcrossNonexistentDSTMinute(t *testing.T) {
	now := time.Date(2026, 3, 8, 6, 0, 0, 0, time.UTC) // 1 AM before spring-forward.
	end, quiet, err := quietHoursEnd(now, "America/New_York", "00:30", "02:30")
	if err != nil || !quiet {
		t.Fatalf("quiet end: quiet=%v err=%v", quiet, err)
	}
	local := end.In(mustLocation(t, "America/New_York"))
	if local.Hour() != 3 || local.Minute() != 0 {
		t.Fatalf("nonexistent 02:30 should advance to 03:00, got %v", local)
	}
}

func TestQuietHoursEndChoosesLaterAmbiguousDSTMinute(t *testing.T) {
	now := time.Date(2026, 11, 1, 4, 45, 0, 0, time.UTC) // 12:45 AM before fall-back.
	end, quiet, err := quietHoursEnd(now, "America/New_York", "00:30", "01:30")
	if err != nil || !quiet {
		t.Fatalf("quiet end: quiet=%v err=%v", quiet, err)
	}
	if want := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC); !end.Equal(want) {
		t.Fatalf("ambiguous end=%v want later occurrence %v", end, want)
	}
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return location
}
