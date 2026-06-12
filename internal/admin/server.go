// Package admin serves the plugin's admin HTTP endpoints. The SPA at /admin
// (Phase 12+) calls into these. All endpoints except /whoami are gated on the
// host-injected X-Silo-User-Role: admin header.
//
// See spec Layer 4.9 for the endpoint contract.
package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	pluginauth "github.com/RXWatcher/silo-plugin-whmcs-login/internal/auth"
	pluginrt "github.com/RXWatcher/silo-plugin-whmcs-login/internal/runtime"
	"github.com/RXWatcher/silo-plugin-whmcs-login/internal/whmcs"
)

// APIFactory returns a freshly-constructed admin API client using the live
// credentials. main.go wires this; tests inject a fake. Returns nil when
// admin API credentials are not configured — handlers that need it should
// render a clear message rather than 500.
type APIFactory func(cfg pluginrt.Config) WHMCSAPI

// WHMCSAPI is the subset of whmcs.APIClient the admin server needs. Declared
// here as an interface so tests can stub it without going through HTTP.
type WHMCSAPI interface {
	GetClientByEmail(ctx context.Context, email string) (*whmcs.Client, error)
	GetClientsProducts(ctx context.Context, clientID string) ([]whmcs.ClientProduct, error)
	GetClientsDetails(ctx context.Context, clientID string) (whmcs.ClientDetails, error)
}

// Deps is the wiring this package needs from main.go.
type Deps struct {
	ConfigFn       func() pluginrt.Config
	ProductCacheFn func() *whmcs.ProductCache // nil when admin API creds are not configured
	UpdateConfigFn func(context.Context, pluginrt.Config) error
	// APIFactory builds a WHMCS admin API client from the supplied config.
	// nil disables the simulate-login endpoint (returns a clear message).
	APIFactory APIFactory
}

// Server holds the wired deps and exposes individual handler funcs +
// middleware that internal/server composes into the chi router.
type Server struct {
	deps Deps
}

func NewServer(d Deps) *Server { return &Server{deps: d} }

// RequireAdmin is a chi middleware that 403s any request without
// X-Silo-User-Role: admin. Use it on every endpoint EXCEPT /whoami,
// which deliberately admits non-admins so the SPA can render an "admin
// required" notice instead of a 403 page.
func (s *Server) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Silo-User-Role") != "admin" {
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
		"user_id": r.Header.Get("X-Silo-User-Id"),
		"role":    r.Header.Get("X-Silo-User-Role"),
		"theme":   r.Header.Get("X-Silo-User-Theme"),
	})
}

// HandleProducts returns the WHMCS product list (cached for 5 minutes). When
// admin API credentials are not configured yet, return an empty non-error
// payload so the admin SPA can render setup guidance without throwing a 5xx.
//
// The response also carries freshness diagnostics so the SPA can show last
// successful sync time, last attempted refresh, and the last error (if any).
func (s *Server) HandleProducts(w http.ResponseWriter, r *http.Request) {
	cache := s.productCache()
	if cache == nil {
		writeJSON(w, http.StatusOK, productsEmpty())
		return
	}
	prods, err := cache.Get(r.Context())
	if err != nil {
		// Refresh already recorded the failure on the cache; render it as a
		// 200 so the SPA can show the freshness panel + error message instead
		// of forcing a generic 5xx.
		writeJSON(w, http.StatusOK, productsResponse(cache, nil, err))
		return
	}
	writeJSON(w, http.StatusOK, productsResponse(cache, prods, nil))
}

// HandleProductsRefresh forces a re-fetch of the WHMCS product list and
// returns the result.
func (s *Server) HandleProductsRefresh(w http.ResponseWriter, r *http.Request) {
	cache := s.productCache()
	if cache == nil {
		writeJSON(w, http.StatusOK, productsEmpty())
		return
	}
	prods, err := cache.Refresh(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, productsResponse(cache, nil, err))
		return
	}
	writeJSON(w, http.StatusOK, productsResponse(cache, prods, nil))
}

func productsEmpty() map[string]any {
	return map[string]any{
		"products":        []whmcs.Product{},
		"cached_at":       nil,
		"last_attempt_at": nil,
		"last_error":      "",
		"configured":      false,
		"message":         "WHMCS admin API credentials are required before products can be fetched.",
	}
}

