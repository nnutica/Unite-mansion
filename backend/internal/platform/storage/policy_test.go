package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type memStore map[string]ObjectInfo

func (m memStore) PresignPut(context.Context, PutObject, time.Duration) (PresignedRequest, error) {
	return PresignedRequest{}, nil
}
func (m memStore) PresignGet(context.Context, string, time.Duration) (PresignedRequest, error) {
	return PresignedRequest{}, nil
}
func (m memStore) Stat(_ context.Context, key string) (ObjectInfo, error) {
	if info, ok := m[key]; ok {
		return info, nil
	}
	return ObjectInfo{}, ErrObjectNotFound
}
func (m memStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("data-of-" + key)), nil
}
func (m memStore) Delete(context.Context, string) error { return nil }

func TestCheckImage(t *testing.T) {
	if err := CheckImage("image/png", 1); err != nil {
		t.Fatal(err)
	}
	if err := CheckImage("image/jpeg", MaxImageBytes); err != nil {
		t.Fatal(err)
	}
	if err := CheckImage("application/pdf", 10); !errors.Is(err, ErrFileTypeNotAllowed) {
		t.Fatalf("pdf err = %v", err)
	}
	if err := CheckImage("image/png", MaxImageBytes+1); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("too large err = %v", err)
	}
	if err := CheckImage("image/png", 0); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("empty err = %v", err)
	}
}

func TestImageKey(t *testing.T) {
	if got := ImageKey("slips/inv-1/", "abc", "image/jpeg"); got != "slips/inv-1/abc.jpg" {
		t.Fatalf("ImageKey = %q", got)
	}
}

func TestVerifyImageUpload(t *testing.T) {
	fs := memStore{
		"slips/inv-1/a.png":   {Size: 100, ContentType: "image/png"},
		"slips/inv-1/big.png": {Size: MaxImageBytes + 1, ContentType: "image/png"},
		"slips/inv-1/x.html":  {Size: 10, ContentType: "text/html"},
	}
	ctx := context.Background()

	if _, err := VerifyImageUpload(ctx, fs, "slips/inv-1", "slips/inv-1/a.png"); err != nil {
		t.Fatal(err)
	}
	cases := map[string]error{
		"slips/inv-2/a.png":          ErrInvalidFileKey, // another invoice's key
		"slips/inv-1/../inv-2/a.png": ErrInvalidFileKey,
		"slips/inv-1x/a.png":         ErrInvalidFileKey,
		"slips/inv-1/missing.png":    ErrUploadNotFound,
		"slips/inv-1/big.png":        ErrFileTooLarge,
		"slips/inv-1/x.html":         ErrFileTypeNotAllowed,
	}
	for key, want := range cases {
		if _, err := VerifyImageUpload(ctx, fs, "slips/inv-1", key); !errors.Is(err, want) {
			t.Errorf("VerifyImageUpload(%q) err = %v, want %v", key, err, want)
		}
	}
}

func TestSHA256(t *testing.T) {
	a, err := SHA256(context.Background(), memStore{}, "k1")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := SHA256(context.Background(), memStore{}, "k2")
	if len(a) != 64 || a == b {
		t.Fatalf("hashes %q %q", a, b)
	}
}

func TestCheckDocument(t *testing.T) {
	if err := CheckDocument("application/pdf", MaxDocumentBytes); err != nil {
		t.Fatal(err)
	}
	if err := CheckDocument("application/pdf", MaxDocumentBytes+1); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("too large err = %v", err)
	}
	if err := CheckDocument("text/plain", 1); !errors.Is(err, ErrFileTypeNotAllowed) {
		t.Fatalf("text err = %v", err)
	}
	if got := DocumentKey("documents/acc", "x", "application/pdf"); got != "documents/acc/x.pdf" {
		t.Fatalf("DocumentKey = %q", got)
	}
	// A PDF is fine as a document but not as a slip image.
	fs := memStore{"documents/acc/x.pdf": {Size: 100, ContentType: "application/pdf"}}
	if _, err := VerifyDocumentUpload(context.Background(), fs, "documents/acc", "documents/acc/x.pdf"); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyImageUpload(context.Background(), fs, "documents/acc", "documents/acc/x.pdf"); !errors.Is(err, ErrFileTypeNotAllowed) {
		t.Fatalf("pdf as image err = %v", err)
	}
}
