// Package runtime implements the plugin's Runtime gRPC server. Its Configure
// handler parses the global config payload into Config and invokes a callback
// supplied by main.go so the plugin can (re)wire its in-process state.
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"
	"github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtimedefault"
)

// ClaimRoleMap entry: if the authenticated user owns the WHMCS product
// identified by ProductID, the host elevates them to Role.
type ClaimRoleMap struct {
	ProductID string `json:"product_id"`
	Role      string `json:"role"`
}

// Config is the parsed plugin global config.
type Config struct {
	DatabaseURL          string
	WHMCSServerURL       string
	ClientID             string
	ClientSecret         string
	DisplayName          string
	IconURLPath          string
	AllowedProductIDs    []string // parsed from comma-separated string
	WHMCSAdminAPIID      string
	WHMCSAdminAPISecret  string
	FetchDiscordID       bool
	DiscordIDCustomField string
	ClaimRoleMapping     []ClaimRoleMap
}

// Server implements the plugin's Runtime service. Configure parses the
// global config payload and invokes onCfg so main.go can rewire its
// in-process state (AuthServer, AdminServer, ProductCache, etc.).
type Server struct {
	runtimedefault.Server
	manifest *pluginv1.PluginManifest
	onCfg    func(Config) error

	mu  sync.RWMutex
	cfg Config
}

func New(manifest *pluginv1.PluginManifest, onConfig func(Config) error) *Server {
	return &Server{manifest: manifest, onCfg: onConfig}
}

func (s *Server) GetManifest(_ context.Context, _ *pluginv1.GetManifestRequest) (*pluginv1.GetManifestResponse, error) {
	return &pluginv1.GetManifestResponse{Manifest: s.manifest}, nil
}

func (s *Server) Configure(_ context.Context, req *pluginv1.ConfigureRequest) (*pluginv1.ConfigureResponse, error) {
	cfg, err := loadConfig(req.GetConfig())
	if err != nil {
		return nil, err
	}

	if s.onCfg != nil {
		if err := s.onCfg(cfg); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	return &pluginv1.ConfigureResponse{}, nil
}

func (s *Server) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// loadConfig parses a slice of ConfigEntry protos into a Config. Exported as
// a standalone function so tests can exercise it without a gRPC server.
func loadConfig(entries []*pluginv1.ConfigEntry) (Config, error) {
	cfg := Config{
		DiscordIDCustomField: "Discord ID",
	}

	for _, e := range entries {
		v := e.GetValue()
		if v == nil {
			continue
		}
		m := v.AsMap()
		switch e.GetKey() {
		case "database_url":
			cfg.DatabaseURL = stringFromMap(m)
		case "whmcs_server_url":
			cfg.WHMCSServerURL = strings.TrimRight(stringFromMap(m), "/")
		case "client_id":
			cfg.ClientID = stringFromMap(m)
		case "client_secret":
			cfg.ClientSecret = stringFromMap(m)
		case "display_name":
			cfg.DisplayName = stringFromMap(m)
		case "icon_url_path":
			cfg.IconURLPath = strings.TrimSpace(stringFromMap(m))
		case "allowed_product_ids":
			raw := stringFromMap(m)
			if raw != "" {
				for _, p := range strings.Split(raw, ",") {
					p = strings.TrimSpace(p)
					if p != "" {
						n, err := parsePositiveID(p)
						if err != nil {
							return Config{}, fmt.Errorf("allowed_product_ids: %w", err)
						}
						cfg.AllowedProductIDs = append(cfg.AllowedProductIDs, strconv.Itoa(n))
					}
				}
			}
		case "whmcs_admin_api_id":
			cfg.WHMCSAdminAPIID = stringFromMap(m)
		case "whmcs_admin_api_secret":
			cfg.WHMCSAdminAPISecret = stringFromMap(m)
		case "fetch_discord_id":
			cfg.FetchDiscordID = boolFromMap(m)
		case "discord_id_custom_field":
			if s := stringFromMap(m); s != "" {
				cfg.DiscordIDCustomField = s
			}
		case "claim_role_mapping":
			mappings, err := parseClaimRoleMapping(m["value"])
			if err != nil {
				return Config{}, fmt.Errorf("claim_role_mapping: %w", err)
			}
			cfg.ClaimRoleMapping = mappings
		}
	}

	if err := ValidateConfig(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func ValidateConfig(cfg *Config) error {
	// Configure must not reject incomplete setup. The host calls Configure
	// before forwarding the plugin admin SPA, so operational prerequisites are
	// enforced by the auth/admin handlers that actually need them.
	if cfg.WHMCSServerURL != "" {
		if err := validateServerURL(cfg.WHMCSServerURL); err != nil {
			return err
		}
	}
	for i, m := range cfg.ClaimRoleMapping {
		n, err := parsePositiveID(m.ProductID)
		if err != nil {
			return fmt.Errorf("claim_role_mapping[%d]: product_id: %w", i, err)
		}
		cfg.ClaimRoleMapping[i].ProductID = strconv.Itoa(n)
		if m.Role != "user" && m.Role != "admin" {
			return fmt.Errorf("claim_role_mapping[%d]: role must be 'user' or 'admin', got %q", i, m.Role)
		}
	}
	return nil
}

func validateServerURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("whmcs_server_url is not a valid URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("whmcs_server_url scheme must be http or https")
	}
	if u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("whmcs_server_url must be an origin URL without credentials, query, or fragment")
	}
	if u.Scheme == "http" && !isLocalhost(u.Hostname()) {
		return fmt.Errorf("whmcs_server_url must use https except for localhost")
	}
	return nil
}

func isLocalhost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func parsePositiveID(raw string) (int, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("is required")
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("must be a positive integer")
	}
	return n, nil
}

func stringFromMap(m map[string]any) string {
	if s, ok := m["value"].(string); ok {
		return s
	}
	return ""
}

func boolFromMap(m map[string]any) bool {
	if b, ok := m["value"].(bool); ok {
		return b
	}
	return false
}

// parseClaimRoleMapping accepts either the native []any (decoded from
// structpb) or a raw JSON string and produces a typed slice.
func parseClaimRoleMapping(raw any) ([]ClaimRoleMap, error) {
	if raw == nil {
		return nil, nil
	}
	// structpb arrays come back as []any of map[string]any.
	if arr, ok := raw.([]any); ok {
		out := make([]ClaimRoleMap, 0, len(arr))
		for i, item := range arr {
			obj, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("entry %d is not an object", i)
			}
			pid, _ := obj["product_id"].(string)
			role, _ := obj["role"].(string)
			out = append(out, ClaimRoleMap{ProductID: pid, Role: role})
		}
		return out, nil
	}
	// Tolerate raw JSON strings too (manual entry via admin form).
	if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
		var out []ClaimRoleMap
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			return nil, fmt.Errorf("decode json: %w", err)
		}
		return out, nil
	}
	return nil, nil
}
