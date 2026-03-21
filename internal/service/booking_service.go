package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"ticket/internal/entities"
	"ticket/internal/ports"
)

type bookingService struct {
	bookingRepo    ports.BookingRepository
	eventRepo      ports.EventRepository
	auditRepo      ports.AuditRepository
	lockManager    ports.LockManager
	transactor     ports.Transactor
	reservationTTL time.Duration
}

func NewBookingService(
	bookingRepo ports.BookingRepository,
	eventRepo ports.EventRepository,
	auditRepo ports.AuditRepository,
	lockManager ports.LockManager,
	transactor ports.Transactor,
	reservationTTL time.Duration,
) ports.BookingService {
	return &bookingService{
		bookingRepo:    bookingRepo,
		eventRepo:      eventRepo,
		auditRepo:      auditRepo,
		lockManager:    lockManager,
		transactor:     transactor,
		reservationTTL: reservationTTL,
	}
}

// reservationLockKey uses event_id:user_id (with a namespace prefix for Redis).
func reservationLockKey(eventID, userID string) string {
	return "reservation:" + eventID + ":" + userID
}

func reservationLockValue(userID string, quantity int, bookingID string) string {
	return userID + "|" + strconv.Itoa(quantity) + "|" + bookingID
}

func (s *bookingService) Reserve(ctx context.Context, userID, eventID string, quantity int) (*entities.Booking, error) {
	if quantity <= 0 {
		quantity = 1
	}

	if err := s.bookingRepo.CancelExpiredReservations(ctx); err != nil {
		slog.Warn("failed to cleanup expired reservations", "error", err)
	}

	var booking *entities.Booking

	txErr := s.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := s.bookingRepo.CancelExpiredReservations(txCtx); err != nil {
			return err
		}

		capacity, err := s.eventRepo.LockEventCapacity(txCtx, eventID)
		if err != nil {
			return err
		}

		allocated, err := s.bookingRepo.SumAllocatedQuantityForEvent(txCtx, eventID)
		if err != nil {
			return err
		}

		if capacity-allocated < quantity {
			s.writeAudit(txCtx, entities.AuditActionBookingCreated, entities.AuditOutcomeFailure,
				"", userID, intPtr(quantity), map[string]any{"reason": "not enough available seats", "eventId": eventID})
			return entities.ErrTicketUnavailable
		}

		has, err := s.bookingRepo.HasActiveReservedBookingForUserEvent(txCtx, userID, eventID)
		if err != nil {
			return err
		}
		if has {
			s.writeAudit(txCtx, entities.AuditActionBookingCreated, entities.AuditOutcomeFailure,
				"", userID, intPtr(quantity), map[string]any{"reason": "active reservation exists for event", "eventId": eventID})
			return entities.ErrConflict
		}

		now := time.Now()
		expiresAt := now.Add(s.reservationTTL)
		b := &entities.Booking{
			UserID:     userID,
			EventID:    eventID,
			Quantity:   quantity,
			Status:     entities.BookingStatusReserved,
			ExpiresAt:  &expiresAt,
			CreatedAt:  now,
			UpdatedAt:  now,
		}

		if err := s.bookingRepo.Create(txCtx, b); err != nil {
			s.writeAudit(txCtx, entities.AuditActionBookingCreated, entities.AuditOutcomeFailure,
				"", userID, intPtr(quantity), map[string]any{"reason": err.Error(), "eventId": eventID})
			return fmt.Errorf("create booking: %w", err)
		}

		booking = b
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}

	lockKey := reservationLockKey(eventID, userID)
	lockVal := reservationLockValue(userID, quantity, booking.ID)

	ok, lockErr := s.lockManager.Acquire(ctx, lockKey, lockVal, s.reservationTTL)
	if lockErr != nil {
		if cancelErr := s.bookingRepo.UpdateStatus(ctx, booking.ID, entities.BookingStatusCancelled); cancelErr != nil {
			slog.Error("failed to cancel booking after redis error", "bookingId", booking.ID, "error", cancelErr)
		}
		s.writeAudit(ctx, entities.AuditActionBookingCreated, entities.AuditOutcomeFailure,
			booking.ID, userID, intPtr(quantity), map[string]any{"reason": lockErr.Error(), "eventId": eventID})
		return nil, fmt.Errorf("acquire reservation lock: %w", lockErr)
	}
	if !ok {
		if cancelErr := s.bookingRepo.UpdateStatus(ctx, booking.ID, entities.BookingStatusCancelled); cancelErr != nil {
			slog.Error("failed to cancel booking after lock contention", "bookingId", booking.ID, "error", cancelErr)
		}
		s.writeAudit(ctx, entities.AuditActionBookingCreated, entities.AuditOutcomeFailure,
			booking.ID, userID, intPtr(quantity), map[string]any{"reason": "reservation lock already held", "eventId": eventID})
		return nil, entities.ErrConflict
	}

	s.writeAudit(ctx, entities.AuditActionBookingCreated, entities.AuditOutcomeSuccess,
		booking.ID, userID, intPtr(quantity), map[string]any{
			"bookingId": booking.ID,
			"eventId":   booking.EventID,
		})

	return booking, nil
}