func productsResponse(cache *whmcs.ProductCache, prods []whmcs.Product, err error) map[string]any {
	resp := map[string]any{
		"products":        prods,
		"cached_at":       nilIfZero(cache.CachedAt()),
		"last_attempt_at": nilIfZero(cache.LastAttemptAt()),
		"last_error":      cache.LastError(),
		"configured":      true,
	}
	if err != nil && resp["last_error"] == "" {
		resp["last_error"] = err.Error()
	}
	return resp
}

func nilIfZero(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func (s *Server) productCache() *whmcs.ProductCache {
	if s.deps.ProductCacheFn == nil {
		return nil
	}
	return s.deps.ProductCacheFn()
}

// HandleConfigSummary returns a redacted projection of the live in-process
// config. Secrets are NEVER returned in plaintext; only has_* booleans signal
// whether the host has injected them. The secrets themselves are host-managed
// (global_config_schema, secret: true) and changed from the host admin UI.
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
		"link_by_email":              cfg.LinkByEmail,
	})
}

func (s *Server) HandleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if s.deps.UpdateConfigFn == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "config store unavailable"})
		return
	}
	cur := s.deps.ConfigFn()
	// NOTE: client_secret and whmcs_admin_api_secret are host-managed secrets
	// (declared in global_config_schema with secret: true). They are stored
	// encrypted by the host and injected via Configure, so they are NOT
	// accepted or persisted through this admin endpoint.
	var req struct {
		WHMCSServerURL       *string                  `json:"whmcs_server_url"`
		ClientID             *string                  `json:"client_id"`
		DisplayName          *string                  `json:"display_name"`
		IconURLPath          *string                  `json:"icon_url_path"`
		AllowedProductIDs    *[]int                   `json:"allowed_product_ids"`
		WHMCSAdminAPIID      *string                  `json:"whmcs_admin_api_id"`
		FetchDiscordID       *bool                    `json:"fetch_discord_id"`
		DiscordIDCustomField *string                  `json:"discord_id_custom_field"`
		ClaimRoleMapping     *[]pluginrt.ClaimRoleMap `json:"claim_role_mapping"`
		LinkByEmail          *bool                    `json:"link_by_email"`
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
	if req.FetchDiscordID != nil {
		cur.FetchDiscordID = *req.FetchDiscordID
	}
	if req.DiscordIDCustomField != nil {
		cur.DiscordIDCustomField = strings.TrimSpace(*req.DiscordIDCustomField)
	}
	if req.ClaimRoleMapping != nil {
		cur.ClaimRoleMapping = *req.ClaimRoleMapping
	}
	if req.LinkByEmail != nil {
		cur.LinkByEmail = *req.LinkByEmail
	}
	cur.DatabaseURL = ""
	if err := s.deps.UpdateConfigFn(r.Context(), cur); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// simulateLoginReq is the SPA-facing body for /simulate-login. The admin
// supplies either an email OR a client ID. AllowedProductIDs / RoleMapping
// are pointers so the SPA can preview unsaved page state without persisting.
type simulateLoginReq struct {
	Email             string                   `json:"email,omitempty"`
	ClientID          string                   `json:"client_id,omitempty"`
	AllowedProductIDs *[]int                   `json:"allowed_product_ids,omitempty"`
	ClaimRoleMapping  *[]pluginrt.ClaimRoleMap `json:"claim_role_mapping,omitempty"`
	FetchDiscordID    *bool                    `json:"fetch_discord_id,omitempty"`
}

// HandleSimulateLogin replays the entitlement evaluation the auth-provider
// ExchangeCode handler runs after a real OAuth callback, but against a WHMCS
// client the admin specifies by email or client ID. Useful to test product
// gating + role mapping changes before they go live. Honours unsaved page
// state (allowed_product_ids, claim_role_mapping, fetch_discord_id overrides
// in the request body) so admins can preview without saving first.
func (s *Server) HandleSimulateLogin(w http.ResponseWriter, r *http.Request) {
	var req simulateLoginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	req.ClientID = strings.TrimSpace(req.ClientID)
	if req.Email == "" && req.ClientID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "provide either client_id or email",
		})
		return
	}

	cfg := s.deps.ConfigFn()
	allowed := cfg.AllowedProductIDs
	if req.AllowedProductIDs != nil {
		allowed = make([]string, 0, len(*req.AllowedProductIDs))
		for _, id := range *req.AllowedProductIDs {
			allowed = append(allowed, strconv.Itoa(id))
		}
	}
	mapping := cfg.ClaimRoleMapping
	if req.ClaimRoleMapping != nil {
		mapping = *req.ClaimRoleMapping
	}
	fetchDiscord := cfg.FetchDiscordID
	if req.FetchDiscordID != nil {
		fetchDiscord = *req.FetchDiscordID
	}

	if s.deps.APIFactory == nil || cfg.WHMCSAdminAPIID == "" || cfg.WHMCSAdminAPISecret == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     false,
			"reason": "admin_api_required",
			"error":  "Configure WHMCS admin API credentials before running the simulator.",
		})
		return
	}
	api := s.deps.APIFactory(cfg)
	if api == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     false,
			"reason": "admin_api_required",
			"error":  "WHMCS admin API client unavailable.",
		})
		return
	}

	clientID := req.ClientID
	var resolvedClient *whmcs.Client
	if req.Email != "" {
		c, err := api.GetClientByEmail(r.Context(), req.Email)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":     false,
				"reason": "client_lookup_failed",
				"error":  err.Error(),
			})
			return
		}
		if c == nil || c.ID == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":     false,
				"reason": "client_not_found",
				"error":  "No WHMCS client found for that email.",
			})
			return
		}
		resolvedClient = c
		clientID = c.ID
	}

	prods, err := api.GetClientsProducts(r.Context(), clientID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     false,
			"reason": "products_lookup_failed",
			"error":  err.Error(),
		})
		return
	}
	ownedActive := pluginauth.ActiveProductIDs(prods)

	gatePassed := true
	if len(allowed) > 0 {
		gatePassed = pluginauth.AnyMatch(allowed, ownedActive)
	}

	role := "user"
	if len(ownedActive) > 0 {
		role = pluginauth.RoleFromProducts(ownedActive, mapping)
	}

	// Always pull client details — they're cheap and let the SPA show the
	// resolved client (id/email/name) alongside the entitlement decision.
	// Failure is non-fatal: the simulator still reports gating + role.
	details := map[string]any{}
	var discordID string
	var detailsErr string
	cd, derr := api.GetClientsDetails(r.Context(), clientID)
	if derr != nil {
		detailsErr = derr.Error()
	} else {
		details["id"] = cd.ID
		details["email"] = cd.Email
		details["first_name"] = cd.FirstName
		details["last_name"] = cd.LastName
		if fetchDiscord {
			field := cfg.DiscordIDCustomField
			if field == "" {
				field = "Discord ID"
			}
			if v, ok := cd.CustomFields[field]; ok {
				discordID = v
			}
		}
	}

	// Annotate each owned product with its active status + whether it's in
	// the allow-list + whether it triggered an admin-role mapping rule so the
	// SPA can render a colour-coded table.
	type productRow struct {
		PID     int    `json:"pid"`
		Name    string `json:"name"`
		Status  string `json:"status"`
		Active  bool   `json:"active"`
		Allowed bool   `json:"allowed"`
		RoleHit string `json:"role_hit,omitempty"`
	}
	rows := make([]productRow, 0, len(prods))
	for _, p := range prods {
		statusLabel := "(unspecified)"
		active := false
		if p.Status == nil {
			active = true
		} else if strings.EqualFold(strings.TrimSpace(*p.Status), "Active") {
			active = true
			statusLabel = strings.TrimSpace(*p.Status)
		} else {
			statusLabel = strings.TrimSpace(*p.Status)
			if statusLabel == "" {
				statusLabel = "(empty)"
			}
		}
		pid := strconv.Itoa(p.PID)
		inAllowed := false
		if active {
			for _, a := range allowed {
				if a == pid {
					inAllowed = true
					break
				}
			}
		}
		roleHit := ""
		if active {
			for _, m := range mapping {
				if m.ProductID == pid && (m.Role == "admin" || m.Role == "user") {
					roleHit = m.Role
					break
				}
			}
		}
		rows = append(rows, productRow{
			PID: p.PID, Name: p.Name, Status: statusLabel,
			Active: active, Allowed: inAllowed, RoleHit: roleHit,
		})
	}

	resp := map[string]any{
		"ok":           true,
		"allowed":      gatePassed,
		"role":         role,
		"client_id":    clientID,
		"products":     rows,
		"owned_active": ownedActive,
		"gating": map[string]any{
			"required":  len(allowed) > 0,
			"allow_set": allowed,
			"passed":    gatePassed,
		},
		"role_mapping_count": len(mapping),
	}
	if resolvedClient != nil {
		resp["resolved_by_email"] = true
		resp["resolved_email"] = resolvedClient.Email
	}
	if len(details) > 0 {
		resp["client_details"] = details
	}
	if detailsErr != "" {
		resp["client_details_error"] = detailsErr
	}
	if fetchDiscord {
		resp["discord_id"] = discordID
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
