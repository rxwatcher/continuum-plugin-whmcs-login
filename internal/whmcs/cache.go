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
type ProductCache struct {
	fetcher ProductsFetcher
	ttl     time.Duration

	mu       sync.RWMutex
	products []Product
	at       time.Time
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
// list (or the underlying error without mutating the cache on failure).
func (c *ProductCache) Refresh(ctx context.Context) ([]Product, error) {
	products, err := c.fetcher.GetProducts(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.products = products
	c.at = time.Now()
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
