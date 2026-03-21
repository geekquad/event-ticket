package service

import (
	"context"

	"ticket/internal/entities"
	"ticket/internal/ports"
)

type eventService struct {
	eventRepo ports.EventRepository
}

func NewEventService(eventRepo ports.EventRepository) ports.EventService {
	return &eventService{
		eventRepo: eventRepo,
	}
}

func (s *eventService) GetEvent(ctx context.Context, eventID string) (*entities.Event, error) {
	return s.eventRepo.GetByID(ctx, eventID)
}

func (s *eventService) ListEvents(ctx context.Context, params ports.EventSearchParams) ([]entities.Event, int, error) {
	return s.eventRepo.List(ctx, params)
}
