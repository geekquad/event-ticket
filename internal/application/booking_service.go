package application

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"ticket/internal/entities"
	"ticket/internal/ports"
)

type bookingService struct {
	bookingRepo    ports.BookingRepository
	ticketRepo     ports.TicketRepository
	auditRepo      ports.AuditRepository
	lockManager    ports.LockManager
	transactor     ports.Transactor
	reservationTTL time.Duration
}

func NewBookingService(
	bookingRepo ports.BookingRepository,
	ticketRepo ports.TicketRepository,
	auditRepo ports.AuditRepository,
	lockManager ports.LockManager,
	transactor ports.Transactor,
	reservationTTL time.Duration,
) ports.BookingService {
	return &bookingService{
		bookingRepo:    bookingRepo,
		ticketRepo:     ticketRepo,
		auditRepo:      auditRepo,
		lockManager:    lockManager,
		transactor:     transactor,
		reservationTTL: reservationTTL,
	}
}

func (s *bookingService) Reserve(ctx context.Context, userID string, ticketIDs []string) (*entities.Booking, error) {
	// 1. Validate all tickets exist and are AVAILABLE
	tickets, err := s.ticketRepo.GetByIDs(ctx, ticketIDs)
	if err != nil {
		return nil, fmt.Errorf("get tickets: %w", err)
	}

	if len(tickets) != len(ticketIDs) {
		s.writeAudit(ctx, entities.AuditActionBookingCreated, entities.AuditOutcomeFailure,
			"", userID, map[string]any{"reason": "some tickets not found", "ticketIds": ticketIDs})
		return nil, entities.ErrNotFound
	}

	for _, t := range tickets {
		if t.Status != entities.TicketStatusAvailable {
			s.writeAudit(ctx, entities.AuditActionBookingCreated, entities.AuditOutcomeFailure,
				"", userID, map[string]any{"reason": "ticket not available", "ticketId": t.ID})
			return nil, entities.ErrTicketUnavailable
		}
	}

	// 2. Acquire Redis locks -- all-or-nothing
	var acquiredKeys []string
	for _, id := range ticketIDs {
		key := "ticket:" + id
		ok, lockErr := s.lockManager.Acquire(ctx, key, userID, s.reservationTTL)
		if lockErr != nil {
			s.releaseKeys(ctx, acquiredKeys, userID)
			return nil, fmt.Errorf("acquire lock: %w", lockErr)
		}
		if !ok {
			s.releaseKeys(ctx, acquiredKeys, userID)
			s.writeAudit(ctx, entities.AuditActionBookingCreated, entities.AuditOutcomeFailure,
				"", userID, map[string]any{"reason": "lock contention", "ticketId": id})
			return nil, entities.ErrTicketUnavailable
		}
		acquiredKeys = append(acquiredKeys, key)
	}

	// 3. Compute total price
	var totalPrice float64
	for _, t := range tickets {
		totalPrice += t.Price
	}

	// 4. Create booking
	now := time.Now()
	booking := &entities.Booking{
		ID:         uuid.New().String(),
		UserID:     userID,
		EventID:    tickets[0].EventID,
		TicketIDs:  ticketIDs,
		TotalPrice: totalPrice,
		Status:     entities.BookingStatusReserved,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// 5. Persist booking -- if fails, release all locks
	if err := s.bookingRepo.Create(ctx, booking); err != nil {
		s.releaseKeys(ctx, acquiredKeys, userID)
		s.writeAudit(ctx, entities.AuditActionBookingCreated, entities.AuditOutcomeFailure,
			"", userID, map[string]any{"reason": err.Error(), "ticketIds": ticketIDs})
		return nil, fmt.Errorf("create booking: %w", err)
	}

	// 6. Audit success
	s.writeAudit(ctx, entities.AuditActionBookingCreated, entities.AuditOutcomeSuccess,
		booking.ID, userID, map[string]any{
			"bookingId": booking.ID,
			"eventId":   booking.EventID,
			"ticketIds": ticketIDs,
		})

	return booking, nil
}

func (s *bookingService) Confirm(ctx context.Context, userID, bookingID string) (*entities.Booking, error) {
	// 1. Get booking
	booking, err := s.bookingRepo.GetByID(ctx, bookingID)
	if err != nil {
		return nil, err
	}

	// 2. Validate ownership
	if booking.UserID != userID {
		s.writeAudit(ctx, entities.AuditActionBookingConfirmed, entities.AuditOutcomeFailure,
			bookingID, userID, map[string]any{"reason": "unauthorized"})
		return nil, entities.ErrUnauthorized
	}

	// 3. Validate status
	if booking.Status != entities.BookingStatusReserved {
		s.writeAudit(ctx, entities.AuditActionBookingConfirmed, entities.AuditOutcomeFailure,
			bookingID, userID, map[string]any{"reason": "not in reserved state", "status": string(booking.Status)})
		return nil, entities.ErrConflict
	}

	// 4. Validate lock ownership for each ticket (before DB transaction)
	for _, ticketID := range booking.TicketIDs {
		owner, lockErr := s.lockManager.GetOwner(ctx, "ticket:"+ticketID)
		if lockErr != nil {
			return nil, fmt.Errorf("check lock owner: %w", lockErr)
		}
		if owner != userID {
			s.writeAudit(ctx, entities.AuditActionBookingConfirmed, entities.AuditOutcomeFailure,
				bookingID, userID, map[string]any{"reason": "reservation expired", "ticketId": ticketID})
			return nil, entities.ErrTicketUnavailable
		}
	}

	// 5. DB transaction: mark tickets as BOOKED + confirm booking
	err = s.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		if txErr := s.ticketRepo.BulkUpdateStatus(txCtx, booking.TicketIDs, entities.TicketStatusBooked, &booking.ID); txErr != nil {
			return txErr
		}
		return s.bookingRepo.UpdateStatus(txCtx, bookingID, entities.BookingStatusConfirmed)
	})
	if err != nil {
		s.writeAudit(ctx, entities.AuditActionBookingConfirmed, entities.AuditOutcomeFailure,
			bookingID, userID, map[string]any{"reason": err.Error()})
		return nil, fmt.Errorf("confirm booking: %w", err)
	}

	// 6. Release all Redis locks
	for _, ticketID := range booking.TicketIDs {
		if releaseErr := s.lockManager.Release(ctx, "ticket:"+ticketID, userID); releaseErr != nil {
			slog.Error("failed to release lock after confirm",
				"ticketId", ticketID, "error", releaseErr)
		}
	}

	// 7. Audit success
	s.writeAudit(ctx, entities.AuditActionBookingConfirmed, entities.AuditOutcomeSuccess,
		bookingID, userID, map[string]any{
			"bookingId": bookingID,
			"eventId":   booking.EventID,
		})

	booking.Status = entities.BookingStatusConfirmed
	return booking, nil
}

