package conversation

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/booking"
)

const (
	evidenceReasonNoResult              = "no_result"
	evidenceReasonOwnerRequestInvalid   = "owner_request_invalid"
	evidenceReasonInternalResultInvalid = "internal_result_invalid"
	evidenceReasonExternalResultInvalid = "external_result_invalid"
	evidenceReasonPartyResultInvalid    = "party_result_invalid"
)

type internalEvidenceSnapshot struct {
	SessionID                      string
	AttemptID                      string
	AttemptStatus                  string
	OperationType                  string
	TargetVersion                  sql.NullInt64
	AttemptVersion                 int
	AttemptSchedulingVersion       int64
	AttemptConfigVersion           int64
	EventType                      string
	EventVersion                   int
	EventSchedulingVersion         int64
	EventConfigVersion             int64
	AppointmentID                  string
	AppointmentStatus              string
	AppointmentVersion             int
	AppointmentSchedulingVersion   int64
	AppointmentConfigVersion       int64
	PartySize                      int
	HasProviderEvidence            bool
	AttemptChildCount              int
	ResultChildCount               int
	InvalidResultChildCount        int
	CurrentActiveChildCount        int
	InvalidCurrentActiveChildCount int
}

type externalEvidenceSnapshot struct {
	SessionID                      string
	AttemptID                      string
	AttemptAuthority               string
	AttemptAuthorityProvider       string
	AttemptAuthorityID             string
	AttemptAuthorityVersion        int
	AttemptPOSProvider             string
	AttemptPOSID                   string
	AttemptPOSVersion              int
	AttemptStatus                  string
	OperationType                  string
	OperationKey                   string
	ProviderOutcome                string
	RetryPolicy                    string
	ReconciliationStatus           string
	ErrorCode                      string
	ErrorMessage                   string
	TargetAppointmentID            string
	TargetAuthorityVersion         sql.NullInt64
	AppointmentID                  string
	AppointmentBookingAttemptID    string
	AppointmentAuthority           string
	AppointmentAuthorityProvider   string
	AppointmentAuthorityID         string
	AppointmentAuthorityVersion    int
	AppointmentPOSProvider         string
	AppointmentPOSID               string
	AppointmentPOSVersion          int
	AppointmentStatus              string
	AttemptSegmentCount            int
	InvalidAttemptSegmentCount     int
	AppointmentSegmentCount        int
	InvalidAppointmentSegmentCount int
	SegmentsMatch                  bool
}

type splitEvidenceInput struct {
	SessionID     string
	AttemptID     string
	AppointmentID string
	OperationKey  string
	Ordinal       int
}

func incompleteSchedulingResultEvidence(reason string) *SchedulingResultEvidence {
	return &SchedulingResultEvidence{
		Kind:             SchedulingEvidenceKindIncomplete,
		ResultStatus:     SchedulingEvidenceStatusIncomplete,
		CurrentStatus:    SchedulingEvidenceStatusIncomplete,
		IncompleteReason: reason,
	}
}

func (r *Repository) hydrateSchedulingResultEvidence(
	ctx context.Context,
	salonID string,
	ownerUserID string,
	sessions []*Session,
) error {
	if len(sessions) == 0 {
		return nil
	}
	ids := make([]string, 0, len(sessions))
	byID := make(map[string]*Session, len(sessions))
	for _, session := range sessions {
		if session == nil {
			continue
		}
		session.SchedulingResultEvidence = incompleteSchedulingResultEvidence(evidenceReasonNoResult)
		if session.SchedulingRequestID != "" {
			session.SchedulingResultEvidence = incompleteSchedulingResultEvidence(evidenceReasonOwnerRequestInvalid)
		}
		ids = append(ids, session.ID)
		byID[session.ID] = session
	}
	if len(ids) == 0 {
		return nil
	}
	if err := r.hydrateOwnerRequestEvidence(ctx, salonID, ownerUserID, ids, byID); err != nil {
		return err
	}
	if err := r.hydrateInternalResultEvidence(ctx, salonID, ownerUserID, ids, byID); err != nil {
		return err
	}
	if err := r.hydrateExternalResultEvidence(ctx, salonID, ownerUserID, sessions, byID); err != nil {
		return err
	}
	return nil
}

func (r *Repository) hydrateOwnerRequestEvidence(
	ctx context.Context,
	salonID string,
	ownerUserID string,
	sessionIDs []string,
	byID map[string]*Session,
) error {
	rows, err := r.db.QueryContext(ctx, `
		SELECT cs.id::text, request.id::text, request.operation_type, request.status,
		       request.scheduling_authority, request.target_scheduling_authority
		FROM call_sessions cs
		JOIN salons salon ON salon.id = cs.salon_id
		JOIN scheduling_requests request
		  ON request.salon_id = cs.salon_id
		 AND request.call_session_id = cs.id
		 AND request.id = cs.scheduling_request_id
		WHERE cs.salon_id = $1
		  AND salon.owner_user_id = $2
		  AND cs.id = ANY($3::uuid[])
	`, salonID, ownerUserID, pq.Array(sessionIDs))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sessionID, requestID, operationType, status, authority string
		var targetAuthority sql.NullString
		if err := rows.Scan(&sessionID, &requestID, &operationType, &status, &authority, &targetAuthority); err != nil {
			return err
		}
		session := byID[sessionID]
		if session == nil {
			continue
		}
		evidence, ok := validateOwnerRequestEvidence(*session, requestID, operationType, status, authority, targetAuthority)
		if !ok {
			if session.SchedulingRequestID != "" {
				session.SchedulingResultEvidence = incompleteSchedulingResultEvidence(evidenceReasonOwnerRequestInvalid)
			}
			continue
		}
		session.SchedulingResultEvidence = evidence
	}
	return rows.Err()
}

