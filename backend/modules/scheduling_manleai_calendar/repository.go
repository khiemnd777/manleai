package scheduling_manleai_calendar

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/scheduling/fence"
)

const reconciliationLockPrefix = fence.AdvisoryKeyPrefix

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetAggregate(ctx context.Context, salonID string, ownerUserID string) (*Aggregate, error) {
	return loadAggregate(ctx, r.db, salonID, ownerUserID)
}

type mutationSpec struct {
	meta            MutationMeta
	fingerprint     string
	eventType       string
	createEventType string
	targetID        string
	payload         any
	allowCreate     bool
	requireReady    bool
	apply           func(context.Context, *sql.Tx, bool) error
}

func (r *Repository) PutConfig(ctx context.Context, salonID string, ownerUserID string, req CalendarConfigInput, fingerprint string) (*Aggregate, bool, error) {
	return r.mutate(ctx, salonID, ownerUserID, mutationSpec{
		meta: req.MutationMeta, fingerprint: fingerprint, eventType: EventConfigUpdated, createEventType: EventConfigCreated,
		payload: req, allowCreate: true,
		apply: func(ctx context.Context, tx *sql.Tx, exists bool) error {
			if !exists {
				_, err := tx.ExecContext(ctx, `
					INSERT INTO manleai_calendar_configs (
						salon_id, slot_step_minutes, minimum_booking_notice_minutes,
						booking_horizon_days, reschedule_cutoff_minutes, cancellation_cutoff_minutes,
						max_party_size, default_buffer_before_minutes, default_buffer_after_minutes
					) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
				`, salonID, req.SlotStepMinutes, req.MinimumBookingNoticeMinutes, req.BookingHorizonDays,
					nullableInt(req.RescheduleCutoffMinutes), nullableInt(req.CancellationCutoffMinutes), req.MaxPartySize,
					req.DefaultBufferBeforeMinutes, req.DefaultBufferAfterMinutes)
				return err
			}
			_, err := tx.ExecContext(ctx, `
				UPDATE manleai_calendar_configs
				SET slot_step_minutes = $2,
				    minimum_booking_notice_minutes = $3,
				    booking_horizon_days = $4,
				    reschedule_cutoff_minutes = $5,
				    cancellation_cutoff_minutes = $6,
				    max_party_size = $7,
				    default_buffer_before_minutes = $8,
				    default_buffer_after_minutes = $9,
				    updated_at = now()
				WHERE salon_id = $1
			`, salonID, req.SlotStepMinutes, req.MinimumBookingNoticeMinutes, req.BookingHorizonDays,
				nullableInt(req.RescheduleCutoffMinutes), nullableInt(req.CancellationCutoffMinutes), req.MaxPartySize,
				req.DefaultBufferBeforeMinutes, req.DefaultBufferAfterMinutes)
			return err
		},
	})
}

