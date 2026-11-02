package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/logging"
)

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		path := c.Request.URL.Path
		id := uuid.NewString()
		c.Header("X-Request-ID", id)
		ctx := logging.WithAttrs(c.Request.Context(), "request_id", id)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
		level := slog.LevelInfo
		if c.Writer.Status() >= 500 {
			level = slog.LevelError
		} else if c.Writer.Status() >= 400 {
			level = slog.LevelWarn
		} else if path == "/api/health" {
			level = slog.LevelDebug
		}
		logging.FromContext(ctx).Log(ctx, level, "http request",
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(started).Milliseconds(),
			"response_size_bytes", c.Writer.Size(),
		)
	}
}

func recoveryLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				var reason string
				switch value := recovered.(type) {
				case error:
					reason = value.Error()
				case string:
					reason = value
				default:
					reason = "panic with non-error value"
				}
				logging.FromContext(c.Request.Context()).ErrorContext(c.Request.Context(), "http handler panic",
					"method", c.Request.Method,
					"path", c.Request.URL.Path,
					"panic_type", fmt.Sprintf("%T", recovered),
					"error", logging.Sanitize(reason),
					"stack", string(debug.Stack()),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			}
		}()
		c.Next()
	}
}
