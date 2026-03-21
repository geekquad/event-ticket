package ports

import (
	"context"

	"ticket/internal/entities"
)

type EventRepository interface {
	List(ctx context.Context) ([]entities.Event, error)
	// LockEventCapacity locks the event row and returns the venue's capacity for headcount checks.
	LockEventCapacity(ctx context.Context, eventID string) (capacity int, err error)
}

type BookingRepository interface {
	Create(ctx context.Context, booking *entities.Booking) error
	GetByID(ctx context.Context, id string) (*entities.Booking, error)
	GetByUserID(ctx context.Context, userID string) ([]entities.Booking, error)
	UpdateStatus(ctx context.Context, bookingID string, status entities.BookingStatus) error
	// ConfirmReservation sets CONFIRMED only when the row is still RESERVED (returns rows affected).
	ConfirmReservation(ctx context.Context, bookingID string) (int64, error)
	// CancelExpiredReservations marks RESERVED bookings whose expires_at has passed as CANCELLED.
	CancelExpiredReservations(ctx context.Context) error
	// SumAllocatedQuantityForEvent sums quantities for CONFIRMED and non-expired RESERVED bookings.
	SumAllocatedQuantityForEvent(ctx context.Context, eventID string) (int, error)
	// HasActiveReservedBookingForUserEvent is true if the user has a RESERVED booking for the event that is not expired.
	HasActiveReservedBookingForUserEvent(ctx context.Context, userID, eventID string) (bool, error)
}

type AuditRepository interface {
	Log(ctx context.Context, entry *entities.AuditLog) error
}

type UserRepository interface {
	List(ctx context.Context) ([]entities.User, error)
	GetByID(ctx context.Context, id string) (*entities.User, error)
}

type Transactor interface {
	WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}
