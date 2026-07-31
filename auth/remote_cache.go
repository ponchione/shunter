package auth

import (
	"context"
	"hash/fnv"
	"sync"
	"time"
)

const (
	defaultRemoteAuthCacheEntries = 128
	defaultRemoteAuthCacheIdleTTL = time.Hour
	defaultRemoteAuthRetryDelay   = time.Second
	maxRemoteAuthRetryDelay       = 30 * time.Second
)

var defaultRemoteJWTCaches = newRemoteJWTCacheSet(defaultRemoteAuthCacheEntries, defaultRemoteAuthCacheIdleTTL)

type remoteAuthFlight struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
}

// remoteJWTCacheSet is bounded for the compatibility ValidateJWT entry point
// and runtime-owned when used through Validator. Evicted entries remain safe
// for callers that already hold a pointer; they simply stop being shared by
// future lookups.
type remoteJWTCacheSet struct {
	mu          sync.Mutex
	maxEntries  int
	idleTTL     time.Duration
	jwks        map[string]*jwksCache
	discoveries map[string]*oidcDiscoveryCache
}

func newRemoteJWTCacheSet(maxEntries int, idleTTL time.Duration) *remoteJWTCacheSet {
	if maxEntries <= 0 {
		maxEntries = defaultRemoteAuthCacheEntries
	}
	if idleTTL <= 0 {
		idleTTL = defaultRemoteAuthCacheIdleTTL
	}
	return &remoteJWTCacheSet{
		maxEntries:  maxEntries,
		idleTTL:     idleTTL,
		jwks:        make(map[string]*jwksCache),
		discoveries: make(map[string]*oidcDiscoveryCache),
	}
}

func (c *remoteJWTCacheSet) jwksCache(key string) *jwksCache {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if cache := c.jwks[key]; cache != nil {
		cache.touch(now)
		return cache
	}
	pruneRemoteCaches(c.jwks, c.maxEntries, now.Add(-c.idleTTL))
	cache := &jwksCache{lastUsed: now}
	if len(c.jwks) < c.maxEntries {
		c.jwks[key] = cache
	}
	return cache
}

func (c *remoteJWTCacheSet) discoveryCache(key string) *oidcDiscoveryCache {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if cache := c.discoveries[key]; cache != nil {
		cache.touch(now)
		return cache
	}
	pruneRemoteCaches(c.discoveries, c.maxEntries, now.Add(-c.idleTTL))
	cache := &oidcDiscoveryCache{lastUsed: now}
	if len(c.discoveries) < c.maxEntries {
		c.discoveries[key] = cache
	}
	return cache
}

type remoteAuthCache interface {
	idleBefore(time.Time) bool
	evictionState() (time.Time, bool)
}

func pruneRemoteCaches[Cache remoteAuthCache](caches map[string]Cache, maxEntries int, idleCutoff time.Time) {
	for key, cache := range caches {
		if cache.idleBefore(idleCutoff) {
			delete(caches, key)
		}
	}
	for len(caches) >= maxEntries {
		key := oldestEvictableRemoteCache(caches)
		if key == "" {
			return
		}
		delete(caches, key)
	}
}

func oldestEvictableRemoteCache[Cache remoteAuthCache](caches map[string]Cache) string {
	var oldestKey string
	var oldest time.Time
	for key, cache := range caches {
		lastUsed, evictable := cache.evictionState()
		if evictable && (oldestKey == "" || lastUsed.Before(oldest)) {
			oldestKey, oldest = key, lastUsed
		}
	}
	return oldestKey
}

func remoteAuthRetryDelay(cacheKey string, consecutiveFailures uint) time.Duration {
	var shift uint
	if consecutiveFailures > 1 {
		shift = consecutiveFailures - 1
	}
	if shift > 5 {
		shift = 5
	}
	delay := defaultRemoteAuthRetryDelay << shift
	if delay > maxRemoteAuthRetryDelay {
		delay = maxRemoteAuthRetryDelay
	}

	// Stable per-source jitter avoids synchronized retries without requiring a
	// shared random generator in the authentication hot path. The factor is in
	// the inclusive range [0.8, 1.2].
	h := fnv.New32a()
	_, _ = h.Write([]byte(cacheKey))
	var failureBytes [8]byte
	for i := range failureBytes {
		failureBytes[i] = byte(consecutiveFailures >> (8 * i))
	}
	_, _ = h.Write(failureBytes[:])
	percent := 80 + int(h.Sum32()%41)
	return time.Duration(int64(delay) * int64(percent) / 100)
}
