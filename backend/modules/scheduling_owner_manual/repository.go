package scheduling_owner_manual

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/scheduling"
	"github.com/manleai/ai-receptionist/modules/scheduling/fence"
)

const ownerManualNotificationType = "owner_manual_request_pending"

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) SchedulingTargetReadinessFacts(ctx context.Context, salonID string, ownerUserID string) (int64, int, error) {
	var authorityVersion int64
	var eligibleServiceCount int
	err := r.db.QueryRowContext(ctx, `
		SELECT settings.scheduling_authority_version,
		       (
		         SELECT COUNT(*)
		         FROM services service
		         WHERE service.salon_id = salon.id
		           AND service.active = true
		           AND service.ai_bookable = true
		           AND service.archived_at IS NULL
		           AND service.duration_minutes > 0
		       )
		FROM salons salon
		JOIN salon_settings settings ON settings.salon_id = salon.id
		WHERE salon.id::text = $1
		  AND (
		      public.app_rls_system_salon_allowed(salon.id)
		      OR public.has_active_tenant_membership(salon.id, $2::uuid)
		      OR
		      public.app_actor_feature_access($2::uuid, salon.id, 'calls.read', 'calls')
		      OR public.app_actor_feature_access($2::uuid, salon.id, 'calls.simulate', 'calls')
		  )
	`, salonID, ownerUserID).Scan(&authorityVersion, &eligibleServiceCount)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, pos.ErrNotFound
	}
	return authorityVersion, eligibleServiceCount, err
}

