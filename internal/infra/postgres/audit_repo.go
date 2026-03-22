package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"ticket/internal/entities"
	"ticket/internal/ports"
)

type auditRepo struct {
	// Always uses the root db — never enrolled in a booking transaction
	// so failure audit entries survive rollbacks.
	db *sql.DB
}

func NewAuditRepo(db *sql.DB) ports.AuditRepository {
	return &auditRepo{db: db}
}

func (r *auditRepo) Log(ctx context.Context, entry *entities.AuditLog) error {
	var qty sql.NullInt64
	if entry.Quantity != nil {
		qty = sql.NullInt64{Int64: int64(*entry.Quantity), Valid: true}
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO audit_logs
		 (entity_type, entity_id, action, user_id, outcome, quantity, metadata, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		entry.EntityType, entry.EntityID,
		entry.Action, entry.UserID, entry.Outcome,
		qty, entry.Metadata, entry.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}

func (r *auditRepo) ListRecent(ctx context.Context, limit int, eventID *string) ([]entities.AuditLog, error) {
	var eventArg any
	if eventID != nil && *eventID != "" {
		eventArg = *eventID
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id, a.entity_type, a.entity_id, a.action, a.user_id, a.outcome, a.quantity, a.metadata, a.created_at
		FROM audit_logs a
		WHERE (
			$1::uuid IS NULL
			OR (
				a.entity_type = 'booking'
				AND EXISTS (
					SELECT 1 FROM bookings b
					WHERE b.id::text = a.entity_id AND b.event_id = $1::uuid
				)
			)
		)
		ORDER BY a.created_at DESC
		LIMIT $2`, eventArg, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit logs: %w", err)
	}
	defer rows.Close()

	var out []entities.AuditLog
	for rows.Next() {
		var (
			log  entities.AuditLog
			qty  sql.NullInt64
			meta []byte
		)
		if err := rows.Scan(
			&log.ID,
			&log.EntityType,
			&log.EntityID,
			&log.Action,
			&log.UserID,
			&log.Outcome,
			&qty,
			&meta,
			&log.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan audit log: %w", err)
		}
		if qty.Valid {
			q := int(qty.Int64)
			log.Quantity = &q
		}
		if len(meta) > 0 {
			log.Metadata = append(json.RawMessage(nil), meta...)
		}
		out = append(out, log)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit log rows: %w", err)
	}
	return out, nil
}
