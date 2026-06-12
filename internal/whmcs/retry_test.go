package whmcs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// withFastRetry replaces retrySleep with a no-op for the duration of a test so
// backoff doesn't slow the suite. Returns a restore func.
func withFastRetry(t *testing.T) {
	t.Helper()
	prev := retrySleep
	retrySleep = func(ctx context.Context, _ time.Duration) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	}
	t.Cleanup(func() { retrySleep = prev })
}

func TestRetry_RetriesTransient5xxThenSucceeds(t *testing.T) {
	withFastRetry(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusBadGateway) // 502 transient
			_, _ = w.Write([]byte(`{"result":"error"}`))
			return
		}
		_, _ = w.Write([]byte(`{"result":"success","clients":{"client":[{"id":7,"email":"u@x.com"}]}}`))
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "id", "secret")
	client, err := c.GetClientByEmail(context.Background(), "u@x.com")
	if err != nil {
		t.Fatalf("GetClientByEmail: %v", err)
	}
	if client == nil || client.ID != "7" {
		t.Fatalf("client = %+v", client)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("upstream calls = %d, want 3 (2 retries)", got)
	}
}

func TestRetry_DoesNotRetry4xx(t *testing.T) {
	withFastRetry(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest) // 400 permanent
		_, _ = w.Write([]byte(`{"result":"error"}`))
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "id", "secret")
	_, err := c.GetClientsProducts(context.Background(), "7")
	if err == nil {
		t.Fatal("expected error on 4xx")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("upstream calls = %d, want 1 (no retry on 4xx)", got)
	}
}

func TestRetry_GivesUpAfterMaxAttempts(t *testing.T) {
	withFastRetry(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable) // 503 always
		_, _ = w.Write([]byte(`{"result":"error"}`))
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "id", "secret")
	_, err := c.GetClientsDetails(context.Background(), "7")
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := atomic.LoadInt32(&calls); got != retryMaxAttempts {
		t.Errorf("upstream calls = %d, want %d", got, retryMaxAttempts)
	}
}

func TestRetry_ContextCancellationAborts(t *testing.T) {
	// A cancelled context must abort the retry loop promptly.
	prev := retrySleep
	retrySleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	defer func() { retrySleep = prev }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := retry(ctx, func(context.Context) (int, error) {
		return 0, markRetryable(errors.New("transient"))
	})
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestRetry_PermanentErrorUnwrapsMarker(t *testing.T) {
	sentinel := errors.New("boom")
	_, err := retry(context.Background(), func(context.Context) (int, error) {
		return 0, sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want sentinel", err)
	}
}
