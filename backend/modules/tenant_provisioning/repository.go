package tenant_provisioning

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/salon"
	registration "github.com/manleai/ai-receptionist/modules/tenant_registration"
)

type Repository struct {
	db     *sql.DB
	salons *salon.Service
	now    func() time.Time
}

func NewRepository(db *sql.DB, salonService *salon.Service) *Repository {
	return &Repository{db: db, salons: salonService, now: func() time.Time { return time.Now().UTC() }}
}

func (r *Repository) Provision(ctx context.Context, actorUserID, requestID, requestFingerprint string, req ProvisionRequest) (*ProvisionResult, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status registration.Status
	var version int64
	var convertedSalonID string
	err = tx.QueryRowContext(ctx, `SELECT status,version,COALESCE(converted_salon_id::text,'') FROM tenant_registration_requests WHERE id=$1 FOR UPDATE`, requestID).Scan(&status, &version, &convertedSalonID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if replay, found, err := loadProvisionReplay(ctx, tx, requestID, req.ActionKey, requestFingerprint); err != nil {
		return nil, err
	} else if found {
		replay.Replayed = true
		return replay, tx.Commit()
	}
	if status != registration.StatusQualified && status != registration.StatusSetupInProgress {
		return nil, ErrStatusConflict
	}
	if version != req.ExpectedVersion {
		return nil, ErrVersionConflict
	}
	ownerUserID, err := r.resolveOwner(ctx, tx, req.Owner)
	if err != nil {
		return nil, err
	}
	created, err := r.salons.CreateInTx(ctx, tx, ownerUserID, salon.CreateSalonRequest{
		OperationKey: "registration-provision:" + requestID, SchedulingAuthority: booking.SchedulingAuthorityOwnerManual,
		Name: req.Salon.Name, Phone: req.Salon.Phone, Address: req.Salon.Address, City: req.Salon.City, State: req.Salon.State, ZipCode: req.Salon.ZipCode,
		Timezone: req.Salon.Timezone, PrimaryLanguage: req.Salon.PrimaryLanguage, SecondaryLanguage: req.Salon.SecondaryLanguage, HandoffPhone: req.Salon.HandoffPhone,
	})
	if err != nil {
		return nil, classifyError(err)
	}
	nextVersion := version + 1
	if _, err := tx.ExecContext(ctx, `UPDATE tenant_registration_requests SET status='converted',version=$1,converted_salon_id=$2,converted_at=now(),terminal_at=now(),retention_expires_at=now()+interval '180 days',updated_at=now() WHERE id=$3`, nextVersion, created.ID, requestID); err != nil {
		return nil, classifyError(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tenant_registration_request_events(request_id,actor_user_id,event_type,from_status,to_status,request_version,details) VALUES($1,$2,'tenant_provisioned',$3,'converted',$4,jsonb_build_object('salon_id',$5::text,'owner_mode',$6))`, requestID, actorUserID, string(status), nextVersion, created.ID, req.Owner.Mode); err != nil {
		return nil, err
	}
	result := &ProvisionResult{RequestID: requestID, SalonID: created.ID, OwnerUserID: ownerUserID, RequestVersion: nextVersion, SchedulingAuthority: booking.SchedulingAuthorityOwnerManual}
	if err := storeSafeAction(ctx, tx, requestID, actorUserID, req.ActionKey, "tenant_provisioned", requestFingerprint, result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) resolveOwner(ctx context.Context, tx *sql.Tx, input OwnerIdentityInput) (string, error) {
	switch input.Mode {
	case OwnerModeCreateInvited:
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE lower(email)=lower($1))`, input.Email).Scan(&exists); err != nil {
			return "", err
		}
		if exists {
			return "", ErrIdentityConflict
		}
		var userID string
		err := tx.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,full_name,phone,status,principal_scope) VALUES($1,'!manleai-invited',$2,NULLIF($3,''),'invited','tenant') RETURNING id::text`, input.Email, input.FullName, input.Phone).Scan(&userID)
		if err != nil {
			return "", classifyError(err)
		}
		return userID, nil
	case OwnerModeUseExisting:
		var userID string
		err := tx.QueryRowContext(ctx, `SELECT id::text FROM users WHERE id=$1 AND lower(email)=lower($2) AND principal_scope='tenant' AND status='active'`, input.UserID, input.Email).Scan(&userID)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrIdentityConflict
		}
		return userID, err
	default:
		return "", ErrValidation
	}
}

func (r *Repository) CreateInvitation(ctx context.Context, actorUserID, requestID, requestFingerprint, tokenHash, rawToken string, req InvitationRequest) (*InvitationResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status registration.Status
	var version int64
	var salonID string
	err = tx.QueryRowContext(ctx, `SELECT status,version,COALESCE(converted_salon_id::text,'') FROM tenant_registration_requests WHERE id=$1 FOR UPDATE`, requestID).Scan(&status, &version, &salonID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if replay, found, err := loadInvitationReplay(ctx, tx, requestID, req.ActionKey, requestFingerprint); err != nil {
		return nil, err
	} else if found {
		replay.Replayed = true
		replay.RawToken = ""
		replay.TokenAvailable = false
		return replay, tx.Commit()
	}
	if status != registration.StatusConverted || salonID == "" {
		return nil, ErrInvitationUnavailable
	}
	if version != req.ExpectedVersion {
		return nil, ErrVersionConflict
	}
	var ownerUserID, userStatus string
	err = tx.QueryRowContext(ctx, `SELECT account.id::text,account.status FROM salons salon JOIN users account ON account.id=salon.owner_user_id WHERE salon.id=$1 AND account.principal_scope='tenant' FOR UPDATE OF account`, salonID).Scan(&ownerUserID, &userStatus)
	if err != nil {
		return nil, err
	}
	if userStatus != "invited" {
		return nil, ErrInvitationUnavailable
	}
	var activeID string
	activeErr := tx.QueryRowContext(ctx, `SELECT id::text FROM tenant_owner_invitations WHERE user_id=$1 AND status='active' FOR UPDATE`, ownerUserID).Scan(&activeID)
	if activeErr == nil && !req.Rotate {
		return nil, ErrInvitationUnavailable
	}
	if activeErr != nil && !errors.Is(activeErr, sql.ErrNoRows) {
		return nil, activeErr
	}
	if activeErr == nil {
		if _, err := tx.ExecContext(ctx, `UPDATE tenant_owner_invitations SET status='revoked',revoked_at=now(),updated_at=now() WHERE id=$1`, activeID); err != nil {
			return nil, err
		}
	}
	expiresAt := r.now().Add(InvitationTTL)
	var invitationID string
	err = tx.QueryRowContext(ctx, `INSERT INTO tenant_owner_invitations(request_id,salon_id,user_id,token_hash,expires_at,created_by_user_id) VALUES($1,$2,$3,$4,$5,$6) RETURNING id::text`, requestID, salonID, ownerUserID, tokenHash, expiresAt, actorUserID).Scan(&invitationID)
	if err != nil {
		return nil, classifyError(err)
	}
	nextVersion := version + 1
	if _, err := tx.ExecContext(ctx, `UPDATE tenant_registration_requests SET version=$1,updated_at=now() WHERE id=$2`, nextVersion, requestID); err != nil {
		return nil, err
	}
	eventType := "owner_invitation_created"
	if activeErr == nil {
		eventType = "owner_invitation_rotated"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tenant_registration_request_events(request_id,actor_user_id,event_type,from_status,to_status,request_version,details) VALUES($1,$2,$3,'converted','converted',$4,jsonb_build_object('invitation_id',$5::text,'expires_at',$6::timestamptz))`, requestID, actorUserID, eventType, nextVersion, invitationID, expiresAt); err != nil {
		return nil, err
	}
	safe := &InvitationResult{RequestID: requestID, InvitationID: invitationID, RequestVersion: nextVersion, ExpiresAt: expiresAt, TokenAvailable: false}
	if err := storeSafeAction(ctx, tx, requestID, actorUserID, req.ActionKey, eventType, requestFingerprint, safe); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	safe.RawToken = rawToken
	safe.TokenAvailable = true
	return safe, nil
}

