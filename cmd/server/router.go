package main

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"ticket/internal/ports"
)

func NewRouter(
	eventService ports.EventService,
	bookingService ports.BookingService,
	userService ports.UserService,
	frontendDir string,
) *gin.Engine {
	if mode := os.Getenv("GIN_MODE"); mode != "" {
		gin.SetMode(mode)
	}

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.Use(CORSMiddleware())

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	eventHandler := NewEventHandler(eventService)
	router.GET("/events", eventHandler.ListEvents)

	bookingHandler := NewBookingHandler(bookingService)
	router.POST("/booking/reserve", bookingHandler.Reserve)
	router.POST("/booking/confirm", bookingHandler.Confirm)
	router.DELETE("/booking/:bookingId", bookingHandler.Cancel)
	router.GET("/booking/mine", bookingHandler.GetMyBookings)

	userHandler := NewUserHandler(userService)
	router.GET("/users", userHandler.ListUsers)

	// Serve frontend
	if frontendDir != "" {
		indexPath := filepath.Join(frontendDir, "index.html")
		if _, err := os.Stat(indexPath); err != nil {
			slog.Warn("frontend not found; set FRONTEND_DIR to the directory containing index.html",
				"path", indexPath, "error", err)
		} else {
			slog.Info("serving frontend", "dir", frontendDir)
			router.GET("/", func(c *gin.Context) {
				c.File(indexPath)
			})
			router.StaticFile("/styles.css", filepath.Join(frontendDir, "styles.css"))
			router.StaticFile("/app.js", filepath.Join(frontendDir, "app.js"))
		}
	}

	return router
}
