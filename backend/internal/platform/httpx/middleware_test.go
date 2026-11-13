package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

var errTestNotDraft = errors.New("invoice is not draft")

func newTestRouter(handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := gin.New()
	r.Use(
		RequestIDMiddleware(),
		Errors(log, ErrorMapping{Err: errTestNotDraft, Status: http.StatusConflict, Code: "INVOICE_NOT_DRAFT"}),
		Recovery(log),
	)
	r.NoRoute(NotFound)
	r.GET("/x", handler)
	return r
}

func do(t *testing.T, r *gin.Engine, path string) (*httptest.ResponseRecorder, Problem) {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	var p Problem
	if strings.HasPrefix(w.Header().Get("Content-Type"), problemContentType) {
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatalf("decode problem: %v", err)
		}
	}
	return w, p
}

func TestErrorsMapsWrappedDomainError(t *testing.T) {
	r := newTestRouter(func(c *gin.Context) {
		c.Error(fmt.Errorf("issue invoice 42: %w", errTestNotDraft))
	})
	w, p := do(t, r, "/x")

	if w.Code != http.StatusConflict || p.Code != "INVOICE_NOT_DRAFT" {
		t.Fatalf("got %d %q, want 409 INVOICE_NOT_DRAFT", w.Code, p.Code)
	}
	if p.Detail != "" {
		t.Fatalf("domain error leaked detail %q", p.Detail)
	}
	if p.RequestID == "" || p.RequestID != w.Header().Get(requestIDHeader) {
		t.Fatalf("requestId %q does not match header", p.RequestID)
	}
}

func TestErrorsHidesUnknownErrors(t *testing.T) {
	r := newTestRouter(func(c *gin.Context) {
		c.Error(errors.New(`pq: relation "invoices" does not exist`))
	})
	w, p := do(t, r, "/x")

	if w.Code != http.StatusInternalServerError || p.Code != CodeInternal {
		t.Fatalf("got %d %q, want 500 INTERNAL_ERROR", w.Code, p.Code)
	}
	if strings.Contains(w.Body.String(), "invoices") {
		t.Fatalf("internal error text leaked: %s", w.Body.String())
	}
}

func TestErrorsBadRequestKeepsDetail(t *testing.T) {
	r := newTestRouter(func(c *gin.Context) {
		RequestError(c, errors.New("period must be yyyy-mm"))
	})
	w, p := do(t, r, "/x")

	if w.Code != http.StatusBadRequest || p.Code != CodeBadRequest || !strings.Contains(p.Detail, "yyyy-mm") {
		t.Fatalf("got %d %q %q", w.Code, p.Code, p.Detail)
	}
}

func TestRecoveryReturnsProblem(t *testing.T) {
	r := newTestRouter(func(*gin.Context) { panic("boom") })
	w, p := do(t, r, "/x")

	if w.Code != http.StatusInternalServerError || p.Code != CodeInternal {
		t.Fatalf("got %d %q, want 500 INTERNAL_ERROR", w.Code, p.Code)
	}
}

func TestNotFoundReturnsProblem(t *testing.T) {
	r := newTestRouter(func(*gin.Context) {})
	w, p := do(t, r, "/nope")

	if w.Code != http.StatusNotFound || p.Code != CodeNotFound {
		t.Fatalf("got %d %q, want 404 NOT_FOUND", w.Code, p.Code)
	}
}
