package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"ticket/internal/entities"
	"ticket/internal/ports"
)

type eventRepo struct {
	db *sql.DB
}

func NewEventRepo(db *sql.DB) ports.EventRepository {
	return &eventRepo{db: db}
}

func (r *eventRepo) exec(ctx context.Context) executor {
	return execFromContext(ctx, r.db)
}

func (r *eventRepo) List(ctx context.Context) ([]entities.Event, error) {
	rows, err := r.exec(ctx).QueryContext(ctx, `
		SELECT
			e.id, e.name, e.description, e.date_time,
			v.capacity - COALESCE((
				SELECT SUM(b.quantity) FROM bookings b
				WHERE b.event_id = e.id
				AND (b.status = 'CONFIRMED' OR (b.status = 'RESERVED' AND b.expires_at > NOW()))
			), 0) AS available_count,
			e.created_at,
			v.id, v.name, v.address, v.capacity, v.seat_map, v.created_at
		FROM events e
		JOIN venues v ON e.venue_id = v.id
		ORDER BY e.date_time ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []entities.Event
	for rows.Next() {
		var event entities.Event
		if err := rows.Scan(
			&event.ID, &event.Name, &event.Description, &event.DateTime,
			&event.AvailableCount, &event.CreatedAt,
			&event.Venue.ID, &event.Venue.Name, &event.Venue.Address,
			&event.Venue.Capacity, &event.Venue.SeatMap, &event.Venue.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan event row: %w", err)
		}
		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event rows: %w", err)
	}

	return events, nil
}

func (r *eventRepo) LockEventCapacity(ctx context.Context, eventID string) (int, error) {
	var capacity int
	err := r.exec(ctx).QueryRowContext(ctx,
		`SELECT v.capacity FROM events e JOIN venues v ON e.venue_id = v.id WHERE e.id = $1 FOR UPDATE OF e`,
		eventID,
	).Scan(&capacity)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, entities.ErrNotFound
		}
		return 0, fmt.Errorf("lock event capacity: %w", err)
	}
	return capacity, nil
}
