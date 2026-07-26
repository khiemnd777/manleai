package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/lib/pq"
)

var (
	ErrNotFound             = errors.New("auth record not found")
	ErrBootstrapRoleMissing = errors.New("salon owner role not found")
)

const refreshRotationReplayGrace = 5 * time.Second

type CreateFirstOwnerParams struct {
	Email        string
	PasswordHash string
	FullName     string
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) BootstrapAvailable(ctx context.Context) (bool, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return false, err
	}
	return count == 0, nil
}

func (r *Repository) CreateFirstOwner(ctx context.Context, params CreateFirstOwnerParams) (*User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `LOCK TABLE users IN EXCLUSIVE MODE`); err != nil {
		return nil, err
	}

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, ErrBootstrapClosed
	}

	row := tx.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, full_name, status)
		VALUES ($1, $2, $3, 'active')
		RETURNING id::text, email, password_hash, full_name, COALESCE(phone, ''), status, created_at, updated_at
	`, params.Email, params.PasswordHash, params.FullName)
	user, err := scanUser(row)
	if err != nil {
		return nil, err
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO user_roles (user_id, role_id)
		SELECT $1, id
		FROM roles
		WHERE name = 'salon_owner'
	`, user.ID)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrBootstrapRoleMissing
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return user, nil
}

func (r *Repository) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text, email, password_hash, full_name, COALESCE(phone, ''), status, created_at, updated_at
		FROM users
		WHERE lower(email) = lower($1)
	`, email)
	return scanUser(row)
}

func (r *Repository) FindUserByID(ctx context.Context, id string) (*User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text, email, password_hash, full_name, COALESCE(phone, ''), status, created_at, updated_at
		FROM users
		WHERE id = $1
	`, id)
	return scanUser(row)
}

func (r *Repository) RolesForUser(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT roles.name
		FROM roles
		JOIN user_roles ON user_roles.role_id = roles.id
		WHERE user_roles.user_id = $1
		ORDER BY roles.name
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (r *Repository) PrimarySalonIDForUser(ctx context.Context, userID string) (string, error) {
	var salonID string
	err := r.db.QueryRowContext(ctx, `
		SELECT id::text
		FROM salons
		WHERE owner_user_id = $1
		ORDER BY created_at
		LIMIT 1
	`, userID).Scan(&salonID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return salonID, err
}

// ResolveAccessPrincipal returns the current server-owned principal for an
// active user. The signed access token proves identity only; tenant and role
// assignments are reloaded from PostgreSQL for every protected request.
func (r *Repository) ResolveAccessPrincipal(ctx context.Context, userID string) (string, string, []string, error) {
	var resolvedUserID string
	var salonID string
	var roles []string
	err := r.db.QueryRowContext(ctx, `
		SELECT account.id::text,
		       COALESCE((
		           SELECT salon.id::text
		           FROM salons AS salon
		           WHERE salon.owner_user_id = account.id
		           ORDER BY salon.created_at, salon.id
		           LIMIT 1
		       ), ''),
		       ARRAY(
		           SELECT role.name
		           FROM user_roles AS assignment
		           JOIN roles AS role ON role.id = assignment.role_id
		           WHERE assignment.user_id = account.id
		           ORDER BY role.name
		       )
		FROM users AS account
		WHERE account.id = $1
		  AND account.status = 'active'
	`, userID).Scan(&resolvedUserID, &salonID, pq.Array(&roles))
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil, ErrNotFound
	}
	if err != nil {
		return "", "", nil, err
	}
	return resolvedUserID, salonID, roles, nil
}

func (r *Repository) StoreRefreshToken(ctx context.Context, userID string, token string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, userID, hashToken(token), expiresAt)
	return err
}

func (r *Repository) RotateRefreshToken(ctx context.Context, currentToken string, replacementToken string, replacementExpiresAt time.Time) (*User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var tokenID string
	var revokedAt sql.NullTime
	var replayEligible bool
	var user User
	err = tx.QueryRowContext(ctx, `
		SELECT token.id::text,
		       account.id::text, account.email, account.password_hash, account.full_name,
		       COALESCE(account.phone, ''), account.status, account.created_at, account.updated_at,
		       token.revoked_at,
		       token.revoked_at IS NOT NULL
		         AND token.revoked_at >= now() - make_interval(secs => $2)
		FROM refresh_tokens AS token
		JOIN users AS account ON account.id = token.user_id
		WHERE token.token_hash = $1
		  AND token.expires_at > now()
		FOR UPDATE OF token, account
	`, hashToken(currentToken), int(refreshRotationReplayGrace/time.Second)).Scan(
		&tokenID,
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.FullName,
		&user.Phone,
		&user.Status,
		&user.CreatedAt,
		&user.UpdatedAt,
		&revokedAt,
		&replayEligible,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if revokedAt.Valid {
		if !replayEligible || user.Status != "active" {
			return nil, ErrNotFound
		}
		var successorExists bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM refresh_tokens AS successor
				WHERE successor.user_id = $1
				  AND successor.token_hash = $2
				  AND successor.revoked_at IS NULL
				  AND successor.expires_at > now()
			)
		`, user.ID, hashToken(replacementToken)).Scan(&successorExists); err != nil {
			return nil, err
		}
		if !successorExists {
			return nil, ErrNotFound
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &user, nil
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = now()
		WHERE id = $1
		  AND revoked_at IS NULL
		  AND expires_at > now()
	`, tokenID)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		return nil, ErrNotFound
	}

	if user.Status != "active" {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, ErrDisabledUser
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, user.ID, hashToken(replacementToken), replacementExpiresAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *Repository) RevokeRefreshToken(ctx context.Context, token string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = now()
		WHERE token_hash = $1
	`, hashToken(token))
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (*User, error) {
	var user User
	err := row.Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.FullName,
		&user.Phone,
		&user.Status,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