func validateOwnerRequestEvidence(
	session Session,
	requestID string,
	operationType string,
	status string,
	authority string,
	targetAuthority sql.NullString,
) (*SchedulingResultEvidence, bool) {
	if requestID == "" || !isSchedulingRequestStatus(status) ||
		session.SchedulingRequestID != requestID || session.BookingAttemptID != "" ||
		session.AppointmentID != "" || authority != booking.SchedulingAuthorityOwnerManual ||
		normalizedBookingAction(session) != operationType || session.Outcome != OutcomeOwnerReviewPending {
		return nil, false
	}
	target := ""
	if targetAuthority.Valid {
		target = strings.TrimSpace(targetAuthority.String)
		if target != targetAuthority.String || !isSchedulingAuthorityToken(target) {
			return nil, false
		}
	}
	return &SchedulingResultEvidence{
		Complete:                  true,
		Kind:                      SchedulingEvidenceKindPendingOwnerReview,
		SchedulingAuthority:       booking.SchedulingAuthorityOwnerManual,
		TargetSchedulingAuthority: target,
		OperationType:             operationType,
		ResultStatus:              OutcomeOwnerReviewPending,
		CurrentStatus:             status,
		IsCurrent:                 true,
		SchedulingRequestID:       requestID,
	}, true
}

func isSchedulingRequestStatus(value string) bool {
	switch value {
	case "pending", "contacted", "resolved", "dismissed":
		return true
	default:
		return false
	}
}

func isSchedulingAuthorityToken(value string) bool {
	switch value {
	case booking.SchedulingAuthorityOwnerManual,
		booking.SchedulingAuthorityManleAICalendar,
		booking.SchedulingAuthorityExternalProvider:
		return true
	default:
		return false
	}
}

