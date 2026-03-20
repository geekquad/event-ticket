package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lib/pq"

	"ticket/internal/entities"
	"ticket/internal/ports"
)

type ticketRepo struct {
	db *sql.DB
}

func NewTicketRepo(db *sql.DB) ports.TicketRepository {
	return &ticketRepo{db: db}
}

func (r *ticketRepo) exec(ctx context.Context) executor {
	return execFromContext(ctx, r.db)
}

func (r *ticketRepo) GetByEventID(ctx context.Context, eventID string) ([]entities.Ticket, error) {
	rows, err := r.exec(ctx).QueryContext(ctx,
		`SELECT id, event_id, seat_number, row, section, price, status, booking_id, created_at
		 FROM tickets
		 WHERE event_id = $1
		 ORDER BY section, row, seat_number`,
		eventID,
	)
	if err != nil {
		return nil, fmt.Errorf("query tickets by event: %w", err)
	}
	defer rows.Close()

	var tickets []entities.Ticket
	for rows.Next() {
		var t entities.Ticket
		if err := rows.Scan(
			&t.ID, &t.EventID, &t.SeatNumber, &t.Row, &t.Section,
			&t.Price, &t.Status, &t.BookingID, &t.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan ticket row: %w", err)
		}
		tickets = append(tickets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ticket rows: %w", err)
	}
	return tickets, nil
}

func (r *ticketRepo) GetByIDs(ctx context.Context, ids []string) ([]entities.Ticket, error) {
	rows, err := r.exec(ctx).QueryContext(ctx,
		`SELECT id, event_id, seat_number, row, section, price, status, booking_id, created_at
		 FROM tickets
		 WHERE id = ANY($1)`,
		pq.Array(ids),
	)
	if err != nil {
		return nil, fmt.Errorf("query tickets by ids: %w", err)
	}
	defer rows.Close()

	var tickets []entities.Ticket
	for rows.Next() {
		var t entities.Ticket
		if err := rows.Scan(
			&t.ID, &t.EventID, &t.SeatNumber, &t.Row, &t.Section,
			&t.Price, &t.Status, &t.BookingID, &t.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan ticket row: %w", err)
		}
		tickets = append(tickets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ticket rows: %w", err)
	}
	return tickets, nil
}

func (r *ticketRepo) BulkUpdateStatus(ctx context.Context, ticketIDs []string, status entities.TicketStatus, bookingID *string) error {
	_, err := r.exec(ctx).ExecContext(ctx,
		`UPDATE tickets SET status = $1, booking_id = $2 WHERE id = ANY($3)`,
		status, bookingID, pq.Array(ticketIDs),
	)
	if err != nil {
		return fmt.Errorf("bulk update ticket status: %w", err)
	}
	return nil
}
