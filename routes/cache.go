package routes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"media-proxy/validation"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type CacheValue struct {
	Body        []byte
	ContentType string
}

func cacheKey(url string, params *validation.ImageContext) string {
	// Use string builder for more efficient cache key generation
	var builder strings.Builder
	builder.WriteString(url)
	builder.WriteString(";quality=")
	builder.WriteString(strconv.Itoa(params.Quality))
	builder.WriteString(";width=")
	builder.WriteString(strconv.Itoa(params.Width))
	builder.WriteString(";height=")
	builder.WriteString(strconv.Itoa(params.Height))
	builder.WriteString(";scale=")
	builder.WriteString(strconv.FormatFloat(params.Scale, 'f', -1, 64))
	builder.WriteString(";interpolation=")
	builder.WriteString(strconv.Itoa(int(params.Interpolation)))
	builder.WriteString(";webp=")
	builder.WriteString(strconv.FormatBool(params.Webp))
	return builder.String()
}

// S3Cache holds a MinIO client and configuration for persistent caching
type S3Cache struct {
	Enabled bool
	Client  *minio.Client
	Bucket  string
	Prefix  string
}

// NewS3Cache creates a new S3Cache from configuration values. If not enabled or misconfigured, returns a disabled cache.
func NewS3Cache(enabled bool, endpoint, accessKeyID, secretAccessKey, bucket string, useSSL bool, prefix string) (*S3Cache, error) {
	if !enabled {
		return &S3Cache{Enabled: false}, nil
	} else if endpoint == "" || accessKeyID == "" || secretAccessKey == "" || bucket == "" {
		return &S3Cache{Enabled: false}, nil
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, err
	}

	return &S3Cache{Enabled: true, Client: client, Bucket: bucket, Prefix: prefix}, nil
}

// objectKeyFromCacheKey produces a deterministic S3 object key for a given cache key
func objectKeyFromCacheKey(prefix, cacheKey string) string {
	sum := sha256.Sum256([]byte(cacheKey))
	hexSum := hex.EncodeToString(sum[:])

	// Partition for better listing behavior: aa/bb/<hex>
	var b strings.Builder
	if prefix != "" {
		b.WriteString(strings.TrimPrefix(prefix, "/"))
		if !strings.HasSuffix(prefix, "/") {
			b.WriteString("/")
		}
	}

	b.WriteString(hexSum[0:2])
	b.WriteString("/")
	b.WriteString(hexSum[2:4])
	b.WriteString("/")
	b.WriteString(hexSum)
	return b.String()
}

// objectKeyFromExplicitLocation joins a configured prefix with a sanitized, explicit location
func objectKeyFromExplicitLocation(prefix, location string) string {
	if prefix == "" {
		return location
	}
	if strings.HasSuffix(prefix, "/") {
		return prefix + location
	}
	return prefix + "/" + location
}

// Get tries to fetch an object from S3 by cache key. Returns nil if missing or disabled.
func (s *S3Cache) Get(ctx context.Context, cacheKey string) (*CacheValue, error) {
	if s == nil || !s.Enabled || s.Client == nil {
		return nil, nil
	}

	objKey := objectKeyFromCacheKey(s.Prefix, cacheKey)
	obj, err := s.Client.GetObject(ctx, s.Bucket, objKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, nil
	}

	// Read all content
	data, rerr := io.ReadAll(obj)
	if rerr != nil {
		return nil, nil
	}

	// Try to get content-type from object info
	info, herr := obj.Stat()
	contentType := "application/octet-stream"
	if herr == nil {
		if ct, ok := info.Metadata["Content-Type"]; ok && len(ct) > 0 {
			contentType = ct[0]
		} else if info.ContentType != "" {
			contentType = info.ContentType
		}
	}

	return &CacheValue{Body: data, ContentType: contentType}, nil
}

// GetAtLocation fetches an object from S3 by explicit object key (location)
func (s *S3Cache) GetAtLocation(ctx context.Context, location string) (*CacheValue, error) {
	if s == nil || !s.Enabled || s.Client == nil {
		return nil, nil
	}
	objKey := objectKeyFromExplicitLocation(s.Prefix, location)
	obj, err := s.Client.GetObject(ctx, s.Bucket, objKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, nil
	}
	data, rerr := io.ReadAll(obj)
	if rerr != nil {
		return nil, nil
	}
	info, herr := obj.Stat()
	contentType := "application/octet-stream"
	if herr == nil {
		if ct, ok := info.Metadata["Content-Type"]; ok && len(ct) > 0 {
			contentType = ct[0]
		} else if info.ContentType != "" {
			contentType = info.ContentType
		}
	}
	return &CacheValue{Body: data, ContentType: contentType}, nil
}

// Put uploads object to S3 by cache key with content type. Best-effort, errors are returned but non-fatal to caller.
func (s *S3Cache) Put(ctx context.Context, cacheKey string, body []byte, contentType string) error {
	if s == nil || !s.Enabled || s.Client == nil {
		return nil
	}

	objKey := objectKeyFromCacheKey(s.Prefix, cacheKey)
	reader := bytes.NewReader(body)
	_, err := s.Client.PutObject(ctx, s.Bucket, objKey, reader, int64(len(body)), minio.PutObjectOptions{
		ContentType: contentType,
		Expires:     time.Now().Add(time.Hour * 24),
	})
	return err
}

// PutAtLocation uploads object to S3 by explicit location key
func (s *S3Cache) PutAtLocation(ctx context.Context, location string, body []byte, contentType string) error {
	return s.PutAtLocationExpiring(ctx, location, body, contentType, time.Now().Add(time.Hour*24))
}

// PutAtLocationExpiring uploads object to S3 by explicit location key with a specified TTL
func (s *S3Cache) PutAtLocationExpiring(ctx context.Context, location string, body []byte, contentType string, expire time.Time) error {
	if s == nil || !s.Enabled || s.Client == nil {
		return nil
	}
	objKey := objectKeyFromExplicitLocation(s.Prefix, location)
	reader := bytes.NewReader(body)
	_, err := s.Client.PutObject(ctx, s.Bucket, objKey, reader, int64(len(body)), minio.PutObjectOptions{
		ContentType: contentType,
		Expires:     expire,
	})
	return err
}