func (r *Repository) PutHours(ctx context.Context, salonID string, ownerUserID string, req ReplaceBusinessHoursInput, fingerprint string) (*Aggregate, bool, error) {
	return r.mutate(ctx, salonID, ownerUserID, mutationSpec{
		meta: req.MutationMeta, fingerprint: fingerprint, eventType: EventSalonHoursReplaced, payload: req,
		apply: func(ctx context.Context, tx *sql.Tx, _ bool) error {
			if _, err := tx.ExecContext(ctx, `DELETE FROM salon_business_hour_periods WHERE salon_id = $1 AND source = 'local_override'`, salonID); err != nil {
				return err
			}
			indices := make(map[int]int)
			for _, period := range req.Periods {
				indices[period.DayOfWeek]++
				endAtMidnight := period.EndMinute == 1440
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO salon_business_hour_periods (
						salon_id, day_of_week, start_local_time, end_local_time, end_at_midnight,
						source, provider, provider_location_id, provider_period_index, last_synced_at
					) VALUES ($1,$2,$3::time,$4::time,$5,'local_override','','',$6,NULL)
				`, salonID, period.DayOfWeek, minuteTime(period.StartMinute), minuteTime(period.EndMinute), endAtMidnight, indices[period.DayOfWeek]); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

func (r *Repository) PutStaffProfile(ctx context.Context, salonID string, ownerUserID string, staffID string, req StaffProfileInput, fingerprint string) (*Aggregate, bool, error) {
	return r.mutate(ctx, salonID, ownerUserID, mutationSpec{
		meta: req.MutationMeta, fingerprint: fingerprint, eventType: EventStaffScheduleReplaced, targetID: staffID, payload: req,
		apply: func(ctx context.Context, tx *sql.Tx, _ bool) error {
			if err := requireTenantEntity(ctx, tx, "staff", salonID, staffID); err != nil {
				return err
			}
			if err := requireTenantEntities(ctx, tx, "services", salonID, req.EligibleServiceIDs); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM manleai_calendar_staff_weekly_periods WHERE salon_id = $1 AND staff_id = $2`, salonID, staffID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM manleai_calendar_service_staff WHERE salon_id = $1 AND staff_id = $2`, salonID, staffID); err != nil {
				return err
			}
			for _, period := range req.WeeklyPeriods {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO manleai_calendar_staff_weekly_periods (salon_id, staff_id, day_of_week, start_minute, end_minute)
					VALUES ($1,$2,$3,$4,$5)
				`, salonID, staffID, period.DayOfWeek, period.StartMinute, period.EndMinute); err != nil {
					return err
				}
			}
			for _, serviceID := range req.EligibleServiceIDs {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO manleai_calendar_service_staff (salon_id, service_id, staff_id) VALUES ($1,$2,$3)
				`, salonID, serviceID, staffID); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

func (r *Repository) PutServicePolicy(ctx context.Context, salonID string, ownerUserID string, serviceID string, req ServicePolicyInput, fingerprint string) (*Aggregate, bool, error) {
	return r.mutate(ctx, salonID, ownerUserID, mutationSpec{
		meta: req.MutationMeta, fingerprint: fingerprint, eventType: EventServicePolicyUpdated, targetID: serviceID, payload: req,
		apply: func(ctx context.Context, tx *sql.Tx, _ bool) error {
			if err := requireTenantEntity(ctx, tx, "services", salonID, serviceID); err != nil {
				return err
			}
			if err := requireTenantEntities(ctx, tx, "staff", salonID, req.EligibleStaffIDs); err != nil {
				return err
			}
			poolIDs := make([]string, 0, len(req.ResourceRequirements))
			for _, item := range req.ResourceRequirements {
				poolIDs = append(poolIDs, item.ResourcePoolID)
			}
			if err := requireTenantEntities(ctx, tx, "manleai_calendar_resource_pools", salonID, poolIDs); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `
				INSERT INTO manleai_calendar_service_policies (
					salon_id, service_id, enabled, capacity_mode, buffer_before_minutes, buffer_after_minutes
				) VALUES ($1,$2,$3,$4,$5,$6)
				ON CONFLICT (salon_id, service_id) DO UPDATE SET
					enabled = EXCLUDED.enabled,
					capacity_mode = EXCLUDED.capacity_mode,
					buffer_before_minutes = EXCLUDED.buffer_before_minutes,
					buffer_after_minutes = EXCLUDED.buffer_after_minutes,
					updated_at = now()
			`, salonID, serviceID, req.Enabled, nullableString(req.CapacityMode), nullableInt(req.BufferBeforeMinutesOverride), nullableInt(req.BufferAfterMinutesOverride))
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM manleai_calendar_service_staff WHERE salon_id = $1 AND service_id = $2`, salonID, serviceID); err != nil {
				return err
			}
			for _, staffID := range req.EligibleStaffIDs {
				if _, err = tx.ExecContext(ctx, `INSERT INTO manleai_calendar_service_staff (salon_id, service_id, staff_id) VALUES ($1,$2,$3)`, salonID, serviceID, staffID); err != nil {
					return err
				}
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM manleai_calendar_service_resources WHERE salon_id = $1 AND service_id = $2`, salonID, serviceID); err != nil {
				return err
			}
			for _, item := range req.ResourceRequirements {
				if _, err = tx.ExecContext(ctx, `
					INSERT INTO manleai_calendar_service_resources (salon_id, service_id, resource_pool_id, units_required)
					VALUES ($1,$2,$3,$4)
				`, salonID, serviceID, item.ResourcePoolID, item.UnitsRequired); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

func (r *Repository) CreateResource(ctx context.Context, salonID string, ownerUserID string, req ResourcePoolInput, fingerprint string) (*Aggregate, bool, error) {
	resourceID := uuid.NewString()
	return r.mutate(ctx, salonID, ownerUserID, mutationSpec{
		meta: req.MutationMeta, fingerprint: fingerprint, eventType: EventResourcePoolCreated, targetID: resourceID, payload: req,
		apply: func(ctx context.Context, tx *sql.Tx, _ bool) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO manleai_calendar_resource_pools (id, salon_id, name, capacity) VALUES ($1,$2,$3,$4)`, resourceID, salonID, req.Name, req.Capacity)
			return err
		},
	})
}

