package tenant_registration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lib/pq"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Submit(ctx context.Context, requestID, reference, payloadFingerprint string, req PublicSubmissionRequest) (*PublicSubmissionResponse, error) {
	var response PublicSubmissionResponse
	response.Status = "received"
	err := r.db.QueryRowContext(ctx, `
		SELECT public_reference, replayed
		FROM public.create_tenant_registration_request(
			$1::uuid,$2,$3::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,
			$17,$18,$19,$20,$21,$22,$23,$24,$25,$26
		)
	`, requestID, reference, req.SubmissionKey, payloadFingerprint,
		req.ContactFullName, req.ContactEmail, req.ContactEmailNormalized,
		req.ContactPhone, req.ContactPhoneNormalized, req.SalonName, req.SalonPhone,
		req.SalonPhoneNormalized, req.SalonWebsite, req.City, req.State, req.ZipCode,
		req.LocationCount, req.PreferredContactLanguage, req.CurrentBookingSystem,
		req.EstimatedWeeklyCallVolume, req.RequestedHelp, req.Notes, req.Locale,
		req.SourcePage, req.MarketingPlanInterest, req.ConsentVersion,
	).Scan(&response.RequestReference, &response.Replayed)
	if err != nil {
		if postgresMessage(err) == "TENANT_REGISTRATION_SUBMISSION_CONFLICT" {
			return nil, ErrSubmissionConflict
		}
		return nil, err
	}
	return &response, nil
}

