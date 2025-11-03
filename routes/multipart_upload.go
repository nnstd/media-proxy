package routes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// DefaultChunkSize is 80MB per part
	DefaultChunkSize = 80 * 1024 * 1024
	// UploadKeyPrefix is the Redis key prefix for upload tracking
	UploadKeyPrefix = "upload:"
	// UploadTTL is how long upload tracking data is kept in Redis
	UploadTTL = 24 * time.Hour
)

// UploadPart represents information about a single upload part
type UploadPart struct {
	Index  int   `json:"index"`
	Offset int64 `json:"offset"`
	Size   int64 `json:"size"`
}

// UploadInfo represents the multi-part upload tracking information
type UploadInfo struct {
	UploadID      string       `json:"uploadId"`
	UploadToken   string       `json:"uploadToken"` // Token specific to this upload session
	Location      string       `json:"location"`
	TotalSize     int64        `json:"totalSize"`
	ChunkSize     int64        `json:"chunkSize"`
	PartsCount    int          `json:"partsCount"`
	Parts         []UploadPart `json:"parts"`
	UploadedParts []int        `json:"uploadedParts"`
	ContentType   string       `json:"contentType"`
	CreatedAt     time.Time    `json:"createdAt"`
	ExpiresAt     time.Time    `json:"expiresAt"`
}

// RedisUploadTracker manages multi-part upload state in Redis
type RedisUploadTracker struct {
	client *redis.Client
}

// NewRedisUploadTracker creates a new upload tracker
func NewRedisUploadTracker(addr, password string, db int) (*RedisUploadTracker, error) {
	if addr == "" {
		return nil, nil // Redis not configured
	}

	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisUploadTracker{client: client}, nil
}

// Close closes the Redis connection
func (r *RedisUploadTracker) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}

// generateUploadToken generates a secure random token for upload authentication
func generateUploadToken() (string, error) {
	bytes := make([]byte, 32) // 256-bit token
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// InitializeUpload creates upload tracking information and returns part details
func (r *RedisUploadTracker) InitializeUpload(ctx context.Context, uploadID, location string, totalSize int64, chunkSize int64, contentType string, deadline time.Time) (*UploadInfo, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("redis not configured")
	}

	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}

	// Generate upload-specific token
	uploadToken, err := generateUploadToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate upload token: %w", err)
	}

	// Calculate parts
	partsCount := int((totalSize + chunkSize - 1) / chunkSize) // Ceiling division

	parts := make([]UploadPart, partsCount)
	var offset int64 = 0

	for i := 0; i < partsCount; i++ {
		partSize := chunkSize
		if i == partsCount-1 {
			// Last part: calculate remaining size
			partSize = totalSize - int64(i)*chunkSize
		}

		parts[i] = UploadPart{
			Index:  i,
			Offset: offset,
			Size:   partSize,
		}
		offset += partSize
	}

	uploadInfo := &UploadInfo{
		UploadID:      uploadID,
		UploadToken:   uploadToken,
		Location:      location,
		TotalSize:     totalSize,
		ChunkSize:     chunkSize,
		PartsCount:    partsCount,
		Parts:         parts,
		UploadedParts: []int{},
		ContentType:   contentType,
		CreatedAt:     time.Now(),
		ExpiresAt:     deadline,
	}

	// Store in Redis
	key := UploadKeyPrefix + uploadID
	data, err := json.Marshal(uploadInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal upload info: %w", err)
	}

	ttl := time.Until(deadline)
	if ttl > UploadTTL {
		ttl = UploadTTL
	}

	if err := r.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return nil, fmt.Errorf("failed to store upload info: %w", err)
	}

	return uploadInfo, nil
}

// GetUploadInfo retrieves upload tracking information
func (r *RedisUploadTracker) GetUploadInfo(ctx context.Context, uploadID string) (*UploadInfo, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("redis not configured")
	}

	key := UploadKeyPrefix + uploadID
	data, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("upload not found or expired")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get upload info: %w", err)
	}

	var uploadInfo UploadInfo
	if err := json.Unmarshal(data, &uploadInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal upload info: %w", err)
	}

	return &uploadInfo, nil
}

// MarkPartUploaded marks a part as uploaded
func (r *RedisUploadTracker) MarkPartUploaded(ctx context.Context, uploadID string, partIndex int) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("redis not configured")
	}

	uploadInfo, err := r.GetUploadInfo(ctx, uploadID)
	if err != nil {
		return err
	}

	// Check if part index is valid
	if partIndex < 0 || partIndex >= uploadInfo.PartsCount {
		return fmt.Errorf("invalid part index: %d", partIndex)
	}

	// Check if already uploaded
	for _, uploaded := range uploadInfo.UploadedParts {
		if uploaded == partIndex {
			return nil // Already marked
		}
	}

	// Add to uploaded parts
	uploadInfo.UploadedParts = append(uploadInfo.UploadedParts, partIndex)

	// Update in Redis
	key := UploadKeyPrefix + uploadID
	data, err := json.Marshal(uploadInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal upload info: %w", err)
	}

	// Keep the same TTL
	ttl := time.Until(uploadInfo.ExpiresAt)
	if ttl <= 0 {
		return fmt.Errorf("upload has expired")
	}

	if err := r.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to update upload info: %w", err)
	}

	return nil
}

// IsUploadComplete checks if all parts have been uploaded
func (r *RedisUploadTracker) IsUploadComplete(ctx context.Context, uploadID string) (bool, error) {
	uploadInfo, err := r.GetUploadInfo(ctx, uploadID)
	if err != nil {
		return false, err
	}

	return len(uploadInfo.UploadedParts) == uploadInfo.PartsCount, nil
}

// DeleteUpload removes upload tracking information
func (r *RedisUploadTracker) DeleteUpload(ctx context.Context, uploadID string) error {
	if r == nil || r.client == nil {
		return nil
	}

	key := UploadKeyPrefix + uploadID
	return r.client.Del(ctx, key).Err()
}
