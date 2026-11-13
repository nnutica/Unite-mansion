// Package httpx holds the shared Gin middleware: request ID, access/error
// logging, panic recovery and RFC 9457 Problem Details responses.
package httpx

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

const problemContentType = "application/problem+json"

// Problem is an RFC 9457 Problem Details body. Code is the stable
// UPPER_SNAKE_CASE identifier the frontend maps to a Thai message.
type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Code      string `json:"code"`
	Detail    string `json:"detail,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

// ErrorMapping maps a domain error (matched with errors.Is) to an HTTP status and code.
// Each bounded context exposes its own mappings and cmd/api wires them in.
type ErrorMapping struct {
	Err    error
	Status int
	Code   string
}

// Codes for errors raised by the HTTP layer itself rather than a domain.
const (
	CodeInternal   = "INTERNAL_ERROR"
	CodeNotFound   = "NOT_FOUND"
	CodeBadRequest = "BAD_REQUEST"
)

// ErrBadRequest marks request validation/binding failures.
var ErrBadRequest = errors.New("bad request")

type mapper []ErrorMapping

func (m mapper) resolve(err error) (int, string) {
	if errors.Is(err, ErrBadRequest) {
		return http.StatusBadRequest, CodeBadRequest
	}
	for _, em := range m {
		if errors.Is(err, em.Err) {
			return em.Status, em.Code
		}
	}
	return http.StatusInternalServerError, CodeInternal
}

func writeProblem(c *gin.Context, status int, code, detail string) {
	c.Header("Content-Type", problemContentType)
	c.AbortWithStatusJSON(status, Problem{
		Type:      "about:blank",
		Title:     http.StatusText(status),
		Status:    status,
		Code:      code,
		Detail:    detail,
		RequestID: RequestID(c),
	})
}

// NotFound answers unknown routes with a Problem body.
func NotFound(c *gin.Context) {
	writeProblem(c, http.StatusNotFound, CodeNotFound, "")
}