func (r *Repository) CreateOrReplay(ctx context.Context, salonID string, ownerUserID string, req scheduling.ActionRequest, requestFingerprint string) (*scheduling.SchedulingRequest, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, fence.AdvisoryKey(salonID)); err != nil {
		return nil, false, err
	}

	var existingRequestID string
	var existingFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT request.id::text, request.request_fingerprint
		FROM scheduling_requests request
		JOIN salons salon ON salon.id = request.salon_id
		WHERE request.salon_id::text = $1
		  AND (public.app_rls_system_salon_allowed(salon.id)
		       OR public.has_active_tenant_membership(salon.id, $2::uuid)
		       OR public.app_actor_feature_access($2::uuid, salon.id, 'calls.simulate', 'calls'))
		  AND request.scheduling_authority = $3
		  AND request.operation_key = $4
		FOR UPDATE OF request
	`, salonID, ownerUserID, booking.SchedulingAuthorityOwnerManual, req.OperationKey).Scan(&existingRequestID, &existingFingerprint)
	if err == nil {
		if existingFingerprint != requestFingerprint {
			return nil, false, booking.ErrOperationConflict
		}
		request, err := getRequestTx(ctx, tx, salonID, ownerUserID, existingRequestID)
		if err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return request, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	var currentAuthority string
	var bookingMode string
	if err := tx.QueryRowContext(ctx, `SELECT settings.scheduling_authority, settings.booking_mode FROM salon_settings settings JOIN salons salon ON salon.id=settings.salon_id WHERE settings.salon_id::text=$1 AND (public.app_rls_system_salon_allowed(salon.id) OR public.has_active_tenant_membership(salon.id, $2::uuid) OR public.app_actor_feature_access($2::uuid, salon.id, 'calls.simulate', 'calls')) FOR SHARE OF settings, salon`, salonID, ownerUserID).Scan(&currentAuthority, &bookingMode); errors.Is(err, sql.ErrNoRows) {
		return nil, false, pos.ErrNotFound
	} else if err != nil {
		return nil, false, err
	}
	if bookingMode != string(scheduling.BookingModePendingApproval) {
		return nil, false, booking.ErrSchedulingAuthorityNotReady
	}
	if req.OperationType == scheduling.OperationKindBook {
		if req.TargetAuthority == "" && currentAuthority != booking.SchedulingAuthorityOwnerManual {
			return nil, false, booking.ErrSchedulingAuthorityNotReady
		}
		if req.TargetAuthority != "" && req.TargetAuthority != currentAuthority {
			return nil, false, booking.ErrOperationConflict
		}
	}
	if (req.OperationType == scheduling.OperationKindReschedule || req.OperationType == scheduling.OperationKindCancel) && req.TargetAppointmentID == "" {
		if req.TargetAuthority == "" && currentAuthority != booking.SchedulingAuthorityOwnerManual {
			return nil, false, booking.ErrSchedulingAuthorityNotReady
		}
		if req.TargetAuthority != "" && req.TargetAuthority != currentAuthority {
			return nil, false, booking.ErrOperationConflict
		}
	}

	var salonTimezone string
	if err := tx.QueryRowContext(ctx, `
		SELECT timezone
		FROM salons salon
		WHERE salon.id::text = $1
		  AND (public.app_rls_system_salon_allowed(salon.id)
		       OR public.has_active_tenant_membership(salon.id, $2::uuid)
		       OR public.app_actor_feature_access($2::uuid, salon.id, 'calls.simulate', 'calls'))
		FOR SHARE
	`, salonID, ownerUserID).Scan(&salonTimezone); errors.Is(err, sql.ErrNoRows) {
		return nil, false, pos.ErrNotFound
	} else if err != nil {
		return nil, false, err
	}
	if req.RequestedTimezone != salonTimezone {
		return nil, false, scheduling.ErrInvalidSchedulingAction
	}
	if req.CallSessionID != "" {
		var exists bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM call_sessions
				WHERE id::text = $1 AND salon_id::text = $2
			)
		`, req.CallSessionID, salonID).Scan(&exists); err != nil {
			return nil, false, err
		}
		if !exists {
			return nil, false, pos.ErrNotFound
		}
	}

	segments, requestedEndTime, err := resolveSegmentSnapshots(ctx, tx, salonID, req)
	if err != nil {
		return nil, false, err
	}
	if req.RequestedEndTime.IsZero() {
		req.RequestedEndTime = requestedEndTime
	}

	var requestID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO scheduling_requests (
			salon_id, scheduling_authority, operation_key, request_fingerprint,
			operation_type, source, status, version, call_session_id,
			target_appointment_id, target_scheduling_authority, target_description,
			customer_name, customer_phone, customer_email, requested_timezone,
			party_size, requested_start_time, requested_end_time, notes
		)
		VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, 1, NULLIF($8, '')::uuid,
			NULLIF($9, '')::uuid, NULLIF($10, ''), NULLIF($11, ''),
			$12, $13, NULLIF($14, ''), $15,
			$16, $17, $18, NULLIF($19, '')
		)
		ON CONFLICT (salon_id, scheduling_authority, operation_key) DO NOTHING
		RETURNING id::text
	`, salonID, booking.SchedulingAuthorityOwnerManual, req.OperationKey, requestFingerprint,
		req.OperationType, req.Source, scheduling.SchedulingRequestStatusPending, req.CallSessionID,
		req.TargetAppointmentID, req.TargetAuthority, req.TargetDescription,
		req.CustomerName, req.CustomerPhone, req.CustomerEmail, req.RequestedTimezone,
		req.PartySize, nullTime(req.RequestedStartTime), nullTime(req.RequestedEndTime), req.Notes).Scan(&requestID)
	if errors.Is(err, sql.ErrNoRows) {
		var existingFingerprint string
		if err := tx.QueryRowContext(ctx, `
			SELECT id::text, request_fingerprint
			FROM scheduling_requests
			WHERE salon_id::text = $1
			  AND scheduling_authority = $2
			  AND operation_key = $3
			FOR UPDATE
		`, salonID, booking.SchedulingAuthorityOwnerManual, req.OperationKey).Scan(&requestID, &existingFingerprint); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, false, pos.ErrNotFound
			}
			return nil, false, err
		}
		if existingFingerprint != requestFingerprint {
			return nil, false, booking.ErrOperationConflict
		}
		request, err := getRequestTx(ctx, tx, salonID, ownerUserID, requestID)
		if err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return request, true, nil
	}
	if err != nil {
		return nil, false, err
	}

	for i, segment := range segments {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO scheduling_request_segments (
				salon_id, scheduling_request_id, service_id, service_name,
				guest_reference, quantity, staff_id, staff_name, staff_selection_mode,
				duration_minutes, requested_start_time, requested_end_time, sort_order
			)
			VALUES (
				$1, $2, $3, $4,
				NULLIF($5, ''), $6, NULLIF($7, '')::uuid, NULLIF($8, ''), $9,
				$10, $11, $12, $13
			)
		`, salonID, requestID, segment.ServiceID, segment.ServiceName,
			segment.GuestReference, segment.Quantity, segment.StaffID, segment.StaffName, segment.StaffSelectionMode,
			segment.DurationMinutes, nullTime(segment.RequestedStartTime), nullTime(segment.RequestedEndTime), i+1); err != nil {
			return nil, false, err
		}
	}

	eventPayload, err := json.Marshal(map[string]any{
		"operation_type":              req.OperationType,
		"source":                      req.Source,
		"status":                      scheduling.SchedulingRequestStatusPending,
		"target_scheduling_authority": req.TargetAuthority,
	})
	if err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scheduling_request_events (
			salon_id, scheduling_request_id, action_key, action_fingerprint,
			event_type, request_version, actor_user_id, payload
		)
		VALUES ($1, $2, $3, $4, $5, 1, $6, $7::jsonb)
	`, salonID, requestID, "request-created:"+req.OperationKey, requestFingerprint,
		scheduling.SchedulingRequestEventCreated, requestCreatedActorUserID(req.Source, ownerUserID), eventPayload); err != nil {
		return nil, false, err
	}

	notificationPayload, err := json.Marshal(map[string]any{
		"operation_type":              req.OperationType,
		"scheduling_authority":        booking.SchedulingAuthorityOwnerManual,
		"target_scheduling_authority": req.TargetAuthority,
		"scheduling_request_id":       requestID,
		"status":                      scheduling.SchedulingRequestStatusPending,
	})
	if err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO owner_notifications (
			salon_id, scheduling_request_id, type, status, title, message,
			dedupe_key, payload, delivery_status
		)
		VALUES ($1, $2, $3, 'pending', $4, $5, $6, $7::jsonb, 'queued')
		ON CONFLICT (salon_id, dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING
	`, salonID, requestID, ownerManualNotificationType,
		"Scheduling request needs owner review",
		"A customer scheduling request is waiting for owner review.",
		"owner-manual-request-pending:"+requestID, notificationPayload); err != nil {
		return nil, false, err
	}

	request, err := getRequestTx(ctx, tx, salonID, ownerUserID, requestID)
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return request, false, nil
}

