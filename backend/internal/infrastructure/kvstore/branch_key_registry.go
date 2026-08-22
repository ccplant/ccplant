package kvstore

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrBranchKeyNotFound = errors.New("active branch key not found")

type BranchKeyRecord struct {
	Provider   string
	KeyID      string
	Generation int64
	KMSKeyRef  string
	WrappedKey []byte
	CreatedAt  time.Time
}

type BranchKeyRegistry interface {
	GetActiveBranchKey(context.Context, string, string) (BranchKeyRecord, error)
	NextBranchKeyGeneration(context.Context, string, string) (int64, error)
	CreateActiveBranchKey(context.Context, BranchKeyRecord) error
}

type MemoryBranchKeyRegistry struct {
	mu      sync.Mutex
	active  map[string]BranchKeyRecord
	maximum map[string]int64
}

func NewMemoryBranchKeyRegistry() *MemoryBranchKeyRegistry {
	return &MemoryBranchKeyRegistry{active: make(map[string]BranchKeyRecord), maximum: make(map[string]int64)}
}

func branchRegistryKey(provider, keyID string) string { return provider + "\x00" + keyID }

func (r *MemoryBranchKeyRegistry) GetActiveBranchKey(_ context.Context, provider, keyID string) (BranchKeyRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.active[branchRegistryKey(provider, keyID)]
	if !ok {
		return BranchKeyRecord{}, ErrBranchKeyNotFound
	}
	record.WrappedKey = append([]byte(nil), record.WrappedKey...)
	return record, nil
}

func (r *MemoryBranchKeyRegistry) NextBranchKeyGeneration(_ context.Context, provider, keyID string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.maximum[branchRegistryKey(provider, keyID)] + 1, nil
}

func (r *MemoryBranchKeyRegistry) CreateActiveBranchKey(_ context.Context, record BranchKeyRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := branchRegistryKey(record.Provider, record.KeyID)
	if _, exists := r.active[key]; exists {
		return ErrConflict
	}
	record.WrappedKey = append([]byte(nil), record.WrappedKey...)
	r.active[key] = record
	if record.Generation > r.maximum[key] {
		r.maximum[key] = record.Generation
	}
	return nil
}
