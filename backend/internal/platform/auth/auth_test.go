package auth

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"unite-mansion/internal/platform/httpx"
	"unite-mansion/internal/shared"
)

type fakeAuth map[string]Principal

func (f fakeAuth) Authenticate(_ context.Context, token string) (Principal, error) {
	if p, ok := f[token]; ok {
		return p, nil
	}
	return Principal{}, ErrUnauthenticated
}

func newRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := gin.New()
	r.ContextWithFallback = true
	r.Use(
		httpx.Errors(log, ErrorMappings()...),
		Middleware(fakeAuth{
			"admin":   {Role: RoleAdmin, AccountID: "a1"},
			"active":  {Role: RoleTenant, AccountID: "t1", TenantStatus: "ACTIVE", RoomID: "r1"},
			"pending": {Role: RoleTenant, AccountID: "t2", TenantStatus: "PENDING"},
		}),
		Guard("/api/v1"),
	)
	ok := func(c *gin.Context) {
		a, _ := shared.ActorFrom(c)
		c.String(http.StatusOK, string(a.Type)+":"+a.ID)
	}
	r.GET("/api/v1/admin/rooms", ok)
	r.GET("/api/v1/tenant/room", ok)
	r.GET("/api/v1/auth/me", ok)
	r.GET("/api/v1/health", ok)
	return r
}

func call(r *gin.Engine, path, token string) (int, string, string) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var p struct{ Code string }
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	return w.Code, p.Code, w.Body.String()
}

func TestGuard(t *testing.T) {
	r := newRouter()
	tests := []struct {
		path, token string
		wantStatus  int
		wantCode    string
	}{
		{"/api/v1/admin/rooms", "", 401, "UNAUTHENTICATED"},
		{"/api/v1/admin/rooms", "bogus", 401, "UNAUTHENTICATED"},
		{"/api/v1/admin/rooms", "active", 403, "FORBIDDEN"},
		{"/api/v1/admin/rooms", "admin", 200, ""},
		{"/api/v1/tenant/room", "", 401, "UNAUTHENTICATED"},
		{"/api/v1/tenant/room", "admin", 403, "FORBIDDEN"},
		{"/api/v1/tenant/room", "pending", 403, "ACCOUNT_NOT_ACTIVE"},
		{"/api/v1/tenant/room", "active", 200, ""},
		{"/api/v1/auth/me", "", 401, "UNAUTHENTICATED"},
		{"/api/v1/auth/me", "pending", 200, ""},
		{"/api/v1/health", "", 200, ""},
	}
	for _, tt := range tests {
		t.Run(tt.path+"/"+tt.token, func(t *testing.T) {
			status, code, _ := call(r, tt.path, tt.token)
			if status != tt.wantStatus || code != tt.wantCode {
				t.Fatalf("got %d %q, want %d %q", status, code, tt.wantStatus, tt.wantCode)
			}
		})
	}
}

func TestMiddlewareSetsActor(t *testing.T) {
	r := newRouter()
	if _, _, body := call(r, "/api/v1/admin/rooms", "admin"); body != "ADMIN:a1" {
		t.Fatalf("admin actor = %q", body)
	}
	if _, _, body := call(r, "/api/v1/tenant/room", "active"); body != "TENANT:t1" {
		t.Fatalf("tenant actor = %q", body)
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	c := SessionCookie("tok", testTime(), true)
	for _, want := range []string{"bm_session=tok", "Path=/api", "HttpOnly", "Secure", "SameSite=Lax"} {
		if !strings.Contains(c, want) {
			t.Errorf("cookie %q missing %q", c, want)
		}
	}
	if strings.Contains(SessionCookie("tok", testTime(), false), "Secure") {
		t.Error("Secure set when secure=false")
	}
	if !strings.Contains(ClearCookie(false), "Max-Age=0") {
		t.Errorf("clear cookie = %q", ClearCookie(false))
	}
}

func testTime() time.Time { return time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC) }
