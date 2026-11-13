// Package storage defines FileStorage, the single interface for private files
// (slips, maintenance photos, tenant documents). The only implementation is
// s3store, which talks to RustFS in dev and Cloudflare R2 in UAT.
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrObjectNotFound is returned by Stat and Open when the key does not exist.
var ErrObjectNotFound = errors.New("storage: object not found")

// PutObject describes an upload the client will perform with a presigned URL.
// ContentType and ContentLength are part of the signature, so the client must
// send exactly these values or the store rejects the upload.
type PutObject struct {
	Key           string
	ContentType   string
	ContentLength int64
}

// PresignedRequest is what the API hands to the client.
type PresignedRequest struct {
	URL       string
	Method    string
	Headers   map[string]string // headers the client must send as-is
	ExpiresAt time.Time
}

type ObjectInfo struct {
	Key         string
	Size        int64
	ContentType string
}

// FileStorage stores private objects. All reads and writes by clients go
// through short-lived presigned URLs (context.md §6.4, §7).
type FileStorage interface {
	// PresignPut returns a URL the client can PUT the object to until ttl expires.
	PresignPut(ctx context.Context, obj PutObject, ttl time.Duration) (PresignedRequest, error)
	// PresignGet returns a URL the client can GET the object from until ttl expires.
	PresignGet(ctx context.Context, key string, ttl time.Duration) (PresignedRequest, error)
	// Stat confirms an uploaded object exists and reports its size and type.
	Stat(ctx context.Context, key string) (ObjectInfo, error)
	// Open streams an object, e.g. to hash a slip for duplicate detection.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes an object. Deleting a missing key succeeds (idempotent).
	Delete(ctx context.Context, key string) error
}
