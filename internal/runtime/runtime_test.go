package runtime

import (
	"testing"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func entry(t *testing.T, key string, v any) *pluginv1.ConfigEntry {
	t.Helper()
	s, err := structpb.NewStruct(map[string]any{"value": v})
	if err != nil {
		t.Fatalf("structpb: %v", err)
	}
	return &pluginv1.ConfigEntry{Key: key, Value: s}
}

func entryWithRaw(t *testing.T, key string, v any) *pluginv1.ConfigEntry {
	t.Helper()
	val, err := structpb.NewValue(v)
	if err != nil {
		t.Fatalf("structpb value: %v", err)
	}
	s, err := structpb.NewStruct(map[string]any{"value": val.AsInterface()})
	if err != nil {
		t.Fatalf("structpb struct: %v", err)
	}
	return &pluginv1.ConfigEntry{Key: key, Value: s}
}

func TestLoadConfig_HappyPath(t *testing.T) {
	entries := []*pluginv1.ConfigEntry{
		entry(t, "whmcs_server_url", "https://billing.example.com/"),
		entry(t, "client_id", "cid"),
		entry(t, "client_secret", "shh"),
	}
	cfg, err := loadConfig(entries)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.WHMCSServerURL != "https://billing.example.com" {
		t.Errorf("WHMCSServerURL = %q (should have trailing slash stripped)", cfg.WHMCSServerURL)
	}
	if cfg.ClientID != "cid" || cfg.ClientSecret != "shh" {
		t.Errorf("client fields wrong: %+v", cfg)
	}
	if cfg.DiscordIDCustomField != "Discord ID" {
		t.Errorf("default DiscordIDCustomField = %q", cfg.DiscordIDCustomField)
	}
}

func TestLoadConfig_AllowsIncompleteSetup(t *testing.T) {
	cases := map[string][]*pluginv1.ConfigEntry{
		"empty":          nil,
		"missing url":    {entry(t, "client_id", "c"), entry(t, "client_secret", "s")},
		"missing client": {entry(t, "whmcs_server_url", "https://x"), entry(t, "client_secret", "s")},
		"missing secret": {entry(t, "whmcs_server_url", "https://x"), entry(t, "client_id", "c")},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := loadConfig(entries); err != nil {
				t.Errorf("loadConfig(%s): %v", name, err)
			}
		})
	}
}

func TestLoadConfig_AllowedProductsParsedFromCSV(t *testing.T) {
	entries := []*pluginv1.ConfigEntry{
		entry(t, "whmcs_server_url", "https://x"),
		entry(t, "client_id", "c"),
		entry(t, "client_secret", "s"),
		entry(t, "allowed_product_ids", " 1 ,5,  12 "),
		entry(t, "whmcs_admin_api_id", "aid"),
		entry(t, "whmcs_admin_api_secret", "asec"),
	}
	cfg, err := loadConfig(entries)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	want := []string{"1", "5", "12"}
	if len(cfg.AllowedProductIDs) != len(want) {
		t.Fatalf("AllowedProductIDs = %v, want %v", cfg.AllowedProductIDs, want)
	}
	for i, p := range want {
		if cfg.AllowedProductIDs[i] != p {
			t.Errorf("[%d] = %q, want %q", i, cfg.AllowedProductIDs[i], p)
		}
	}
}

func TestLoadConfig_AllowedProductsRequireAdminAPI(t *testing.T) {
	entries := []*pluginv1.ConfigEntry{
		entry(t, "whmcs_server_url", "https://x"),
		entry(t, "client_id", "c"),
		entry(t, "client_secret", "s"),
		entry(t, "allowed_product_ids", "1,2"),
	}
	if _, err := loadConfig(entries); err != nil {
		t.Errorf("loadConfig should allow incomplete admin API setup for SPA access: %v", err)
	}
}

func TestLoadConfig_FetchDiscordRequiresAdminAPI(t *testing.T) {
	entries := []*pluginv1.ConfigEntry{
		entry(t, "whmcs_server_url", "https://x"),
		entry(t, "client_id", "c"),
		entry(t, "client_secret", "s"),
		entry(t, "fetch_discord_id", true),
	}
	if _, err := loadConfig(entries); err != nil {
		t.Errorf("loadConfig should allow incomplete admin API setup for SPA access: %v", err)
	}
}

func TestLoadConfig_ClaimRoleMapping_Array(t *testing.T) {
	entries := []*pluginv1.ConfigEntry{
		entry(t, "whmcs_server_url", "https://x"),
		entry(t, "client_id", "c"),
		entry(t, "client_secret", "s"),
		entryWithRaw(t, "claim_role_mapping", []any{
			map[string]any{"product_id": "5", "role": "admin"},
			map[string]any{"product_id": "9", "role": "user"},
		}),
	}
	cfg, err := loadConfig(entries)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(cfg.ClaimRoleMapping) != 2 {
		t.Fatalf("mappings = %+v", cfg.ClaimRoleMapping)
	}
	if cfg.ClaimRoleMapping[0].ProductID != "5" || cfg.ClaimRoleMapping[0].Role != "admin" {
		t.Errorf("first mapping = %+v", cfg.ClaimRoleMapping[0])
	}
}

func TestLoadConfig_ClaimRoleMapping_RejectsInvalidRole(t *testing.T) {
	entries := []*pluginv1.ConfigEntry{
		entry(t, "whmcs_server_url", "https://x"),
		entry(t, "client_id", "c"),
		entry(t, "client_secret", "s"),
		entryWithRaw(t, "claim_role_mapping", []any{
			map[string]any{"product_id": "5", "role": "superadmin"},
		}),
	}
	if _, err := loadConfig(entries); err == nil {
		t.Error("expected error for invalid role")
	}
}
