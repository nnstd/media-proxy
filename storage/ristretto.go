package storage

import (
	"time"

	"github.com/dgraph-io/ristretto/v2"
)

// RistrettoStorage implements fiber.Storage using Ristretto cache
type RistrettoStorage struct {
	cache *ristretto.Cache[string, []byte]
}

// NewRistrettoStorage creates a new RistrettoStorage
func NewRistrettoStorage(cache *ristretto.Cache[string, []byte]) *RistrettoStorage {
	return &RistrettoStorage{cache: cache}
}

// Get retrieves data from cache
func (r *RistrettoStorage) Get(key string) ([]byte, error) {
	if value, found := r.cache.Get(key); found {
		return value, nil
	}
	return nil, nil
}

// Set stores data in cache
func (r *RistrettoStorage) Set(key string, val []byte, exp time.Duration) error {
	r.cache.SetWithTTL(key, val, 1, exp)
	return nil
}

// Delete removes data from cache
func (r *RistrettoStorage) Delete(key string) error {
	r.cache.Del(key)
	return nil
}

// Reset clears all data from cache
func (r *RistrettoStorage) Reset() error {
	r.cache.Clear()
	return nil
}

// Close closes the storage (no-op for ristretto)
func (r *RistrettoStorage) Close() error {
	r.cache.Close()
	return nil
}
