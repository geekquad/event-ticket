package ports

import (
	"context"

	"ticket/internal/entities"
)

type EventService interface {
	ListEvents(ctx context.Context) ([]entities.Event, error)
}

type BookingService interface {
	Reserve(ctx context.Context, userID, eventID string, quantity int) (*entities.Booking, error)
	Confirm(ctx context.Context, userID, bookingID string) (*entities.Booking, error)
	Cancel(ctx context.Context, userID, bookingID string) error
	GetUserBookings(ctx context.Context, userID string) ([]entities.Booking, error)
}

type UserService interface {
	ListUsers(ctx context.Context) ([]entities.User, error)
}
