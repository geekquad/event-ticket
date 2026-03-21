package main

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ticket/internal/ports"
)

type EventHandler struct {
	eventService ports.EventService
}

func NewEventHandler(eventService ports.EventService) *EventHandler {
	return &EventHandler{eventService: eventService}
}

// GET /events
func (h *EventHandler) ListEvents(c *gin.Context) {
	events, err := h.eventService.ListEvents(c.Request.Context())
	if err != nil {
		handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"events": events})
}