func (r *Repository) List(ctx context.Context, filter ListFilter) ([]ListItem, map[Status]int64, bool, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT request.id::text, request.public_reference, request.status, request.version,
		       COALESCE(request.contact_full_name,''), COALESCE(request.contact_email,''),
		       COALESCE(request.contact_phone,''), COALESCE(request.salon_name,''),
		       COALESCE(request.salon_phone,''), COALESCE(request.city,''), COALESCE(request.state,''),
		       COALESCE(request.marketing_plan_interest,''),
		       COALESCE(request.assigned_to_user_id::text,''), COALESCE(assignee.full_name,''),
		       request.possible_duplicate, COALESCE(request.converted_salon_id::text,''),
		       request.created_at, request.updated_at, request.retention_expires_at, request.redacted_at
		FROM tenant_registration_requests request
		LEFT JOIN users assignee ON assignee.id = request.assigned_to_user_id
		WHERE ($1='' OR request.status=$1)
		  AND ($2='' OR position(lower($2) in lower(COALESCE(request.public_reference,'') || ' ' || COALESCE(request.contact_full_name,'') || ' ' || COALESCE(request.contact_email,'') || ' ' || COALESCE(request.contact_phone,'') || ' ' || COALESCE(request.salon_name,'') || ' ' || COALESCE(request.salon_phone,''))) > 0)
		  AND ($3='' OR ($3='unassigned' AND request.assigned_to_user_id IS NULL) OR request.assigned_to_user_id::text=$3)
		  AND ($4::timestamptz IS NULL OR request.created_at >= $4)
		  AND ($5::timestamptz IS NULL OR request.created_at <= $5)
		ORDER BY request.created_at DESC, request.id
		LIMIT $6 OFFSET $7
	`, string(filter.Status), filter.Query, filter.AssignedTo, filter.CreatedFrom, filter.CreatedTo, filter.Limit+1, filter.Offset)
	if err != nil {
		return nil, nil, false, err
	}
	defer rows.Close()
	items := make([]ListItem, 0, filter.Limit)
	for rows.Next() {
		var item ListItem
		var email, contactPhone, salonPhone string
		if err := rows.Scan(&item.ID, &item.PublicReference, &item.Status, &item.Version,
			&item.ContactFullName, &email, &contactPhone, &item.SalonName, &salonPhone,
			&item.City, &item.State, &item.MarketingPlanInterest, &item.AssignedToUserID,
			&item.AssignedToName, &item.PossibleDuplicate, &item.ConvertedSalonID,
			&item.CreatedAt, &item.UpdatedAt, &item.RetentionExpiresAt, &item.RedactedAt); err != nil {
			return nil, nil, false, err
		}
		item.ContactEmailMasked, item.ContactPhoneMasked, item.SalonPhoneMasked = maskEmail(email), maskPhone(contactPhone), maskPhone(salonPhone)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, false, err
	}
	more := len(items) > filter.Limit
	if more {
		items = items[:filter.Limit]
	}
	counts, err := r.statusCounts(ctx)
	return items, counts, more, err
}

func (r *Repository) statusCounts(ctx context.Context) (map[Status]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT status, count(*) FROM tenant_registration_requests GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[Status]int64)
	for rows.Next() {
		var status Status
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	return counts, rows.Err()
}

func (r *Repository) Get(ctx context.Context, requestID string) (*Detail, error) {
	item, err := scanDetail(r.db.QueryRowContext(ctx, detailSelect, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	events, err := r.listEvents(ctx, requestID)
	if err != nil {
		return nil, err
	}
	notes, err := r.listNotes(ctx, requestID)
	if err != nil {
		return nil, err
	}
	item.Events, item.InternalNotes = events, notes
	return item, nil
}

func (r *Repository) Mutate(ctx context.Context, actorUserID, requestID, requestFingerprint string, req MutationRequest) (*MutationResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	current, err := scanDetail(tx.QueryRowContext(ctx, detailSelect+` FOR UPDATE`, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if replay, found, err := loadMutationReplay(ctx, tx, requestID, req.ActionKey, requestFingerprint); err != nil {
		return nil, err
	} else if found {
		replay.Replayed = true
		return replay, tx.Commit()
	}
	if terminalStatus(current.Status) {
		return nil, ErrTerminal
	}
	if current.Version != req.ExpectedVersion {
		return nil, ErrVersionConflict
	}
	nextStatus := current.Status
	if req.Status != nil {
		if *req.Status == StatusConverted || !CanTransition(current.Status, *req.Status) {
			return nil, ErrTransition
		}
		nextStatus = *req.Status
	}
	assignedTo := current.AssignedToUserID
	if req.AssignedToUserID != nil {
		assignedTo = *req.AssignedToUserID
	}
	if assignedTo != "" {
		var valid bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users account JOIN platform_role_assignments assignment ON assignment.user_id=account.id AND assignment.status='active' WHERE account.id=$1 AND account.status='active' AND account.principal_scope='platform')`, assignedTo).Scan(&valid); err != nil {
			return nil, err
		}
		if !valid {
			return nil, ErrValidation
		}
	}
	nextVersion := current.Version + 1
	terminal := terminalStatus(nextStatus)
	draftUpdated := req.ProvisioningDraft != nil
	draftJSON := []byte(`{}`)
	if draftUpdated {
		draftJSON, err = json.Marshal(req.ProvisioningDraft)
		if err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tenant_registration_requests
		SET status=$1, assigned_to_user_id=NULLIF($2,'')::uuid, version=$3,
		    terminal_at=CASE WHEN $4 THEN now() ELSE NULL END,
		    retention_expires_at=CASE WHEN $4 THEN now() + interval '180 days' ELSE NULL END,
		    provisioning_draft=CASE WHEN $5 THEN $6::jsonb ELSE provisioning_draft END,
		    provisioning_draft_updated_by_user_id=CASE WHEN $5 THEN $7::uuid ELSE provisioning_draft_updated_by_user_id END,
		    provisioning_draft_updated_at=CASE WHEN $5 THEN now() ELSE provisioning_draft_updated_at END,
		    updated_at=now()
		WHERE id=$8
	`, string(nextStatus), assignedTo, nextVersion, terminal, draftUpdated, draftJSON, actorUserID, requestID); err != nil {
		return nil, classifyConstraint(err)
	}
	eventType := "updated"
	if nextStatus != current.Status {
		eventType = "status_changed"
	}
	details, _ := json.Marshal(map[string]any{"assignment_changed": assignedTo != current.AssignedToUserID, "provisioning_draft_updated": draftUpdated})
	if _, err := tx.ExecContext(ctx, `INSERT INTO tenant_registration_request_events(request_id,actor_user_id,event_type,from_status,to_status,request_version,details) VALUES($1,$2,$3,$4,$5,$6,$7)`, requestID, actorUserID, eventType, string(current.Status), string(nextStatus), nextVersion, details); err != nil {
		return nil, err
	}
	result := &MutationResult{RequestID: requestID, Status: nextStatus, Version: nextVersion, AssignedToUserID: assignedTo}
	if err := storeAction(ctx, tx, requestID, actorUserID, req.ActionKey, "review_mutation", requestFingerprint, result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) AddNote(ctx context.Context, actorUserID, requestID, requestFingerprint string, req AddNoteRequest) (*AddNoteResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var currentStatus Status
	var currentVersion int64
	if err := tx.QueryRowContext(ctx, `SELECT status,version FROM tenant_registration_requests WHERE id=$1 FOR UPDATE`, requestID).Scan(&currentStatus, &currentVersion); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if replay, found, err := loadNoteReplay(ctx, tx, requestID, req.ActionKey, requestFingerprint); err != nil {
		return nil, err
	} else if found {
		replay.Replayed = true
		return replay, tx.Commit()
	}
	if terminalStatus(currentStatus) {
		return nil, ErrTerminal
	}
	if currentVersion != req.ExpectedVersion {
		return nil, ErrVersionConflict
	}
	nextVersion := currentVersion + 1
	var noteID string
	if err := tx.QueryRowContext(ctx, `INSERT INTO tenant_registration_request_notes(request_id,author_user_id,request_version,content) VALUES($1,$2,$3,$4) RETURNING id::text`, requestID, actorUserID, nextVersion, req.Content).Scan(&noteID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tenant_registration_requests SET version=$1,updated_at=now() WHERE id=$2`, nextVersion, requestID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tenant_registration_request_events(request_id,actor_user_id,event_type,from_status,to_status,request_version,details) VALUES($1,$2,'note_added',$3,$3,$4,'{}')`, requestID, actorUserID, string(currentStatus), nextVersion); err != nil {
		return nil, err
	}
	result := &AddNoteResult{RequestID: requestID, NoteID: noteID, Version: nextVersion}
	if err := storeAction(ctx, tx, requestID, actorUserID, req.ActionKey, "note_added", requestFingerprint, result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) RedactDue(ctx context.Context, limit int) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT public.redact_due_tenant_registration_requests($1)`, limit).Scan(&count)
	return count, err
}

