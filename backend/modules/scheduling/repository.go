package scheduling

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/salon"
)

type salonSettingsReader interface {
	GetSettings(ctx context.Context, salonID string, ownerUserID string) (*salon.Settings, error)
}

// Repository resolves scheduling authority exclusively from owner-scoped salon
// settings. It intentionally has no environment or provider fallback.
type Repository struct {
	settings salonSettingsReader
	db       *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{settings: salon.NewRepository(db), db: db}
}

func (r *Repository) ResolveAvailabilityQuoteSchedulingAuthority(ctx context.Context, salonID string, ownerUserID string, quoteID string) (string, error) {
	var authority string
	err := r.db.QueryRowContext(ctx, `
		SELECT quote.scheduling_authority
		FROM availability_quotes quote
		JOIN salons salon ON salon.id = quote.salon_id
		WHERE quote.salon_id = $1
		  AND salon.owner_user_id = $2
		  AND quote.id::text = $3
	`, salonID, ownerUserID, strings.TrimSpace(quoteID)).Scan(&authority)
	if errors.Is(err, sql.ErrNoRows) {
		return "", booking.ErrAvailabilityQuoteStale
	}
	if err != nil {
		return "", err
	}
	return authority, nil
}

func (r *Repository) FindOperationSchedulingAuthority(ctx context.Context, salonID string, ownerUserID string, operationKey string) (string, bool, error) {
	origin, found, err := r.FindOperationSchedulingOrigin(ctx, salonID, ownerUserID, operationKey)
	return origin.SchedulingAuthority, found, err
}

func (r *Repository) FindOperationSchedulingOrigin(ctx context.Context, salonID string, ownerUserID string, operationKey string) (PersistedOperationOrigin, bool, error) {
	operationKey = strings.TrimSpace(operationKey)
	if operationKey == "" {
		return PersistedOperationOrigin{}, false, nil
	}
	rows, err := r.db.QueryContext(ctx, `
			SELECT 'booking_attempt', attempt.scheduling_authority, NULL::text
			FROM booking_attempts attempt
			JOIN salons salon ON salon.id = attempt.salon_id
			WHERE attempt.salon_id = $1
			  AND salon.owner_user_id = $2
			  AND attempt.operation_key = $3
			UNION ALL
			SELECT 'scheduling_request', request.scheduling_authority, request.target_scheduling_authority
			FROM scheduling_requests request
			JOIN salons salon ON salon.id = request.salon_id
			WHERE request.salon_id = $1
			  AND salon.owner_user_id = $2
			  AND request.operation_key = $3
	`, salonID, ownerUserID, operationKey)
	if err != nil {
		return PersistedOperationOrigin{}, false, err
	}
	defer rows.Close()
	var origin PersistedOperationOrigin
	found := false
	for rows.Next() {
		var source string
		var authority string
		var target sql.NullString
		if err := rows.Scan(&source, &authority, &target); err != nil {
			return PersistedOperationOrigin{}, false, err
		}
		if found && origin.SchedulingAuthority != authority {
			return PersistedOperationOrigin{}, false, booking.ErrOperationConflict
		}
		found = true
		origin.SchedulingAuthority = authority
		if source == "scheduling_request" {
			if origin.SchedulingRequest && (origin.RequestTargetAuthorityPresent != target.Valid || (target.Valid && origin.RequestTargetAuthority != target.String)) {
				return PersistedOperationOrigin{}, false, booking.ErrOperationConflict
			}
			origin.SchedulingRequest = true
			origin.RequestTargetAuthorityPresent = target.Valid
			if target.Valid {
				origin.RequestTargetAuthority = target.String
			}
		}
	}
	if err := rows.Err(); err != nil {
		return PersistedOperationOrigin{}, false, err
	}
	return origin, found, nil
}

func (r *Repository) ResolveAttemptSchedulingAuthority(ctx context.Context, salonID string, ownerUserID string, attemptID string) (string, error) {
	var authority string
	err := r.db.QueryRowContext(ctx, `
		SELECT attempt.scheduling_authority
		FROM booking_attempts attempt
		JOIN salons salon ON salon.id = attempt.salon_id
		WHERE attempt.salon_id = $1
		  AND salon.owner_user_id = $2
		  AND attempt.id::text = $3
	`, salonID, ownerUserID, strings.TrimSpace(attemptID)).Scan(&authority)
	if errors.Is(err, sql.ErrNoRows) {
		return "", pos.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return authority, nil
}

func (r *Repository) ResolveAvailabilityRetrySchedulingAuthority(ctx context.Context, salonID string, ownerUserID string, attemptID string) (string, error) {
	var authority string
	err := r.db.QueryRowContext(ctx, `
		SELECT attempt.scheduling_authority
		FROM booking_attempts attempt
		JOIN salons salon ON salon.id = attempt.salon_id
		WHERE attempt.salon_id = $1
		  AND salon.owner_user_id = $2
		  AND attempt.id::text = $3
		  AND attempt.scheduling_authority = 'external_provider'
		  AND COALESCE(attempt.operation_type, 'book') = 'book'
		  AND attempt.status = 'fallback_pending'
		  AND attempt.retry_policy = 'safe'
		  AND attempt.superseded_at IS NULL
	`, salonID, ownerUserID, strings.TrimSpace(attemptID)).Scan(&authority)
	if errors.Is(err, sql.ErrNoRows) {
		return "", booking.ErrOperationConflict
	}
	return authority, err
}

func (r *Repository) ResolveAppointmentSchedulingAuthority(ctx context.Context, salonID string, ownerUserID string, appointmentID string) (string, error) {
	var authority string
	err := r.db.QueryRowContext(ctx, `
		SELECT appointment.scheduling_authority
		FROM appointments appointment
		JOIN salons salon ON salon.id = appointment.salon_id
		WHERE appointment.salon_id = $1
		  AND salon.owner_user_id = $2
		  AND appointment.id::text = $3
	`, salonID, ownerUserID, strings.TrimSpace(appointmentID)).Scan(&authority)
	if errors.Is(err, sql.ErrNoRows) {
		return "", pos.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return authority, nil
}

func newRepository(settings salonSettingsReader) *Repository {
	return &Repository{settings: settings}
}

func (r *Repository) ResolveSchedulingAuthority(ctx context.Context, salonID string, ownerUserID string) (string, error) {
	settings, err := r.settings.GetSettings(ctx, salonID, ownerUserID)
	if errors.Is(err, salon.ErrNotFound) {
		return "", pos.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return settings.SchedulingAuthority, nil
}

func (r *Repository) ResolveConversationSchedulingPolicy(ctx context.Context, salonID string, ownerUserID string) (ConversationPolicyFence, error) {
	settings, err := r.settings.GetSettings(ctx, salonID, ownerUserID)
	if errors.Is(err, salon.ErrNotFound) {
		return ConversationPolicyFence{}, pos.ErrNotFound
	}
	if err != nil {
		return ConversationPolicyFence{}, err
	}
	return ConversationPolicyFence{
		BookingMode:         BookingMode(strings.TrimSpace(settings.BookingMode)),
		SchedulingAuthority: strings.TrimSpace(settings.SchedulingAuthority),
	}, nil
}
