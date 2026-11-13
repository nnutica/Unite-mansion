// Package s3store implements storage.FileStorage on any S3-compatible service
// with AWS SDK for Go v2. Dev uses RustFS, UAT uses Cloudflare R2; only the
// configuration differs.
package s3store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"unite-mansion/internal/platform/storage"
)

// maxPresignTTL is the SigV4 presign limit.
const maxPresignTTL = 7 * 24 * time.Hour

type Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	ForcePathStyle  bool
}

type Store struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
	now     func() time.Time
}

var _ storage.FileStorage = (*Store)(nil)

func New(cfg Config) (*Store, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errors.New("s3store: endpoint, bucket and credentials are required")
	}
	client := s3.New(s3.Options{
		Region:       cfg.Region,
		BaseEndpoint: aws.String(cfg.Endpoint),
		UsePathStyle: cfg.ForcePathStyle,
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
	})
	return &Store{
		client:  client,
		presign: s3.NewPresignClient(client),
		bucket:  cfg.Bucket,
		now:     time.Now,
	}, nil
}

func (s *Store) PresignPut(ctx context.Context, obj storage.PutObject, ttl time.Duration) (storage.PresignedRequest, error) {
	if obj.Key == "" || obj.ContentType == "" || obj.ContentLength <= 0 {
		return storage.PresignedRequest{}, fmt.Errorf("s3store: presign put %q: key, content type and positive length are required", obj.Key)
	}
	if err := checkTTL(ttl); err != nil {
		return storage.PresignedRequest{}, err
	}
	req, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(obj.Key),
		ContentType:   aws.String(obj.ContentType),
		ContentLength: aws.Int64(obj.ContentLength),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return storage.PresignedRequest{}, fmt.Errorf("s3store: presign put %q: %w", obj.Key, err)
	}
	return s.toPresigned(req.URL, req.Method, req.SignedHeader, ttl), nil
}

func (s *Store) PresignGet(ctx context.Context, key string, ttl time.Duration) (storage.PresignedRequest, error) {
	if key == "" {
		return storage.PresignedRequest{}, errors.New("s3store: presign get: key is required")
	}
	if err := checkTTL(ttl); err != nil {
		return storage.PresignedRequest{}, err
	}
	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return storage.PresignedRequest{}, fmt.Errorf("s3store: presign get %q: %w", key, err)
	}
	return s.toPresigned(req.URL, req.Method, req.SignedHeader, ttl), nil
}

func (s *Store) Stat(ctx context.Context, key string) (storage.ObjectInfo, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return storage.ObjectInfo{}, fmt.Errorf("s3store: stat %q: %w", key, mapNotFound(err))
	}
	return storage.ObjectInfo{
		Key:         key,
		Size:        aws.ToInt64(out.ContentLength),
		ContentType: aws.ToString(out.ContentType),
	}, nil
}

func (s *Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3store: open %q: %w", key, mapNotFound(err))
	}
	return out.Body, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil && !errors.Is(mapNotFound(err), storage.ErrObjectNotFound) {
		return fmt.Errorf("s3store: delete %q: %w", key, err)
	}
	return nil
}

func (s *Store) toPresigned(url, method string, signed http.Header, ttl time.Duration) storage.PresignedRequest {
	headers := make(map[string]string, len(signed))
	for name, values := range signed {
		// The browser sets Host itself and refuses to let scripts send it.
		if strings.EqualFold(name, "Host") || len(values) == 0 {
			continue
		}
		headers[name] = values[0]
	}
	return storage.PresignedRequest{
		URL:       url,
		Method:    method,
		Headers:   headers,
		ExpiresAt: s.now().Add(ttl).UTC(),
	}
}

func checkTTL(ttl time.Duration) error {
	if ttl <= 0 || ttl > maxPresignTTL {
		return fmt.Errorf("s3store: presign ttl %s out of range (0, %s]", ttl, maxPresignTTL)
	}
	return nil
}

// mapNotFound turns a 404 from the store into storage.ErrObjectNotFound,
// keeping the original error in the chain.
func mapNotFound(err error) error {
	var re *awshttp.ResponseError
	if errors.As(err, &re) && re.HTTPStatusCode() == http.StatusNotFound {
		return errors.Join(storage.ErrObjectNotFound, err)
	}
	return err
}