func (s *bookingService) Cancel(ctx context.Context, userID, bookingID string) error {
	// 1. Get booking
	booking, err := s.bookingRepo.GetByID(ctx, bookingID)
	if err != nil {
		s.writeAudit(ctx, entities.AuditActionBookingCancelled, entities.AuditOutcomeFailure,
			bookingID, userID, map[string]any{"bookingId": bookingID, "reason": "not_found"})
		return err
	}

	// 2. Validate ownership
	if booking.UserID != userID {
		s.writeAudit(ctx, entities.AuditActionBookingCancelled, entities.AuditOutcomeFailure,
			bookingID, userID, map[string]any{"bookingId": bookingID, "reason": "unauthorized"})
		return entities.ErrUnauthorized
	}

	// 3. Validate not already cancelled
	if booking.Status == entities.BookingStatusCancelled {
		s.writeAudit(ctx, entities.AuditActionBookingCancelled, entities.AuditOutcomeFailure,
			bookingID, userID, map[string]any{"bookingId": bookingID, "reason": "already_cancelled"})
		return entities.ErrConflict
	}

	wasReserved := booking.Status == entities.BookingStatusReserved

	// 4. DB transaction
	err = s.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		// If confirmed, release tickets back to AVAILABLE
		if booking.Status == entities.BookingStatusConfirmed {
			if txErr := s.ticketRepo.BulkUpdateStatus(txCtx, booking.TicketIDs, entities.TicketStatusAvailable, nil); txErr != nil {
				return txErr
			}
		}
		return s.bookingRepo.UpdateStatus(txCtx, bookingID, entities.BookingStatusCancelled)
	})
	if err != nil {
		s.writeAudit(ctx, entities.AuditActionBookingCancelled, entities.AuditOutcomeFailure,
			bookingID, userID, map[string]any{"bookingId": bookingID, "reason": err.Error()})
		return err
	}

	// 5. If was RESERVED, release Redis locks AFTER the DB transaction commits
	if wasReserved {
		for _, ticketID := range booking.TicketIDs {
			if releaseErr := s.lockManager.Release(ctx, "ticket:"+ticketID, userID); releaseErr != nil {
				slog.Error("failed to release lock after cancel",
					"ticketId", ticketID, "error", releaseErr)
			}
		}
	}

	// 6. Audit success
	s.writeAudit(ctx, entities.AuditActionBookingCancelled, entities.AuditOutcomeSuccess,
		bookingID, userID, map[string]any{"bookingId": bookingID, "eventId": booking.EventID})

	return nil
}

func (s *bookingService) GetUserBookings(ctx context.Context, userID string) ([]entities.Booking, error) {
	return s.bookingRepo.GetByUserID(ctx, userID)
}

func (s *bookingService) releaseKeys(ctx context.Context, keys []string, value string) {
	for _, key := range keys {
		if err := s.lockManager.Release(ctx, key, value); err != nil {
			slog.Error("failed to release lock", "key", key, "error", err)
		}
	}
}

func (s *bookingService) writeAudit(
	ctx context.Context,
	action entities.AuditAction,
	outcome entities.AuditOutcome,
	entityID, userID string,
	meta map[string]any,
) {
	metadata, err := json.Marshal(meta)
	if err != nil {
		slog.Error("failed to marshal audit metadata", "error", err)
		return
	}

	entry := &entities.AuditLog{
		ID:         uuid.New().String(),
		EntityType: "booking",
		EntityID:   entityID,
		Action:     action,
		UserID:     userID,
		Outcome:    outcome,
		Metadata:   metadata,
		CreatedAt:  time.Now(),
	}

	if err := s.auditRepo.Log(ctx, entry); err != nil {
		slog.Error("failed to write audit log",
			"action", action, "outcome", outcome, "entityId", entityID, "error", err)
	}
}