func (r *Repository) hydrateInternalResultEvidence(
	ctx context.Context,
	salonID string,
	ownerUserID string,
	sessionIDs []string,
	byID map[string]*Session,
) error {
	rows, err := r.db.QueryContext(ctx, `
		SELECT cs.id::text,
		       attempt.id::text, attempt.status, attempt.operation_type,
		       attempt.target_authority_appointment_version,
		       attempt.authority_appointment_version,
		       attempt.scheduling_authority_version,
		       attempt.authority_config_version,
		       event.event_type, event.authority_appointment_version,
		       event.scheduling_authority_version, event.authority_config_version,
		       appointment.id::text, appointment.status,
		       appointment.authority_appointment_version,
		       appointment.scheduling_authority_version,
		       appointment.authority_config_version,
		       appointment.party_size,
		       (
		         attempt.pos_provider IS NOT NULL OR attempt.pos_booking_id IS NOT NULL OR
		         attempt.pos_booking_version IS NOT NULL OR attempt.pos_idempotency_key IS NOT NULL OR
		         attempt.provider_location_id IS NOT NULL OR attempt.provider_snapshot_generation IS NOT NULL OR
		         attempt.authority_provider IS NOT NULL OR attempt.authority_location_id IS NOT NULL OR
		         attempt.authority_snapshot_generation IS NOT NULL OR
		         appointment.pos_provider IS NOT NULL OR appointment.pos_appointment_id IS NOT NULL OR
		         appointment.pos_appointment_version IS NOT NULL OR appointment.pos_customer_id IS NOT NULL OR
		         appointment.pos_sync_status IS NOT NULL OR appointment.last_pos_synced_at IS NOT NULL OR
		         appointment.pos_sync_error IS NOT NULL OR appointment.authority_provider IS NOT NULL
		       ) AS has_provider_evidence,
		       (SELECT count(*) FROM booking_attempt_segments child
		         WHERE child.salon_id = attempt.salon_id
		           AND child.booking_attempt_id = attempt.id
		           AND child.scheduling_authority = 'manleai_calendar'),
		       (SELECT count(*) FROM appointment_services child
		         WHERE child.salon_id = appointment.salon_id
		           AND child.appointment_id = appointment.id
		           AND child.scheduling_authority = 'manleai_calendar'
		           AND child.plan_version = CASE WHEN event.event_type = 'appointment_cancelled'
		                                         THEN attempt.target_authority_appointment_version
		                                         ELSE event.authority_appointment_version END
		           AND (event.event_type <> 'appointment_cancelled' OR child.released_by_attempt_id = attempt.id)),
		       (SELECT count(*) FROM appointment_services child
		         WHERE child.salon_id = appointment.salon_id
		           AND child.appointment_id = appointment.id
		           AND child.scheduling_authority = 'manleai_calendar'
		           AND child.plan_version = CASE WHEN event.event_type = 'appointment_cancelled'
		                                         THEN attempt.target_authority_appointment_version
		                                         ELSE event.authority_appointment_version END
		           AND (event.event_type <> 'appointment_cancelled' OR child.released_by_attempt_id = attempt.id)
		           AND (child.service_id IS NULL OR child.staff_id IS NULL OR child.duration_minutes <= 0 OR
		                child.sort_order <= 0 OR child.scheduled_start_time IS NULL OR
		                child.scheduled_end_time <= child.scheduled_start_time OR
		                child.occupied_start_time IS NULL OR child.occupied_end_time IS NULL OR
		                child.occupied_end_time <= child.occupied_start_time)),
		       (SELECT count(*) FROM appointment_services child
		         WHERE child.salon_id = appointment.salon_id
		           AND child.appointment_id = appointment.id
		           AND child.scheduling_authority = 'manleai_calendar'
		           AND child.plan_version = appointment.authority_appointment_version
		           AND child.released_at IS NULL),
		       (SELECT count(*) FROM appointment_services child
		         WHERE child.salon_id = appointment.salon_id
		           AND child.appointment_id = appointment.id
		           AND child.scheduling_authority = 'manleai_calendar'
		           AND child.plan_version = appointment.authority_appointment_version
		           AND child.released_at IS NULL
		           AND (child.service_id IS NULL OR child.staff_id IS NULL OR child.duration_minutes <= 0 OR
		                child.sort_order <= 0 OR child.scheduled_start_time IS NULL OR
		                child.scheduled_end_time <= child.scheduled_start_time OR
		                child.occupied_start_time IS NULL OR child.occupied_end_time IS NULL OR
		                child.occupied_end_time <= child.occupied_start_time))
		FROM call_sessions cs
		JOIN salons salon ON salon.id = cs.salon_id
		JOIN booking_attempts attempt
		  ON attempt.salon_id = cs.salon_id AND attempt.id = cs.booking_attempt_id
		JOIN manleai_calendar_execution_events event
		  ON event.salon_id = attempt.salon_id AND event.booking_attempt_id = attempt.id
		JOIN appointments appointment
		  ON appointment.salon_id = event.salon_id
		 AND appointment.id = event.appointment_id
		 AND appointment.id = cs.appointment_id
		WHERE cs.salon_id = $1
		  AND salon.owner_user_id = $2
		  AND cs.id = ANY($3::uuid[])
		  AND attempt.scheduling_authority = 'manleai_calendar'
		  AND appointment.scheduling_authority = 'manleai_calendar'
		  AND attempt.authority_appointment_id = appointment.id::text
		  AND appointment.authority_appointment_id = appointment.id::text
	`, salonID, ownerUserID, pq.Array(sessionIDs))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var snapshot internalEvidenceSnapshot
		if err := rows.Scan(
			&snapshot.SessionID, &snapshot.AttemptID, &snapshot.AttemptStatus,
			&snapshot.OperationType, &snapshot.TargetVersion, &snapshot.AttemptVersion,
			&snapshot.AttemptSchedulingVersion, &snapshot.AttemptConfigVersion,
			&snapshot.EventType, &snapshot.EventVersion, &snapshot.EventSchedulingVersion,
			&snapshot.EventConfigVersion, &snapshot.AppointmentID, &snapshot.AppointmentStatus,
			&snapshot.AppointmentVersion, &snapshot.AppointmentSchedulingVersion,
			&snapshot.AppointmentConfigVersion, &snapshot.PartySize, &snapshot.HasProviderEvidence,
			&snapshot.AttemptChildCount, &snapshot.ResultChildCount,
			&snapshot.InvalidResultChildCount, &snapshot.CurrentActiveChildCount,
			&snapshot.InvalidCurrentActiveChildCount,
		); err != nil {
			return err
		}
		session := byID[snapshot.SessionID]
		if session == nil {
			continue
		}
		evidence, ok := validateInternalEvidence(*session, snapshot)
		if !ok {
			session.SchedulingResultEvidence = incompleteSchedulingResultEvidence(evidenceReasonInternalResultInvalid)
			continue
		}
		session.SchedulingResultEvidence = evidence
	}
	return rows.Err()
}

