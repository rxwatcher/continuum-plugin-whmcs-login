package whmcs_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RXWatcher/silo-plugin-whmcs-login/internal/whmcs"
)

type fakeEntitlementAPI struct {
	emailCalls int32
	prodCalls  int32
	client     *whmcs.Client
	products   []whmcs.ClientProduct
	emailErr   error
	prodErr    error
	emailBlock chan struct{} // when non-nil, GetClientByEmail blocks until closed
	prodBlock  chan struct{}
}

func (f *fakeEntitlementAPI) GetClientByEmail(_ context.Context, _ string) (*whmcs.Client, error) {
	atomic.AddInt32(&f.emailCalls, 1)
	if f.emailBlock != nil {
		<-f.emailBlock
	}
	return f.client, f.emailErr
}

func (f *fakeEntitlementAPI) GetClientsProducts(_ context.Context, _ string) ([]whmcs.ClientProduct, error) {
	atomic.AddInt32(&f.prodCalls, 1)
	if f.prodBlock != nil {
		<-f.prodBlock
	}
	return f.products, f.prodErr
}

func TestEntitlementResolver_NegativeCache_Email(t *testing.T) {
	f := &fakeEntitlementAPI{client: nil} // not found
	r := whmcs.NewEntitlementResolver(f, time.Minute)

	for i := 0; i < 3; i++ {
		c, err := r.GetClientByEmail(context.Background(), "nobody@x.com")
		if err != nil {
			t.Fatalf("GetClientByEmail: %v", err)
		}
		if c != nil {
			t.Fatalf("expected nil client, got %+v", c)
		}
	}
	if got := atomic.LoadInt32(&f.emailCalls); got != 1 {
		t.Errorf("email upstream calls = %d, want 1 (negative cache should absorb repeats)", got)
	}
}

func TestEntitlementResolver_NegativeCache_Products(t *testing.T) {
	f := &fakeEntitlementAPI{products: nil} // no products
	r := whmcs.NewEntitlementResolver(f, time.Minute)

	for i := 0; i < 3; i++ {
		if _, err := r.GetClientsProducts(context.Background(), "7"); err != nil {
			t.Fatalf("GetClientsProducts: %v", err)
		}
	}
	if got := atomic.LoadInt32(&f.prodCalls); got != 1 {
		t.Errorf("product upstream calls = %d, want 1", got)
	}
}

func TestEntitlementResolver_NegativeCacheExpires(t *testing.T) {
	f := &fakeEntitlementAPI{client: nil}
	r := whmcs.NewEntitlementResolver(f, 5*time.Millisecond)

	if _, err := r.GetClientByEmail(context.Background(), "x@y.com"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Millisecond)
	if _, err := r.GetClientByEmail(context.Background(), "x@y.com"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&f.emailCalls); got != 2 {
		t.Errorf("email calls = %d, want 2 (cache should expire)", got)
	}
}

func TestEntitlementResolver_PositiveResultNotCached(t *testing.T) {
	// Positive product results must NOT be cached: every login re-checks live
	// status so a suspension takes effect immediately.
	f := &fakeEntitlementAPI{products: []whmcs.ClientProduct{{PID: 1, Name: "P"}}}
	r := whmcs.NewEntitlementResolver(f, time.Minute)

	if _, err := r.GetClientsProducts(context.Background(), "7"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetClientsProducts(context.Background(), "7"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&f.prodCalls); got != 2 {
		t.Errorf("product calls = %d, want 2 (positive results must not be cached)", got)
	}
}

func TestEntitlementResolver_CoalescesConcurrentProductLookups(t *testing.T) {
	block := make(chan struct{})
	f := &fakeEntitlementAPI{
		products:  []whmcs.ClientProduct{{PID: 1, Name: "P"}},
		prodBlock: block,
	}
	r := whmcs.NewEntitlementResolver(f, time.Minute)

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = r.GetClientsProducts(context.Background(), "7")
		}()
	}
	// Give goroutines time to all enter the singleflight before releasing.
	time.Sleep(20 * time.Millisecond)
	close(block)
	wg.Wait()

	if got := atomic.LoadInt32(&f.prodCalls); got != 1 {
		t.Errorf("product upstream calls = %d, want 1 (singleflight should coalesce)", got)
	}
}

func TestEntitlementResolver_ErrorNotNegativeCached(t *testing.T) {
	// A transient error must not be cached as a negative — the next attempt
	// must reach upstream again.
	f := &fakeEntitlementAPI{prodErr: errors.New("upstream down")}
	r := whmcs.NewEntitlementResolver(f, time.Minute)

	if _, err := r.GetClientsProducts(context.Background(), "7"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := r.GetClientsProducts(context.Background(), "7"); err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&f.prodCalls); got != 2 {
		t.Errorf("product calls = %d, want 2 (errors must not be negative-cached)", got)
	}
}
