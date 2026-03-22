package ports

import (
	"context"

	"ticket/internal/entities"
)

type EventRepository interface {
	List(ctx context.Context) ([]entities.Event, error)
	// TryAddReservedSlots increments reserved_slots when capacity allows. Returns (true, nil) on success;
	// (false, ErrInsufficientCapacity) if the event exists but not enough seats; (false, ErrNotFound) if no event.
	TryAddReservedSlots(ctx context.Context, eventID string, quantity int) (ok bool, err error)
	// TransferReservedToBooked moves quantity from reserved to booked (confirm path).
	TransferReservedToBooked(ctx context.Context, eventID string, quantity int) (ok bool, err error)
	// ReleaseReservedSlots decrements reserved_slots (cancel reserved or expiry).
	ReleaseReservedSlots(ctx context.Context, eventID string, quantity int) (ok bool, err error)
	// ReleaseBookedSlots decrements booked_slots (cancel confirmed).
	ReleaseBookedSlots(ctx context.Context, eventID string, quantity int) (ok bool, err error)
}

type BookingRepository interface {
	Create(ctx context.Context, booking *entities.Booking) error
	GetByID(ctx context.Context, id string) (*entities.Booking, error)
	GetByUserID(ctx context.Context, userID string) ([]entities.Booking, error)
	UpdateStatus(ctx context.Context, bookingID string, status entities.BookingStatus) error
	// ConfirmReservation sets CONFIRMED only when the row is still RESERVED (returns rows affected).
	ConfirmReservation(ctx context.Context, bookingID string) (int64, error)
	// CancelExpiredReservations marks RESERVED bookings whose expires_at has passed as CANCELLED and updates event counters.
	CancelExpiredReservations(ctx context.Context) error
	// CancelReservationIfExpired cancels a single RESERVED booking if expired (caller must decrement reserved_slots in same tx).
	CancelReservationIfExpired(ctx context.Context, bookingID string) (eventID string, quantity int, ok bool, err error)
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
