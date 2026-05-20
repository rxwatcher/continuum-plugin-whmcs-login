package whmcs

import (
	"context"
	"sync"
	"time"
)

// ProductsFetcher is the subset of APIClient that ProductCache depends on.
// Decoupling here keeps cache_test.go from needing a full WHMCS upstream.
type ProductsFetcher interface {
	GetProducts(ctx context.Context) ([]Product, error)
}

// ProductCache is a tiny in-process cache for the WHMCS product list.
// Fetches go through the supplied ProductsFetcher; results are kept for the
// configured TTL. Concurrent Get/Refresh are safe.
//
// In addition to the cached list itself, the cache records the time of the
// last attempted refresh and the error from the most recent failed attempt
// (if any) so the admin SPA can render a freshness diagnostic.
type ProductCache struct {
	fetcher ProductsFetcher
	ttl     time.Duration

	mu          sync.RWMutex
	products    []Product
	at          time.Time // last successful fetch
	lastAttempt time.Time // last attempted refresh (success or failure)
	lastErr     string    // empty when the last attempt succeeded
}

func NewProductCache(f ProductsFetcher, ttl time.Duration) *ProductCache {
	return &ProductCache{fetcher: f, ttl: ttl}
}

// Get returns the cached product list if it is still fresh, otherwise
// triggers a Refresh.
func (c *ProductCache) Get(ctx context.Context) ([]Product, error) {
	c.mu.RLock()
	if !c.at.IsZero() && time.Since(c.at) < c.ttl {
		out := c.products
		c.mu.RUnlock()
		return out, nil
	}
	c.mu.RUnlock()
	return c.Refresh(ctx)
}

// Refresh forces a re-fetch and updates the cache. Returns the new product
// list (or the underlying error without mutating the cached list on
// failure). Always updates lastAttempt + lastErr so the SPA can show
// failure-since-last-success state.
func (c *ProductCache) Refresh(ctx context.Context) ([]Product, error) {
	products, err := c.fetcher.GetProducts(ctx)
	now := time.Now()
	c.mu.Lock()
	c.lastAttempt = now
	if err != nil {
		c.lastErr = err.Error()
		c.mu.Unlock()
		return nil, err
	}
	c.lastErr = ""
	c.products = products
	c.at = now
	c.mu.Unlock()
	return products, nil
}

// CachedAt returns when the cache was last populated. Zero value before the
// first successful fetch.
func (c *ProductCache) CachedAt() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.at
}

// LastAttemptAt returns when the most recent refresh was attempted (success
// or failure). Zero value before any attempt.
func (c *ProductCache) LastAttemptAt() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastAttempt
}

// LastError returns the error message from the most recent refresh attempt,
// or empty string if the last attempt succeeded (or none has been made).
func (c *ProductCache) LastError() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastErr
}
