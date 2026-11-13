//go:build integration

// Package s3test starts a throwaway RustFS (same image as docker-compose)
// with an empty bucket. Run with: go test -tags integration ./...
package s3test

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"unite-mansion/internal/platform/storage/s3store"
)

const image = "rustfs/rustfs:1.0.4"

// NewStore returns a FileStorage backed by a fresh RustFS container.
func NewStore(t *testing.T) *s3store.Store {
	t.Helper()
	ctx := context.Background()
	// Throwaway container: random credentials, never a fixed value in the repo.
	accessKey, secretKey := "t"+rand.Text()[:15], rand.Text()

	ctr, err := testcontainers.Run(ctx, image,
		testcontainers.WithExposedPorts("9000/tcp"),
		testcontainers.WithEnv(map[string]string{
			"RUSTFS_ADDRESS":    ":9000",
			"RUSTFS_ACCESS_KEY": accessKey,
			"RUSTFS_SECRET_KEY": secretKey,
		}),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("9000/tcp").WithStartupTimeout(60*time.Second)),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("start rustfs: %v", err)
	}
	endpoint, err := ctr.PortEndpoint(ctx, "9000/tcp", "http")
	if err != nil {
		t.Fatalf("rustfs endpoint: %v", err)
	}

	client := s3.New(s3.Options{
		Region: "auto", BaseEndpoint: aws.String(endpoint), UsePathStyle: true,
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
	})
	deadline := time.Now().Add(30 * time.Second)
	for {
		_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("bmansion-test")})
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	store, err := s3store.New(s3store.Config{
		Endpoint: endpoint, Region: "auto", Bucket: "bmansion-test",
		AccessKeyID: accessKey, SecretAccessKey: secretKey, ForcePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}
