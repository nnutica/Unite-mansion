package domain

import (
	"errors"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 13, 3, 0, 0, 0, time.UTC)

func newAccount(t *testing.T) *TenantAccount {
	t.Helper()
	a, err := NewTenantAccount("acc-1", "somchai", "hash", "สมชาย ใจดี", "0812345678", now)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestNewTenantAccountValidation(t *testing.T) {
	tests := []struct {
		name, username, fullName, phone string
	}{
		{"short username", "ab", "x", "0812345678"},
		{"upper case username", "Somchai", "x", "0812345678"},
		{"empty name", "somchai", "  ", "0812345678"},
		{"phone letters", "somchai", "x", "08-1234567"},
		{"phone too short", "somchai", "x", "12345678"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewTenantAccount("id", tt.username, "h", tt.fullName, tt.phone, now); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
		})
	}
	if a := newAccount(t); a.Status != StatusPending || a.RoomID != nil {
		t.Fatalf("new account = %+v", a)
	}
	if NormalizeUsername("  SomChai ") != "somchai" {
		t.Fatal("NormalizeUsername")
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("1234567"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("7 chars accepted")
	}
	if err := ValidatePassword("12345678"); err != nil {
		t.Fatal(err)
	}
}

func TestAccountLifecycle(t *testing.T) {
	a := newAccount(t)

	if err := a.Unassign(now); !errors.Is(err, ErrInvalidAccountStatus) {
		t.Fatalf("unassign PENDING err = %v", err)
	}
	if err := a.AssignRoom("room-302", "302", now); err != nil {
		t.Fatal(err)
	}
	if a.Status != StatusActive || *a.RoomID != "room-302" {
		t.Fatalf("after assign = %+v", a)
	}
	if err := a.AssignRoom("room-303", "303", now); !errors.Is(err, ErrInvalidAccountStatus) {
		t.Fatalf("assign ACTIVE err = %v", err)
	}

	later := now.Add(time.Hour)
	if err := a.Unassign(later); err != nil {
		t.Fatal(err)
	}
	if a.Status != StatusInactive || a.RoomID != nil || a.UnassignedAt == nil || !a.UnassignedAt.Equal(later) {
		t.Fatalf("after unassign = %+v", a)
	}

	// Re-binding an INACTIVE account clears the move-out time (§6.6).
	if err := a.AssignRoom("room-401", "401", later); err != nil {
		t.Fatal(err)
	}
	if a.Status != StatusActive || a.UnassignedAt != nil {
		t.Fatalf("after re-assign = %+v", a)
	}

	names := []string{}
	for _, e := range a.PullEvents() {
		names = append(names, e.EventName())
	}
	want := []string{"AccountAssignedToRoom", "AccountUnassigned", "AccountAssignedToRoom"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Fatalf("events = %v, want %v", names, want)
	}
}

func TestSessionPolicy(t *testing.T) {
	admin := NewSession([]byte("h"), SubjectAdmin, "a", now)
	if !admin.ExpiresAt.Equal(now.Add(12 * time.Hour)) {
		t.Fatalf("admin expiry = %v", admin.ExpiresAt)
	}
	if _, ok := admin.RenewedExpiry(now.Add(11 * time.Hour)); ok {
		t.Fatal("admin session must not slide")
	}
	if !admin.Expired(now.Add(12*time.Hour)) || admin.Expired(now.Add(12*time.Hour-time.Second)) {
		t.Fatal("admin expiry boundary")
	}

	tenant := NewSession([]byte("h"), SubjectTenant, "t", now)
	if _, ok := tenant.RenewedExpiry(now.Add(time.Hour)); ok {
		t.Fatal("tenant renewed within the first day")
	}
	exp, ok := tenant.RenewedExpiry(now.Add(48 * time.Hour))
	if !ok || !exp.Equal(now.Add(48*time.Hour+TenantSessionTTL)) {
		t.Fatalf("tenant renewal = %v %v", exp, ok)
	}
}
