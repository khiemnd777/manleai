package booking

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/pos"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM salons WHERE id = $1 AND owner_user_id = $2)`, salonID, ownerUserID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return pos.ErrNotFound
	}
	return nil
}

func (r *Repository) GetBookableService(ctx context.Context, salonID string, serviceID string) (*ServiceRef, error) {
	var item ServiceRef
	err := r.db.QueryRowContext(ctx, `
		SELECT id::text, pos_provider, pos_service_id, COALESCE(pos_service_version, 0),
		       name, duration_minutes, COALESCE(price_from, 0)
		FROM services
		WHERE id = $1
		  AND salon_id = $2
		  AND active = true
		  AND ai_bookable = true
	`, serviceID, salonID).Scan(
		&item.ID,
		&item.POSProvider,
		&item.POSServiceID,
		&item.POSServiceVersion,
		&item.Name,
		&item.DurationMinutes,
		&item.PriceFrom,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pos.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) GetBookableStaff(ctx context.Context, salonID string, staffID string) (*StaffRef, error) {
	var item StaffRef
	err := r.db.QueryRowContext(ctx, `
		SELECT id::text, pos_provider, pos_staff_id, name
		FROM staff
		WHERE id = $1
		  AND salon_id = $2
		  AND active = true
		  AND ai_bookable = true
	`, staffID, salonID).Scan(&item.ID, &item.POSProvider, &item.POSStaffID, &item.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pos.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) ListBookableStaffRefs(ctx context.Context, salonID string) ([]StaffRef, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, pos_provider, pos_staff_id, name
		FROM staff
		WHERE salon_id = $1
		  AND active = true
		  AND ai_bookable = true
		ORDER BY name ASC
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]StaffRef, 0)
	for rows.Next() {
		var item StaffRef
		if err := rows.Scan(&item.ID, &item.POSProvider, &item.POSStaffID, &item.Name); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetSchedule(ctx context.Context, salonID string) (*Schedule, error) {
	var schedule Schedule
	if err := r.db.QueryRowContext(ctx, `
		SELECT timezone
		FROM salons
		WHERE id = $1
	`, salonID).Scan(&schedule.Timezone); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT day_of_week, COALESCE(open_time::text, ''), COALESCE(close_time::text, ''), is_closed
		FROM salon_business_hours
		WHERE salon_id = $1
		ORDER BY day_of_week ASC
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	schedule.BusinessHours = make([]BusinessHour, 0)
	for rows.Next() {
		var hour BusinessHour
		if err := rows.Scan(&hour.DayOfWeek, &hour.OpenTime, &hour.CloseTime, &hour.IsClosed); err != nil {
			return nil, err
		}
		schedule.BusinessHours = append(schedule.BusinessHours, hour)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &schedule, nil
}

func (r *Repository) CreatePendingBookingAttempt(ctx context.Context, record PendingBookingRecord) (*BookingAttempt, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	attempt := BookingAttempt{
		SalonID:            record.SalonID,
		Source:             record.Source,
		Status:             StatusPOSPending,
		POSProvider:        record.Provider,
		POSIdempotencyKey:  record.POSIdempotencyKey,
		CustomerName:       record.CustomerName,
		CustomerPhone:      record.CustomerPhone,
		CustomerEmail:      record.CustomerEmail,
		ServiceID:          record.Service.ID,
		StaffID:            record.Staff.ID,
		StaffSelectionMode: record.StaffSelectionMode,
		RequestedStartTime: record.StartTime,
		RequestedEndTime:   record.EndTime,
		Notes:              record.Notes,
	}
	if attempt.StaffSelectionMode == "" {
		attempt.StaffSelectionMode = StaffSelectionSpecific
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO booking_attempts (
			salon_id, source, status, pos_provider, pos_idempotency_key, customer_name, customer_phone,
			customer_email, service_id, staff_id, staff_selection_mode, requested_start_time, requested_end_time, notes
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, NULLIF($8, ''), $9, $10, $11, $12, $13, NULLIF($14, ''))
		RETURNING id::text, created_at, updated_at
	`, attempt.SalonID, attempt.Source, attempt.Status, attempt.POSProvider, attempt.POSIdempotencyKey, attempt.CustomerName, attempt.CustomerPhone, attempt.CustomerEmail, attempt.ServiceID, attempt.StaffID, attempt.StaffSelectionMode, attempt.RequestedStartTime, attempt.RequestedEndTime, attempt.Notes).Scan(&attempt.ID, &attempt.CreatedAt, &attempt.UpdatedAt); err != nil {
		return nil, err
	}
	if err := insertBookingAttemptSegments(ctx, tx, attempt.ID, record.Segments); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &attempt, nil
}

func (r *Repository) SaveConfirmedBooking(ctx context.Context, record ConfirmedBookingRecord) (*BookingAttempt, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	attempt := BookingAttempt{
		ID:                 record.AttemptID,
		SalonID:            record.SalonID,
		Source:             record.Source,
		Status:             StatusConfirmed,
		POSProvider:        record.Provider,
		POSBookingID:       record.POSBookingID,
		CustomerName:       record.CustomerName,
		CustomerPhone:      record.CustomerPhone,
		CustomerEmail:      record.CustomerEmail,
		ServiceID:          record.Service.ID,
		StaffID:            record.Staff.ID,
		StaffSelectionMode: record.StaffSelectionMode,
		RequestedStartTime: record.StartTime,
		RequestedEndTime:   record.EndTime,
		Notes:              record.Notes,
	}
	if attempt.StaffSelectionMode == "" {
		attempt.StaffSelectionMode = StaffSelectionSpecific
	}
	if err := tx.QueryRowContext(ctx, `
		UPDATE booking_attempts
		SET status = $1,
		    pos_booking_id = $2,
		    staff_selection_mode = $3,
		    requested_start_time = $4,
		    requested_end_time = $5,
		    notes = NULLIF($6, ''),
		    error_code = NULL,
		    error_message = NULL,
		    updated_at = now()
		WHERE id = $7
		  AND salon_id = $8
		  AND status = $9
		RETURNING created_at, updated_at
	`, attempt.Status, attempt.POSBookingID, attempt.StaffSelectionMode, attempt.RequestedStartTime, attempt.RequestedEndTime, attempt.Notes, attempt.ID, attempt.SalonID, StatusPOSPending).Scan(&attempt.CreatedAt, &attempt.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}

	appointment := Appointment{
		SalonID:               record.SalonID,
		BookingAttemptID:      attempt.ID,
		POSProvider:           record.Provider,
		POSAppointmentID:      record.POSBookingID,
		POSAppointmentVersion: record.POSBookingVersion,
		Status:                StatusConfirmed,
		CustomerName:          record.CustomerName,
		CustomerPhone:         record.CustomerPhone,
		CustomerEmail:         record.CustomerEmail,
		ServiceID:             record.Service.ID,
		StaffID:               record.Staff.ID,
		StaffSelectionMode:    attempt.StaffSelectionMode,
		StartTime:             record.StartTime,
		EndTime:               record.EndTime,
		Notes:                 record.Notes,
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO appointments (
			salon_id, booking_attempt_id, pos_provider, pos_appointment_id, pos_appointment_version, status, customer_name,
			customer_phone, customer_email, service_id, staff_id, staff_selection_mode, start_time, end_time, notes
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''), $10, $11, $12, $13, $14, NULLIF($15, ''))
		RETURNING id::text, created_at, updated_at
	`, appointment.SalonID, appointment.BookingAttemptID, appointment.POSProvider, appointment.POSAppointmentID, appointment.POSAppointmentVersion, appointment.Status, appointment.CustomerName, appointment.CustomerPhone, appointment.CustomerEmail, appointment.ServiceID, appointment.StaffID, appointment.StaffSelectionMode, appointment.StartTime, appointment.EndTime, appointment.Notes).Scan(&appointment.ID, &appointment.CreatedAt, &appointment.UpdatedAt); err != nil {
		return nil, err
	}

	segments := record.Segments
	if len(segments) == 0 {
		segments = []BookingSegmentRecord{{Service: record.Service, Staff: record.Staff, StaffSelectionMode: appointment.StaffSelectionMode, SortOrder: 1}}
	}
	if err := insertAppointmentServices(ctx, tx, appointment.ID, segments); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	attempt.Appointment = &appointment
	return &attempt, nil
}

func (r *Repository) SaveFallbackBooking(ctx context.Context, record FallbackBookingRecord) (*BookingAttempt, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	attempt := BookingAttempt{
		ID:                 record.AttemptID,
		SalonID:            record.SalonID,
		Source:             record.Source,
		Status:             StatusFallbackPending,
		POSProvider:        record.Provider,
		CustomerName:       record.CustomerName,
		CustomerPhone:      record.CustomerPhone,
		CustomerEmail:      record.CustomerEmail,
		ServiceID:          record.Service.ID,
		StaffID:            record.Staff.ID,
		StaffSelectionMode: record.StaffSelectionMode,
		RequestedStartTime: record.StartTime,
		RequestedEndTime:   record.EndTime,
		Notes:              record.Notes,
		ErrorCode:          record.ErrorCode,
		ErrorMessage:       record.ErrorMessage,
	}
	if attempt.StaffSelectionMode == "" {
		attempt.StaffSelectionMode = StaffSelectionSpecific
	}
	if err := tx.QueryRowContext(ctx, `
		UPDATE booking_attempts
		SET status = $1,
		    staff_selection_mode = $2,
		    requested_start_time = $3,
		    requested_end_time = $4,
		    notes = NULLIF($5, ''),
		    error_code = $6,
		    error_message = $7,
		    updated_at = now()
		WHERE id = $8
		  AND salon_id = $9
		  AND status = $10
		RETURNING created_at, updated_at
	`, attempt.Status, attempt.StaffSelectionMode, attempt.RequestedStartTime, attempt.RequestedEndTime, attempt.Notes, attempt.ErrorCode, attempt.ErrorMessage, attempt.ID, attempt.SalonID, StatusPOSPending).Scan(&attempt.CreatedAt, &attempt.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pos_errors (salon_id, provider, operation, error_code, error_message)
		VALUES ($1, $2, $3, $4, $5)
	`, record.SalonID, record.Provider, record.Operation, record.ErrorCode, record.ErrorMessage); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO owner_notifications (salon_id, booking_attempt_id, type, status, title, message)
		VALUES ($1, $2, $3, 'pending', $4, $5)
	`, record.SalonID, attempt.ID, NotificationTypeBookingFallback, "Booking request needs review", fallbackNotificationMessage(record)); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &attempt, nil
}

func (r *Repository) GetAppointmentForOwner(ctx context.Context, salonID string, ownerUserID string, appointmentID string) (*AppointmentActionRef, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT a.id::text, a.salon_id::text, a.booking_attempt_id::text, a.pos_provider,
		       a.pos_appointment_id, COALESCE(a.pos_appointment_version, 0), a.status,
		       a.customer_name, a.customer_phone, COALESCE(a.customer_email, ''),
		       COALESCE(a.start_time, now()), COALESCE(a.end_time, now()), COALESCE(a.notes, ''),
		       a.created_at, a.updated_at, COALESCE(a.staff_selection_mode, 'specific'),
		       COALESCE(s.id::text, ''), COALESCE(s.pos_provider, a.pos_provider),
		       COALESCE(s.pos_service_id, ''), COALESCE(s.pos_service_version, 0),
		       COALESCE(s.name, ''), COALESCE(s.duration_minutes, 0),
		       COALESCE(s.price_from, 0),
		       COALESCE(st.id::text, ''), COALESCE(st.pos_provider, a.pos_provider), COALESCE(st.pos_staff_id, ''),
		       COALESCE(st.name, '')
		FROM appointments a
		JOIN salons salon ON salon.id = a.salon_id
		LEFT JOIN services s ON s.id = a.service_id
		LEFT JOIN staff st ON st.id = a.staff_id
		WHERE a.id = $1
		  AND a.salon_id = $2
		  AND salon.owner_user_id = $3
	`, appointmentID, salonID, ownerUserID)

	var item AppointmentActionRef
	err := row.Scan(
		&item.ID,
		&item.SalonID,
		&item.BookingAttemptID,
		&item.POSProvider,
		&item.POSAppointmentID,
		&item.POSAppointmentVersion,
		&item.Status,
		&item.CustomerName,
		&item.CustomerPhone,
		&item.CustomerEmail,
		&item.StartTime,
		&item.EndTime,
		&item.Notes,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.StaffSelectionMode,
		&item.Service.ID,
		&item.Service.POSProvider,
		&item.Service.POSServiceID,
		&item.Service.POSServiceVersion,
		&item.Service.Name,
		&item.Service.DurationMinutes,
		&item.Service.PriceFrom,
		&item.Staff.ID,
		&item.Staff.POSProvider,
		&item.Staff.POSStaffID,
		&item.Staff.Name,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pos.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	segments, err := r.loadAppointmentActionSegments(ctx, item.ID, item.POSProvider)
	if err != nil {
		return nil, err
	}
	if len(segments) > 0 {
		item.Segments = segments
		item.Service = segments[0].Service
		item.Staff = segments[0].Staff
	}
	return &item, nil
}

func (r *Repository) loadAppointmentActionSegments(ctx context.Context, appointmentID string, provider string) ([]BookingSegmentRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT COALESCE(aps.service_id::text, ''),
		       COALESCE(s.pos_provider, $2),
		       COALESCE(aps.pos_service_id, s.pos_service_id, ''),
		       COALESCE(aps.pos_service_version, s.pos_service_version, 0),
		       aps.name,
		       COALESCE(aps.duration_minutes, 0),
		       COALESCE(aps.price_from, s.price_from, 0),
		       COALESCE(aps.staff_id::text, ''),
		       COALESCE(st.pos_provider, $2),
		       COALESCE(st.pos_staff_id, ''),
		       COALESCE(st.name, ''),
		       COALESCE(aps.staff_selection_mode, 'specific'),
		       COALESCE(aps.sort_order, 1)
		FROM appointment_services aps
		LEFT JOIN services s ON s.id = aps.service_id
		LEFT JOIN staff st ON st.id = aps.staff_id
		WHERE aps.appointment_id = $1
		ORDER BY aps.sort_order ASC, aps.created_at ASC
	`, appointmentID, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	segments := make([]BookingSegmentRecord, 0)
	for rows.Next() {
		var segment BookingSegmentRecord
		if err := rows.Scan(
			&segment.Service.ID,
			&segment.Service.POSProvider,
			&segment.Service.POSServiceID,
			&segment.Service.POSServiceVersion,
			&segment.Service.Name,
			&segment.Service.DurationMinutes,
			&segment.Service.PriceFrom,
			&segment.Staff.ID,
			&segment.Staff.POSProvider,
			&segment.Staff.POSStaffID,
			&segment.Staff.Name,
			&segment.StaffSelectionMode,
			&segment.SortOrder,
		); err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return segments, nil
}

func (r *Repository) CreatePendingAppointmentAction(ctx context.Context, record PendingAppointmentActionRecord) (*BookingAttempt, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	source := record.Source
	if source == "" {
		source = SourceOwnerDashboard
	}
	segments := record.Segments
	if len(segments) == 0 {
		segments = appointmentActionSegments(record.Appointment)
	}
	primary := segments[0]
	attempt := BookingAttempt{
		SalonID:            record.SalonID,
		Source:             source,
		Status:             StatusPOSPending,
		POSProvider:        record.Provider,
		POSBookingID:       record.Appointment.POSAppointmentID,
		POSIdempotencyKey:  record.POSIdempotencyKey,
		CustomerName:       record.Appointment.CustomerName,
		CustomerPhone:      record.Appointment.CustomerPhone,
		CustomerEmail:      record.Appointment.CustomerEmail,
		ServiceID:          primary.Service.ID,
		StaffID:            primary.Staff.ID,
		StaffSelectionMode: primary.StaffSelectionMode,
		RequestedStartTime: record.RequestedStartTime,
		RequestedEndTime:   record.RequestedEndTime,
		Notes:              record.Notes,
	}
	if attempt.StaffSelectionMode == "" {
		attempt.StaffSelectionMode = StaffSelectionSpecific
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO booking_attempts (
			salon_id, source, status, pos_provider, pos_booking_id, pos_idempotency_key, customer_name, customer_phone,
			customer_email, service_id, staff_id, staff_selection_mode, requested_start_time, requested_end_time, notes
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8, NULLIF($9, ''), $10, $11, $12, $13, $14, NULLIF($15, ''))
		RETURNING id::text, created_at, updated_at
	`, attempt.SalonID, attempt.Source, attempt.Status, attempt.POSProvider, attempt.POSBookingID, attempt.POSIdempotencyKey, attempt.CustomerName, attempt.CustomerPhone, attempt.CustomerEmail, attempt.ServiceID, attempt.StaffID, attempt.StaffSelectionMode, attempt.RequestedStartTime, attempt.RequestedEndTime, attempt.Notes).Scan(&attempt.ID, &attempt.CreatedAt, &attempt.UpdatedAt); err != nil {
		return nil, err
	}
	if err := insertBookingAttemptSegments(ctx, tx, attempt.ID, segments); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	attempt.Segments = bookingSegmentSnapshots(segments)
	return &attempt, nil
}

func (r *Repository) SaveRescheduledAppointment(ctx context.Context, record RescheduledAppointmentRecord) (*Appointment, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	source := record.Source
	if source == "" {
		source = SourceOwnerDashboard
	}
	segments := record.Segments
	if len(segments) == 0 {
		segments = appointmentActionSegments(record.Appointment)
		if record.Staff.ID != "" {
			segments = applyStaffToBookingSegments(segments, record.Staff)
		}
	}
	primary := segments[0]
	attempt := BookingAttempt{
		ID:                 record.AttemptID,
		SalonID:            record.Appointment.SalonID,
		Source:             source,
		Status:             StatusRescheduled,
		POSProvider:        record.Appointment.POSProvider,
		POSBookingID:       record.Appointment.POSAppointmentID,
		CustomerName:       record.Appointment.CustomerName,
		CustomerPhone:      record.Appointment.CustomerPhone,
		CustomerEmail:      record.Appointment.CustomerEmail,
		ServiceID:          primary.Service.ID,
		StaffID:            primary.Staff.ID,
		StaffSelectionMode: primary.StaffSelectionMode,
		RequestedStartTime: record.StartTime,
		RequestedEndTime:   record.EndTime,
		Notes:              record.Notes,
	}
	if attempt.StaffSelectionMode == "" {
		attempt.StaffSelectionMode = StaffSelectionSpecific
	}
	if err := tx.QueryRowContext(ctx, `
		UPDATE booking_attempts
		SET status = $1,
		    pos_booking_id = NULLIF($2, ''),
		    staff_id = $3,
		    staff_selection_mode = $4,
		    requested_start_time = $5,
		    requested_end_time = $6,
		    notes = NULLIF($7, ''),
		    error_code = NULL,
		    error_message = NULL,
		    updated_at = now()
		WHERE id = $8
		  AND salon_id = $9
		  AND status = $10
		RETURNING created_at, updated_at
	`, attempt.Status, attempt.POSBookingID, attempt.StaffID, attempt.StaffSelectionMode, attempt.RequestedStartTime, attempt.RequestedEndTime, attempt.Notes, attempt.ID, attempt.SalonID, StatusPOSPending).Scan(&attempt.CreatedAt, &attempt.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}

	appointment, err := scanAppointment(tx.QueryRowContext(ctx, `
		UPDATE appointments
		SET status = $1,
		    staff_id = $2,
		    staff_selection_mode = $3,
		    start_time = $4,
		    end_time = $5,
		    notes = NULLIF($6, ''),
		    pos_appointment_version = $7,
		    updated_at = now()
		WHERE id = $8
		  AND salon_id = $9
		RETURNING id::text, salon_id::text, booking_attempt_id::text, pos_provider, pos_appointment_id,
		          COALESCE(pos_appointment_version, 0), status, customer_name, customer_phone,
		          COALESCE(customer_email, ''), COALESCE(service_id::text, ''), COALESCE(staff_id::text, ''), COALESCE(staff_selection_mode, 'specific'),
		          start_time, end_time, COALESCE(notes, ''), created_at, updated_at
	`, StatusRescheduled, primary.Staff.ID, attempt.StaffSelectionMode, record.StartTime, record.EndTime, record.Notes, record.POSBookingVersion, record.Appointment.ID, record.Appointment.SalonID))
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM appointment_services WHERE appointment_id = $1`, record.Appointment.ID); err != nil {
		return nil, err
	}
	if err := insertAppointmentServices(ctx, tx, record.Appointment.ID, segments); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	appointment.Segments = bookingSegmentSnapshots(segments)
	return appointment, nil
}

func (r *Repository) SaveCancelledAppointment(ctx context.Context, record CancelledAppointmentRecord) (*Appointment, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	source := record.Source
	if source == "" {
		source = SourceOwnerDashboard
	}
	segments := appointmentActionSegments(record.Appointment)
	primary := segments[0]
	attempt := BookingAttempt{
		ID:                 record.AttemptID,
		SalonID:            record.Appointment.SalonID,
		Source:             source,
		Status:             StatusCancelled,
		POSProvider:        record.Appointment.POSProvider,
		POSBookingID:       record.Appointment.POSAppointmentID,
		CustomerName:       record.Appointment.CustomerName,
		CustomerPhone:      record.Appointment.CustomerPhone,
		CustomerEmail:      record.Appointment.CustomerEmail,
		ServiceID:          primary.Service.ID,
		StaffID:            primary.Staff.ID,
		StaffSelectionMode: primary.StaffSelectionMode,
		RequestedStartTime: record.Appointment.StartTime,
		RequestedEndTime:   record.Appointment.EndTime,
		Notes:              record.Reason,
	}
	if attempt.StaffSelectionMode == "" {
		attempt.StaffSelectionMode = StaffSelectionSpecific
	}
	if err := tx.QueryRowContext(ctx, `
		UPDATE booking_attempts
		SET status = $1,
		    pos_booking_id = NULLIF($2, ''),
		    staff_selection_mode = $3,
		    notes = NULLIF($4, ''),
		    error_code = NULL,
		    error_message = NULL,
		    updated_at = now()
		WHERE id = $5
		  AND salon_id = $6
		  AND status = $7
		RETURNING created_at, updated_at
	`, attempt.Status, attempt.POSBookingID, attempt.StaffSelectionMode, attempt.Notes, attempt.ID, attempt.SalonID, StatusPOSPending).Scan(&attempt.CreatedAt, &attempt.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}

	appointment, err := scanAppointment(tx.QueryRowContext(ctx, `
		UPDATE appointments
		SET status = $1,
		    pos_appointment_version = $2,
		    updated_at = now()
		WHERE id = $3
		  AND salon_id = $4
		RETURNING id::text, salon_id::text, booking_attempt_id::text, pos_provider, pos_appointment_id,
		          COALESCE(pos_appointment_version, 0), status, customer_name, customer_phone,
		          COALESCE(customer_email, ''), COALESCE(service_id::text, ''), COALESCE(staff_id::text, ''), COALESCE(staff_selection_mode, 'specific'),
		          start_time, end_time, COALESCE(notes, ''), created_at, updated_at
	`, StatusCancelled, record.POSBookingVersion, record.Appointment.ID, record.Appointment.SalonID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	appointment.Segments = bookingSegmentSnapshots(segments)
	return appointment, nil
}

func (r *Repository) SaveAppointmentActionFallback(ctx context.Context, record AppointmentActionFallbackRecord) (*BookingAttempt, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	source := record.Source
	if source == "" {
		source = SourceOwnerDashboard
	}
	segments := record.Segments
	if len(segments) == 0 {
		segments = appointmentActionSegments(record.Appointment)
	}
	primary := segments[0]
	attempt := BookingAttempt{
		ID:                 record.AttemptID,
		SalonID:            record.SalonID,
		Source:             source,
		Status:             StatusFallbackPending,
		POSProvider:        record.Provider,
		POSBookingID:       record.Appointment.POSAppointmentID,
		CustomerName:       record.Appointment.CustomerName,
		CustomerPhone:      record.Appointment.CustomerPhone,
		CustomerEmail:      record.Appointment.CustomerEmail,
		ServiceID:          primary.Service.ID,
		StaffID:            primary.Staff.ID,
		StaffSelectionMode: primary.StaffSelectionMode,
		RequestedStartTime: record.RequestedStartTime,
		RequestedEndTime:   record.RequestedEndTime,
		Notes:              record.Notes,
		ErrorCode:          record.ErrorCode,
		ErrorMessage:       record.ErrorMessage,
	}
	if attempt.StaffSelectionMode == "" {
		attempt.StaffSelectionMode = StaffSelectionSpecific
	}
	if err := tx.QueryRowContext(ctx, `
		UPDATE booking_attempts
		SET status = $1,
		    pos_booking_id = NULLIF($2, ''),
		    staff_selection_mode = $3,
		    requested_start_time = $4,
		    requested_end_time = $5,
		    notes = NULLIF($6, ''),
		    error_code = $7,
		    error_message = $8,
		    updated_at = now()
		WHERE id = $9
		  AND salon_id = $10
		  AND status = $11
		RETURNING created_at, updated_at
	`, attempt.Status, attempt.POSBookingID, attempt.StaffSelectionMode, attempt.RequestedStartTime, attempt.RequestedEndTime, attempt.Notes, attempt.ErrorCode, attempt.ErrorMessage, attempt.ID, attempt.SalonID, StatusPOSPending).Scan(&attempt.CreatedAt, &attempt.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pos.ErrNotFound
		}
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pos_errors (salon_id, provider, operation, error_code, error_message)
		VALUES ($1, $2, $3, $4, $5)
	`, record.SalonID, record.Provider, record.Operation, record.ErrorCode, record.ErrorMessage); err != nil {
		return nil, err
	}

	notificationType := record.NotificationType
	if notificationType == "" {
		notificationType = NotificationTypeBookingFallback
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO owner_notifications (salon_id, booking_attempt_id, appointment_id, type, status, title, message)
		VALUES ($1, $2, $3, $4, 'pending', $5, $6)
	`, record.SalonID, attempt.ID, record.Appointment.ID, notificationType, appointmentActionFallbackTitle(record), appointmentActionFallbackMessage(record)); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	attempt.Segments = bookingSegmentSnapshots(segments)
	return &attempt, nil
}

func (r *Repository) LatestTestBooking(ctx context.Context, salonID string, ownerUserID string) (*TestBookingRecord, error) {
	var item TestBookingRecord
	err := r.db.QueryRowContext(ctx, `
		SELECT ba.id::text, ba.status, COALESCE(ba.pos_booking_id, ''), ba.customer_name,
		       ba.customer_phone, COALESCE(ba.service_id::text, ''), COALESCE(ba.staff_id::text, ''),
		       ba.requested_start_time, ba.requested_end_time, COALESCE(ba.error_code, ''),
		       COALESCE(ba.error_message, ''), ba.created_at, ba.updated_at
		FROM booking_attempts ba
		JOIN salons s ON s.id = ba.salon_id
		WHERE ba.salon_id = $1
		  AND s.owner_user_id = $2
		  AND ba.source = $3
		ORDER BY ba.created_at DESC
		LIMIT 1
	`, salonID, ownerUserID, SourceSquareTestBooking).Scan(
		&item.BookingAttemptID,
		&item.Status,
		&item.POSBookingID,
		&item.CustomerName,
		&item.CustomerPhone,
		&item.ServiceID,
		&item.StaffID,
		&item.StartTime,
		&item.EndTime,
		&item.ErrorCode,
		&item.ErrorMessage,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pos.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if item.POSBookingID == "" {
		return &item, nil
	}
	var appointmentUpdatedAt time.Time
	err = r.db.QueryRowContext(ctx, `
		SELECT id::text, status, COALESCE(pos_appointment_version, 0), start_time, end_time, updated_at
		FROM appointments
		WHERE salon_id = $1
		  AND pos_provider = $2
		  AND pos_appointment_id = $3
	`, salonID, pos.ProviderSquare, item.POSBookingID).Scan(
		&item.AppointmentID,
		&item.AppointmentStatus,
		&item.POSAppointmentVersion,
		&item.StartTime,
		&item.EndTime,
		&appointmentUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return &item, nil
	}
	if err != nil {
		return nil, err
	}
	if appointmentUpdatedAt.After(item.UpdatedAt) {
		item.UpdatedAt = appointmentUpdatedAt
	}
	return &item, nil
}

func (r *Repository) ListAppointments(ctx context.Context, salonID string, ownerUserID string, limit int) ([]Appointment, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, salon_id::text, booking_attempt_id::text, pos_provider, pos_appointment_id,
		       COALESCE(pos_appointment_version, 0), status, customer_name, customer_phone, COALESCE(customer_email, ''), COALESCE(service_id::text, ''),
		       COALESCE(staff_id::text, ''), COALESCE(staff_selection_mode, 'specific'), start_time, end_time, COALESCE(notes, ''), created_at, updated_at
		FROM appointments
		WHERE salon_id = $1
		ORDER BY start_time DESC
		LIMIT $2
	`, salonID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Appointment, 0)
	for rows.Next() {
		var item Appointment
		if err := rows.Scan(
			&item.ID,
			&item.SalonID,
			&item.BookingAttemptID,
			&item.POSProvider,
			&item.POSAppointmentID,
			&item.POSAppointmentVersion,
			&item.Status,
			&item.CustomerName,
			&item.CustomerPhone,
			&item.CustomerEmail,
			&item.ServiceID,
			&item.StaffID,
			&item.StaffSelectionMode,
			&item.StartTime,
			&item.EndTime,
			&item.Notes,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.attachAppointmentSegments(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) ListBookingAttempts(ctx context.Context, salonID string, ownerUserID string, limit int) ([]BookingAttempt, error) {
	if err := r.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, salon_id::text, source, status, pos_provider, COALESCE(pos_booking_id, ''),
		       customer_name, customer_phone, COALESCE(customer_email, ''), COALESCE(service_id::text, ''),
		       COALESCE(staff_id::text, ''), COALESCE(staff_selection_mode, 'specific'), requested_start_time, requested_end_time, COALESCE(notes, ''),
		       COALESCE(error_code, ''), COALESCE(error_message, ''), created_at, updated_at
		FROM booking_attempts
		WHERE salon_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, salonID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]BookingAttempt, 0)
	for rows.Next() {
		var item BookingAttempt
		if err := rows.Scan(
			&item.ID,
			&item.SalonID,
			&item.Source,
			&item.Status,
			&item.POSProvider,
			&item.POSBookingID,
			&item.CustomerName,
			&item.CustomerPhone,
			&item.CustomerEmail,
			&item.ServiceID,
			&item.StaffID,
			&item.StaffSelectionMode,
			&item.RequestedStartTime,
			&item.RequestedEndTime,
			&item.Notes,
			&item.ErrorCode,
			&item.ErrorMessage,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.attachBookingAttemptSegments(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) attachAppointmentSegments(ctx context.Context, items []Appointment) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	indexByID := make(map[string]int, len(items))
	for index, item := range items {
		ids = append(ids, item.ID)
		indexByID[item.ID] = index
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT aps.appointment_id::text,
		       COALESCE(aps.service_id::text, ''), aps.name,
		       COALESCE(aps.staff_id::text, ''), COALESCE(st.name, ''),
		       COALESCE(aps.staff_selection_mode, 'specific'),
		       COALESCE(aps.duration_minutes, 0), aps.sort_order
		FROM appointment_services aps
		LEFT JOIN staff st ON st.id = aps.staff_id
		WHERE aps.appointment_id = ANY($1::uuid[])
		ORDER BY aps.appointment_id, aps.sort_order ASC
	`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var appointmentID string
		var segment BookingSegmentSnapshot
		if err := rows.Scan(
			&appointmentID,
			&segment.ServiceID,
			&segment.ServiceName,
			&segment.StaffID,
			&segment.StaffName,
			&segment.StaffSelectionMode,
			&segment.DurationMinutes,
			&segment.SortOrder,
		); err != nil {
			return err
		}
		if index, ok := indexByID[appointmentID]; ok {
			items[index].Segments = append(items[index].Segments, segment)
		}
	}
	return rows.Err()
}

func (r *Repository) attachBookingAttemptSegments(ctx context.Context, items []BookingAttempt) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	indexByID := make(map[string]int, len(items))
	for index, item := range items {
		ids = append(ids, item.ID)
		indexByID[item.ID] = index
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT bas.booking_attempt_id::text,
		       COALESCE(bas.service_id::text, ''), bas.name,
		       COALESCE(bas.staff_id::text, ''), COALESCE(st.name, ''),
		       COALESCE(bas.staff_selection_mode, 'specific'),
		       COALESCE(bas.duration_minutes, 0), bas.sort_order
		FROM booking_attempt_segments bas
		LEFT JOIN staff st ON st.id = bas.staff_id
		WHERE bas.booking_attempt_id = ANY($1::uuid[])
		ORDER BY bas.booking_attempt_id, bas.sort_order ASC
	`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var attemptID string
		var segment BookingSegmentSnapshot
		if err := rows.Scan(
			&attemptID,
			&segment.ServiceID,
			&segment.ServiceName,
			&segment.StaffID,
			&segment.StaffName,
			&segment.StaffSelectionMode,
			&segment.DurationMinutes,
			&segment.SortOrder,
		); err != nil {
			return err
		}
		if index, ok := indexByID[attemptID]; ok {
			items[index].Segments = append(items[index].Segments, segment)
		}
	}
	return rows.Err()
}

type appointmentScanner interface {
	Scan(dest ...any) error
}

func scanAppointment(row appointmentScanner) (*Appointment, error) {
	var item Appointment
	err := row.Scan(
		&item.ID,
		&item.SalonID,
		&item.BookingAttemptID,
		&item.POSProvider,
		&item.POSAppointmentID,
		&item.POSAppointmentVersion,
		&item.Status,
		&item.CustomerName,
		&item.CustomerPhone,
		&item.CustomerEmail,
		&item.ServiceID,
		&item.StaffID,
		&item.StaffSelectionMode,
		&item.StartTime,
		&item.EndTime,
		&item.Notes,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pos.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func insertBookingAttemptSegments(ctx context.Context, tx *sql.Tx, attemptID string, segments []BookingSegmentRecord) error {
	for index, segment := range segments {
		sortOrder := segment.SortOrder
		if sortOrder <= 0 {
			sortOrder = index + 1
		}
		mode := segment.StaffSelectionMode
		if mode == "" {
			mode = StaffSelectionSpecific
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO booking_attempt_segments (
				booking_attempt_id, service_id, staff_id, staff_selection_mode, pos_service_id,
				pos_service_version, name, duration_minutes, price_from, sort_order
			)
			VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4, $5, NULLIF($6::bigint, 0), $7, $8, NULLIF($9, 0), $10)
		`, attemptID, segment.Service.ID, segment.Staff.ID, mode, segment.Service.POSServiceID, segment.Service.POSServiceVersion, segment.Service.Name, segment.Service.DurationMinutes, segment.Service.PriceFrom, sortOrder); err != nil {
			return err
		}
	}
	return nil
}

func insertAppointmentServices(ctx context.Context, tx *sql.Tx, appointmentID string, segments []BookingSegmentRecord) error {
	for index, segment := range segments {
		sortOrder := segment.SortOrder
		if sortOrder <= 0 {
			sortOrder = index + 1
		}
		mode := segment.StaffSelectionMode
		if mode == "" {
			mode = StaffSelectionSpecific
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO appointment_services (
				appointment_id, service_id, staff_id, staff_selection_mode, pos_service_id,
				pos_service_version, name, duration_minutes, price_from, sort_order
			)
			VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4, $5, NULLIF($6::bigint, 0), $7, $8, NULLIF($9, 0), $10)
		`, appointmentID, segment.Service.ID, segment.Staff.ID, mode, segment.Service.POSServiceID, segment.Service.POSServiceVersion, segment.Service.Name, segment.Service.DurationMinutes, segment.Service.PriceFrom, sortOrder); err != nil {
			return err
		}
	}
	return nil
}

func fallbackNotificationMessage(record FallbackBookingRecord) string {
	when := record.StartTime.Format(time.RFC3339)
	return fmt.Sprintf("%s requested %s with %s at %s. POS returned %s: %s", record.CustomerName, record.Service.Name, record.Staff.Name, when, record.ErrorCode, record.ErrorMessage)
}

func appointmentActionFallbackTitle(record AppointmentActionFallbackRecord) string {
	switch record.NotificationType {
	case NotificationTypeRescheduleFallback:
		return "Reschedule request needs review"
	case NotificationTypeCancellationFallback:
		return "Cancellation request needs review"
	default:
		return "Appointment request needs review"
	}
}

func appointmentActionFallbackMessage(record AppointmentActionFallbackRecord) string {
	when := record.RequestedStartTime.Format(time.RFC3339)
	switch record.NotificationType {
	case NotificationTypeRescheduleFallback:
		return fmt.Sprintf("%s requested reschedule for %s with %s at %s. POS returned %s: %s", record.Appointment.CustomerName, record.Appointment.Service.Name, record.Appointment.Staff.Name, when, record.ErrorCode, record.ErrorMessage)
	case NotificationTypeCancellationFallback:
		return fmt.Sprintf("%s requested cancellation for %s at %s. POS returned %s: %s", record.Appointment.CustomerName, record.Appointment.Service.Name, when, record.ErrorCode, record.ErrorMessage)
	default:
		return fmt.Sprintf("%s appointment action for %s at %s needs review. POS returned %s: %s", record.Appointment.CustomerName, record.Appointment.Service.Name, when, record.ErrorCode, record.ErrorMessage)
	}
}
