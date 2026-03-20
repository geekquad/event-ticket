package ports

import (
	"context"

	"ticket/internal/entities"
)

type EventService interface {
	GetEvent(ctx context.Context, eventID string) (*entities.Event, error)
	ListEvents(ctx context.Context, params EventSearchParams) ([]entities.Event, int, error)
}

type BookingService interface {
	Reserve(ctx context.Context, userID string, ticketIDs []string) (*entities.Booking, error)
	Confirm(ctx context.Context, userID, bookingID string) (*entities.Booking, error)
	Cancel(ctx context.Context, userID, bookingID string) error
	GetUserBookings(ctx context.Context, userID string) ([]entities.Booking, error)
}

type UserService interface {
	ListUsers(ctx context.Context) ([]entities.User, error)
}