func (r *Repository) List(ctx context.Context, salonID string, ownerUserID string, status scheduling.SchedulingRequestStatus, limit int, offset int) (*scheduling.ListSchedulingRequestsResponse, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT request.id::text
		FROM scheduling_requests request
		JOIN salons salon ON salon.id = request.salon_id
		WHERE request.salon_id::text = $1
		  AND public.has_active_tenant_membership(salon.id, $2::uuid)
		  AND ($3 = '' OR request.status = $3)
		ORDER BY request.created_at DESC, request.id DESC
		LIMIT $4 OFFSET $5
	`, salonID, ownerUserID, status, limit+1, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0, limit+1)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		var exists bool
		if err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM salons WHERE id::text = $1 AND public.has_active_tenant_membership(id, $2::uuid))`, salonID, ownerUserID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, pos.ErrNotFound
		}
	}
	hasMore := len(ids) > limit
	if hasMore {
		ids = ids[:limit]
	}
	items := make([]scheduling.SchedulingRequest, 0, len(ids))
	for _, id := range ids {
		request, err := r.Get(ctx, salonID, ownerUserID, id)
		if err != nil {
			return nil, err
		}
		items = append(items, *request)
	}
	return &scheduling.ListSchedulingRequestsResponse{
		SchedulingRequests: items,
		Limit:              limit,
		Offset:             offset,
		HasMore:            hasMore,
	}, nil
}

func (r *Repository) Get(ctx context.Context, salonID string, ownerUserID string, requestID string) (*scheduling.SchedulingRequest, error) {
	return getRequest(ctx, r.db, salonID, ownerUserID, requestID)
}

