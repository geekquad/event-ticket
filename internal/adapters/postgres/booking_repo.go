package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"ticket/internal/entities"
	"ticket/internal/ports"
)

type bookingRepo struct {
	db *sql.DB
}

func NewBookingRepo(db *sql.DB) ports.BookingRepository {
	return &bookingRepo{db: db}
}

func (r *bookingRepo) exec(ctx context.Context) executor {
	return execFromContext(ctx, r.db)
}

func (r *bookingRepo) Create(ctx context.Context, booking *entities.Booking) error {
	if booking.ID != "" {
		_, err := r.exec(ctx).ExecContext(ctx,
			`INSERT INTO bookings (id, user_id, event_id, quantity, status, expires_at, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			booking.ID, booking.UserID, booking.EventID, booking.Quantity,
			booking.Status, booking.ExpiresAt, booking.CreatedAt, booking.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert booking: %w", err)
		}
		return nil
	}
	err := r.exec(ctx).QueryRowContext(ctx,
		`INSERT INTO bookings (user_id, event_id, quantity, status, expires_at, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id`,
		booking.UserID, booking.EventID, booking.Quantity,
		booking.Status, booking.ExpiresAt, booking.CreatedAt, booking.UpdatedAt,
	).Scan(&booking.ID)
	if err != nil {
		return fmt.Errorf("insert booking: %w", err)
	}
	return nil
}

func (r *bookingRepo) GetByID(ctx context.Context, id string) (*entities.Booking, error) {
	var booking entities.Booking
	var expiresAt time.Time
	err := r.exec(ctx).QueryRowContext(ctx,
		`SELECT id, user_id, event_id, quantity, status, expires_at, created_at, updated_at
		 FROM bookings WHERE id = $1 FOR UPDATE`,
		id,
	).Scan(
		&booking.ID, &booking.UserID, &booking.EventID, &booking.Quantity,
		&booking.Status, &expiresAt, &booking.CreatedAt, &booking.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, entities.ErrNotFound
		}
		return nil, fmt.Errorf("query booking by id: %w", err)
	}
	booking.ExpiresAt = &expiresAt

	return &booking, nil
}

func (r *bookingRepo) GetByUserID(ctx context.Context, userID string) ([]entities.Booking, error) {
	rows, err := r.exec(ctx).QueryContext(ctx,
		`SELECT b.id, b.user_id, b.event_id, b.quantity, b.status, b.expires_at, b.created_at, b.updated_at
		 FROM bookings b
		 WHERE b.user_id = $1 AND b.status IN ('RESERVED', 'CONFIRMED')
		 ORDER BY b.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query bookings by user: %w", err)
	}
	defer rows.Close()

	var bookings []entities.Booking
	for rows.Next() {
		var b entities.Booking
		var expiresAt time.Time
		if err := rows.Scan(
			&b.ID, &b.UserID, &b.EventID, &b.Quantity,
			&b.Status, &expiresAt, &b.CreatedAt, &b.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan booking row: %w", err)
		}
		b.ExpiresAt = &expiresAt

		bookings = append(bookings, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate booking rows: %w", err)
	}
	return bookings, nil
}

func (r *bookingRepo) UpdateStatus(ctx context.Context, bookingID string, status entities.BookingStatus) error {
	_, err := r.exec(ctx).ExecContext(ctx,
		`UPDATE bookings SET status = $1, updated_at = NOW() WHERE id = $2`,
		status, bookingID,
	)
	if err != nil {
		return fmt.Errorf("update booking status: %w", err)
	}
	return nil
}

func (r *bookingRepo) ConfirmReservation(ctx context.Context, bookingID string) (int64, error) {
	res, err := r.exec(ctx).ExecContext(ctx,
		`UPDATE bookings SET status = 'CONFIRMED', updated_at = NOW() WHERE id = $1 AND status = 'RESERVED'`,
		bookingID,
	)
	if err != nil {
		return 0, fmt.Errorf("confirm reservation: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}

// CancelExpiredReservations cancels expired RESERVED bookings and decrements event reserved_slots (atomic).
func (r *bookingRepo) CancelExpiredReservations(ctx context.Context) error {
	_, err := r.exec(ctx).ExecContext(ctx, `
		WITH expired AS (
			UPDATE bookings SET status = 'CANCELLED', updated_at = NOW()
			WHERE status = 'RESERVED' AND expires_at <= NOW()
			RETURNING event_id, quantity
		),
		agg AS (
			SELECT event_id, SUM(quantity) AS sub FROM expired GROUP BY event_id
		)
		UPDATE events e
		SET reserved_slots = e.reserved_slots - a.sub
		FROM agg a
		WHERE e.id = a.event_id AND e.reserved_slots >= a.sub
	`)
	if err != nil {
		return fmt.Errorf("cancel expired reservations: %w", err)
	}
	return nil
}

func (r *bookingRepo) CancelReservationIfExpired(ctx context.Context, bookingID string) (string, int, bool, error) {
	var eventID string
	var qty int
	err := r.exec(ctx).QueryRowContext(ctx, `
		UPDATE bookings SET status = 'CANCELLED', updated_at = NOW()
		WHERE id = $1::uuid AND status = 'RESERVED' AND expires_at <= NOW()
		RETURNING event_id, quantity
	`, bookingID).Scan(&eventID, &qty)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, false, nil
		}
		return "", 0, false, fmt.Errorf("cancel reservation if expired: %w", err)
	}
	return eventID, qty, true, nil
}

func (r *bookingRepo) HasActiveReservedBookingForUserEvent(ctx context.Context, userID, eventID string) (bool, error) {
	var exists bool
	err := r.exec(ctx).QueryRowContext(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM bookings
			WHERE user_id = $1 AND event_id = $2
			AND status = 'RESERVED' AND expires_at > NOW()
		)`,
		userID, eventID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("has active reserved booking: %w", err)
	}
	return exists, nil
}
