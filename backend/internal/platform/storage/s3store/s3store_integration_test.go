//go:build integration

package s3store

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"unite-mansion/internal/platform/storage"
)

// Same image as docker-compose.yml so tests exercise the dev store.
const rustfsImage = "rustfs/rustfs:1.0.4"

func newRustFSStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	// Throwaway container: random credentials, never a fixed value in the repo.
	accessKey, secretKey := "t"+rand.Text()[:15], rand.Text()

	ctr, err := testcontainers.Run(ctx, rustfsImage,
		testcontainers.WithExposedPorts("9000/tcp"),
		testcontainers.WithEnv(map[string]string{
			"RUSTFS_ADDRESS":    ":9000",
			"RUSTFS_ACCESS_KEY": accessKey,
			"RUSTFS_SECRET_KEY": secretKey,
		}),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("9000/tcp").WithStartupTimeout(60*time.Second),
		),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("start rustfs: %v", err)
	}
	endpoint, err := ctr.PortEndpoint(ctx, "9000/tcp", "http")
	if err != nil {
		t.Fatalf("rustfs endpoint: %v", err)
	}

	s, err := New(Config{
		Endpoint: endpoint, Region: "auto", Bucket: "bmansion-test",
		AccessKeyID: accessKey, SecretAccessKey: secretKey, ForcePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// The server may accept connections slightly before it serves S3 calls.
	deadline := time.Now().Add(30 * time.Second)
	for {
		_, err = s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("bmansion-test")})
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	return s
}

func doPresigned(t *testing.T, req storage.PresignedRequest, body []byte, override map[string]string) *http.Response {
	t.Helper()
	httpReq, err := http.NewRequest(req.Method, req.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	for k, v := range override {
		httpReq.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestPresignedUploadReadDelete(t *testing.T) {
	s := newRustFSStore(t)
	ctx := context.Background()
	key := "slips/2026-11/test.png"
	body := []byte("fake-png-bytes")

	put, err := s.PresignPut(ctx, storage.PutObject{Key: key, ContentType: "image/png", ContentLength: int64(len(body))}, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if resp := doPresigned(t, put, body, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("presigned PUT status = %d", resp.StatusCode)
	}

	info, err := s.Stat(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != int64(len(body)) || info.ContentType != "image/png" {
		t.Fatalf("Stat = %+v", info)
	}

	get, err := s.PresignGet(ctx, key, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	resp := doPresigned(t, get, nil, nil)
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, body) {
		t.Fatalf("presigned GET = %d %q", resp.StatusCode, got)
	}

	rc, err := s.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	opened, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(opened, body) {
		t.Fatalf("Open = %q", opened)
	}

	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stat(ctx, key); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("Stat after delete err = %v, want ErrObjectNotFound", err)
	}
	if _, err := s.Open(ctx, key); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("Open after delete err = %v, want ErrObjectNotFound", err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("second Delete err = %v, want nil (idempotent)", err)
	}
}

func TestPresignedPutRejectsMismatchedUpload(t *testing.T) {
	s := newRustFSStore(t)
	ctx := context.Background()
	body := []byte("0123456789")

	put, err := s.PresignPut(ctx, storage.PutObject{Key: "slips/locked.png", ContentType: "image/png", ContentLength: int64(len(body))}, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		body     []byte
		override map[string]string
	}{
		{"different content type", body, map[string]string{"Content-Type": "text/html"}},
		{"different length", append(body, []byte("-extra")...), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doPresigned(t, put, tc.body, tc.override)
			if resp.StatusCode == http.StatusOK {
				t.Fatalf("upload with %s was accepted", tc.name)
			}
		})
	}

	if _, err := s.Stat(ctx, "slips/locked.png"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("rejected upload left an object: err = %v", err)
	}
}

func TestPresignedGetExpires(t *testing.T) {
	s := newRustFSStore(t)
	ctx := context.Background()
	body := []byte("x")
	put, _ := s.PresignPut(ctx, storage.PutObject{Key: "k", ContentType: "image/png", ContentLength: 1}, time.Minute)
	if resp := doPresigned(t, put, body, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d", resp.StatusCode)
	}

	get, err := s.PresignGet(ctx, "k", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	if resp := doPresigned(t, get, nil, nil); resp.StatusCode == http.StatusOK {
		t.Fatalf("expired GET returned %d", resp.StatusCode)
	}
}
