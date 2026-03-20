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

func (r *eventRepo) GetByID(ctx context.Context, id string) (*entities.Event, error) {
	query := `
		SELECT
			e.id, e.name, e.description, e.date_time, e.capacity,
			(SELECT COUNT(*) FROM tickets WHERE event_id = e.id AND status = 'AVAILABLE') as available_count,
			e.created_at,
			v.id, v.name, v.address, v.capacity, v.seat_map, v.created_at,
			p.id, p.name, p.description, p.created_at
		FROM events e
		JOIN venues v ON e.venue_id = v.id
		JOIN performers p ON e.performer_id = p.id
		WHERE e.id = $1
	`

	var event entities.Event
	err := r.exec(ctx).QueryRowContext(ctx, query, id).Scan(
		&event.ID, &event.Name, &event.Description, &event.DateTime,
		&event.Capacity, &event.AvailableCount, &event.CreatedAt,
		&event.Venue.ID, &event.Venue.Name, &event.Venue.Address,
		&event.Venue.Capacity, &event.Venue.SeatMap, &event.Venue.CreatedAt,
		&event.Performer.ID, &event.Performer.Name, &event.Performer.Description, &event.Performer.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, entities.ErrNotFound
		}
		return nil, fmt.Errorf("query event by id: %w", err)
	}

	return &event, nil
}

func (r *eventRepo) List(ctx context.Context, params ports.EventSearchParams) ([]entities.Event, int, error) {
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}

	where := "WHERE 1=1"
	args := []interface{}{}
	argIdx := 1

	if params.Keyword != "" {
		where += fmt.Sprintf(" AND (e.name ILIKE $%d OR e.description ILIKE $%d)", argIdx, argIdx)
		args = append(args, "%"+params.Keyword+"%")
		argIdx++
	}

	if params.StartDate != nil {
		where += fmt.Sprintf(" AND e.date_time >= $%d", argIdx)
		args = append(args, *params.StartDate)
		argIdx++
	}

	if params.EndDate != nil {
		where += fmt.Sprintf(" AND e.date_time <= $%d", argIdx)
		args = append(args, *params.EndDate)
		argIdx++
	}

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM events e %s", where)
	if err := r.exec(ctx).QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count events: %w", err)
	}

	offset := (params.Page - 1) * params.PageSize
	dataQuery := fmt.Sprintf(`
		SELECT
			e.id, e.name, e.description, e.date_time, e.capacity,
			(SELECT COUNT(*) FROM tickets WHERE event_id = e.id AND status = 'AVAILABLE') as available_count,
			e.created_at,
			v.id, v.name, v.address, v.capacity, v.seat_map, v.created_at,
			p.id, p.name, p.description, p.created_at
		FROM events e
		JOIN venues v ON e.venue_id = v.id
		JOIN performers p ON e.performer_id = p.id
		%s
		ORDER BY e.date_time ASC
		LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1)

	args = append(args, params.PageSize, offset)

	rows, err := r.exec(ctx).QueryContext(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []entities.Event
	for rows.Next() {
		var event entities.Event
		if err := rows.Scan(
			&event.ID, &event.Name, &event.Description, &event.DateTime,
			&event.Capacity, &event.AvailableCount, &event.CreatedAt,
			&event.Venue.ID, &event.Venue.Name, &event.Venue.Address,
			&event.Venue.Capacity, &event.Venue.SeatMap, &event.Venue.CreatedAt,
			&event.Performer.ID, &event.Performer.Name, &event.Performer.Description, &event.Performer.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan event row: %w", err)
		}
		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate event rows: %w", err)
	}

	return events, total, nil
}