func (r *Repository) UpdateResource(ctx context.Context, salonID string, ownerUserID string, resourceID string, req ResourcePoolInput, fingerprint string) (*Aggregate, bool, error) {
	return r.mutate(ctx, salonID, ownerUserID, mutationSpec{
		meta: req.MutationMeta, fingerprint: fingerprint, eventType: EventResourcePoolUpdated, targetID: resourceID, payload: req,
		apply: func(ctx context.Context, tx *sql.Tx, _ bool) error {
			result, err := tx.ExecContext(ctx, `
				UPDATE manleai_calendar_resource_pools SET name = $3, capacity = $4, updated_at = now()
				WHERE salon_id = $1 AND id = $2 AND archived_at IS NULL
			`, salonID, resourceID, req.Name, req.Capacity)
			if err != nil {
				return err
			}
			rows, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if rows == 1 {
				return nil
			}
			if err := requireTenantEntity(ctx, tx, "manleai_calendar_resource_pools", salonID, resourceID); err != nil {
				return err
			}
			return ErrValidation
		},
	})
}

func (r *Repository) ArchiveResource(ctx context.Context, salonID string, ownerUserID string, resourceID string, req MutationMeta, fingerprint string) (*Aggregate, bool, error) {
	return r.mutate(ctx, salonID, ownerUserID, mutationSpec{
		meta: req, fingerprint: fingerprint, eventType: EventResourcePoolArchived, targetID: resourceID, payload: req,
		apply: func(ctx context.Context, tx *sql.Tx, _ bool) error {
			result, err := tx.ExecContext(ctx, `
				UPDATE manleai_calendar_resource_pools
				SET archived_at = COALESCE(archived_at, now()), updated_at = now()
				WHERE salon_id = $1 AND id = $2
			`, salonID, resourceID)
			return requireAffected(result, err)
		},
	})
}

func (r *Repository) CreateException(ctx context.Context, salonID string, ownerUserID string, req ExceptionInput, fingerprint string) (*Aggregate, bool, error) {
	exceptionID := uuid.NewString()
	return r.mutate(ctx, salonID, ownerUserID, mutationSpec{
		meta: req.MutationMeta, fingerprint: fingerprint, eventType: EventExceptionCreated, targetID: exceptionID, payload: req,
		apply: func(ctx context.Context, tx *sql.Tx, _ bool) error {
			if req.StaffID != "" {
				if err := requireTenantEntity(ctx, tx, "staff", salonID, req.StaffID); err != nil {
					return err
				}
			}
			if req.ResourcePoolID != "" {
				if err := requireTenantEntity(ctx, tx, "manleai_calendar_resource_pools", salonID, req.ResourcePoolID); err != nil {
					return err
				}
			}
			_, err := tx.ExecContext(ctx, `
				INSERT INTO manleai_calendar_exceptions (
					id, salon_id, scope_type, staff_id, resource_pool_id, effect, starts_at, ends_at,
					capacity_override, reason, created_by_user_id
				) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			`, exceptionID, salonID, req.ScopeType, nullIfEmpty(req.StaffID), nullIfEmpty(req.ResourcePoolID), req.Effect,
				req.StartsAt, req.EndsAt, nullableInt(req.CapacityOverride), nullIfEmpty(req.Reason), ownerUserID)
			return err
		},
	})
}