func (r *Repository) Transition(ctx context.Context, salonID string, ownerUserID string, requestID string, req scheduling.TransitionSchedulingRequest, actionFingerprint string) (*scheduling.SchedulingRequest, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	current, err := lockRequestTx(ctx, tx, salonID, ownerUserID, requestID)
	if err != nil {
		return nil, false, err
	}
	var existingFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT action_fingerprint
		FROM scheduling_request_events
		WHERE salon_id::text = $1
		  AND scheduling_request_id::text = $2
		  AND action_key = $3
	`, salonID, requestID, req.ActionKey).Scan(&existingFingerprint)
	if err == nil {
		if existingFingerprint != actionFingerprint {
			return nil, false, booking.ErrOperationConflict
		}
		request, err := getRequestTx(ctx, tx, salonID, ownerUserID, requestID)
		if err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return request, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	if current.Version != req.ExpectedVersion {
		return nil, false, scheduling.ErrSchedulingRequestVersion
	}
	if current.Status == scheduling.SchedulingRequestStatusResolved || current.Status == scheduling.SchedulingRequestStatusDismissed {
		return nil, false, scheduling.ErrSchedulingRequestTerminal
	}
	if !transitionAllowed(current.Status, req.Status) {
		return nil, false, scheduling.ErrSchedulingRequestTransition
	}

	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `
		UPDATE scheduling_requests
		SET status = $1,
		    version = version + 1,
		    resolution_reason = CASE WHEN $1 IN ('resolved', 'dismissed') THEN $2 ELSE NULL END,
		    contacted_at = CASE WHEN $1 = 'contacted' THEN COALESCE(contacted_at, $3) ELSE contacted_at END,
		    resolved_at = CASE WHEN $1 = 'resolved' THEN $3 ELSE NULL END,
		    dismissed_at = CASE WHEN $1 = 'dismissed' THEN $3 ELSE NULL END,
		    updated_at = $3
		WHERE id::text = $4
		  AND salon_id::text = $5
		  AND version = $6
	`, req.Status, nullString(req.ResolutionReason), now, requestID, salonID, req.ExpectedVersion)
	if err := requireOneRow(result, err); err != nil {
		return nil, false, err
	}
	newVersion := current.Version + 1
	payload, err := json.Marshal(map[string]any{
		"from_status":       current.Status,
		"note":              req.Note,
		"resolution_reason": req.ResolutionReason,
		"to_status":         req.Status,
	})
	if err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scheduling_request_events (
			salon_id, scheduling_request_id, action_key, action_fingerprint,
			event_type, request_version, actor_user_id, payload
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
	`, salonID, requestID, req.ActionKey, actionFingerprint,
		scheduling.SchedulingRequestEventStatusChanged, newVersion, ownerUserID, payload); err != nil {
		return nil, false, err
	}
	request, err := getRequestTx(ctx, tx, salonID, ownerUserID, requestID)
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return request, false, nil
}

type segmentSnapshot struct {
	ServiceID          string
	ServiceName        string
	StaffID            string
	StaffName          string
	StaffSelectionMode string
	GuestReference     string
	Quantity           int
	DurationMinutes    int
	RequestedStartTime time.Time
	RequestedEndTime   time.Time
}

func resolveSegmentSnapshots(ctx context.Context, tx *sql.Tx, salonID string, req scheduling.ActionRequest) ([]segmentSnapshot, time.Time, error) {
	segments := make([]segmentSnapshot, 0, len(req.Segments))
	var requestEnd time.Time
	for _, input := range req.Segments {
		var snapshot segmentSnapshot
		snapshot.ServiceID = input.ServiceID
		snapshot.StaffID = input.StaffID
		snapshot.StaffSelectionMode = input.StaffSelectionMode
		snapshot.GuestReference = input.GuestReference
		snapshot.Quantity = input.Quantity
		if err := tx.QueryRowContext(ctx, `
			SELECT name, duration_minutes
			FROM services
			WHERE id::text = $1
			  AND salon_id::text = $2
			  AND active = true
			  AND ai_bookable = true
			  AND archived_at IS NULL
			  AND duration_minutes > 0
		`, input.ServiceID, salonID).Scan(&snapshot.ServiceName, &snapshot.DurationMinutes); errors.Is(err, sql.ErrNoRows) {
			return nil, time.Time{}, pos.ErrNotFound
		} else if err != nil {
			return nil, time.Time{}, err
		}
		if input.StaffSelectionMode == booking.StaffSelectionSpecific {
			if err := tx.QueryRowContext(ctx, `
				SELECT name
				FROM staff
				WHERE id::text = $1
				  AND salon_id::text = $2
				  AND active = true
				  AND ai_bookable = true
				  AND archived_at IS NULL
			`, input.StaffID, salonID).Scan(&snapshot.StaffName); errors.Is(err, sql.ErrNoRows) {
				return nil, time.Time{}, pos.ErrNotFound
			} else if err != nil {
				return nil, time.Time{}, err
			}
		}
		snapshot.RequestedStartTime = input.RequestedStartTime
		if snapshot.RequestedStartTime.IsZero() {
			snapshot.RequestedStartTime = req.RequestedStartTime
		}
		snapshot.RequestedEndTime = input.RequestedEndTime
		if snapshot.RequestedEndTime.IsZero() && !snapshot.RequestedStartTime.IsZero() {
			snapshot.RequestedEndTime = snapshot.RequestedStartTime.Add(time.Duration(snapshot.DurationMinutes) * time.Minute)
		}
		if snapshot.RequestedEndTime.After(requestEnd) {
			requestEnd = snapshot.RequestedEndTime
		}
		segments = append(segments, snapshot)
	}
	return segments, requestEnd, nil
}

type requestQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func getRequest(ctx context.Context, queryer requestQuerier, salonID string, ownerUserID string, requestID string) (*scheduling.SchedulingRequest, error) {
	request, err := scanRequest(queryer.QueryRowContext(ctx, requestSelect+`
		WHERE request.id::text = $1
		  AND request.salon_id::text = $2
		  AND (public.app_rls_system_salon_allowed(salon.id)
		       OR public.has_active_tenant_membership(salon.id, $3::uuid)
		       OR public.app_actor_feature_access($3::uuid, salon.id, 'calls.simulate', 'calls'))
	`, requestID, salonID, ownerUserID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pos.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := hydrateRequest(ctx, queryer, request); err != nil {
		return nil, err
	}
	return request, nil
}

func getRequestTx(ctx context.Context, tx *sql.Tx, salonID string, ownerUserID string, requestID string) (*scheduling.SchedulingRequest, error) {
	return getRequest(ctx, tx, salonID, ownerUserID, requestID)
}

func lockRequestTx(ctx context.Context, tx *sql.Tx, salonID string, ownerUserID string, requestID string) (*scheduling.SchedulingRequest, error) {
	request, err := scanRequest(tx.QueryRowContext(ctx, requestSelect+`
		WHERE request.id::text = $1
		  AND request.salon_id::text = $2
		  AND (public.app_rls_system_salon_allowed(salon.id)
		       OR public.has_active_tenant_membership(salon.id, $3::uuid)
		       OR public.app_actor_feature_access($3::uuid, salon.id, 'calls.simulate', 'calls'))
		FOR UPDATE OF request
	`, requestID, salonID, ownerUserID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pos.ErrNotFound
	}
	return request, err
}

const requestSelect = `
	SELECT request.id::text,
	       request.salon_id::text,
	       request.scheduling_authority,
	       request.operation_key,
	       request.operation_type,
	       request.status,
	       request.version,
	       request.source,
	       COALESCE(request.call_session_id::text, ''),
	       COALESCE(request.target_appointment_id::text, ''),
	       COALESCE(request.target_scheduling_authority, ''),
	       COALESCE(request.target_description, ''),
	       request.customer_name,
	       request.customer_phone,
	       COALESCE(request.customer_email, ''),
	       request.requested_timezone,
	       request.party_size,
	       request.requested_start_time,
	       request.requested_end_time,
	       COALESCE(request.notes, ''),
	       COALESCE(request.resolution_reason, ''),
	       request.contacted_at,
	       request.resolved_at,
	       request.dismissed_at,
	       request.redacted_at,
	       COALESCE(request.redaction_version, 0),
	       request.created_at,
	       request.updated_at
	FROM scheduling_requests request
	JOIN salons salon ON salon.id = request.salon_id
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRequest(row rowScanner) (*scheduling.SchedulingRequest, error) {
	var item scheduling.SchedulingRequest
	var operationType string
	var status string
	var requestedStart sql.NullTime
	var requestedEnd sql.NullTime
	var contactedAt sql.NullTime
	var resolvedAt sql.NullTime
	var dismissedAt sql.NullTime
	var redactedAt sql.NullTime
	if err := row.Scan(
		&item.ID,
		&item.SalonID,
		&item.SchedulingAuthority,
		&item.OperationKey,
		&operationType,
		&status,
		&item.Version,
		&item.Source,
		&item.CallSessionID,
		&item.TargetAppointmentID,
		&item.TargetAuthority,
		&item.TargetDescription,
		&item.CustomerName,
		&item.CustomerPhone,
		&item.CustomerEmail,
		&item.RequestedTimezone,
		&item.PartySize,
		&requestedStart,
		&requestedEnd,
		&item.Notes,
		&item.ResolutionReason,
		&contactedAt,
		&resolvedAt,
		&dismissedAt,
		&redactedAt,
		&item.RedactionVersion,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	item.OperationType = scheduling.OperationKind(operationType)
	item.Status = scheduling.SchedulingRequestStatus(status)
	item.RequestedStartTime = nullableTimePointer(requestedStart)
	item.RequestedEndTime = nullableTimePointer(requestedEnd)
	item.ContactedAt = nullableTimePointer(contactedAt)
	item.ResolvedAt = nullableTimePointer(resolvedAt)
	item.DismissedAt = nullableTimePointer(dismissedAt)
	item.RedactedAt = nullableTimePointer(redactedAt)
	item.Redacted = redactedAt.Valid
	return &item, nil
}

func hydrateRequest(ctx context.Context, queryer requestQuerier, request *scheduling.SchedulingRequest) error {
	segmentRows, err := queryer.QueryContext(ctx, `
		SELECT id::text, scheduling_request_id::text, service_id::text, service_name,
		       COALESCE(staff_id::text, ''), COALESCE(staff_name, ''), staff_selection_mode,
		       COALESCE(guest_reference, ''), quantity, duration_minutes,
		       requested_start_time, requested_end_time, sort_order,
		       redacted_at, COALESCE(redaction_version, 0), created_at
		FROM scheduling_request_segments
		WHERE salon_id::text = $1 AND scheduling_request_id::text = $2
		ORDER BY sort_order ASC, id ASC
	`, request.SalonID, request.ID)
	if err != nil {
		return err
	}
	defer segmentRows.Close()
	request.Segments = make([]scheduling.SchedulingRequestSegment, 0)
	for segmentRows.Next() {
		var segment scheduling.SchedulingRequestSegment
		var start sql.NullTime
		var end sql.NullTime
		var redactedAt sql.NullTime
		if err := segmentRows.Scan(
			&segment.ID, &segment.SchedulingRequestID, &segment.ServiceID, &segment.ServiceName,
			&segment.StaffID, &segment.StaffName, &segment.StaffSelectionMode,
			&segment.GuestReference, &segment.Quantity, &segment.DurationMinutes,
			&start, &end, &segment.SortOrder, &redactedAt, &segment.RedactionVersion,
			&segment.CreatedAt,
		); err != nil {
			return err
		}
		segment.RequestedStartTime = nullableTimePointer(start)
		segment.RequestedEndTime = nullableTimePointer(end)
		segment.RedactedAt = nullableTimePointer(redactedAt)
		segment.Redacted = redactedAt.Valid
		request.Segments = append(request.Segments, segment)
	}
	if err := segmentRows.Err(); err != nil {
		return err
	}

	eventRows, err := queryer.QueryContext(ctx, `
		SELECT id::text, scheduling_request_id::text, action_key, event_type,
		       request_version, COALESCE(actor_user_id::text, ''), payload,
		       redacted_at, COALESCE(redaction_version, 0), created_at
		FROM scheduling_request_events
		WHERE salon_id::text = $1 AND scheduling_request_id::text = $2
		ORDER BY created_at ASC, id ASC
	`, request.SalonID, request.ID)
	if err != nil {
		return err
	}
	defer eventRows.Close()
	request.Events = make([]scheduling.SchedulingRequestEvent, 0)
	for eventRows.Next() {
		var event scheduling.SchedulingRequestEvent
		var payload []byte
		var redactedAt sql.NullTime
		if err := eventRows.Scan(
			&event.ID, &event.SchedulingRequestID, &event.ActionKey, &event.EventType,
			&event.RequestVersion, &event.ActorUserID, &payload, &redactedAt,
			&event.RedactionVersion, &event.CreatedAt,
		); err != nil {
			return err
		}
		event.Payload = append(json.RawMessage(nil), payload...)
		event.RedactedAt = nullableTimePointer(redactedAt)
		event.Redacted = redactedAt.Valid
		request.Events = append(request.Events, event)
	}
	return eventRows.Err()
}

func transitionAllowed(from scheduling.SchedulingRequestStatus, to scheduling.SchedulingRequestStatus) bool {
	switch from {
	case scheduling.SchedulingRequestStatusPending:
		return to == scheduling.SchedulingRequestStatusContacted || to == scheduling.SchedulingRequestStatusResolved || to == scheduling.SchedulingRequestStatusDismissed
	case scheduling.SchedulingRequestStatusContacted:
		return to == scheduling.SchedulingRequestStatusResolved || to == scheduling.SchedulingRequestStatusDismissed
	default:
		return false
	}
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

// Creation events name an owner actor only when the trusted dashboard handler
// set the owner-dashboard source. Conversation and voice sources are system
// actions and intentionally persist a NULL actor_user_id.
func requestCreatedActorUserID(source string, ownerUserID string) any {
	if source == booking.SourceOwnerDashboard {
		return ownerUserID
	}
	return nil
}

func requireOneRow(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("expected one scheduling request row, got %d", rows)
	}
	return nil
}

var _ Store = (*Repository)(nil)
