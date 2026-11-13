package s3store

import (
	"context"
	"strings"
	"testing"
	"time"

	"unite-mansion/internal/platform/storage"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(Config{
		Endpoint: "http://localhost:1", Region: "auto", Bucket: "b",
		AccessKeyID: "k", SecretAccessKey: "s", ForcePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC) }
	return s
}

func TestNewRequiresConfig(t *testing.T) {
	if _, err := New(Config{Endpoint: "http://x"}); err == nil {
		t.Fatal("expected error for missing bucket and credentials")
	}
}

func TestPresignPutSignsTypeAndLength(t *testing.T) {
	s := newTestStore(t)
	req, err := s.PresignPut(context.Background(), storage.PutObject{
		Key: "slips/a.png", ContentType: "image/png", ContentLength: 1234,
	}, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if req.Method != "PUT" || !strings.Contains(req.URL, "/b/slips/a.png") {
		t.Fatalf("unexpected request %s %s", req.Method, req.URL)
	}
	if !strings.Contains(req.URL, "X-Amz-SignedHeaders=content-length%3Bcontent-type%3Bhost") {
		t.Fatalf("content-type/length not signed: %s", req.URL)
	}
	if req.Headers["Content-Type"] != "image/png" {
		t.Fatalf("headers = %v, want Content-Type image/png", req.Headers)
	}
	for name := range req.Headers {
		if strings.EqualFold(name, "Host") {
			t.Fatal("Host must not be handed to the client")
		}
	}
	if want := s.now().Add(10 * time.Minute); !req.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", req.ExpiresAt, want)
	}
}

func TestPresignPutRejectsIncompleteObject(t *testing.T) {
	s := newTestStore(t)
	bad := []storage.PutObject{
		{ContentType: "image/png", ContentLength: 1},
		{Key: "k", ContentLength: 1},
		{Key: "k", ContentType: "image/png"},
	}
	for _, obj := range bad {
		if _, err := s.PresignPut(context.Background(), obj, time.Minute); err == nil {
			t.Errorf("PresignPut(%+v) succeeded, want error", obj)
		}
	}
}

func TestPresignRejectsTTLOutOfRange(t *testing.T) {
	s := newTestStore(t)
	for _, ttl := range []time.Duration{0, -time.Second, 8 * 24 * time.Hour} {
		if _, err := s.PresignGet(context.Background(), "k", ttl); err == nil {
			t.Errorf("PresignGet ttl=%s succeeded, want error", ttl)
		}
	}
}
