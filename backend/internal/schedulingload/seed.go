package schedulingload

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

type seededRun struct {
	OwnerID      string
	OtherOwnerID string
	OwnerManual  seededSalon
	Calendar     seededSalon
	SwitchReplay seededSalon
	SwitchRace   seededSalon
}

type seededSalon struct {
	SalonID       string
	ServiceID     string
	StaffID       string
	SecondStaffID string
}

func seedRun(ctx context.Context, db *sql.DB, config Config) (seededRun, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return seededRun{}, fmt.Errorf("begin synthetic seed: %w", err)
	}
	defer tx.Rollback()

	seed := seededRun{}
	for target, role := range map[*string]string{
		&seed.OwnerID:      "owner",
		&seed.OtherOwnerID: "other-owner",
	} {
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO users (email, password_hash, full_name)
			VALUES ($1, 'scheduling-load-harness-disabled-login', $2)
			RETURNING id::text
		`, syntheticEmail(config.RunID, role), "Synthetic Load "+role).Scan(target); err != nil {
			var postgresError *pq.Error
			if role == "owner" && errors.As(err, &postgresError) && postgresError.Code == "23505" {
				return seededRun{}, ErrRunAlreadyExists
			}
			return seededRun{}, fmt.Errorf("insert synthetic %s identity: %w", role, err)
		}
	}

	seed.OwnerManual, err = insertSeededSalon(ctx, tx, config.RunID, seed.OwnerID, "owner-manual", booking.SchedulingAuthorityOwnerManual, scheduling.BookingModePendingApproval, false)
	if err != nil {
		return seededRun{}, err
	}
	seed.Calendar, err = insertSeededSalon(ctx, tx, config.RunID, seed.OwnerID, "calendar", booking.SchedulingAuthorityExternalProvider, scheduling.BookingModeConfirmedBooking, true)
	if err != nil {
		return seededRun{}, err
	}
	seed.SwitchReplay, err = insertSeededSalon(ctx, tx, config.RunID, seed.OwnerID, "switch-replay", booking.SchedulingAuthorityOwnerManual, scheduling.BookingModePendingApproval, false)
	if err != nil {
		return seededRun{}, err
	}
	seed.SwitchRace, err = insertSeededSalon(ctx, tx, config.RunID, seed.OwnerID, "switch-race", booking.SchedulingAuthorityOwnerManual, scheduling.BookingModePendingApproval, false)
	if err != nil {
		return seededRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return seededRun{}, fmt.Errorf("commit synthetic seed: %w", err)
	}
	return seed, nil
}

func insertSeededSalon(
	ctx context.Context,
	tx *sql.Tx,
	runID string,
	ownerID string,
	role string,
	authority string,
	bookingMode scheduling.BookingMode,
	withSecondStaff bool,
) (seededSalon, error) {
	result := seededSalon{}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO salons (name, phone, owner_user_id, timezone)
		VALUES ($1, $2, $3, 'America/Chicago')
		RETURNING id::text
	`, "Synthetic Load "+role+" "+runID, syntheticPhone(runID, role), ownerID).Scan(&result.SalonID); err != nil {
		return seededSalon{}, fmt.Errorf("insert synthetic %s salon: %w", role, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salon_settings (salon_id, scheduling_authority, booking_mode)
		VALUES ($1, $2, $3)
	`, result.SalonID, authority, bookingMode); err != nil {
		return seededSalon{}, fmt.Errorf("insert synthetic %s settings: %w", role, err)
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO services (salon_id, pos_provider, pos_service_id, name, duration_minutes, active, ai_bookable)
		VALUES ($1, 'square', $2, $3, 60, true, true)
		RETURNING id::text
	`, result.SalonID, "synthetic-service-"+runID+"-"+role, "Synthetic Service "+role).Scan(&result.ServiceID); err != nil {
		return seededSalon{}, fmt.Errorf("insert synthetic %s service: %w", role, err)
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO staff (salon_id, pos_provider, pos_staff_id, name, active, ai_bookable)
		VALUES ($1, 'square', $2, $3, true, true)
		RETURNING id::text
	`, result.SalonID, "synthetic-staff-"+runID+"-"+role+"-1", "Synthetic Staff One "+role).Scan(&result.StaffID); err != nil {
		return seededSalon{}, fmt.Errorf("insert synthetic %s staff: %w", role, err)
	}
	if withSecondStaff {
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO staff (salon_id, pos_provider, pos_staff_id, name, active, ai_bookable)
			VALUES ($1, 'square', $2, $3, true, true)
			RETURNING id::text
		`, result.SalonID, "synthetic-staff-"+runID+"-"+role+"-2", "Synthetic Staff Two "+role).Scan(&result.SecondStaffID); err != nil {
			return seededSalon{}, fmt.Errorf("insert synthetic %s second staff: %w", role, err)
		}
	}
	return result, nil
}

func syntheticPhone(runID string, role string) string {
	digest := uuid.NewSHA1(uuid.NameSpaceOID, []byte(runID+":"+role)).String()
	digits := make([]byte, 0, 10)
	for i := 0; i < len(digest) && len(digits) < 10; i++ {
		if digest[i] >= '0' && digest[i] <= '9' {
			digits = append(digits, digest[i])
		}
	}
	for len(digits) < 10 {
		digits = append(digits, '0')
	}
	return "+1" + string(digits)
}
