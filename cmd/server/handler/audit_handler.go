package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"ticket/internal/ports"
)

type AuditHandler struct {
	auditService ports.AuditService
}

func NewAuditHandler(auditService ports.AuditService) *AuditHandler {
	return &AuditHandler{auditService: auditService}
}

// GET /audit/logs?limit=&eventId=
func (h *AuditHandler) ListAuditLogs(c *gin.Context) {
	limit := 0
	if q := c.Query("limit"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 0 {
			respondError(c, http.StatusBadRequest, "invalid limit query parameter")
			return
		}
		limit = n
	}

	var eventID *string
	if raw := c.Query("eventId"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid eventId query parameter")
			return
		}
		s := parsed.String()
		eventID = &s
	}

	logs, err := h.auditService.ListRecent(c.Request.Context(), limit, eventID)
	if err != nil {
		handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"logs": logs})
}
