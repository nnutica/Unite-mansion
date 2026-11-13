package domain

import (
	"errors"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 13, 3, 0, 0, 0, time.UTC)

func newReq(t *testing.T) *Request {
	t.Helper()
	r, err := NewRequest("req-1", "room-1", CategoryWater, " ก๊อกน้ำรั่ว ", []string{"a", "b"}, now)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestNewRequestValidation(t *testing.T) {
	r := newReq(t)
	if r.Status != StatusNew || r.Description != "ก๊อกน้ำรั่ว" || len(r.ImageKeys) != 2 {
		t.Fatalf("request = %+v", r)
	}

	cases := []struct {
		name     string
		category Category
		desc     string
		keys     []string
		want     error
	}{
		{"bad category", "PLUMBING", "x", nil, ErrInvalidInput},
		{"blank description", CategoryWater, "  ", nil, ErrInvalidInput},
		{"four images", CategoryWater, "x", []string{"a", "b", "c", "d"}, ErrTooManyImages}, // §6.6
		{"duplicate image", CategoryWater, "x", []string{"a", "a"}, ErrInvalidInput},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NewRequest("id", "room", c.category, c.desc, c.keys, now); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
	if _, err := NewRequest("id", "room", CategoryFurniture, "ไม่มีรูป", nil, now); err != nil {
		t.Fatalf("request without images rejected: %v", err)
	}
}

func TestStatusMovesForwardOnly(t *testing.T) { // §6.6
	all := []Status{StatusNew, StatusCoordinating, StatusTechnicianDispatched, StatusCompleted}
	for i, from := range all {
		for j, to := range all {
			r := newReq(t)
			r.Status = from
			err := r.ChangeStatus(to, now)
			if j > i && err != nil {
				t.Errorf("%s → %s rejected: %v", from, to, err)
			}
			if j <= i && !errors.Is(err, ErrInvalidStatusTransition) {
				t.Errorf("%s → %s err = %v, want ErrInvalidStatusTransition", from, to, err)
			}
		}
	}

	r := newReq(t)
	if err := r.ChangeStatus(StatusCompleted, now.Add(time.Hour)); err != nil { // skipping steps is allowed
		t.Fatal(err)
	}
	ev := r.PullEvents()[0].(MaintenanceStatusChanged)
	if ev.From != StatusNew || ev.To != StatusCompleted || !r.UpdatedAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("event %+v updatedAt %v", ev, r.UpdatedAt)
	}
	if err := r.ChangeStatus("DONE", now); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown status err = %v", err)
	}
}