func (s *bookingService) Confirm(ctx context.Context, userID, bookingID string) (*entities.Booking, error) {
	booking, err := s.bookingRepo.GetByID(ctx, bookingID)
	if err != nil {
		return nil, err
	}

	if booking.UserID != userID {
		s.writeAudit(ctx, entities.AuditActionBookingConfirmed, entities.AuditOutcomeFailure,
			bookingID, userID, intPtr(booking.Quantity), map[string]any{"reason": "unauthorized"})
		return nil, entities.ErrUnauthorized
	}

	if booking.Status != entities.BookingStatusReserved {
		s.writeAudit(ctx, entities.AuditActionBookingConfirmed, entities.AuditOutcomeFailure,
			bookingID, userID, intPtr(booking.Quantity), map[string]any{"reason": "not in reserved state", "status": string(booking.Status)})
		return nil, entities.ErrConflict
	}

	lockKey := reservationLockKey(booking.EventID, userID)
	wantVal := reservationLockValue(userID, booking.Quantity, bookingID)
	owner, lockErr := s.lockManager.GetOwner(ctx, lockKey)
	if lockErr != nil {
		return nil, fmt.Errorf("check lock owner: %w", lockErr)
	}
	if owner != wantVal {
		s.writeAudit(ctx, entities.AuditActionBookingConfirmed, entities.AuditOutcomeFailure,
			bookingID, userID, intPtr(booking.Quantity), map[string]any{"reason": "reservation expired or lock mismatch"})
		return nil, entities.ErrTicketUnavailable
	}

	err = s.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		n, uerr := s.bookingRepo.ConfirmReservation(txCtx, bookingID)
		if uerr != nil {
			return uerr
		}
		if n == 0 {
			return entities.ErrConflict
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, entities.ErrConflict) {
			s.writeAudit(ctx, entities.AuditActionBookingConfirmed, entities.AuditOutcomeFailure,
				bookingID, userID, intPtr(booking.Quantity), map[string]any{"reason": "booking no longer reserved"})
			return nil, entities.ErrConflict
		}
		s.writeAudit(ctx, entities.AuditActionBookingConfirmed, entities.AuditOutcomeFailure,
			bookingID, userID, intPtr(booking.Quantity), map[string]any{"reason": err.Error()})
		return nil, fmt.Errorf("confirm booking: %w", err)
	}

	if releaseErr := s.lockManager.Release(ctx, lockKey, wantVal); releaseErr != nil {
		slog.Error("failed to release lock after confirm",
			"bookingId", bookingID, "error", releaseErr)
	}

	s.writeAudit(ctx, entities.AuditActionBookingConfirmed, entities.AuditOutcomeSuccess,
		bookingID, userID, intPtr(booking.Quantity), map[string]any{
			"bookingId": bookingID,
			"eventId":   booking.EventID,
		})

	booking.Status = entities.BookingStatusConfirmed
	return booking, nil
}

