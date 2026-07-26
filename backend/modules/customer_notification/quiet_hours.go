package customernotification

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func parseLocalClock(value string) (int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, ErrValidation
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, ErrValidation
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, ErrValidation
	}
	return hour*60 + minute, nil
}

func formatLocalClock(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// quietHoursEnd returns the first safe dispatch instant at or after the
// configured local quiet end. Ambiguous wall times choose the later occurrence;
// nonexistent wall times advance to the first real local minute.
func quietHoursEnd(now time.Time, timezone, quietStart, quietEnd string) (time.Time, bool, error) {
	location, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return time.Time{}, false, ErrValidation
	}
	start, err := parseLocalClock(quietStart)
	if err != nil {
		return time.Time{}, false, err
	}
	end, err := parseLocalClock(quietEnd)
	if err != nil || start == end {
		return time.Time{}, false, ErrValidation
	}
	local := now.In(location)
	clock := local.Hour()*60 + local.Minute()
	quiet := false
	endDate := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	if start < end {
		quiet = clock >= start && clock < end
	} else {
		quiet = clock >= start || clock < end
		if clock >= start {
			endDate = endDate.AddDate(0, 0, 1)
		}
	}
	if !quiet {
		return time.Time{}, false, nil
	}
	resolved, err := resolveLocalWallMinute(location, endDate.Year(), endDate.Month(), endDate.Day(), end)
	if err != nil {
		return time.Time{}, false, err
	}
	return resolved, true, nil
}

func resolveLocalWallMinute(location *time.Location, year int, month time.Month, day, targetMinute int) (time.Time, error) {
	// Search real instants around the local date. Exact ambiguous matches are
	// collected so the later occurrence can be selected deterministically.
	anchor := time.Date(year, month, day, 12, 0, 0, 0, location).UTC()
	from := anchor.Add(-18 * time.Hour)
	to := anchor.Add(18 * time.Hour)
	exact := make([]time.Time, 0, 2)
	var firstAfter time.Time
	for instant := from; !instant.After(to); instant = instant.Add(time.Minute) {
		local := instant.In(location)
		if local.Year() != year || local.Month() != month || local.Day() != day {
			continue
		}
		minute := local.Hour()*60 + local.Minute()
		if minute == targetMinute {
			exact = append(exact, instant)
			continue
		}
		if minute > targetMinute && firstAfter.IsZero() {
			firstAfter = instant
		}
	}
	if len(exact) > 0 {
		return exact[len(exact)-1], nil
	}
	if !firstAfter.IsZero() {
		return firstAfter, nil
	}
	return time.Time{}, ErrValidation
}
