package postgres

import (
	"context"
	"database/sql"
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
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO audit_logs
		 (id, entity_type, entity_id, action, user_id, outcome, metadata, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		entry.ID, entry.EntityType, entry.EntityID,
		entry.Action, entry.UserID, entry.Outcome,
		entry.Metadata, entry.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}
