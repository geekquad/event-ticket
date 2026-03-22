package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"

	"ticket/internal/entities"
	"ticket/internal/ports"
)

type bookingService struct {
	bookingRepo            ports.BookingRepository
	eventRepo              ports.EventRepository
	auditRepo              ports.AuditRepository
	lockManager            ports.LockManager
	transactor             ports.Transactor
	reservationTTL         time.Duration
	maxSeatsPerReservation int
}

func NewBookingService(
	bookingRepo ports.BookingRepository,
	eventRepo ports.EventRepository,
	auditRepo ports.AuditRepository,
	lockManager ports.LockManager,
	transactor ports.Transactor,
	reservationTTL time.Duration,
	maxSeatsPerReservation int,
) ports.BookingService {
	return &bookingService{
		bookingRepo:            bookingRepo,
		eventRepo:              eventRepo,
		auditRepo:              auditRepo,
		lockManager:            lockManager,
		transactor:             transactor,
		reservationTTL:         reservationTTL,
		maxSeatsPerReservation: maxSeatsPerReservation,
	}
}

// reservationLockKey uses event_id:user_id (with a namespace prefix for Redis).
func reservationLockKey(eventID, userID string) string {
	return "reservation:" + eventID + ":" + userID
}

// reservationLockValue is the Redis string value; userID is only in the key (reservation:eventId:userId).
func reservationLockValue(quantity int, bookingID string) string {
	return strconv.Itoa(quantity) + "|" + bookingID
}

func (s *bookingService) Reserve(ctx context.Context, userID, eventID string, quantity int) (*entities.Booking, error) {
	if quantity <= 0 {
		quantity = 1
	}
	if quantity > s.maxSeatsPerReservation {
		s.writeAudit(ctx, entities.AuditActionBookingCreated, entities.AuditOutcomeFailure,
			"", userID, intPtr(quantity), map[string]any{
				"reason": "quantity exceeds maximum per reservation", "eventId": eventID, "max": s.maxSeatsPerReservation,
			})
		return nil, entities.ErrInvalidQuantity
	}

	bookingID := uuid.New().String()
	lockKey := reservationLockKey(eventID, userID)
	lockVal := reservationLockValue(quantity, bookingID)

	ok, lockErr := s.lockManager.Acquire(ctx, lockKey, lockVal, s.reservationTTL)
	if lockErr != nil {
		s.writeAudit(ctx, entities.AuditActionBookingCreated, entities.AuditOutcomeFailure,
			"", userID, intPtr(quantity), map[string]any{"reason": lockErr.Error(), "eventId": eventID})
		return nil, fmt.Errorf("acquire reservation lock: %w", lockErr)
	}
	if !ok {
		s.writeAudit(ctx, entities.AuditActionBookingCreated, entities.AuditOutcomeFailure,
			"", userID, intPtr(quantity), map[string]any{"reason": "reservation lock already held", "eventId": eventID})
		return nil, entities.ErrConflict
	}

	lockAcquired := true
	defer func() {
		if lockAcquired {
			_ = s.lockManager.Release(ctx, lockKey, lockVal)
		}
	}()

	var booking *entities.Booking
	txErr := s.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := s.bookingRepo.CancelExpiredReservations(txCtx); err != nil {
			return err
		}

		ok, err := s.eventRepo.TryAddReservedSlots(txCtx, eventID, quantity)
		if err != nil {
			if errors.Is(err, entities.ErrInsufficientCapacity) {
				s.writeAudit(txCtx, entities.AuditActionBookingCreated, entities.AuditOutcomeFailure,
					"", userID, intPtr(quantity), map[string]any{"reason": "not enough available seats", "eventId": eventID})
				return entities.ErrInsufficientCapacity
			}
			return err
		}
		if !ok {
			return fmt.Errorf("try add reserved slots: unexpected outcome")
		}

		has, err := s.bookingRepo.HasActiveReservedBookingForUserEvent(txCtx, userID, eventID)
		if err != nil {
			return err
		}
		if has {
			if _, relErr := s.eventRepo.ReleaseReservedSlots(txCtx, eventID, quantity); relErr != nil {
				return relErr
			}
			s.writeAudit(txCtx, entities.AuditActionBookingCreated, entities.AuditOutcomeFailure,
				"", userID, intPtr(quantity), map[string]any{"reason": "active reservation exists for event", "eventId": eventID})
			return entities.ErrConflict
		}

		now := time.Now()
		expiresAt := now.Add(s.reservationTTL)
		b := &entities.Booking{
			ID:        bookingID,
			UserID:    userID,
			EventID:   eventID,
			Quantity:  quantity,
			Status:    entities.BookingStatusReserved,
			ExpiresAt: &expiresAt,
			CreatedAt: now,
			UpdatedAt: now,
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

	lockAcquired = false
	s.writeAudit(ctx, entities.AuditActionBookingCreated, entities.AuditOutcomeSuccess,
		booking.ID, userID, intPtr(quantity), map[string]any{
			"bookingId": booking.ID,
			"eventId":   booking.EventID,
		})

	go s.scheduleReservationUnlock(booking.ID)

	return booking, nil
}

// scheduleReservationUnlock runs after the reservation TTL (aligned with Redis lock expiry).
// If the booking is still RESERVED and expired, it cancels the row and decrements reserved_slots.
// Confirm/Cancel paths update inventory earlier; this is idempotent.
func (s *bookingService) scheduleReservationUnlock(bookingID string) {
	timer := time.NewTimer(s.reservationTTL)
	defer timer.Stop()
	<-timer.C

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := s.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		eid, qty, ok, err := s.bookingRepo.CancelReservationIfExpired(txCtx, bookingID)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		released, err := s.eventRepo.ReleaseReservedSlots(txCtx, eid, qty)
		if err != nil {
			return err
		}
		if !released {
			return fmt.Errorf("release reserved slots after expiry: inventory mismatch for event %s", eid)
		}
		return nil
	})
	if err != nil {
		slog.Error("reservation unlock failed", "bookingId", bookingID, "error", err)
	}
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
	wantVal := reservationLockValue(booking.Quantity, bookingID)
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
		ok, uerr := s.eventRepo.TransferReservedToBooked(txCtx, booking.EventID, booking.Quantity)
		if uerr != nil {
			return uerr
		}
		if !ok {
			return fmt.Errorf("transfer reserved to booked: inventory mismatch for event %s", booking.EventID)
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
	wasConfirmed := booking.Status == entities.BookingStatusConfirmed

	err = s.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := s.bookingRepo.UpdateStatus(txCtx, bookingID, entities.BookingStatusCancelled); err != nil {
			return err
		}
		if wasReserved {
			ok, err := s.eventRepo.ReleaseReservedSlots(txCtx, booking.EventID, booking.Quantity)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("release reserved slots: inventory mismatch for event %s", booking.EventID)
			}
		}
		if wasConfirmed {
			ok, err := s.eventRepo.ReleaseBookedSlots(txCtx, booking.EventID, booking.Quantity)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("release booked slots: inventory mismatch for event %s", booking.EventID)
			}
		}
		return nil
	})
	if err != nil {
		s.writeAudit(ctx, entities.AuditActionBookingCancelled, entities.AuditOutcomeFailure,
			bookingID, userID, intPtr(booking.Quantity), map[string]any{"bookingId": bookingID, "reason": err.Error()})
		return err
	}

	if wasReserved {
		lockKey := reservationLockKey(booking.EventID, userID)
		lockVal := reservationLockValue(booking.Quantity, bookingID)
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
