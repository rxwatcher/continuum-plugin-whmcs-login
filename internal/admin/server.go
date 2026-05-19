// Package admin serves the plugin's admin HTTP endpoints. The SPA at /admin
// (Phase 12+) calls into these. All endpoints except /whoami are gated on the
// host-injected X-Continuum-User-Role: admin header.
//
// See spec Layer 4.9 for the endpoint contract.
package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	pluginrt "github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/runtime"
	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/whmcs"
)

// Deps is the wiring this package needs from main.go.
type Deps struct {
	ConfigFn       func() pluginrt.Config
	ProductCacheFn func() *whmcs.ProductCache // nil when admin API creds are not configured
	UpdateConfigFn func(context.Context, pluginrt.Config) error
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

// HandleProducts returns the WHMCS product list (cached for 5 minutes). When
// admin API credentials are not configured yet, return an empty non-error
// payload so the admin SPA can render setup guidance without throwing a 5xx.
func (s *Server) HandleProducts(w http.ResponseWriter, r *http.Request) {
	cache := s.productCache()
	if cache == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"products":   []whmcs.Product{},
			"cached_at":  "",
			"configured": false,
			"message":    "WHMCS admin API credentials are required before products can be fetched.",
		})
		return
	}
	prods, err := cache.Get(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"products":  prods,
		"cached_at": cache.CachedAt(),
	})
}

// HandleProductsRefresh forces a re-fetch of the WHMCS product list and
// returns the result.
func (s *Server) HandleProductsRefresh(w http.ResponseWriter, r *http.Request) {
	cache := s.productCache()
	if cache == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"products":   []whmcs.Product{},
			"cached_at":  "",
			"configured": false,
			"message":    "WHMCS admin API credentials are required before products can be fetched.",
		})
		return
	}
	prods, err := cache.Refresh(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"products":  prods,
		"cached_at": cache.CachedAt(),
	})
}

func (s *Server) productCache() *whmcs.ProductCache {
	if s.deps.ProductCacheFn == nil {
		return nil
	}
	return s.deps.ProductCacheFn()
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
		"icon_url_path":              cfg.IconURLPath,
		"display_name":               cfg.DisplayName,
		"whmcs_admin_api_id":         cfg.WHMCSAdminAPIID,
		"has_whmcs_admin_api_secret": cfg.WHMCSAdminAPISecret != "",
		"allowed_product_ids":        intIDs,
		"claim_role_mapping":         cfg.ClaimRoleMapping,
		"fetch_discord_id":           cfg.FetchDiscordID,
		"discord_id_custom_field":    cfg.DiscordIDCustomField,
	})
}

func (s *Server) HandleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if s.deps.UpdateConfigFn == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "config store unavailable"})
		return
	}
	cur := s.deps.ConfigFn()
	var req struct {
		WHMCSServerURL       *string                  `json:"whmcs_server_url"`
		ClientID             *string                  `json:"client_id"`
		ClientSecret         *string                  `json:"client_secret"`
		DisplayName          *string                  `json:"display_name"`
		IconURLPath          *string                  `json:"icon_url_path"`
		AllowedProductIDs    *[]int                   `json:"allowed_product_ids"`
		WHMCSAdminAPIID      *string                  `json:"whmcs_admin_api_id"`
		WHMCSAdminAPISecret  *string                  `json:"whmcs_admin_api_secret"`
		FetchDiscordID       *bool                    `json:"fetch_discord_id"`
		DiscordIDCustomField *string                  `json:"discord_id_custom_field"`
		ClaimRoleMapping     *[]pluginrt.ClaimRoleMap `json:"claim_role_mapping"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return
	}
	if req.WHMCSServerURL != nil {
		cur.WHMCSServerURL = strings.TrimRight(strings.TrimSpace(*req.WHMCSServerURL), "/")
	}
	if req.ClientID != nil {
		cur.ClientID = strings.TrimSpace(*req.ClientID)
	}
	if req.ClientSecret != nil && *req.ClientSecret != "" {
		cur.ClientSecret = *req.ClientSecret
	}
	if req.DisplayName != nil {
		cur.DisplayName = strings.TrimSpace(*req.DisplayName)
	}
	if req.IconURLPath != nil {
		cur.IconURLPath = strings.TrimSpace(*req.IconURLPath)
	}
	if req.AllowedProductIDs != nil {
		cur.AllowedProductIDs = make([]string, 0, len(*req.AllowedProductIDs))
		for _, id := range *req.AllowedProductIDs {
			cur.AllowedProductIDs = append(cur.AllowedProductIDs, strconv.Itoa(id))
		}
	}
	if req.WHMCSAdminAPIID != nil {
		cur.WHMCSAdminAPIID = strings.TrimSpace(*req.WHMCSAdminAPIID)
	}
	if req.WHMCSAdminAPISecret != nil && *req.WHMCSAdminAPISecret != "" {
		cur.WHMCSAdminAPISecret = *req.WHMCSAdminAPISecret
	}
	if req.FetchDiscordID != nil {
		cur.FetchDiscordID = *req.FetchDiscordID
	}
	if req.DiscordIDCustomField != nil {
		cur.DiscordIDCustomField = strings.TrimSpace(*req.DiscordIDCustomField)
	}
	if req.ClaimRoleMapping != nil {
		cur.ClaimRoleMapping = *req.ClaimRoleMapping
	}
	cur.DatabaseURL = ""
	if err := s.deps.UpdateConfigFn(r.Context(), cur); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
