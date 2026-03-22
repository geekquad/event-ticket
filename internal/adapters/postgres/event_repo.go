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
			GREATEST(v.capacity - e.booked_slots - e.reserved_slots, 0) AS available_count,
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

func (r *eventRepo) TryAddReservedSlots(ctx context.Context, eventID string, quantity int) (bool, error) {
	res, err := r.exec(ctx).ExecContext(ctx, `
		UPDATE events e
		SET reserved_slots = e.reserved_slots + $2
		FROM venues v
		WHERE e.id = $1::uuid AND e.venue_id = v.id
		  AND e.booked_slots + e.reserved_slots + $2 <= v.capacity
	`, eventID, quantity)
	if err != nil {
		return false, fmt.Errorf("try add reserved slots: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		var dummy int
		err := r.exec(ctx).QueryRowContext(ctx, `SELECT 1 FROM events WHERE id = $1::uuid`, eventID).Scan(&dummy)
		if errors.Is(err, sql.ErrNoRows) {
			return false, entities.ErrNotFound
		}
		if err != nil {
			return false, fmt.Errorf("lookup event: %w", err)
		}
		return false, nil
	}
	return true, nil
}

func (r *eventRepo) TransferReservedToBooked(ctx context.Context, eventID string, quantity int) (bool, error) {
	res, err := r.exec(ctx).ExecContext(ctx, `
		UPDATE events e
		SET reserved_slots = e.reserved_slots - $2,
		    booked_slots = e.booked_slots + $2
		WHERE e.id = $1::uuid AND e.reserved_slots >= $2
	`, eventID, quantity)
	if err != nil {
		return false, fmt.Errorf("transfer reserved to booked: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return n == 1, nil
}

func (r *eventRepo) ReleaseReservedSlots(ctx context.Context, eventID string, quantity int) (bool, error) {
	res, err := r.exec(ctx).ExecContext(ctx, `
		UPDATE events e
		SET reserved_slots = e.reserved_slots - $2
		WHERE e.id = $1::uuid AND e.reserved_slots >= $2
	`, eventID, quantity)
	if err != nil {
		return false, fmt.Errorf("release reserved slots: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return n == 1, nil
}

func (r *eventRepo) ReleaseBookedSlots(ctx context.Context, eventID string, quantity int) (bool, error) {
	res, err := r.exec(ctx).ExecContext(ctx, `
		UPDATE events e
		SET booked_slots = e.booked_slots - $2
		WHERE e.id = $1::uuid AND e.booked_slots >= $2
	`, eventID, quantity)
	if err != nil {
		return false, fmt.Errorf("release booked slots: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return n == 1, nil
}
