package scheduling_manleai_calendar

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

// loadAuthoritativeInternalResultTx builds result evidence only from the
// persisted attempt, execution event, appointment root, and exact plan graph.
// It intentionally supports historical replay after a later lifecycle change:
// book/reschedule return the plan installed at their event version, while
// cancel returns the exact old plan released by the cancellation attempt and
// reports zero active children.
func loadAuthoritativeInternalResultTx(
	ctx context.Context,
	tx *sql.Tx,
	salonID string,
	ownerUserID string,
	appointmentID string,
	attemptID string,
) (*InternalCreateResult, error) {
	var (
		attemptStatus           string
		operationType           string
		targetVersion           sql.NullInt64
		attemptVersion          int
		eventType               string
		eventVersion            int
		rootStatus              string
		rootVersion             int
		hasProviderEvidence     bool
		currentActiveChildCount int
		attemptSnapshotCount    int
	)
	err := tx.QueryRowContext(ctx, `
		SELECT attempt.status, attempt.operation_type,
		       attempt.target_authority_appointment_version,
		       attempt.authority_appointment_version,
		       event.event_type, event.authority_appointment_version,
		       appointment.status, appointment.authority_appointment_version,
		       (
		           attempt.pos_provider IS NOT NULL OR
		           attempt.pos_booking_id IS NOT NULL OR
		           attempt.pos_booking_version IS NOT NULL OR
		           attempt.pos_idempotency_key IS NOT NULL OR
		           attempt.provider_location_id IS NOT NULL OR
		           attempt.provider_snapshot_generation IS NOT NULL OR
		           attempt.authority_provider IS NOT NULL OR
		           attempt.authority_location_id IS NOT NULL OR
		           attempt.authority_snapshot_generation IS NOT NULL OR
		           appointment.pos_provider IS NOT NULL OR
		           appointment.pos_appointment_id IS NOT NULL OR
		           appointment.pos_appointment_version IS NOT NULL OR
		           appointment.pos_customer_id IS NOT NULL OR
		           appointment.pos_sync_status IS NOT NULL OR
		           appointment.last_pos_synced_at IS NOT NULL OR
		           appointment.pos_sync_error IS NOT NULL OR
		           appointment.authority_provider IS NOT NULL
		       ),
		       (
		           SELECT count(*)
		           FROM appointment_services active_child
		           WHERE active_child.salon_id = appointment.salon_id
		             AND active_child.appointment_id = appointment.id
		             AND active_child.scheduling_authority = 'manleai_calendar'
		             AND active_child.plan_version = appointment.authority_appointment_version
		             AND active_child.released_at IS NULL
		       ),
		       (
		           SELECT count(*)
		           FROM booking_attempt_segments attempt_child
		           WHERE attempt_child.salon_id = attempt.salon_id
		             AND attempt_child.booking_attempt_id = attempt.id
		             AND attempt_child.scheduling_authority = 'manleai_calendar'
		       )
		FROM booking_attempts attempt
		JOIN salons salon ON salon.id = attempt.salon_id
		JOIN manleai_calendar_execution_events event
		  ON event.salon_id = attempt.salon_id
		 AND event.booking_attempt_id = attempt.id
		JOIN appointments appointment
		  ON appointment.salon_id = event.salon_id
		 AND appointment.id = event.appointment_id
		WHERE attempt.salon_id = $1
		  AND public.has_active_tenant_membership(salon.id, $2::uuid)
		  AND appointment.id::text = $3
		  AND attempt.id::text = $4
		  AND attempt.scheduling_authority = 'manleai_calendar'
		  AND appointment.scheduling_authority = 'manleai_calendar'
	`, salonID, ownerUserID, strings.TrimSpace(appointmentID), strings.TrimSpace(attemptID)).Scan(
		&attemptStatus, &operationType, &targetVersion, &attemptVersion,
		&eventType, &eventVersion, &rootStatus, &rootVersion,
		&hasProviderEvidence, &currentActiveChildCount, &attemptSnapshotCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, scheduling.ErrInvalidSchedulingResult
	}
	if err != nil {
		return nil, err
	}

	resultPlanVersion := eventVersion
	releasedByAttemptID := ""
	resultActiveChildCount := -1
	switch eventType {
	case "appointment_confirmed":
		if operationType != booking.BookingActionBook || attemptStatus != booking.StatusConfirmed ||
			targetVersion.Valid || eventVersion != 1 {
			return nil, scheduling.ErrInvalidSchedulingResult
		}
	case "appointment_rescheduled":
		if operationType != booking.BookingActionReschedule || attemptStatus != booking.StatusRescheduled ||
			!targetVersion.Valid || targetVersion.Int64 < 1 || eventVersion != int(targetVersion.Int64)+1 {
			return nil, scheduling.ErrInvalidSchedulingResult
		}
	case "appointment_cancelled":
		if operationType != booking.BookingActionCancel || attemptStatus != booking.StatusCancelled ||
			!targetVersion.Valid || targetVersion.Int64 < 1 || eventVersion != int(targetVersion.Int64)+1 {
			return nil, scheduling.ErrInvalidSchedulingResult
		}
		resultPlanVersion = int(targetVersion.Int64)
		releasedByAttemptID = strings.TrimSpace(attemptID)
		resultActiveChildCount = 0
	default:
		return nil, scheduling.ErrInvalidSchedulingResult
	}
	if hasProviderEvidence || attemptVersion != eventVersion || rootVersion < eventVersion ||
		(rootVersion == eventVersion && rootStatus != attemptStatus) {
		return nil, scheduling.ErrInvalidSchedulingResult
	}

	children, err := loadAppointmentPlanResultTx(
		ctx, tx, salonID, appointmentID, resultPlanVersion, releasedByAttemptID,
	)
	if err != nil {
		return nil, err
	}
	if len(children) == 0 || attemptSnapshotCount != len(children) {
		return nil, scheduling.ErrInvalidSchedulingResult
	}
	if resultActiveChildCount < 0 {
		resultActiveChildCount = len(children)
	}
	if rootVersion == eventVersion && currentActiveChildCount != resultActiveChildCount {
		return nil, scheduling.ErrInvalidSchedulingResult
	}
	if eventType == "appointment_cancelled" &&
		(rootVersion != eventVersion || rootStatus != booking.StatusCancelled || currentActiveChildCount != 0) {
		return nil, scheduling.ErrInvalidSchedulingResult
	}

	result := &InternalCreateResult{
		AppointmentID:               strings.TrimSpace(appointmentID),
		BookingAttemptID:            strings.TrimSpace(attemptID),
		AppointmentStatus:           attemptStatus,
		AuthorityAppointmentVersion: eventVersion,
		ActiveChildCount:            resultActiveChildCount,
		Children:                    children,
	}
	if targetVersion.Valid {
		result.TargetAuthorityAppointmentVersion = int(targetVersion.Int64)
	}
	return result, nil
}