func (r *Repository) CancelException(ctx context.Context, salonID string, ownerUserID string, exceptionID string, req MutationMeta, fingerprint string) (*Aggregate, bool, error) {
	return r.mutate(ctx, salonID, ownerUserID, mutationSpec{
		meta: req, fingerprint: fingerprint, eventType: EventExceptionCancelled, targetID: exceptionID, payload: req,
		apply: func(ctx context.Context, tx *sql.Tx, _ bool) error {
			result, err := tx.ExecContext(ctx, `
				UPDATE manleai_calendar_exceptions
				SET cancelled_at = now(), cancelled_by_user_id = $3
				WHERE salon_id = $1 AND id = $2 AND cancelled_at IS NULL
			`, salonID, exceptionID, ownerUserID)
			if err != nil {
				return err
			}
			rows, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if rows == 1 {
				return nil
			}
			if err := requireTenantEntity(ctx, tx, "manleai_calendar_exceptions", salonID, exceptionID); err != nil {
				return err
			}
			return ErrValidation
		},
	})
}

func (r *Repository) Activate(ctx context.Context, salonID string, ownerUserID string, req MutationMeta, fingerprint string) (*Aggregate, bool, error) {
	return r.mutate(ctx, salonID, ownerUserID, mutationSpec{
		meta: req, fingerprint: fingerprint, eventType: EventConfigActivated, payload: req, requireReady: true,
		apply: func(ctx context.Context, tx *sql.Tx, _ bool) error {
			_, err := tx.ExecContext(ctx, `
				UPDATE manleai_calendar_configs
				SET activated_at = clock_timestamp(),
				    activated_by_user_id = $2,
				    updated_at = now()
				WHERE salon_id = $1
			`, salonID, ownerUserID)
			return err
		},
	})
}

func (r *Repository) mutate(ctx context.Context, salonID string, ownerUserID string, spec mutationSpec) (*Aggregate, bool, error) {
	payload, err := eventPayload(spec.targetID, spec.payload)
	if err != nil {
		return nil, false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, reconciliationLockPrefix+salonID); err != nil {
		return nil, false, err
	}
	if err = requireCalendarActor(ctx, tx, salonID, ownerUserID); err != nil {
		return nil, false, err
	}
	var storedFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT action_fingerprint FROM manleai_calendar_config_events
		WHERE salon_id = $1 AND action_key = $2
	`, salonID, spec.meta.ActionKey).Scan(&storedFingerprint)
	if err == nil {
		if storedFingerprint != spec.fingerprint {
			return nil, false, ErrActionConflict
		}
		aggregate, loadErr := loadAggregate(ctx, tx, salonID, ownerUserID)
		if loadErr != nil {
			return nil, false, loadErr
		}
		if err = tx.Commit(); err != nil {
			return nil, false, err
		}
		return aggregate, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}

	var currentVersion int64
	err = tx.QueryRowContext(ctx, `SELECT version FROM manleai_calendar_configs WHERE salon_id = $1 FOR UPDATE`, salonID).Scan(&currentVersion)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	if !exists && !spec.allowCreate {
		return nil, false, ErrConfigRequired
	}
	if !exists {
		currentVersion = 0
	}
	if spec.meta.ExpectedConfigVersion != currentVersion {
		return nil, false, describeVersionConflict(spec.meta.ExpectedConfigVersion, currentVersion)
	}
	if err = spec.apply(ctx, tx, exists); err != nil {
		return nil, false, classifyWriteError(err)
	}

	var resultVersion int64
	if err = tx.QueryRowContext(ctx, `SELECT version FROM manleai_calendar_configs WHERE salon_id = $1`, salonID).Scan(&resultVersion); err != nil {
		return nil, false, err
	}
	if resultVersion <= currentVersion {
		if _, err = tx.ExecContext(ctx, `UPDATE manleai_calendar_configs SET updated_at = now() WHERE salon_id = $1`, salonID); err != nil {
			return nil, false, classifyWriteError(err)
		}
		if err = tx.QueryRowContext(ctx, `SELECT version FROM manleai_calendar_configs WHERE salon_id = $1`, salonID).Scan(&resultVersion); err != nil {
			return nil, false, err
		}
	}
	if resultVersion <= currentVersion {
		return nil, false, fmt.Errorf("manleai calendar version did not advance")
	}

	aggregate, err := loadAggregate(ctx, tx, salonID, ownerUserID)
	if err != nil {
		return nil, false, err
	}
	if spec.requireReady && !aggregate.Readiness.ConfigurationReady {
		return nil, false, ErrNotReady
	}
	eventType := spec.eventType
	if !exists && spec.createEventType != "" {
		eventType = spec.createEventType
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_config_events (
			salon_id, action_key, action_fingerprint, event_type,
			previous_version, result_version, actor_user_id, payload
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb)
	`, salonID, spec.meta.ActionKey, spec.fingerprint, eventType, currentVersion, resultVersion, ownerUserID, string(payload)); err != nil {
		return nil, false, classifyWriteError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, false, classifyWriteError(err)
	}
	return aggregate, false, nil
}

type dbReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadAggregate(ctx context.Context, q dbReader, salonID string, ownerUserID string) (*Aggregate, error) {
	aggregate := &Aggregate{
		SalonID: salonID, Hours: []BusinessHourPeriod{}, StaffProfiles: []StaffProfile{}, ServicePolicies: []ServicePolicy{},
		Resources: []ResourcePool{}, Exceptions: []CalendarException{}, Constraints: DefaultConstraints(),
	}
	if err := q.QueryRowContext(ctx, `
		SELECT salon.timezone, settings.scheduling_authority, settings.scheduling_authority_version
		FROM salons salon
		JOIN salon_settings settings ON settings.salon_id = salon.id
		WHERE salon.id = $1
		  AND (
		      public.app_rls_system_salon_allowed(salon.id)
		      OR public.has_active_tenant_membership(salon.id, $2::uuid)
		      OR public.has_platform_salon_capability(salon.id, $2::uuid, 'technical.read')
		      OR public.app_actor_feature_access($2::uuid, salon.id, 'calls.read', 'calls')
		      OR public.app_actor_feature_access($2::uuid, salon.id, 'calls.simulate', 'calls')
		  )
	`, salonID, ownerUserID).Scan(&aggregate.Timezone, &aggregate.SchedulingAuthority, &aggregate.AuthorityVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	config, err := loadConfig(ctx, q, salonID)
	if err != nil {
		return nil, err
	}
	aggregate.Config = config
	if config != nil {
		aggregate.ConfigVersion = config.Version
	}
	if aggregate.Hours, err = loadHours(ctx, q, salonID); err != nil {
		return nil, err
	}
	if aggregate.Resources, err = loadResources(ctx, q, salonID); err != nil {
		return nil, err
	}
	if aggregate.StaffProfiles, err = loadStaffProfiles(ctx, q, salonID); err != nil {
		return nil, err
	}
	if aggregate.ServicePolicies, err = loadServicePolicies(ctx, q, salonID); err != nil {
		return nil, err
	}
	if aggregate.Exceptions, err = loadExceptions(ctx, q, salonID); err != nil {
		return nil, err
	}
	aggregate.Readiness = EvaluateReadiness(aggregate)
	return aggregate, nil
}

func loadConfig(ctx context.Context, q dbReader, salonID string) (*CalendarConfig, error) {
	var config CalendarConfig
	var rescheduleCutoff, cancellationCutoff sql.NullInt64
	var activatedAt sql.NullTime
	var activatedBy sql.NullString
	var activatedVersion sql.NullInt64
	err := q.QueryRowContext(ctx, `
		SELECT salon_id::text, version, slot_step_minutes, minimum_booking_notice_minutes,
		       booking_horizon_days, reschedule_cutoff_minutes, cancellation_cutoff_minutes,
		       max_party_size, default_buffer_before_minutes, default_buffer_after_minutes,
		       activated_at, activated_by_user_id::text, activated_version, created_at, updated_at
		FROM manleai_calendar_configs WHERE salon_id = $1
	`, salonID).Scan(&config.SalonID, &config.Version, &config.SlotStepMinutes, &config.MinimumBookingNoticeMinutes,
		&config.BookingHorizonDays, &rescheduleCutoff, &cancellationCutoff, &config.MaxPartySize,
		&config.DefaultBufferBeforeMinutes, &config.DefaultBufferAfterMinutes, &activatedAt, &activatedBy, &activatedVersion,
		&config.CreatedAt, &config.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	config.RescheduleCutoffMinutes = intPointer(rescheduleCutoff)
	config.CancellationCutoffMinutes = intPointer(cancellationCutoff)
	config.ActivatedAt = timePointer(activatedAt)
	config.ActivatedVersion = int64Pointer(activatedVersion)
	if activatedBy.Valid {
		config.ActivatedByUserID = activatedBy.String
	}
	return &config, nil
}

func loadHours(ctx context.Context, q dbReader, salonID string) ([]BusinessHourPeriod, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id::text, day_of_week,
		       (extract(epoch FROM start_local_time) / 60)::int,
		       CASE WHEN end_at_midnight THEN 1440 ELSE (extract(epoch FROM end_local_time) / 60)::int END,
		       created_at, updated_at
		FROM salon_business_hour_periods
		WHERE salon_id = $1 AND source = 'local_override'
		ORDER BY day_of_week, start_local_time, provider_period_index
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []BusinessHourPeriod{}
	for rows.Next() {
		var item BusinessHourPeriod
		if err := rows.Scan(&item.ID, &item.DayOfWeek, &item.StartMinute, &item.EndMinute, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadResources(ctx context.Context, q dbReader, salonID string) ([]ResourcePool, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id::text, name, capacity, archived_at, created_at, updated_at
		FROM manleai_calendar_resource_pools WHERE salon_id = $1
		ORDER BY (archived_at IS NOT NULL), lower(name), id
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ResourcePool{}
	for rows.Next() {
		var item ResourcePool
		var archived sql.NullTime
		if err := rows.Scan(&item.ID, &item.Name, &item.Capacity, &archived, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.ArchivedAt = timePointer(archived)
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadStaffProfiles(ctx context.Context, q dbReader, salonID string) ([]StaffProfile, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id::text, name, active, ai_bookable, archived_at
		FROM staff WHERE salon_id = $1
		ORDER BY (archived_at IS NOT NULL), lower(name), id
	`, salonID)
	if err != nil {
		return nil, err
	}
	profiles := []StaffProfile{}
	byID := map[string]int{}
	for rows.Next() {
		var item StaffProfile
		var archived sql.NullTime
		if err := rows.Scan(&item.Staff.ID, &item.Staff.Name, &item.Staff.Active, &item.Staff.AIBookable, &archived); err != nil {
			rows.Close()
			return nil, err
		}
		item.Staff.ArchivedAt = timePointer(archived)
		item.WeeklyPeriods = []WeeklyPeriod{}
		item.EligibleServices = []ServiceRef{}
		byID[item.Staff.ID] = len(profiles)
		profiles = append(profiles, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	periodRows, err := q.QueryContext(ctx, `
		SELECT id::text, staff_id::text, day_of_week, start_minute, end_minute, created_at, updated_at
		FROM manleai_calendar_staff_weekly_periods WHERE salon_id = $1
		ORDER BY staff_id, day_of_week, start_minute, id
	`, salonID)
	if err != nil {
		return nil, err
	}
	for periodRows.Next() {
		var item WeeklyPeriod
		if err := periodRows.Scan(&item.ID, &item.StaffID, &item.DayOfWeek, &item.StartMinute, &item.EndMinute, &item.CreatedAt, &item.UpdatedAt); err != nil {
			periodRows.Close()
			return nil, err
		}
		if index, ok := byID[item.StaffID]; ok {
			profiles[index].WeeklyPeriods = append(profiles[index].WeeklyPeriods, item)
		}
	}
	if err := periodRows.Close(); err != nil {
		return nil, err
	}
	if err := periodRows.Err(); err != nil {
		return nil, err
	}
	serviceRows, err := q.QueryContext(ctx, `
		SELECT link.staff_id::text, service.id::text, service.name, service.duration_minutes,
		       service.active, service.ai_bookable, service.archived_at
		FROM manleai_calendar_service_staff link
		JOIN services service ON service.salon_id = link.salon_id AND service.id = link.service_id
		WHERE link.salon_id = $1
		ORDER BY link.staff_id, lower(service.name), service.id
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer serviceRows.Close()
	for serviceRows.Next() {
		var staffID string
		var service ServiceRef
		var archived sql.NullTime
		if err := serviceRows.Scan(&staffID, &service.ID, &service.Name, &service.DurationMinutes, &service.Active, &service.AIBookable, &archived); err != nil {
			return nil, err
		}
		service.ArchivedAt = timePointer(archived)
		if index, ok := byID[staffID]; ok {
			profiles[index].EligibleServices = append(profiles[index].EligibleServices, service)
		}
	}
	return profiles, serviceRows.Err()
}

func loadServicePolicies(ctx context.Context, q dbReader, salonID string) ([]ServicePolicy, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT service.id::text, service.name, service.duration_minutes, service.active, service.ai_bookable, service.archived_at,
		       (policy.service_id IS NOT NULL), policy.enabled, policy.capacity_mode,
		       policy.buffer_before_minutes, policy.buffer_after_minutes, policy.created_at, policy.updated_at
		FROM services service
		LEFT JOIN manleai_calendar_service_policies policy
		  ON policy.salon_id = service.salon_id AND policy.service_id = service.id
		WHERE service.salon_id = $1
		ORDER BY (service.archived_at IS NOT NULL), lower(service.name), service.id
	`, salonID)
	if err != nil {
		return nil, err
	}
	policies := []ServicePolicy{}
	byID := map[string]int{}
	for rows.Next() {
		var item ServicePolicy
		var serviceArchived, createdAt, updatedAt sql.NullTime
		var enabled sql.NullBool
		var capacity sql.NullString
		var before, after sql.NullInt64
		if err := rows.Scan(&item.Service.ID, &item.Service.Name, &item.Service.DurationMinutes, &item.Service.Active, &item.Service.AIBookable,
			&serviceArchived, &item.Configured, &enabled, &capacity, &before, &after, &createdAt, &updatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		item.Service.ArchivedAt = timePointer(serviceArchived)
		item.Enabled = enabled.Valid && enabled.Bool
		item.CapacityMode = stringPointer(capacity)
		item.BufferBeforeMinutesOverride = intPointer(before)
		item.BufferAfterMinutesOverride = intPointer(after)
		item.CreatedAt = timePointer(createdAt)
		item.UpdatedAt = timePointer(updatedAt)
		item.EligibleStaff = []StaffRef{}
		item.ResourceRequirements = []ResourceRequirement{}
		byID[item.Service.ID] = len(policies)
		policies = append(policies, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	staffRows, err := q.QueryContext(ctx, `
		SELECT link.service_id::text, staff.id::text, staff.name, staff.active, staff.ai_bookable, staff.archived_at
		FROM manleai_calendar_service_staff link
		JOIN staff ON staff.salon_id = link.salon_id AND staff.id = link.staff_id
		WHERE link.salon_id = $1
		ORDER BY link.service_id, lower(staff.name), staff.id
	`, salonID)
	if err != nil {
		return nil, err
	}
	for staffRows.Next() {
		var serviceID string
		var staff StaffRef
		var archived sql.NullTime
		if err := staffRows.Scan(&serviceID, &staff.ID, &staff.Name, &staff.Active, &staff.AIBookable, &archived); err != nil {
			staffRows.Close()
			return nil, err
		}
		staff.ArchivedAt = timePointer(archived)
		if index, ok := byID[serviceID]; ok {
			policies[index].EligibleStaff = append(policies[index].EligibleStaff, staff)
		}
	}
	if err := staffRows.Close(); err != nil {
		return nil, err
	}
	if err := staffRows.Err(); err != nil {
		return nil, err
	}
	resourceRows, err := q.QueryContext(ctx, `
		SELECT requirement.service_id::text, requirement.resource_pool_id::text, pool.name,
		       requirement.units_required, pool.capacity, pool.archived_at,
		       requirement.created_at, requirement.updated_at
		FROM manleai_calendar_service_resources requirement
		JOIN manleai_calendar_resource_pools pool
		  ON pool.salon_id = requirement.salon_id AND pool.id = requirement.resource_pool_id
		WHERE requirement.salon_id = $1
		ORDER BY requirement.service_id, lower(pool.name), pool.id
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer resourceRows.Close()
	for resourceRows.Next() {
		var serviceID string
		var requirement ResourceRequirement
		var archived sql.NullTime
		if err := resourceRows.Scan(&serviceID, &requirement.ResourcePoolID, &requirement.ResourceName,
			&requirement.UnitsRequired, &requirement.PoolCapacity, &archived, &requirement.CreatedAt, &requirement.UpdatedAt); err != nil {
			return nil, err
		}
		requirement.PoolArchivedAt = timePointer(archived)
		if index, ok := byID[serviceID]; ok {
			policies[index].ResourceRequirements = append(policies[index].ResourceRequirements, requirement)
		}
	}
	return policies, resourceRows.Err()
}

func loadExceptions(ctx context.Context, q dbReader, salonID string) ([]CalendarException, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id::text, scope_type, staff_id::text, resource_pool_id::text, effect,
		       starts_at, ends_at, capacity_override, reason, created_by_user_id::text,
		       cancelled_at, cancelled_by_user_id::text, created_at
		FROM manleai_calendar_exceptions WHERE salon_id = $1
		ORDER BY starts_at, created_at, id
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CalendarException{}
	for rows.Next() {
		var item CalendarException
		var staffID, poolID, reason, cancelledBy sql.NullString
		var capacity sql.NullInt64
		var cancelledAt sql.NullTime
		if err := rows.Scan(&item.ID, &item.ScopeType, &staffID, &poolID, &item.Effect, &item.StartsAt, &item.EndsAt,
			&capacity, &reason, &item.CreatedByUserID, &cancelledAt, &cancelledBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.StaffID = nullStringValue(staffID)
		item.ResourcePoolID = nullStringValue(poolID)
		item.CapacityOverride = intPointer(capacity)
		item.Reason = nullStringValue(reason)
		item.CancelledAt = timePointer(cancelledAt)
		item.CancelledByUserID = nullStringValue(cancelledBy)
		result = append(result, item)
	}
	return result, rows.Err()
}

func requireCalendarActor(ctx context.Context, q dbReader, salonID string, actorUserID string) error {
	var exists bool
	if err := q.QueryRowContext(ctx, `
		SELECT public.app_manleai_calendar_write_access($1::uuid, $2::uuid)
	`, salonID, actorUserID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func requireTenantEntity(ctx context.Context, q dbReader, table string, salonID string, entityID string) error {
	if table != "staff" && table != "services" && table != "manleai_calendar_resource_pools" && table != "manleai_calendar_exceptions" {
		return ErrValidation
	}
	var exists bool
	query := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE salon_id = $1 AND id = $2)`, table)
	if err := q.QueryRowContext(ctx, query, salonID, entityID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func requireTenantEntities(ctx context.Context, q dbReader, table string, salonID string, entityIDs []string) error {
	for _, id := range entityIDs {
		if err := requireTenantEntity(ctx, q, table, salonID, id); err != nil {
			return err
		}
	}
	return nil
}

func requireAffected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrNotFound
	}
	return nil
}

func eventPayload(targetID string, request any) ([]byte, error) {
	payload, err := json.Marshal(struct {
		TargetID string `json:"target_id,omitempty"`
		Request  any    `json:"request"`
	}{TargetID: targetID, Request: request})
	if err != nil || len(payload) > 16000 {
		return nil, ErrValidation
	}
	return payload, nil
}

func classifyWriteError(err error) error {
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrValidation) || errors.Is(err, ErrConfigRequired) || errors.Is(err, ErrVersionConflict) || errors.Is(err, ErrActionConflict) || errors.Is(err, ErrNotReady) {
		return err
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch string(pqErr.Code) {
		case "22001", "23503", "23505", "23514", "23P01":
			return fmt.Errorf("%w: %s", ErrValidation, pqErr.Constraint)
		}
	}
	return err
}

func minuteTime(minute int) string {
	if minute == 1440 {
		minute = 0
	}
	return fmt.Sprintf("%02d:%02d:00", minute/60, minute%60)
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func intPointer(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	converted := int(value.Int64)
	return &converted
}

func int64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func stringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func timePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func sortStrings(values []string) []string {
	sort.Strings(values)
	return values
}
