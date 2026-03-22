package main

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ticket/internal/ports"
)

type BookingHandler struct {
	bookingService ports.BookingService
}

func NewBookingHandler(bookingService ports.BookingService) *BookingHandler {
	return &BookingHandler{bookingService: bookingService}
}

type reserveRequest struct {
	EventID  string `json:"eventId" binding:"required"`
	Quantity int    `json:"quantity"`
}

// POST /booking/reserve
func (h *BookingHandler) Reserve(c *gin.Context) {
	userID := c.GetHeader("X-User-ID")
	if userID == "" {
		respondError(c, http.StatusBadRequest, "X-User-ID header is required")
		return
	}

	var req reserveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	booking, err := h.bookingService.Reserve(c.Request.Context(), userID, req.EventID, req.Quantity)
	if err != nil {
		handleError(c, err)
		return
	}

	c.JSON(http.StatusCreated, booking)
}

type confirmRequest struct {
	BookingID      string      `json:"bookingId" binding:"required"`
	PaymentDetails interface{} `json:"paymentDetails"`
}

// POST /booking/confirm
func (h *BookingHandler) Confirm(c *gin.Context) {
	userID := c.GetHeader("X-User-ID")
	if userID == "" {
		respondError(c, http.StatusBadRequest, "X-User-ID header is required")
		return
	}

	var req confirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	booking, err := h.bookingService.Confirm(c.Request.Context(), userID, req.BookingID)
	if err != nil {
		handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, booking)
}

// DELETE /booking/:bookingId
func (h *BookingHandler) Cancel(c *gin.Context) {
	userID := c.GetHeader("X-User-ID")
	if userID == "" {
		respondError(c, http.StatusBadRequest, "X-User-ID header is required")
		return
	}

	bookingID := c.Param("bookingId")

	if err := h.bookingService.Cancel(c.Request.Context(), userID, bookingID); err != nil {
		handleError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// GET /booking/mine
func (h *BookingHandler) GetMyBookings(c *gin.Context) {
	userID := c.GetHeader("X-User-ID")
	if userID == "" {
		respondError(c, http.StatusBadRequest, "X-User-ID header is required")
		return
	}

	bookings, err := h.bookingService.GetUserBookings(c.Request.Context(), userID)
	if err != nil {
		handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"bookings": bookings})
}
