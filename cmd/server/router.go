package main

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"ticket/internal/ports"
)

func NewRouter(
	eventService ports.EventService,
	bookingService ports.BookingService,
	userService ports.UserService,
) *gin.Engine {
	if mode := os.Getenv("GIN_MODE"); mode != "" {
		gin.SetMode(mode)
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(CORSMiddleware())
	router.Use(RequestLogger())

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	eventHandler := NewEventHandler(eventService)
	router.GET("/events", eventHandler.ListEvents)
	router.GET("/events/:eventId", eventHandler.GetEvent)

	bookingHandler := NewBookingHandler(bookingService)
	router.POST("/booking/reserve", bookingHandler.Reserve)
	router.POST("/booking/confirm", bookingHandler.Confirm)
	router.DELETE("/booking/:bookingId", bookingHandler.Cancel)
	router.GET("/booking/mine", bookingHandler.GetMyBookings)

	userHandler := NewUserHandler(userService)
	router.GET("/users", userHandler.ListUsers)

	return router
}
