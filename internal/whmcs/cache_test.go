package whmcs_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/whmcs"
)

type fakeProductsFetcher struct {
	mu    sync.Mutex
	calls int
	out   []whmcs.Product
	err   error
}

func (f *fakeProductsFetcher) GetProducts(_ context.Context) ([]whmcs.Product, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
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
