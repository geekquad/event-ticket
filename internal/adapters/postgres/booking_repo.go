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
	_, err := r.exec(ctx).ExecContext(ctx,
		`INSERT INTO bookings (id, user_id, event_id, total_price, status, expires_at, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		booking.ID, booking.UserID, booking.EventID,
		booking.TotalPrice, booking.Status, booking.ExpiresAt,
		booking.CreatedAt, booking.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert booking: %w", err)
	}

	for _, ticketID := range booking.TicketIDs {
		_, err := r.exec(ctx).ExecContext(ctx,
			`INSERT INTO booking_tickets (booking_id, ticket_id) VALUES ($1, $2)`,
			booking.ID, ticketID,
		)
		if err != nil {
			return fmt.Errorf("insert booking_ticket: %w", err)
		}
	}

	return nil
}

func (r *bookingRepo) GetByID(ctx context.Context, id string) (*entities.Booking, error) {
	var booking entities.Booking
	var expiresAt time.Time
	err := r.exec(ctx).QueryRowContext(ctx,
		`SELECT id, user_id, event_id, total_price, status, expires_at, created_at, updated_at
		 FROM bookings WHERE id = $1 FOR UPDATE`,
		id,
	).Scan(
		&booking.ID, &booking.UserID, &booking.EventID,
		&booking.TotalPrice, &booking.Status, &expiresAt,
		&booking.CreatedAt, &booking.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, entities.ErrNotFound
		}
		return nil, fmt.Errorf("query booking by id: %w", err)
	}
	booking.ExpiresAt = &expiresAt

	rows, err := r.exec(ctx).QueryContext(ctx,
		`SELECT ticket_id FROM booking_tickets WHERE booking_id = $1`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("query booking tickets: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ticketID string
		if err := rows.Scan(&ticketID); err != nil {
			return nil, fmt.Errorf("scan booking ticket: %w", err)
		}
		booking.TicketIDs = append(booking.TicketIDs, ticketID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate booking tickets: %w", err)
	}

	return &booking, nil
}

func (r *bookingRepo) GetByUserID(ctx context.Context, userID string) ([]entities.Booking, error) {
	rows, err := r.exec(ctx).QueryContext(ctx,
		`SELECT b.id, b.user_id, b.event_id, b.total_price, b.status, b.expires_at, b.created_at, b.updated_at
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
			&b.ID, &b.UserID, &b.EventID,
			&b.TotalPrice, &b.Status, &expiresAt,
			&b.CreatedAt, &b.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan booking row: %w", err)
		}
		b.ExpiresAt = &expiresAt

		ticketRows, err := r.exec(ctx).QueryContext(ctx,
			`SELECT ticket_id FROM booking_tickets WHERE booking_id = $1`,
			b.ID,
		)
		if err != nil {
			return nil, fmt.Errorf("query booking tickets: %w", err)
		}

		for ticketRows.Next() {
			var ticketID string
			if err := ticketRows.Scan(&ticketID); err != nil {
				ticketRows.Close()
				return nil, fmt.Errorf("scan booking ticket: %w", err)
			}
			b.TicketIDs = append(b.TicketIDs, ticketID)
		}
		if err := ticketRows.Err(); err != nil {
			ticketRows.Close()
			return nil, fmt.Errorf("iterate booking tickets: %w", err)
		}
		ticketRows.Close()

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

// CancelExpiredReservations cancels booking records whose expires_at has passed.
// Tickets are left untouched — they stay AVAILABLE in the DB throughout
// the reservation window (Redis TTL expiry frees the seat automatically).
func (r *bookingRepo) CancelExpiredReservations(ctx context.Context) error {
	_, err := r.exec(ctx).ExecContext(ctx,
		`UPDATE bookings SET status = 'CANCELLED', updated_at = NOW()
		 WHERE status = 'RESERVED' AND expires_at < NOW()`,
	)
	if err != nil {
		return fmt.Errorf("cancel expired reservations: %w", err)
	}
	return nil
}
