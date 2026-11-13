package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	requestIDHeader = "X-Request-ID"
	requestIDKey    = "requestID"
)

// RequestIDMiddleware reuses an incoming X-Request-ID or generates one.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if id == "" || len(id) > 64 {
			id = newRequestID()
		}
		c.Set(requestIDKey, id)
		c.Header(requestIDHeader, id)
		c.Next()
	}
}

// RequestID returns the ID assigned by RequestIDMiddleware.
func RequestID(c *gin.Context) string {
	return c.GetString(requestIDKey)
}

func newRequestID() string {
	b := make([]byte, 8)
	// crypto/rand.Read never returns an error (documented since Go 1.24).
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Recovery turns a panic during a request into a logged 500.
func Recovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("panic recovered",
					"requestId", RequestID(c),
					"method", c.Request.Method,
					"path", c.FullPath(),
					"panic", fmt.Sprint(r),
				)
				writeProblem(c, http.StatusInternalServerError, CodeInternal, "")
			}
		}()
		c.Next()
	}
}

// Errors is the single place that converts handler errors (c.Error) into
// Problem Details and logs the request. Handlers call c.Error(err) and return.
func Errors(log *slog.Logger, mappings ...ErrorMapping) gin.HandlerFunc {
	m := mapper(mappings)
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		attrs := []any{
			"requestId", RequestID(c),
			"method", c.Request.Method,
			"path", c.FullPath(),
			"durationMs", time.Since(start).Milliseconds(),
		}

		last := c.Errors.Last()
		if last == nil {
			log.Info("request", append(attrs, "status", c.Writer.Status())...)
			return
		}

		status, code := m.resolve(last.Err)
		attrs = append(attrs, "status", status, "code", code, "error", last.Err.Error())
		if status >= http.StatusInternalServerError {
			log.Error("request failed", attrs...)
		} else {
			log.Warn("request rejected", attrs...)
		}

		if c.Writer.Written() {
			return
		}
		// Only validation messages are safe to echo; domain and internal error
		// text may contain IDs or DB details, and the frontend maps Code instead.
		detail := ""
		if status == http.StatusBadRequest {
			detail = last.Err.Error()
		}
		writeProblem(c, status, code, detail)
	}
}
