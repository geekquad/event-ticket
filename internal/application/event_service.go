package application

import (
	"context"

	"ticket/internal/entities"
	"ticket/internal/ports"
)

type eventService struct {
	eventRepo  ports.EventRepository
	ticketRepo ports.TicketRepository
}

func NewEventService(eventRepo ports.EventRepository, ticketRepo ports.TicketRepository) ports.EventService {
	return &eventService{
		eventRepo:  eventRepo,
		ticketRepo: ticketRepo,
	}
}

func (s *eventService) GetEvent(ctx context.Context, eventID string) (*entities.Event, error) {
	event, err := s.eventRepo.GetByID(ctx, eventID)
	if err != nil {
		return nil, err
	}

	tickets, err := s.ticketRepo.GetByEventID(ctx, eventID)
	if err != nil {
		return nil, err
	}
	event.Tickets = tickets

	return event, nil
}

func (s *eventService) ListEvents(ctx context.Context, params ports.EventSearchParams) ([]entities.Event, int, error) {
	return s.eventRepo.List(ctx, params)
}
