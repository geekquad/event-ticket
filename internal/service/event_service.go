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

func (s *eventService) ListEvents(ctx context.Context) ([]entities.Event, error) {
	return s.eventRepo.List(ctx)
}
