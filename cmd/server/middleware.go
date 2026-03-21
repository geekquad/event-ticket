package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Reflect Origin so preflight + custom headers work; * is invalid with credentials.
		if o := c.Request.Header.Get("Origin"); o != "" {
			c.Header("Access-Control-Allow-Origin", o)
		} else {
			c.Header("Access-Control-Allow-Origin", "*")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, X-User-ID, Accept, Authorization")
		c.Header("Access-Control-Expose-Headers", "Content-Type")

		// Chrome: requests to loopback from another loopback host may send this preflight header.
		if c.Request.Header.Get("Access-Control-Request-Private-Network") == "true" {
			c.Header("Access-Control-Allow-Private-Network", "true")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
