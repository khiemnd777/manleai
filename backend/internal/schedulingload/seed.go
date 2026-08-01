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
	OwnerID       string
	OtherOwnerID  string
	OwnerManual   seededSalon
	Calendar      seededSalon
	SwitchReplay  seededSalon
	SwitchRace    seededSalon
	External      seededSalon
	ExternalOther seededSalon
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
	seed.External, err = insertSeededSalon(ctx, tx, config.RunID, seed.OwnerID, "external", booking.SchedulingAuthorityExternalProvider, scheduling.BookingModeConfirmedBooking, false)
	if err != nil {
		return seededRun{}, err
	}
	seed.ExternalOther, err = insertSeededSalon(ctx, tx, config.RunID, seed.OwnerID, "external-other", booking.SchedulingAuthorityExternalProvider, scheduling.BookingModeConfirmedBooking, false)
	if err != nil {
		return seededRun{}, err
	}
	if err := configureExternalSeed(ctx, tx, config, seed.OwnerID, seed.External); err != nil {
		return seededRun{}, err
	}
	if err := configureExternalSeed(ctx, tx, config, seed.OwnerID, seed.ExternalOther); err != nil {
		return seededRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return seededRun{}, fmt.Errorf("commit synthetic seed: %w", err)
	}
	return seed, nil
}

func configureExternalSeed(ctx context.Context, tx *sql.Tx, config Config, ownerID string, salon seededSalon) error {
	const providerLocationID = "synthetic-shared-square-location"
	if _, err := tx.ExecContext(ctx, `
		UPDATE salons SET active_pos_provider='square' WHERE id=$1
	`, salon.SalonID); err != nil {
		return fmt.Errorf("select synthetic external provider: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE services
		SET pos_service_id='synthetic-external-service',pos_service_version=1,source='imported',sync_status='synced'
		WHERE salon_id=$1 AND id=$2
	`, salon.SalonID, salon.ServiceID); err != nil {
		return fmt.Errorf("configure synthetic external service: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE staff
		SET pos_staff_id='synthetic-external-staff',source='imported',sync_status='synced'
		WHERE salon_id=$1 AND id=$2
	`, salon.SalonID, salon.StaffID); err != nil {
		return fmt.Errorf("configure synthetic external staff: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pos_entity_links(salon_id,entity_type,entity_id,provider,provider_entity_id,provider_version,sync_status,last_synced_at)
		VALUES($1,'service',$2,'square','synthetic-external-service',1,'synced',now()),
		      ($1,'staff',$3,'square','synthetic-external-staff',1,'synced',now())
	`, salon.SalonID, salon.ServiceID, salon.StaffID); err != nil {
		return fmt.Errorf("link synthetic external catalog: %w", err)
	}
	var configID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO salon_integration_configs(salon_id,provider,enabled,settings)
		VALUES($1,'square',true,'{"api_version":"2026-05-20"}'::jsonb)
		RETURNING id::text
	`, salon.SalonID).Scan(&configID); err != nil {
		return fmt.Errorf("insert synthetic external config: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO technical_resource_versions(salon_id,resource_type,resource_id,version)
		VALUES($1,'integration_config','square',1)
		ON CONFLICT(salon_id,resource_type,resource_id) DO UPDATE SET version=1
	`, salon.SalonID); err != nil {
		return fmt.Errorf("fence synthetic external config: %w", err)
	}
	var connectionID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO pos_connections(salon_id,provider,status,location_id,snapshot_generation,scopes,last_sync_at)
		VALUES($1,'square','active',$2,1,ARRAY['APPOINTMENTS_WRITE'],now())
		RETURNING id::text
	`, salon.SalonID, providerLocationID).Scan(&connectionID); err != nil {
		return fmt.Errorf("insert synthetic external connection: %w", err)
	}
	for day := 0; day < 7; day++ {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO salon_business_hour_periods(
				salon_id,day_of_week,start_local_time,end_local_time,source,provider,provider_location_id,provider_period_index
			) VALUES($1,$2,'00:00','23:59:59','imported','square',$3,0)
		`, salon.SalonID, day, providerLocationID); err != nil {
			return fmt.Errorf("insert synthetic external hours: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO external_provider_scheduling_capability_evidence(
			salon_id,integration_config_id,provider,provider_location_id,config_version,
			verification_contract_version,verification_source,atomic_create_no_overlap,
			atomic_reschedule_no_overlap,concrete_staff_assignment,resource_capacity_enforced,
			atomic_party_create,verified_at,expires_at,evidence,connection_id,
			connection_capability_version,provider_api_version,oauth_scope_fingerprint,
			write_permission_mode,reviewer_user_id,action_key,reconnect_required
		) VALUES(
			$1,$2,'square',$3,1,'square-buyer-single-create-v1','provider_contract',true,
			false,true,false,false,now()-interval '1 minute',now()+interval '1 hour',
			'{"source":"bounded_fake_provider_harness"}'::jsonb,$4,1,'2026-05-20',
			public.square_oauth_scope_fingerprint(ARRAY['APPOINTMENTS_WRITE']),'buyer_write',$5,$6,false
		)
	`, salon.SalonID, configID, providerLocationID, connectionID, ownerID, "load-capability-"+config.RunID+"-"+salon.SalonID); err != nil {
		return fmt.Errorf("insert synthetic external capability evidence: %w", err)
	}
	return nil
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