func (s *bookingService) Cancel(ctx context.Context, userID, bookingID string) error {
	booking, err := s.bookingRepo.GetByID(ctx, bookingID)
	if err != nil {
		s.writeAudit(ctx, entities.AuditActionBookingCancelled, entities.AuditOutcomeFailure,
			bookingID, userID, nil, map[string]any{"bookingId": bookingID, "reason": "not_found"})
		return err
	}

	if booking.UserID != userID {
		s.writeAudit(ctx, entities.AuditActionBookingCancelled, entities.AuditOutcomeFailure,
			bookingID, userID, intPtr(booking.Quantity), map[string]any{"bookingId": bookingID, "reason": "unauthorized"})
		return entities.ErrUnauthorized
	}

	if booking.Status == entities.BookingStatusCancelled {
		s.writeAudit(ctx, entities.AuditActionBookingCancelled, entities.AuditOutcomeFailure,
			bookingID, userID, intPtr(booking.Quantity), map[string]any{"bookingId": bookingID, "reason": "already_cancelled"})
		return entities.ErrConflict
	}

	wasReserved := booking.Status == entities.BookingStatusReserved

	err = s.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		return s.bookingRepo.UpdateStatus(txCtx, bookingID, entities.BookingStatusCancelled)
	})
	if err != nil {
		s.writeAudit(ctx, entities.AuditActionBookingCancelled, entities.AuditOutcomeFailure,
			bookingID, userID, intPtr(booking.Quantity), map[string]any{"bookingId": bookingID, "reason": err.Error()})
		return err
	}

	if wasReserved {
		lockKey := reservationLockKey(booking.EventID, userID)
		lockVal := reservationLockValue(userID, booking.Quantity, bookingID)
		if releaseErr := s.lockManager.Release(ctx, lockKey, lockVal); releaseErr != nil {
			slog.Error("failed to release lock after cancel",
				"bookingId", bookingID, "error", releaseErr)
		}
	}

	s.writeAudit(ctx, entities.AuditActionBookingCancelled, entities.AuditOutcomeSuccess,
		bookingID, userID, intPtr(booking.Quantity), map[string]any{"bookingId": bookingID, "eventId": booking.EventID})

	return nil
}

func (s *bookingService) GetUserBookings(ctx context.Context, userID string) ([]entities.Booking, error) {
	if err := s.bookingRepo.CancelExpiredReservations(ctx); err != nil {
		slog.Warn("failed to cleanup expired reservations", "error", err)
	}

	bookings, err := s.bookingRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range bookings {
		if bookings[i].Status != entities.BookingStatusReserved {
			bookings[i].ExpiresAt = nil
		}
	}
	return bookings, nil
}

func intPtr(n int) *int {
	v := n
	return &v
}

func (s *bookingService) writeAudit(
	ctx context.Context,
	action entities.AuditAction,
	outcome entities.AuditOutcome,
	entityID, userID string,
	quantity *int,
	meta map[string]any,
) {
	metadata, err := json.Marshal(meta)
	if err != nil {
		slog.Error("failed to marshal audit metadata", "error", err)
		return
	}

	entry := &entities.AuditLog{
		EntityType: "booking",
		EntityID:   entityID,
		Action:     action,
		UserID:     userID,
		Outcome:    outcome,
		Quantity:   quantity,
		Metadata:   metadata,
		CreatedAt:  time.Now(),
	}

	if err := s.auditRepo.Log(ctx, entry); err != nil {
		slog.Error("failed to write audit log",
			"action", action, "outcome", outcome, "entityId", entityID, "error", err)
	}
}
