package whmcs_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/RXWatcher/silo-plugin-whmcs-login/internal/whmcs"
)

type fakeProductsFetcher struct {
	mu    sync.Mutex
	calls int
	out   []whmcs.Product
	err   error
	block chan struct{} // when non-nil, GetProducts blocks until closed
}

func (f *fakeProductsFetcher) GetProducts(_ context.Context) ([]whmcs.Product, error) {
	f.mu.Lock()
	block := f.block
	f.calls++
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.out, f.err
}

func (f *fakeProductsFetcher) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestProductCache_HitDoesNotRefetch(t *testing.T) {
	f := &fakeProductsFetcher{out: []whmcs.Product{{PID: 1, Name: "A"}}}
	c := whmcs.NewProductCache(f, 5*time.Minute)
	if _, err := c.Get(context.Background()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, err := c.Get(context.Background()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := f.Calls(); got != 1 {
		t.Errorf("calls = %d (expected cache hit on second)", got)
	}
}

func TestProductCache_RefreshClearsCache(t *testing.T) {
	f := &fakeProductsFetcher{out: []whmcs.Product{{PID: 1, Name: "A"}}}
	c := whmcs.NewProductCache(f, 5*time.Minute)
	if _, err := c.Get(context.Background()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got := f.Calls(); got != 2 {
		t.Errorf("calls = %d (expected refetch after refresh)", got)
	}
}

func TestProductCache_TTLExpiry(t *testing.T) {
	f := &fakeProductsFetcher{out: []whmcs.Product{{PID: 1, Name: "A"}}}
	c := whmcs.NewProductCache(f, 1*time.Millisecond)
	if _, err := c.Get(context.Background()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := c.Get(context.Background()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := f.Calls(); got != 2 {
		t.Errorf("calls = %d (expected refetch after TTL)", got)
	}
}

func TestProductCache_Get_CoalescesConcurrentRefreshes(t *testing.T) {
	// A burst of concurrent Get calls arriving after TTL expiry must collapse
	// into a single upstream fetch (singleflight), not a thundering herd.
	block := make(chan struct{})
	f := &fakeProductsFetcher{out: []whmcs.Product{{PID: 1, Name: "A"}}, block: block}
	c := whmcs.NewProductCache(f, time.Minute)

	const n = 25
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = c.Get(context.Background())
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(block)
	wg.Wait()

	if got := f.Calls(); got != 1 {
		t.Errorf("upstream calls = %d, want 1 (singleflight should coalesce the herd)", got)
	}
}

func TestProductCache_FetchErrorBubbles(t *testing.T) {
	f := &fakeProductsFetcher{err: errors.New("upstream down")}
	c := whmcs.NewProductCache(f, time.Minute)
	if _, err := c.Get(context.Background()); err == nil {
		t.Error("expected error bubble-up")
	}
}

func TestProductCache_CachedAt_ZeroBeforeFirstFetch(t *testing.T) {
	f := &fakeProductsFetcher{}
	c := whmcs.NewProductCache(f, time.Minute)
	if !c.CachedAt().IsZero() {
		t.Errorf("CachedAt = %v (expected zero before first fetch)", c.CachedAt())
	}
}

func TestProductCache_FailureRecordsLastAttempt_PreservesPreviousProducts(t *testing.T) {
	// First call succeeds, second call (after TTL) fails. CachedAt should
	// still point at the successful fetch; LastAttemptAt should advance to
	// the failure; LastError should carry the failure message; the cached
	// product list should be preserved so the SPA can show stale data with a
	// warning.
	f := &fakeProductsFetcher{out: []whmcs.Product{{PID: 1, Name: "A"}}}
	c := whmcs.NewProductCache(f, time.Minute)

	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	firstCachedAt := c.CachedAt()
	if firstCachedAt.IsZero() {
		t.Fatal("expected CachedAt set after successful refresh")
	}
	if c.LastError() != "" {
		t.Errorf("LastError after success = %q", c.LastError())
	}

	f.mu.Lock()
	f.err = errors.New("api timeout")
	f.out = nil
	f.mu.Unlock()
	time.Sleep(2 * time.Millisecond)
	if _, err := c.Refresh(context.Background()); err == nil {
		t.Fatal("expected failure on second Refresh")
	}
	if !c.CachedAt().Equal(firstCachedAt) {
		t.Errorf("CachedAt moved on failure: %v vs %v", c.CachedAt(), firstCachedAt)
	}
	if c.LastAttemptAt().Equal(firstCachedAt) || c.LastAttemptAt().Before(firstCachedAt) {
		t.Errorf("LastAttemptAt did not advance: %v", c.LastAttemptAt())
	}
	if c.LastError() != "api timeout" {
		t.Errorf("LastError = %q", c.LastError())
	}

	// Recovery: a successful refresh should clear LastError and bump CachedAt.
	f.mu.Lock()
	f.err = nil
	f.out = []whmcs.Product{{PID: 2, Name: "B"}}
	f.mu.Unlock()
	time.Sleep(2 * time.Millisecond)
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("recovery Refresh: %v", err)
	}
	if c.LastError() != "" {
		t.Errorf("LastError after recovery = %q", c.LastError())
	}
	if c.CachedAt().Equal(firstCachedAt) {
		t.Errorf("CachedAt did not advance after recovery")
	}
}