const detailSelect = `
	SELECT request.id::text,request.public_reference,request.submission_key::text,request.status,request.version,
	       COALESCE(request.contact_full_name,''),COALESCE(request.contact_email,''),COALESCE(request.contact_phone,''),
	       COALESCE(request.salon_name,''),COALESCE(request.salon_phone,''),COALESCE(request.salon_website,''),
	       COALESCE(request.city,''),COALESCE(request.state,''),COALESCE(request.zip_code,''),COALESCE(request.location_count,0),
	       COALESCE(request.preferred_contact_language,''),COALESCE(request.current_booking_system,''),
	       COALESCE(request.estimated_weekly_call_volume,''),COALESCE(request.requested_help,''),COALESCE(request.notes,''),
	       request.locale,request.source_page,COALESCE(request.marketing_plan_interest,''),request.consent_version,request.consent_at,
	       COALESCE(request.assigned_to_user_id::text,''),COALESCE(assignee.full_name,''),request.possible_duplicate,
	       COALESCE(request.converted_salon_id::text,''),request.converted_at,request.terminal_at,
	       request.retention_expires_at,request.redacted_at,request.provisioning_draft,
	       COALESCE(request.provisioning_draft_updated_by_user_id::text,''),request.provisioning_draft_updated_at,
	       request.created_at,request.updated_at
	FROM tenant_registration_requests request LEFT JOIN users assignee ON assignee.id=request.assigned_to_user_id WHERE request.id=$1`

type rowScanner interface{ Scan(...any) error }

