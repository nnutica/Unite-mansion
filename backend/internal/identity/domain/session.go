package domain

import "time"

type SubjectType string

const (
	SubjectAdmin  SubjectType = "ADMIN"
	SubjectTenant SubjectType = "TENANT"
)

// Session lifetimes (Decision log 2026-11-13): admin 12 hours fixed,
// tenant 30 days sliding.
const (
	AdminSessionTTL  = 12 * time.Hour
	TenantSessionTTL = 30 * 24 * time.Hour
	// A tenant session is extended at most once a day to limit writes.
	tenantRenewAfter = 24 * time.Hour
)

type Session struct {
	TokenHash   []byte
	SubjectType SubjectType
	SubjectID   string
	ExpiresAt   time.Time
}

func NewSession(tokenHash []byte, subject SubjectType, subjectID string, now time.Time) Session {
	ttl := AdminSessionTTL
	if subject == SubjectTenant {
		ttl = TenantSessionTTL
	}
	return Session{TokenHash: tokenHash, SubjectType: subject, SubjectID: subjectID, ExpiresAt: now.Add(ttl).UTC()}
}

func (s Session) Expired(now time.Time) bool { return !now.Before(s.ExpiresAt) }

// RenewedExpiry returns the new expiry for a sliding tenant session, or false
// when no renewal is due (admin sessions never slide).
func (s Session) RenewedExpiry(now time.Time) (time.Time, bool) {
	if s.SubjectType != SubjectTenant {
		return time.Time{}, false
	}
	if s.ExpiresAt.Sub(now) > TenantSessionTTL-tenantRenewAfter {
		return time.Time{}, false
	}
	return now.Add(TenantSessionTTL).UTC(), true
}
