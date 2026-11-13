// Package auth loads the session principal for each request and guards the
// /admin and /tenant route trees (context.md §6.5, §7 Auth).
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"unite-mansion/internal/platform/httpx"
	"unite-mansion/internal/shared"
)

const CookieName = "bm_session"

type Role string

const (
	RoleAdmin  Role = "ADMIN"
	RoleTenant Role = "TENANT"
)

var (
	ErrUnauthenticated  = errors.New("not logged in")
	ErrForbidden        = errors.New("not allowed for this role")
	ErrAccountNotActive = errors.New("tenant account is not active")
)

// ErrorMappings for the httpx.Errors middleware.
func ErrorMappings() []httpx.ErrorMapping {
	return []httpx.ErrorMapping{
		{Err: ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "UNAUTHENTICATED"},
		{Err: ErrForbidden, Status: http.StatusForbidden, Code: "FORBIDDEN"},
		{Err: ErrAccountNotActive, Status: http.StatusForbidden, Code: "ACCOUNT_NOT_ACTIVE"},
	}
}

// Principal is the authenticated user of a request.
type Principal struct {
	Role         Role
	AccountID    string
	Username     string
	TenantStatus string // PENDING | ACTIVE | INACTIVE, tenants only
	RoomID       string // set only for ACTIVE tenants
	SessionHash  []byte
}

func (p Principal) IsActiveTenant() bool {
	return p.Role == RoleTenant && p.TenantStatus == "ACTIVE" && p.RoomID != ""
}

// Authenticator resolves a session token. It returns ErrUnauthenticated for
// unknown or expired tokens.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (Principal, error)
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// Middleware loads the principal from the session cookie, if any, and puts it
// (and the matching shared.Actor for audit) into the request context.
// Requires gin's ContextWithFallback so handlers see request-context values.
func Middleware(a Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(CookieName)
		if err != nil || token == "" {
			c.Next()
			return
		}
		p, err := a.Authenticate(c.Request.Context(), token)
		if errors.Is(err, ErrUnauthenticated) {
			c.Next()
			return
		}
		if err != nil {
			c.Error(err)
			c.Abort()
			return
		}
		actor := shared.Actor{Type: shared.ActorAdmin, ID: p.AccountID}
		if p.Role == RoleTenant {
			actor.Type = shared.ActorTenant
		}
		ctx := shared.WithActor(WithPrincipal(c.Request.Context(), p), actor)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// Guard enforces access by route prefix so every new endpoint under /admin or
// /tenant is protected by default.
func Guard(basePath string) gin.HandlerFunc {
	adminPrefix := basePath + "/admin/"
	tenantPrefix := basePath + "/tenant/"
	sessionOnly := map[string]bool{basePath + "/auth/me": true, basePath + "/auth/logout": true}

	return func(c *gin.Context) {
		path := c.Request.URL.Path
		p, loggedIn := FromContext(c.Request.Context())

		var err error
		switch {
		case strings.HasPrefix(path, adminPrefix):
			err = require(loggedIn, p.Role == RoleAdmin, nil)
		case strings.HasPrefix(path, tenantPrefix):
			err = require(loggedIn, p.Role == RoleTenant, func() error {
				if !p.IsActiveTenant() {
					return ErrAccountNotActive
				}
				return nil
			})
		case sessionOnly[path]:
			err = require(loggedIn, true, nil)
		}
		if err != nil {
			c.Error(err)
			c.Abort()
			return
		}
		c.Next()
	}
}

func require(loggedIn, roleOK bool, extra func() error) error {
	if !loggedIn {
		return ErrUnauthenticated
	}
	if !roleOK {
		return ErrForbidden
	}
	if extra != nil {
		return extra()
	}
	return nil
}

// SessionCookie builds the Set-Cookie value for a new session. The cookie is
// httpOnly, SameSite=Lax (blocks cross-site POSTs) and scoped to the API.
func SessionCookie(token string, expires time.Time, secure bool) string {
	return (&http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/api",
		Expires:  expires.UTC(),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}).String()
}

// ClearCookie builds the Set-Cookie value that removes the session cookie.
func ClearCookie(secure bool) string {
	return (&http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/api",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}).String()
}
