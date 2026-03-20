package main

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"ticket/internal/ports"
)

type EventHandler struct {
	eventService ports.EventService
}

func NewEventHandler(eventService ports.EventService) *EventHandler {
	return &EventHandler{eventService: eventService}
}

// GET /events/:eventId
func (h *EventHandler) GetEvent(c *gin.Context) {
	eventID := c.Param("eventId")

	event, err := h.eventService.GetEvent(c.Request.Context(), eventID)
	if err != nil {
		handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, event)
}

// GET /events?keyword=&start=&end=&page=&pageSize=
func (h *EventHandler) ListEvents(c *gin.Context) {
	params := ports.EventSearchParams{
		Keyword: c.Query("keyword"),
	}

	if startStr := c.Query("start"); startStr != "" {
		t, err := time.Parse(time.RFC3339, startStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid start date format, use RFC3339")
			return
		}
		params.StartDate = &t
	}

	if endStr := c.Query("end"); endStr != "" {
		t, err := time.Parse(time.RFC3339, endStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid end date format, use RFC3339")
			return
		}
		params.EndDate = &t
	}

	page := 1
	if pageStr := c.Query("page"); pageStr != "" {
		p, err := strconv.Atoi(pageStr)
		if err != nil || p < 1 {
			respondError(c, http.StatusBadRequest, "invalid page parameter")
			return
		}
		page = p
	}
	params.Page = page

	pageSize := 20
	if pageSizeStr := c.Query("pageSize"); pageSizeStr != "" {
		ps, err := strconv.Atoi(pageSizeStr)
		if err != nil || ps < 1 {
			respondError(c, http.StatusBadRequest, "invalid pageSize parameter")
			return
		}
		pageSize = ps
	}
	params.PageSize = pageSize

	events, total, err := h.eventService.ListEvents(c.Request.Context(), params)
	if err != nil {
		handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"events":   events,
		"total":    total,
		"page":     params.Page,
		"pageSize": params.PageSize,
	})
}