func validateInternalEvidence(session Session, snapshot internalEvidenceSnapshot) (*SchedulingResultEvidence, bool) {
	resultStatus, eventType, expectedOutcome, ok := operationEvidenceContract(snapshot.OperationType)
	if !ok || snapshot.AttemptStatus != resultStatus || snapshot.EventType != eventType ||
		session.Outcome != expectedOutcome || normalizedBookingAction(session) != snapshot.OperationType ||
		session.BookingAttemptID != snapshot.AttemptID || session.AppointmentID != snapshot.AppointmentID ||
		snapshot.HasProviderEvidence || snapshot.AttemptVersion != snapshot.EventVersion ||
		snapshot.AttemptSchedulingVersion < 1 || snapshot.AttemptConfigVersion < 1 ||
		snapshot.EventSchedulingVersion != snapshot.AttemptSchedulingVersion ||
		snapshot.EventConfigVersion != snapshot.AttemptConfigVersion ||
		snapshot.AppointmentSchedulingVersion < 1 || snapshot.AppointmentConfigVersion < 1 ||
		snapshot.AppointmentVersion < snapshot.EventVersion ||
		snapshot.PartySize < 1 || snapshot.AttemptChildCount < 1 ||
		snapshot.AttemptChildCount != snapshot.ResultChildCount || snapshot.InvalidResultChildCount != 0 {
		return nil, false
	}
	switch snapshot.OperationType {
	case booking.BookingActionBook:
		if snapshot.TargetVersion.Valid || snapshot.EventVersion != 1 {
			return nil, false
		}
	case booking.BookingActionReschedule, booking.BookingActionCancel:
		if !snapshot.TargetVersion.Valid || snapshot.TargetVersion.Int64 < 1 ||
			snapshot.EventVersion != int(snapshot.TargetVersion.Int64)+1 {
			return nil, false
		}
	}
	if snapshot.AppointmentVersion == snapshot.EventVersion {
		if snapshot.AppointmentStatus != snapshot.AttemptStatus ||
			snapshot.AppointmentSchedulingVersion != snapshot.AttemptSchedulingVersion ||
			snapshot.AppointmentConfigVersion != snapshot.AttemptConfigVersion {
			return nil, false
		}
	}
	if snapshot.AppointmentStatus == booking.StatusCancelled {
		if snapshot.CurrentActiveChildCount != 0 {
			return nil, false
		}
	} else if (snapshot.AppointmentStatus != booking.StatusConfirmed && snapshot.AppointmentStatus != booking.StatusRescheduled) ||
		snapshot.CurrentActiveChildCount < 1 || snapshot.InvalidCurrentActiveChildCount != 0 {
		return nil, false
	}
	if snapshot.OperationType == booking.BookingActionCancel {
		if snapshot.AppointmentVersion != snapshot.EventVersion || snapshot.AppointmentStatus != booking.StatusCancelled {
			return nil, false
		}
	}
	return &SchedulingResultEvidence{
		Complete:                           true,
		Kind:                               SchedulingEvidenceKindCompletedOperation,
		SchedulingAuthority:                booking.SchedulingAuthorityManleAICalendar,
		OperationType:                      snapshot.OperationType,
		ResultStatus:                       snapshot.AttemptStatus,
		CurrentStatus:                      snapshot.AppointmentStatus,
		IsCurrent:                          snapshot.AppointmentVersion == snapshot.EventVersion && snapshot.AppointmentStatus == snapshot.AttemptStatus,
		AppointmentID:                      snapshot.AppointmentID,
		BookingAttemptID:                   snapshot.AttemptID,
		AuthorityAppointmentVersion:        snapshot.EventVersion,
		CurrentAuthorityAppointmentVersion: snapshot.AppointmentVersion,
		RootCount:                          1,
		ResultChildCount:                   snapshot.ResultChildCount,
		CurrentActiveChildCount:            snapshot.CurrentActiveChildCount,
	}, true
}

func (r *Repository) hydrateExternalResultEvidence(
	ctx context.Context,
	salonID string,
	ownerUserID string,
	sessions []*Session,
	byID map[string]*Session,
) error {
	regularIDs := make([]string, 0, len(sessions))
	operationKeys := make([]string, 0, len(sessions))
	splitInputs := make([]splitEvidenceInput, 0)
	for _, sessionPointer := range sessions {
		if sessionPointer == nil {
			continue
		}
		if sessionPointer.SchedulingResultEvidence != nil && sessionPointer.SchedulingResultEvidence.Complete {
			continue
		}
		session := *sessionPointer
		if inputs, ok := splitEvidenceInputs(session); ok {
			if len(inputs) == 0 {
				sessionPointer.SchedulingResultEvidence = incompleteSchedulingResultEvidence(evidenceReasonPartyResultInvalid)
				continue
			}
			splitInputs = append(splitInputs, inputs...)
			continue
		}
		regularIDs = append(regularIDs, session.ID)
		operationKeys = append(operationKeys, conversationOperationKey(session, normalizedBookingAction(session), operationTargetKey(session)))
	}
	if len(regularIDs) > 0 {
		if err := r.hydrateRegularExternalResultEvidence(ctx, salonID, ownerUserID, regularIDs, operationKeys, byID); err != nil {
			return err
		}
	}
	if len(splitInputs) > 0 {
		if err := r.hydrateSplitExternalResultEvidence(ctx, salonID, ownerUserID, splitInputs, byID); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) hydrateRegularExternalResultEvidence(
	ctx context.Context,
	salonID string,
	ownerUserID string,
	sessionIDs []string,
	operationKeys []string,
	byID map[string]*Session,
) error {
	rows, err := r.db.QueryContext(ctx, externalEvidenceQuery(false),
		salonID, ownerUserID, pq.Array(sessionIDs), pq.Array(operationKeys))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		snapshot, _, err := scanExternalEvidenceSnapshot(rows, false)
		if err != nil {
			return err
		}
		session := byID[snapshot.SessionID]
		if session == nil {
			continue
		}
		evidence, ok := validateExternalEvidence(*session, snapshot, snapshot.OperationKey, false)
		if !ok {
			session.SchedulingResultEvidence = incompleteSchedulingResultEvidence(evidenceReasonExternalResultInvalid)
			continue
		}
		session.SchedulingResultEvidence = evidence
	}
	return rows.Err()
}