func loadAppointmentPlanResultTx(
	ctx context.Context,
	tx *sql.Tx,
	salonID string,
	appointmentID string,
	planVersion int,
	releasedByAttemptID string,
) ([]InternalCreateSegmentResult, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT segment.id::text, COALESCE(segment.guest_reference,''),
		       segment.service_id::text, segment.staff_id::text,
		       segment.staff_selection_mode, segment.scheduled_start_time,
		       segment.scheduled_end_time, segment.occupied_start_time,
		       segment.occupied_end_time, segment.buffer_before_minutes,
		       segment.buffer_after_minutes
		FROM appointment_services segment
		WHERE segment.salon_id = $1
		  AND segment.appointment_id = $2
		  AND segment.scheduling_authority = 'manleai_calendar'
		  AND segment.plan_version = $3
		  AND ($4 = '' OR segment.released_by_attempt_id::text = $4)
		ORDER BY segment.sort_order, segment.id
	`, salonID, strings.TrimSpace(appointmentID), planVersion, strings.TrimSpace(releasedByAttemptID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	children := []InternalCreateSegmentResult{}
	for rows.Next() {
		var child InternalCreateSegmentResult
		if err := rows.Scan(
			&child.AppointmentServiceID, &child.GuestReference,
			&child.ServiceID, &child.StaffID, &child.StaffSelectionMode,
			&child.ScheduledStartTime, &child.ScheduledEndTime,
			&child.OccupiedStartTime, &child.OccupiedEndTime,
			&child.BufferBeforeMinutes, &child.BufferAfterMinutes,
		); err != nil {
			return nil, err
		}
		child.Quantity = 1
		children = append(children, child)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	for index := range children {
		allocationRows, err := tx.QueryContext(ctx, `
			SELECT allocation.resource_pool_id::text, pool.name,
			       allocation.units_allocated
			FROM manleai_calendar_appointment_resource_allocations allocation
			JOIN manleai_calendar_resource_pools pool
			  ON pool.salon_id = allocation.salon_id
			 AND pool.id = allocation.resource_pool_id
			WHERE allocation.salon_id = $1
			  AND allocation.appointment_service_id = $2
			  AND allocation.plan_version = $3
			ORDER BY allocation.resource_pool_id
		`, salonID, children[index].AppointmentServiceID, planVersion)
		if err != nil {
			return nil, err
		}
		for allocationRows.Next() {
			var allocation InternalResourceAllocation
			if err := allocationRows.Scan(
				&allocation.ResourcePoolID, &allocation.ResourceName,
				&allocation.UnitsAllocated,
			); err != nil {
				allocationRows.Close()
				return nil, err
			}
			children[index].ResourceAllocations = append(
				children[index].ResourceAllocations, allocation,
			)
		}
		if err := allocationRows.Close(); err != nil {
			return nil, err
		}
		if err := allocationRows.Err(); err != nil {
			return nil, err
		}
	}
	return children, nil
}
