package service

import (
	"context"

	"ticket/internal/entities"
	"ticket/internal/ports"
)

const (
	auditListDefaultLimit = 100
	auditListMaxLimit     = 500
)

type auditService struct {
	auditRepo ports.AuditRepository
}

func NewAuditService(auditRepo ports.AuditRepository) ports.AuditService {
	return &auditService{auditRepo: auditRepo}
}

func (s *auditService) ListRecent(ctx context.Context, limit int, eventID *string) ([]entities.AuditLog, error) {
	if limit <= 0 {
		limit = auditListDefaultLimit
	}
	if limit > auditListMaxLimit {
		limit = auditListMaxLimit
	}
	return s.auditRepo.ListRecent(ctx, limit, eventID)
}