func splitEvidenceInputs(session Session) ([]splitEvidenceInput, bool) {
	plan := session.PartyPlan
	if plan == nil || len(plan.SplitBookingAttemptIDs) == 0 && len(plan.SplitAppointmentIDs) == 0 {
		return nil, false
	}
	if plan.PartySize < 2 || len(plan.SplitBookingAttemptIDs) < 2 ||
		len(plan.SplitBookingAttemptIDs) != len(plan.SplitAppointmentIDs) ||
		len(session.BookingSegments) != len(plan.SplitBookingAttemptIDs) ||
		session.BookingAttemptID != strings.TrimSpace(plan.SplitBookingAttemptIDs[0]) ||
		session.AppointmentID != strings.TrimSpace(plan.SplitAppointmentIDs[0]) ||
		normalizedBookingAction(session) != booking.BookingActionBook || session.Outcome != OutcomeBookingConfirmed {
		return []splitEvidenceInput{}, true
	}
	option, ok := selectedPartySplitOption(plan)
	if !ok {
		return []splitEvidenceInput{}, true
	}
	expectedKeys := make([]string, 0, len(plan.SplitBookingAttemptIDs))
	for blockIndex, block := range option.Blocks {
		for segmentIndex := range block.Segments {
			expectedKeys = append(expectedKeys, conversationOperationKey(session, "split", fmt.Sprint(blockIndex), fmt.Sprint(segmentIndex)))
		}
	}
	if len(expectedKeys) != len(plan.SplitBookingAttemptIDs) {
		return []splitEvidenceInput{}, true
	}
	seenAttempts := make(map[string]struct{}, len(expectedKeys))
	seenAppointments := make(map[string]struct{}, len(expectedKeys))
	inputs := make([]splitEvidenceInput, 0, len(expectedKeys))
	for index := range expectedKeys {
		attemptID := strings.TrimSpace(plan.SplitBookingAttemptIDs[index])
		appointmentID := strings.TrimSpace(plan.SplitAppointmentIDs[index])
		if attemptID == "" || appointmentID == "" {
			return []splitEvidenceInput{}, true
		}
		if _, exists := seenAttempts[attemptID]; exists {
			return []splitEvidenceInput{}, true
		}
		if _, exists := seenAppointments[appointmentID]; exists {
			return []splitEvidenceInput{}, true
		}
		seenAttempts[attemptID] = struct{}{}
		seenAppointments[appointmentID] = struct{}{}
		inputs = append(inputs, splitEvidenceInput{
			SessionID: session.ID, AttemptID: attemptID, AppointmentID: appointmentID,
			OperationKey: expectedKeys[index], Ordinal: index,
		})
	}
	return inputs, true
}

