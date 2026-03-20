package ports

import (
	"context"
	"time"

	"ticket/internal/entities"
)

type EventSearchParams struct {
	Keyword   string
	StartDate *time.Time
	EndDate   *time.Time
	Page      int
	PageSize  int
}

type EventRepository interface {
	GetByID(ctx context.Context, id string) (*entities.Event, error)
	List(ctx context.Context, params EventSearchParams) ([]entities.Event, int, error)
}

type TicketRepository interface {
	GetByEventID(ctx context.Context, eventID string) ([]entities.Ticket, error)
	GetAvailableByEventID(ctx context.Context, eventID string, limit int) ([]entities.Ticket, error)
	GetByIDs(ctx context.Context, ids []string) ([]entities.Ticket, error)
	// BulkUpdateStatus returns the number of rows actually updated.
	// When setting BOOKED it only updates tickets that are still AVAILABLE,
	// so the caller can detect a race (n < len(ticketIDs) means another confirm won).
	BulkUpdateStatus(ctx context.Context, ticketIDs []string, status entities.TicketStatus, bookingID *string) (int64, error)
}

type BookingRepository interface {
	Create(ctx context.Context, booking *entities.Booking) error
	GetByID(ctx context.Context, id string) (*entities.Booking, error)
	GetByUserID(ctx context.Context, userID string) ([]entities.Booking, error)
	UpdateStatus(ctx context.Context, bookingID string, status entities.BookingStatus) error
	// CancelExpiredReservations marks RESERVED bookings whose expires_at has passed as CANCELLED.
	CancelExpiredReservations(ctx context.Context) error
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
