package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

var (
	ErrNotFound             = errors.New("auth record not found")
	ErrBootstrapRoleMissing = errors.New("salon owner role not found")
)

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

func (r *Repository) StoreRefreshToken(ctx context.Context, userID string, token string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, userID, hashToken(token), expiresAt)
	return err
}

func (r *Repository) FindRefreshTokenUser(ctx context.Context, token string) (string, error) {
	var userID string
	err := r.db.QueryRowContext(ctx, `
		SELECT user_id::text
		FROM refresh_tokens
		WHERE token_hash = $1
		  AND revoked_at IS NULL
		  AND expires_at > now()
	`, hashToken(token)).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return userID, err
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