func (r *Repository) hydrateSplitExternalResultEvidence(
	ctx context.Context,
	salonID string,
	ownerUserID string,
	inputs []splitEvidenceInput,
	byID map[string]*Session,
) error {
	sessionIDs := make([]string, 0, len(inputs))
	attemptIDs := make([]string, 0, len(inputs))
	appointmentIDs := make([]string, 0, len(inputs))
	operationKeys := make([]string, 0, len(inputs))
	ordinals := make([]int64, 0, len(inputs))
	expectedBySession := make(map[string]int)
	for _, input := range inputs {
		sessionIDs = append(sessionIDs, input.SessionID)
		attemptIDs = append(attemptIDs, input.AttemptID)
		appointmentIDs = append(appointmentIDs, input.AppointmentID)
		operationKeys = append(operationKeys, input.OperationKey)
		ordinals = append(ordinals, int64(input.Ordinal))
		expectedBySession[input.SessionID]++
	}
	rows, err := r.db.QueryContext(ctx, externalEvidenceQuery(true), salonID, ownerUserID,
		pq.Array(sessionIDs), pq.Array(operationKeys), pq.Array(attemptIDs), pq.Array(appointmentIDs), pq.Array(ordinals))
	if err != nil {
		return err
	}
	defer rows.Close()
	type splitAggregate struct {
		valid, roots, resultChildren, currentChildren int
		allCurrent                                    bool
		statuses                                      []string
		maxResultVersion, maxCurrentVersion           int
	}
	aggregates := make(map[string]*splitAggregate)
	for rows.Next() {
		snapshot, expectedKey, err := scanExternalEvidenceSnapshot(rows, true)
		if err != nil {
			return err
		}
		session := byID[snapshot.SessionID]
		if session == nil {
			continue
		}
		aggregate := aggregates[snapshot.SessionID]
		if aggregate == nil {
			aggregate = &splitAggregate{allCurrent: true}
			aggregates[snapshot.SessionID] = aggregate
		}
		aggregate.roots++
		evidence, ok := validateExternalEvidence(*session, snapshot, expectedKey, true)
		if !ok || evidence.ResultChildCount != 1 {
			aggregate.allCurrent = false
			continue
		}
		aggregate.valid++
		aggregate.resultChildren += evidence.ResultChildCount
		aggregate.currentChildren += evidence.CurrentActiveChildCount
		aggregate.allCurrent = aggregate.allCurrent && evidence.IsCurrent
		aggregate.statuses = append(aggregate.statuses, evidence.CurrentStatus)
		if evidence.AuthorityAppointmentVersion > aggregate.maxResultVersion {
			aggregate.maxResultVersion = evidence.AuthorityAppointmentVersion
		}
		if evidence.CurrentAuthorityAppointmentVersion > aggregate.maxCurrentVersion {
			aggregate.maxCurrentVersion = evidence.CurrentAuthorityAppointmentVersion
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for sessionID, expected := range expectedBySession {
		session := byID[sessionID]
		aggregate := aggregates[sessionID]
		if session == nil {
			continue
		}
		if expected < 2 || aggregate == nil || aggregate.roots != expected || aggregate.valid != expected ||
			aggregate.resultChildren != expected || len(aggregate.statuses) != expected {
			session.SchedulingResultEvidence = incompleteSchedulingResultEvidence(evidenceReasonPartyResultInvalid)
			continue
		}
		currentStatus := aggregate.statuses[0]
		for _, status := range aggregate.statuses[1:] {
			if status != currentStatus {
				currentStatus = SchedulingEvidenceStatusMixed
				break
			}
		}
		session.SchedulingResultEvidence = &SchedulingResultEvidence{
			Complete:                           true,
			Kind:                               SchedulingEvidenceKindCompletedOperation,
			SchedulingAuthority:                booking.SchedulingAuthorityExternalProvider,
			OperationType:                      booking.BookingActionBook,
			ResultStatus:                       booking.StatusConfirmed,
			CurrentStatus:                      currentStatus,
			IsCurrent:                          aggregate.allCurrent && currentStatus == booking.StatusConfirmed,
			AppointmentID:                      session.AppointmentID,
			BookingAttemptID:                   session.BookingAttemptID,
			AuthorityAppointmentVersion:        aggregate.maxResultVersion,
			CurrentAuthorityAppointmentVersion: aggregate.maxCurrentVersion,
			RootCount:                          expected,
			ResultChildCount:                   aggregate.resultChildren,
			CurrentActiveChildCount:            aggregate.currentChildren,
		}
	}
	return nil
}

func externalEvidenceQuery(split bool) string {
	input := `
		WITH input AS (
		  SELECT session_id, operation_key
		  FROM unnest($3::uuid[], $4::text[]) AS value(session_id, operation_key)
		)`
	join := `
		JOIN booking_attempts attempt
		  ON attempt.salon_id = cs.salon_id
		 AND attempt.id = CASE WHEN cs.booking_attempt_id IS NOT NULL
		                       THEN cs.booking_attempt_id
		                       ELSE (SELECT candidate.id FROM booking_attempts candidate
		                             WHERE candidate.salon_id = cs.salon_id
		                               AND candidate.operation_key = input.operation_key) END
		JOIN appointments appointment
		  ON appointment.salon_id = cs.salon_id AND appointment.id = cs.appointment_id`
	extraSelect := ""
	if split {
		input = `
		WITH input AS (
		  SELECT session_id, operation_key, attempt_id, appointment_id, ordinal
		  FROM unnest($3::uuid[], $4::text[], $5::uuid[], $6::uuid[], $7::bigint[])
		       AS value(session_id, operation_key, attempt_id, appointment_id, ordinal)
		)`
		join = `
		JOIN booking_attempts attempt
		  ON attempt.salon_id = cs.salon_id AND attempt.id = input.attempt_id
		JOIN appointments appointment
		  ON appointment.salon_id = cs.salon_id AND appointment.id = input.appointment_id`
		extraSelect = ", input.operation_key"
	}
	return input + `
		SELECT cs.id::text,
		       attempt.id::text, attempt.scheduling_authority,
		       COALESCE(attempt.authority_provider,''), COALESCE(attempt.authority_appointment_id,''),
		       COALESCE(attempt.authority_appointment_version,-1),
		       COALESCE(attempt.pos_provider,''), COALESCE(attempt.pos_booking_id,''),
		       COALESCE(attempt.pos_booking_version,-1),
		       attempt.status, attempt.operation_type, COALESCE(attempt.operation_key,''),
		       attempt.provider_outcome, attempt.retry_policy, attempt.reconciliation_status,
		       COALESCE(attempt.error_code,''), COALESCE(attempt.error_message,''),
		       COALESCE(attempt.target_appointment_id::text,''),
		       attempt.target_authority_appointment_version,
		       appointment.id::text, appointment.booking_attempt_id::text,
		       appointment.scheduling_authority,
		       COALESCE(appointment.authority_provider,''), COALESCE(appointment.authority_appointment_id,''),
		       COALESCE(appointment.authority_appointment_version,-1),
		       COALESCE(appointment.pos_provider,''), COALESCE(appointment.pos_appointment_id,''),
		       COALESCE(appointment.pos_appointment_version,-1),
		       appointment.status,
		       (SELECT count(*) FROM booking_attempt_segments child
		         WHERE child.salon_id = attempt.salon_id AND child.booking_attempt_id = attempt.id),
		       (SELECT count(*) FROM booking_attempt_segments child
		         WHERE child.salon_id = attempt.salon_id AND child.booking_attempt_id = attempt.id
		           AND (child.scheduling_authority <> 'external_provider' OR
		                child.pos_service_id IS NULL OR length(trim(child.pos_service_id)) = 0 OR child.sort_order <= 0)),
		       (SELECT count(*) FROM appointment_services child
		         WHERE child.salon_id = appointment.salon_id AND child.appointment_id = appointment.id),
		       (SELECT count(*) FROM appointment_services child
		         WHERE child.salon_id = appointment.salon_id AND child.appointment_id = appointment.id
		           AND (child.scheduling_authority <> 'external_provider' OR
		                child.pos_service_id IS NULL OR length(trim(child.pos_service_id)) = 0 OR child.sort_order <= 0)),
		       COALESCE((
		         SELECT jsonb_agg(jsonb_build_array(
		             COALESCE(child.service_id::text, ''), child.pos_service_id,
		             COALESCE(child.pos_service_version, 0), COALESCE(child.staff_id::text, ''),
		             COALESCE(child.pos_staff_id, ''), COALESCE(child.staff_selection_mode, 'specific'),
		             child.duration_minutes, child.sort_order
		         ) ORDER BY child.sort_order, child.id)
		         FROM booking_attempt_segments child
		         WHERE child.salon_id = attempt.salon_id AND child.booking_attempt_id = attempt.id
		       ), '[]'::jsonb) = COALESCE((
		         SELECT jsonb_agg(jsonb_build_array(
		             COALESCE(child.service_id::text, ''), child.pos_service_id,
		             COALESCE(child.pos_service_version, 0), COALESCE(child.staff_id::text, ''),
		             COALESCE(child.pos_staff_id, ''), COALESCE(child.staff_selection_mode, 'specific'),
		             child.duration_minutes, child.sort_order
		         ) ORDER BY child.sort_order, child.id)
		         FROM appointment_services child
		         WHERE child.salon_id = appointment.salon_id AND child.appointment_id = appointment.id
		       ), '[]'::jsonb) AS segments_match` + extraSelect + `
		FROM input
		JOIN call_sessions cs ON cs.id = input.session_id
		JOIN salons salon ON salon.id = cs.salon_id
		` + join + `
		WHERE cs.salon_id = $1 AND salon.owner_user_id = $2
		  AND attempt.scheduling_authority = 'external_provider'
		  AND appointment.scheduling_authority = 'external_provider'
		ORDER BY cs.id, input.operation_key
	`
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanExternalEvidenceSnapshot(scanner rowScanner, withExpectedKey bool) (externalEvidenceSnapshot, string, error) {
	var snapshot externalEvidenceSnapshot
	var expectedKey string
	destinations := []any{
		&snapshot.SessionID, &snapshot.AttemptID, &snapshot.AttemptAuthority,
		&snapshot.AttemptAuthorityProvider, &snapshot.AttemptAuthorityID,
		&snapshot.AttemptAuthorityVersion, &snapshot.AttemptPOSProvider,
		&snapshot.AttemptPOSID, &snapshot.AttemptPOSVersion, &snapshot.AttemptStatus,
		&snapshot.OperationType, &snapshot.OperationKey, &snapshot.ProviderOutcome,
		&snapshot.RetryPolicy, &snapshot.ReconciliationStatus, &snapshot.ErrorCode,
		&snapshot.ErrorMessage, &snapshot.TargetAppointmentID,
		&snapshot.TargetAuthorityVersion,
		&snapshot.AppointmentID, &snapshot.AppointmentBookingAttemptID,
		&snapshot.AppointmentAuthority, &snapshot.AppointmentAuthorityProvider,
		&snapshot.AppointmentAuthorityID, &snapshot.AppointmentAuthorityVersion,
		&snapshot.AppointmentPOSProvider, &snapshot.AppointmentPOSID,
		&snapshot.AppointmentPOSVersion, &snapshot.AppointmentStatus,
		&snapshot.AttemptSegmentCount,
		&snapshot.InvalidAttemptSegmentCount, &snapshot.AppointmentSegmentCount,
		&snapshot.InvalidAppointmentSegmentCount, &snapshot.SegmentsMatch,
	}
	if withExpectedKey {
		destinations = append(destinations, &expectedKey)
	}
	err := scanner.Scan(destinations...)
	return snapshot, expectedKey, err
}

func validateExternalEvidence(session Session, snapshot externalEvidenceSnapshot, expectedOperationKey string, split bool) (*SchedulingResultEvidence, bool) {
	resultStatus, _, expectedOutcome, ok := operationEvidenceContract(snapshot.OperationType)
	if !ok || snapshot.AttemptStatus != resultStatus || session.Outcome != expectedOutcome ||
		normalizedBookingAction(session) != snapshot.OperationType || snapshot.OperationKey != expectedOperationKey ||
		snapshot.AttemptAuthority != booking.SchedulingAuthorityExternalProvider ||
		snapshot.AppointmentAuthority != booking.SchedulingAuthorityExternalProvider ||
		snapshot.AttemptAuthorityProvider == "" || snapshot.AttemptAuthorityID == "" ||
		snapshot.AttemptAuthorityVersion < 0 || snapshot.AttemptPOSProvider == "" ||
		snapshot.AttemptPOSID == "" || snapshot.AttemptPOSVersion < 0 ||
		snapshot.AppointmentAuthorityProvider == "" || snapshot.AppointmentAuthorityID == "" ||
		snapshot.AppointmentAuthorityVersion < snapshot.AttemptAuthorityVersion ||
		snapshot.AppointmentPOSProvider == "" || snapshot.AppointmentPOSID == "" ||
		snapshot.AppointmentPOSVersion != snapshot.AppointmentAuthorityVersion ||
		snapshot.AttemptAuthorityProvider != snapshot.AttemptPOSProvider ||
		snapshot.AttemptAuthorityID != snapshot.AttemptPOSID ||
		snapshot.AttemptAuthorityVersion != snapshot.AttemptPOSVersion ||
		snapshot.AppointmentAuthorityProvider != snapshot.AppointmentPOSProvider ||
		snapshot.AppointmentAuthorityID != snapshot.AppointmentPOSID ||
		snapshot.AttemptAuthorityProvider != snapshot.AppointmentAuthorityProvider ||
		snapshot.AttemptAuthorityID != snapshot.AppointmentAuthorityID ||
		snapshot.ProviderOutcome != booking.ProviderOutcomeSucceeded ||
		snapshot.RetryPolicy != booking.RetryPolicyNone ||
		snapshot.ReconciliationStatus != booking.ReconciliationNotRequired ||
		snapshot.ErrorCode != "" || snapshot.ErrorMessage != "" ||
		snapshot.InvalidAttemptSegmentCount != 0 || snapshot.InvalidAppointmentSegmentCount != 0 {
		return nil, false
	}
	resultChildren := snapshot.AttemptSegmentCount
	currentChildren := snapshot.AppointmentSegmentCount
	if resultChildren < 1 {
		return nil, false
	}
	if snapshot.AppointmentStatus == booking.StatusCancelled {
		currentChildren = 0
	} else if snapshot.AppointmentStatus != booking.StatusConfirmed && snapshot.AppointmentStatus != booking.StatusRescheduled || currentChildren < 1 {
		return nil, false
	}
	if snapshot.AppointmentAuthorityVersion == snapshot.AttemptAuthorityVersion && snapshot.AppointmentStatus != snapshot.AttemptStatus {
		return nil, false
	}
	switch snapshot.OperationType {
	case booking.BookingActionBook:
		if snapshot.TargetAppointmentID != "" || snapshot.TargetAuthorityVersion.Valid ||
			snapshot.AppointmentBookingAttemptID != snapshot.AttemptID {
			return nil, false
		}
	case booking.BookingActionReschedule:
		if snapshot.TargetAppointmentID != snapshot.AppointmentID || !snapshot.TargetAuthorityVersion.Valid ||
			snapshot.TargetAuthorityVersion.Int64 < 0 || snapshot.AttemptAuthorityVersion <= int(snapshot.TargetAuthorityVersion.Int64) {
			return nil, false
		}
	case booking.BookingActionCancel:
		if snapshot.TargetAppointmentID != snapshot.AppointmentID || !snapshot.TargetAuthorityVersion.Valid ||
			snapshot.TargetAuthorityVersion.Int64 < 0 || snapshot.AttemptAuthorityVersion <= int(snapshot.TargetAuthorityVersion.Int64) {
			return nil, false
		}
	}
	if snapshot.OperationType != booking.BookingActionCancel &&
		snapshot.AppointmentAuthorityVersion == snapshot.AttemptAuthorityVersion &&
		(!snapshot.SegmentsMatch || snapshot.AttemptSegmentCount != snapshot.AppointmentSegmentCount) {
		return nil, false
	}
	if split && (snapshot.OperationType != booking.BookingActionBook || resultChildren != 1) {
		return nil, false
	}
	return &SchedulingResultEvidence{
		Complete:                           true,
		Kind:                               SchedulingEvidenceKindCompletedOperation,
		SchedulingAuthority:                booking.SchedulingAuthorityExternalProvider,
		OperationType:                      snapshot.OperationType,
		ResultStatus:                       snapshot.AttemptStatus,
		CurrentStatus:                      snapshot.AppointmentStatus,
		IsCurrent:                          snapshot.AppointmentAuthorityVersion == snapshot.AttemptAuthorityVersion && snapshot.AppointmentStatus == snapshot.AttemptStatus,
		AppointmentID:                      snapshot.AppointmentID,
		BookingAttemptID:                   snapshot.AttemptID,
		AuthorityAppointmentVersion:        snapshot.AttemptAuthorityVersion,
		CurrentAuthorityAppointmentVersion: snapshot.AppointmentAuthorityVersion,
		RootCount:                          1,
		ResultChildCount:                   resultChildren,
		CurrentActiveChildCount:            currentChildren,
	}, true
}

func normalizedBookingAction(session Session) string {
	switch strings.TrimSpace(session.BookingAction) {
	case booking.BookingActionReschedule:
		return booking.BookingActionReschedule
	case booking.BookingActionCancel:
		return booking.BookingActionCancel
	default:
		return booking.BookingActionBook
	}
}

func operationEvidenceContract(operation string) (resultStatus string, eventType string, expectedOutcome string, ok bool) {
	switch operation {
	case booking.BookingActionBook:
		return booking.StatusConfirmed, "appointment_confirmed", OutcomeBookingConfirmed, true
	case booking.BookingActionReschedule:
		return booking.StatusRescheduled, "appointment_rescheduled", OutcomeBookingRescheduled, true
	case booking.BookingActionCancel:
		return booking.StatusCancelled, "appointment_cancelled", OutcomeBookingCancelled, true
	default:
		return "", "", "", false
	}
}
