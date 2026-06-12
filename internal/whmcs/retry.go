package whmcs

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

// Retry policy for idempotent WHMCS admin GETs (GetClientsProducts,
// GetClientsDetails, GetClientByEmail). A single transient 5xx or network blip
// must not deny an entitled login, so these reads get a small bounded number of
// retries with exponential backoff + full jitter. Mutating calls are never
// retried here — only the explicitly idempotent lookups opt in.
const (
	// retryMaxAttempts is the total number of attempts (initial + retries).
	retryMaxAttempts = 3
	// retryBaseDelay is the base backoff before the first retry; subsequent
	// retries double it (capped at retryMaxDelay) before jitter is applied.
	retryBaseDelay = 150 * time.Millisecond
	// retryMaxDelay caps the per-attempt backoff so a misbehaving upstream
	// can't stretch a single login wait unboundedly.
	retryMaxDelay = 1500 * time.Millisecond
)

// retryableError marks an error returned from doForm as worth retrying. doForm
// wraps transport failures (network blips) and surfaces a sentinel for 5xx
// responses; both are transient and retryable. 4xx and decode/validation
// errors are permanent and must not be retried.
type retryableError struct{ err error }

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

// markRetryable wraps err so retry() will retry it.
func markRetryable(err error) error {
	if err == nil {
		return nil
	}
	return &retryableError{err: err}
}

// isRetryable reports whether err (or anything it wraps) was marked retryable.
func isRetryable(err error) bool {
	var re *retryableError
	return errors.As(err, &re)
}

// retrySleep is the sleep function retry() uses; overridable in tests so they
// don't pay real backoff time.
var retrySleep = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retry runs fn up to retryMaxAttempts times, retrying only while fn returns a
// retryable error (5xx / network). The returned error is the last error from
// fn, unwrapped from its retryable marker so callers see the underlying cause.
// Context cancellation aborts the loop immediately.
func retry[T any](ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	var (
		out  T
		err  error
		zero T
	)
	for attempt := 0; attempt < retryMaxAttempts; attempt++ {
		if attempt > 0 {
			if serr := retrySleep(ctx, backoffDelay(attempt)); serr != nil {
				return zero, serr
			}
		}
		out, err = fn(ctx)
		if err == nil {
			return out, nil
		}
		if !isRetryable(err) {
			return zero, unwrapRetryable(err)
		}
		if ctx.Err() != nil {
			return zero, ctx.Err()
		}
	}
	return zero, unwrapRetryable(err)
}

// unwrapRetryable strips the retryable marker so callers never see the wrapper
// type in their error chain prose.
func unwrapRetryable(err error) error {
	var re *retryableError
	if errors.As(err, &re) {
		return re.err
	}
	return err
}

// backoffDelay returns the backoff for the given retry attempt (1-based among
// retries): exponential growth capped at retryMaxDelay, then full jitter in
// [0, delay] so a synchronized login storm doesn't retry in lockstep.
func backoffDelay(attempt int) time.Duration {
	d := retryBaseDelay << (attempt - 1)
	if d > retryMaxDelay || d <= 0 {
		d = retryMaxDelay
	}
	return time.Duration(rand.Int63n(int64(d) + 1))
}
