package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Upload rules for slips and repair photos (Decision log 2026-11-13, plan 3.a–3.b).
const (
	MaxImageBytes = 5 << 20 // 5 MB
	UploadURLTTL  = 10 * time.Minute
	ReadURLTTL    = 5 * time.Minute
)

var imageExt = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
}

var (
	ErrFileTypeNotAllowed = errors.New("file type not allowed")
	ErrFileTooLarge       = errors.New("file too large")
	ErrInvalidFileKey     = errors.New("file key does not belong to this upload")
	ErrUploadNotFound     = errors.New("uploaded file not found")
)

// CheckImage validates a declared image upload before presigning.
func CheckImage(contentType string, size int64) error {
	if _, ok := imageExt[contentType]; !ok {
		return fmt.Errorf("%w: %q (PNG or JPEG only)", ErrFileTypeNotAllowed, contentType)
	}
	if size <= 0 || size > MaxImageBytes {
		return fmt.Errorf("%w: %d bytes (max %d)", ErrFileTooLarge, size, MaxImageBytes)
	}
	return nil
}

// ImageKey builds an object key under prefix, e.g. "slips/<invoice>/<id>.png".
// The prefix ties the upload to its owner so a key cannot be reused elsewhere.
func ImageKey(prefix, id, contentType string) string {
	return fmt.Sprintf("%s/%s.%s", strings.TrimSuffix(prefix, "/"), id, imageExt[contentType])
}

// VerifyImageUpload checks that key was issued under prefix and that the
// stored object really is an allowed image within the size limit.
func VerifyImageUpload(ctx context.Context, fs FileStorage, prefix, key string) (ObjectInfo, error) {
	if !strings.HasPrefix(key, strings.TrimSuffix(prefix, "/")+"/") || strings.Contains(key, "..") {
		return ObjectInfo{}, ErrInvalidFileKey
	}
	info, err := fs.Stat(ctx, key)
	if errors.Is(err, ErrObjectNotFound) {
		return ObjectInfo{}, ErrUploadNotFound
	}
	if err != nil {
		return ObjectInfo{}, err
	}
	if err := CheckImage(info.ContentType, info.Size); err != nil {
		return ObjectInfo{}, err
	}
	return info, nil
}

// SHA256 hashes a stored object (reading at most MaxImageBytes).
func SHA256(ctx context.Context, fs FileStorage, key string) (string, error) {
	rc, err := fs.Open(ctx, key)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(rc, MaxImageBytes+1)); err != nil {
		return "", fmt.Errorf("hash %s: %w", key, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
