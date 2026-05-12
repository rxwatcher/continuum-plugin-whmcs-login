// Package admin serves the plugin's admin HTTP endpoints. The SPA at /admin
// (Phase 12+) calls into these. All endpoints except /whoami are gated on the
// host-injected X-Continuum-User-Role: admin header.
//
// See spec Layer 4.9 for the endpoint contract.
package admin

import (
	"encoding/json"
	"net/http"
	"strconv"

	pluginrt "github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/runtime"
	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/whmcs"
)

// Deps is the wiring this package needs from main.go.
type Deps struct {
	ConfigFn     func() pluginrt.Config
	ProductCache *whmcs.ProductCache // nil when admin API creds are not configured
}

// Server holds the wired deps and exposes individual handler funcs +
// middleware that internal/server composes into the chi router.
type Server struct {
	deps Deps
}

func NewServer(d Deps) *Server { return &Server{deps: d} }

// RequireAdmin is a chi middleware that 403s any request without
// X-Continuum-User-Role: admin. Use it on every endpoint EXCEPT /whoami,
// which deliberately admits non-admins so the SPA can render an "admin
// required" notice instead of a 403 page.
func (s *Server) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Continuum-User-Role") != "admin" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// HandleWhoami returns the host-injected identity headers so the SPA can
// decide what to render. Available to ANY authenticated user (the host has
// already authenticated by the time it forwards the request).
func (s *Server) HandleWhoami(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id": r.Header.Get("X-Continuum-User-Id"),
		"role":    r.Header.Get("X-Continuum-User-Role"),
		"theme":   r.Header.Get("X-Continuum-User-Theme"),
	})
}

// HandleProducts returns the WHMCS product list (cached for 5 minutes).
// 503 if the admin API credentials are not configured.
func (s *Server) HandleProducts(w http.ResponseWriter, r *http.Request) {
	if s.deps.ProductCache == nil {
		http.Error(w, "product cache not configured (admin API credentials missing?)", http.StatusServiceUnavailable)
		return
	}
	prods, err := s.deps.ProductCache.Get(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"products":  prods,
		"cached_at": s.deps.ProductCache.CachedAt(),
	})
}

// HandleProductsRefresh forces a re-fetch of the WHMCS product list and
// returns the result.
func (s *Server) HandleProductsRefresh(w http.ResponseWriter, r *http.Request) {
	if s.deps.ProductCache == nil {
		http.Error(w, "product cache not configured", http.StatusServiceUnavailable)
		return
	}
	prods, err := s.deps.ProductCache.Refresh(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"products":  prods,
		"cached_at": s.deps.ProductCache.CachedAt(),
	})
}

// HandleConfigSummary returns a redacted projection of the live in-process
// config. Secrets are NEVER returned in plaintext — admins re-enter them in
// the form when changing.
func (s *Server) HandleConfigSummary(w http.ResponseWriter, _ *http.Request) {
	cfg := s.deps.ConfigFn()
	intIDs := make([]int, 0, len(cfg.AllowedProductIDs))
	for _, p := range cfg.AllowedProductIDs {
		if n, err := strconv.Atoi(p); err == nil {
			intIDs = append(intIDs, n)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"whmcs_server_url":           cfg.WHMCSServerURL,
		"client_id":                  cfg.ClientID,
		"has_client_secret":          cfg.ClientSecret != "",
		"whmcs_admin_api_id":         cfg.WHMCSAdminAPIID,
		"has_whmcs_admin_api_secret": cfg.WHMCSAdminAPISecret != "",
		"allowed_product_ids":        intIDs,
		"claim_role_mapping":         cfg.ClaimRoleMapping,
		"fetch_discord_id":           cfg.FetchDiscordID,
		"discord_id_custom_field":    cfg.DiscordIDCustomField,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
