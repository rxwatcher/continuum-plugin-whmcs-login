package whmcs

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// EntitlementAPI is the subset of the admin API the EntitlementResolver fronts.
// *APIClient satisfies it; tests can stub it.
type EntitlementAPI interface {
	GetClientByEmail(ctx context.Context, email string) (*Client, error)
	GetClientsProducts(ctx context.Context, clientID string) ([]ClientProduct, error)
}

// EntitlementResolver fronts the per-client admin lookups used by the login
// entitlement path (email -> client, client -> products) with two protections
// against a login storm fanning out unbounded admin API calls:
//
//   - singleflight coalescing: concurrent lookups for the SAME key collapse
//     into one in-flight upstream call whose result is shared. A burst of
//     simultaneous logins for one client hits WHMCS once, not N times.
//   - a short negative cache: a "no client found" / "no products" answer is
//     remembered for negativeTTL so a hot retry loop (or a script hammering an
//     unknown email) can't re-fan-out to WHMCS on every attempt.
//
// Positive results are deliberately NOT cached here — entitlement decisions
// must reflect live product status (suspension/cancellation) on every login,
// and singleflight already collapses simultaneous duplicates. Only the cheap,
// safe-to-stale negative answers are cached.
type EntitlementResolver struct {
	api         EntitlementAPI
	negativeTTL time.Duration
	now         func() time.Time

	emailSF singleflight.Group
	prodSF  singleflight.Group

	mu       sync.Mutex
	negEmail map[string]time.Time // email -> expiry of a cached "not found"
	negProd  map[string]time.Time // clientID -> expiry of a cached "no products"
}

// NewEntitlementResolver wraps api with coalescing + a negative cache of the
// given TTL. A non-positive TTL disables the negative cache (coalescing stays).
func NewEntitlementResolver(api EntitlementAPI, negativeTTL time.Duration) *EntitlementResolver {
	return &EntitlementResolver{
		api:         api,
		negativeTTL: negativeTTL,
		now:         time.Now,
		negEmail:    make(map[string]time.Time),
		negProd:     make(map[string]time.Time),
	}
}

// GetClientByEmail resolves an email to a WHMCS client, coalescing concurrent
// identical lookups and short-circuiting on a cached negative result.
func (r *EntitlementResolver) GetClientByEmail(ctx context.Context, email string) (*Client, error) {
	if r.negativeCached(r.negEmail, email) {
		return nil, nil
	}
	v, err, _ := r.emailSF.Do(email, func() (any, error) {
		// Re-check the negative cache inside the flight: a prior leader for this
		// key may have just recorded a miss.
		if r.negativeCached(r.negEmail, email) {
			return (*Client)(nil), nil
		}
		c, err := r.api.GetClientByEmail(ctx, email)
		if err != nil {
			return (*Client)(nil), err
		}
		if c == nil || c.ID == "" {
			r.rememberNegative(r.negEmail, email)
			return (*Client)(nil), nil
		}
		return c, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*Client), nil
}

// GetClientsProducts resolves a client's products, coalescing concurrent
// identical lookups and short-circuiting on a cached "no products" result.
func (r *EntitlementResolver) GetClientsProducts(ctx context.Context, clientID string) ([]ClientProduct, error) {
	if r.negativeCached(r.negProd, clientID) {
		return nil, nil
	}
	v, err, _ := r.prodSF.Do(clientID, func() (any, error) {
		if r.negativeCached(r.negProd, clientID) {
			return []ClientProduct(nil), nil
		}
		prods, err := r.api.GetClientsProducts(ctx, clientID)
		if err != nil {
			return []ClientProduct(nil), err
		}
		if len(prods) == 0 {
			r.rememberNegative(r.negProd, clientID)
		}
		return prods, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]ClientProduct), nil
}

func (r *EntitlementResolver) negativeCached(m map[string]time.Time, key string) bool {
	if r.negativeTTL <= 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	exp, ok := m[key]
	if !ok {
		return false
	}
	if r.now().After(exp) {
		delete(m, key)
		return false
	}
	return true
}

func (r *EntitlementResolver) rememberNegative(m map[string]time.Time, key string) {
	if r.negativeTTL <= 0 {
		return
	}
	r.mu.Lock()
	m[key] = r.now().Add(r.negativeTTL)
	r.mu.Unlock()
}
