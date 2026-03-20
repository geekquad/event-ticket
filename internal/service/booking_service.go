package service

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
	// 1. Lazily cancel stale booking records (tickets are already AVAILABLE in DB;
	//    Redis TTL expiry is what frees the seat — no ticket update needed here).
	if err := s.bookingRepo.CancelExpiredReservations(ctx); err != nil {
		slog.Warn("failed to cleanup expired reservations", "error", err)
	}

	// 2. Fetch more candidates than needed so we can skip any that are already
	//    Redis-locked by another active reservation.
	fetchLimit := quantity * 5
	if fetchLimit < 10 {
		fetchLimit = 10
	}
	candidates, err := s.ticketRepo.GetAvailableByEventID(ctx, eventID, fetchLimit)
	if err != nil {
		return nil, fmt.Errorf("get available tickets: %w", err)
	}

	// 3. Acquire Redis locks -- skip tickets that are already held, stop when
	//    we have enough. Roll back all acquired locks if we can't fill the quota.
	var lockedTickets []entities.Ticket
	var acquiredKeys []string
	for _, t := range candidates {
		if len(lockedTickets) == quantity {
			break
		}
		key := "ticket:" + t.ID
		ok, lockErr := s.lockManager.Acquire(ctx, key, userID, s.reservationTTL)
		if lockErr != nil {
			s.releaseKeys(ctx, acquiredKeys, userID)
			return nil, fmt.Errorf("acquire lock: %w", lockErr)
		}
		if ok {
			lockedTickets = append(lockedTickets, t)
			acquiredKeys = append(acquiredKeys, key)
		}
		// lock not acquired = another reservation holds it; just skip to next candidate
	}

	if len(lockedTickets) < quantity {
		s.releaseKeys(ctx, acquiredKeys, userID)
		s.writeAudit(ctx, entities.AuditActionBookingCreated, entities.AuditOutcomeFailure,
			"", userID, map[string]any{"reason": "not enough available seats", "eventId": eventID})
		return nil, entities.ErrTicketUnavailable
	}

	// 4. Persist booking record. Tickets stay AVAILABLE in DB — Redis lock IS the reservation.
	ticketIDs := make([]string, len(lockedTickets))
	var totalPrice float64
	for i, t := range lockedTickets {
		ticketIDs[i] = t.ID
		totalPrice += t.Price
	}

	now := time.Now()
	expiresAt := now.Add(s.reservationTTL)
	booking := &entities.Booking{
		ID:         uuid.New().String(),
		UserID:     userID,
		EventID:    eventID,
		TicketIDs:  ticketIDs,
		TotalPrice: totalPrice,
		Status:     entities.BookingStatusReserved,
		ExpiresAt:  &expiresAt,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := s.bookingRepo.Create(ctx, booking); err != nil {
		s.releaseKeys(ctx, acquiredKeys, userID)
		s.writeAudit(ctx, entities.AuditActionBookingCreated, entities.AuditOutcomeFailure,
			"", userID, map[string]any{"reason": err.Error(), "eventId": eventID})
		return nil, fmt.Errorf("create booking: %w", err)
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

	// 5. DB transaction: mark tickets as BOOKED + confirm booking.
	// BulkUpdateStatus only updates tickets still AVAILABLE, so if another
	// Confirm raced us after our Redis check, rowsAffected < len(ticketIDs).
	err = s.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		n, txErr := s.ticketRepo.BulkUpdateStatus(txCtx, booking.TicketIDs, entities.TicketStatusBooked, &booking.ID)
		if txErr != nil {
			return txErr
		}
		if int(n) != len(booking.TicketIDs) {
			return entities.ErrTicketUnavailable
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
		// CONFIRMED bookings have tickets marked BOOKED in DB — restore them to AVAILABLE.
		// RESERVED bookings keep tickets AVAILABLE in DB throughout (Redis lock is the hold),
		// so no ticket update is needed.
		if booking.Status == entities.BookingStatusConfirmed {
			if _, txErr := s.ticketRepo.BulkUpdateStatus(txCtx, booking.TicketIDs, entities.TicketStatusAvailable, nil); txErr != nil {
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
	// Lazily cancel expired booking records so they don't appear in My Bookings.
	if err := s.bookingRepo.CancelExpiredReservations(ctx); err != nil {
		slog.Warn("failed to cleanup expired reservations", "error", err)
	}

	bookings, err := s.bookingRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	// ExpiresAt is read from DB; nil it out for non-RESERVED bookings so it's
	// not included in the JSON response where it has no meaning.
	for i := range bookings {
		if bookings[i].Status != entities.BookingStatusReserved {
			bookings[i].ExpiresAt = nil
		}
	}
	return bookings, nil
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
