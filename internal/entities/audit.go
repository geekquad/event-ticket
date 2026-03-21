package entities

import (
	"encoding/json"
	"time"
)

type AuditAction string

const (
	AuditActionBookingCreated   AuditAction = "BOOKING_CREATED"
	AuditActionBookingConfirmed AuditAction = "BOOKING_CONFIRMED"
	AuditActionBookingCancelled AuditAction = "BOOKING_CANCELLED"
)

type AuditOutcome string

const (
	AuditOutcomeSuccess AuditOutcome = "SUCCESS"
	AuditOutcomeFailure AuditOutcome = "FAILURE"
)

type AuditLog struct {
	ID         string          `json:"id,omitempty"`
	EntityType string          `json:"entityType"`
	EntityID   string          `json:"entityId"`
	Action     AuditAction     `json:"action"`
	UserID     string          `json:"userId"`
	Outcome    AuditOutcome    `json:"outcome"`
	Quantity   *int            `json:"quantity,omitempty"` // seats reserved or booked, when applicable
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
}
