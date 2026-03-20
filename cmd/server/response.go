package main

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"ticket/internal/entities"
)

type errorResponse struct {
	Error string `json:"error"`
}

func handleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, entities.ErrNotFound):
		respondError(c, http.StatusNotFound, "resource not found")
	case errors.Is(err, entities.ErrTicketUnavailable):
		respondError(c, http.StatusConflict, "ticket unavailable")
	case errors.Is(err, entities.ErrUnauthorized):
		respondError(c, http.StatusForbidden, "unauthorized")
	case errors.Is(err, entities.ErrConflict):
		respondError(c, http.StatusConflict, "conflict")
	default:
		slog.Error("internal server error", "error", err)
		respondError(c, http.StatusInternalServerError, "internal server error")
	}
}

func respondError(c *gin.Context, status int, message string) {
	c.JSON(status, errorResponse{Error: message})
}