func scanDetail(row rowScanner) (*Detail, error) {
	var item Detail
	var draftJSON []byte
	err := row.Scan(&item.ID, &item.PublicReference, &item.SubmissionKey, &item.Status, &item.Version, &item.ContactFullName, &item.ContactEmail, &item.ContactPhone, &item.SalonName, &item.SalonPhone, &item.SalonWebsite, &item.City, &item.State, &item.ZipCode, &item.LocationCount, &item.PreferredContactLanguage, &item.CurrentBookingSystem, &item.EstimatedWeeklyCallVolume, &item.RequestedHelp, &item.ApplicantNotes, &item.Locale, &item.SourcePage, &item.MarketingPlanInterest, &item.ConsentVersion, &item.ConsentAt, &item.AssignedToUserID, &item.AssignedToName, &item.PossibleDuplicate, &item.ConvertedSalonID, &item.ConvertedAt, &item.TerminalAt, &item.RetentionExpiresAt, &item.RedactedAt, &draftJSON, &item.ProvisioningDraftUpdatedByUserID, &item.ProvisioningDraftUpdatedAt, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	item.ContactEmailMasked = maskEmail(item.ContactEmail)
	item.ContactPhoneMasked = maskPhone(item.ContactPhone)
	item.SalonPhoneMasked = maskPhone(item.SalonPhone)
	if string(draftJSON) != "{}" {
		var draft ProvisioningDraft
		if err := json.Unmarshal(draftJSON, &draft); err != nil {
			return nil, err
		}
		item.ProvisioningDraft = &draft
	}
	return &item, nil
}

func (r *Repository) listEvents(ctx context.Context, id string) ([]Event, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id::text,COALESCE(actor_user_id::text,''),event_type,COALESCE(from_status,''),COALESCE(to_status,''),request_version,details,created_at FROM tenant_registration_request_events WHERE request_id=$1 ORDER BY created_at,id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Event{}
	for rows.Next() {
		var item Event
		var raw []byte
		if err := rows.Scan(&item.ID, &item.ActorUserID, &item.EventType, &item.FromStatus, &item.ToStatus, &item.RequestVersion, &raw, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.Details); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (r *Repository) listNotes(ctx context.Context, id string) ([]Note, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT note.id::text,COALESCE(note.author_user_id::text,''),COALESCE(author.full_name,''),note.request_version,COALESCE(note.content,''),note.redacted_at,note.created_at FROM tenant_registration_request_notes note LEFT JOIN users author ON author.id=note.author_user_id WHERE note.request_id=$1 ORDER BY note.created_at,note.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Note{}
	for rows.Next() {
		var item Note
		if err := rows.Scan(&item.ID, &item.AuthorUserID, &item.AuthorName, &item.RequestVersion, &item.Content, &item.RedactedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func storeAction(ctx context.Context, tx *sql.Tx, requestID, actorID, key, actionType, fp string, result any) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tenant_registration_request_actions(request_id,actor_user_id,action_key,action_type,request_fingerprint,result_snapshot) VALUES($1,$2,$3,$4,$5,$6)`, requestID, actorID, key, actionType, fp, raw)
	return classifyConstraint(err)
}
func loadMutationReplay(ctx context.Context, tx *sql.Tx, requestID, key, fp string) (*MutationResult, bool, error) {
	var stored string
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT request_fingerprint,result_snapshot FROM tenant_registration_request_actions WHERE request_id=$1 AND action_key=$2`, requestID, key).Scan(&stored, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if stored != fp {
		return nil, false, ErrActionConflict
	}
	var result MutationResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, false, err
	}
	return &result, true, nil
}
func loadNoteReplay(ctx context.Context, tx *sql.Tx, requestID, key, fp string) (*AddNoteResult, bool, error) {
	var stored string
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT request_fingerprint,result_snapshot FROM tenant_registration_request_actions WHERE request_id=$1 AND action_key=$2`, requestID, key).Scan(&stored, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if stored != fp {
		return nil, false, ErrActionConflict
	}
	var result AddNoteResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, false, err
	}
	return &result, true, nil
}

func maskEmail(value string) string {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, "@")
	if len(parts) != 2 || parts[0] == "" {
		return ""
	}
	local := []rune(parts[0])
	visible := string(local[0])
	return visible + strings.Repeat("•", max(2, len(local)-1)) + "@" + parts[1]
}
func maskPhone(value string) string {
	digits := make([]rune, 0, 10)
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	if len(digits) < 4 {
		return ""
	}
	return "•••-•••-" + string(digits[len(digits)-4:])
}
func postgresMessage(err error) string {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Message
	}
	return ""
}
func classifyConstraint(err error) error {
	if err == nil {
		return nil
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		if pqErr.Constraint == "tenant_registration_assignee_platform_guard" {
			return ErrValidation
		}
		if pqErr.Constraint == "tenant_registration_request_actions_request_id_action_key_key" {
			return ErrActionConflict
		}
	}
	return err
}
