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

func (s *bookingService) Reserve(ctx context.Context, userID, eventID string, quantity int) (*entities.Booking, error) {
	// 1. Lazily expire stale reservations so their tickets become available again
	cutoff := time.Now().Add(-s.reservationTTL)
	if err := s.bookingRepo.CancelExpiredReservations(ctx, cutoff); err != nil {
		slog.Warn("failed to cleanup expired reservations", "error", err)
	}

	// 2. Atomically pick + mark tickets as RESERVED in the DB.
	//    FOR UPDATE SKIP LOCKED inside the transaction prevents two concurrent
	//    requests from selecting the same seats.
	var tickets []entities.Ticket
	var booking *entities.Booking

	txErr := s.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		var err error
		tickets, err = s.ticketRepo.GetAvailableByEventID(txCtx, eventID, quantity)
		if err != nil {
			return fmt.Errorf("get available tickets: %w", err)
		}
		if len(tickets) < quantity {
			return entities.ErrTicketUnavailable
		}

		ticketIDs := make([]string, len(tickets))
		for i, t := range tickets {
			ticketIDs[i] = t.ID
		}

		if err := s.ticketRepo.BulkUpdateStatus(txCtx, ticketIDs, entities.TicketStatusReserved, nil); err != nil {
			return fmt.Errorf("mark tickets reserved: %w", err)
		}

		var totalPrice float64
		for _, t := range tickets {
			totalPrice += t.Price
		}

		now := time.Now()
		booking = &entities.Booking{
			ID:         uuid.New().String(),
			UserID:     userID,
			EventID:    eventID,
			TicketIDs:  ticketIDs,
			TotalPrice: totalPrice,
			Status:     entities.BookingStatusReserved,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		return s.bookingRepo.Create(txCtx, booking)
	})

	if txErr != nil {
		s.writeAudit(ctx, entities.AuditActionBookingCreated, entities.AuditOutcomeFailure,
			"", userID, map[string]any{"reason": txErr.Error(), "eventId": eventID})
		return nil, txErr
	}

	// 3. Set Redis TTL keys so Confirm can validate the reservation hasn't expired.
	//    Non-critical: if Redis is down the user gets a 409 on Confirm.
	for _, t := range tickets {
		if _, err := s.lockManager.Acquire(ctx, "ticket:"+t.ID, userID, s.reservationTTL); err != nil {
			slog.Error("failed to set reservation TTL", "ticketId", t.ID, "error", err)
		}
	}

	ticketIDs := make([]string, len(tickets))
	for i, t := range tickets {
		ticketIDs[i] = t.ID
	}
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
		// Release tickets back to AVAILABLE for both RESERVED and CONFIRMED bookings
		if booking.Status == entities.BookingStatusReserved || booking.Status == entities.BookingStatusConfirmed {
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