func (r *Repository) AcceptInvitation(ctx context.Context, tokenHash, passwordHash string) error {
	var userID string
	err := r.db.QueryRowContext(ctx, `SELECT user_id::text FROM public.accept_tenant_owner_invitation($1,$2)`, tokenHash, passwordHash).Scan(&userID)
	if err != nil {
		if postgresMessage(err) == "OWNER_INVITATION_INVALID" {
			return ErrInvitationInvalid
		}
		return err
	}
	if strings.TrimSpace(userID) == "" {
		return ErrInvitationInvalid
	}
	return nil
}

func (r *Repository) SearchTenantIdentities(ctx context.Context, query string, limit int) ([]TenantIdentity, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text,email,full_name,status
		FROM users
		WHERE principal_scope='tenant'
		  AND status='active'
		  AND (position(lower($1) in lower(email))>0 OR position(lower($1) in lower(full_name))>0)
		ORDER BY full_name,email,id
		LIMIT $2
	`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TenantIdentity, 0)
	for rows.Next() {
		var item TenantIdentity
		if err := rows.Scan(&item.ID, &item.Email, &item.FullName, &item.Status); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func storeSafeAction(ctx context.Context, tx *sql.Tx, requestID, actorID, key, actionType, fp string, result any) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tenant_registration_request_actions(request_id,actor_user_id,action_key,action_type,request_fingerprint,result_snapshot) VALUES($1,$2,$3,$4,$5,$6)`, requestID, actorID, key, actionType, fp, raw)
	return classifyError(err)
}
func loadProvisionReplay(ctx context.Context, tx *sql.Tx, requestID, key, fp string) (*ProvisionResult, bool, error) {
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
	var result ProvisionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, false, err
	}
	return &result, true, nil
}
func loadInvitationReplay(ctx context.Context, tx *sql.Tx, requestID, key, fp string) (*InvitationResult, bool, error) {
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
	var result InvitationResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, false, err
	}
	return &result, true, nil
}
func postgresMessage(err error) string {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Message
	}
	return ""
}
func classifyError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, salon.ErrCreateOperationConflict) {
		return ErrActionConflict
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		if pqErr.Code == "23505" && strings.Contains(pqErr.Constraint, "users_email") {
			return ErrIdentityConflict
		}
		if pqErr.Constraint == "tenant_registration_request_actions_request_id_action_key_key" {
			return ErrActionConflict
		}
	}
	return err
}
