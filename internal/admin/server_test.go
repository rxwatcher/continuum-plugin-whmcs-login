package admin_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/admin"
	pluginrt "github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/runtime"
	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/whmcs"
)

var errFake = errors.New("fake api outage")

type fakeProductsFetcher struct {
	out []whmcs.Product
	err error
}

func (f *fakeProductsFetcher) GetProducts(_ context.Context) ([]whmcs.Product, error) {
	return f.out, f.err
}

func newAdminServer(cfg pluginrt.Config, prods []whmcs.Product) *admin.Server {
	cache := whmcs.NewProductCache(&fakeProductsFetcher{out: prods}, time.Minute)
	return admin.NewServer(admin.Deps{
		ConfigFn:       func() pluginrt.Config { return cfg },
		ProductCacheFn: func() *whmcs.ProductCache { return cache },
	})
}

// mountRouter is a helper that mounts the admin handlers behind chi so we
// exercise the same routing the server.go uses.
func mountRouter(a *admin.Server) http.Handler {
	r := chi.NewRouter()
	r.Get("/api/v1/admin/whoami", a.HandleWhoami)
	r.Group(func(r chi.Router) {
		r.Use(a.RequireAdmin)
		r.Get("/api/v1/admin/products", a.HandleProducts)
		r.Post("/api/v1/admin/products/refresh", a.HandleProductsRefresh)
		r.Get("/api/v1/admin/config-summary", a.HandleConfigSummary)
		r.Patch("/api/v1/admin/config", a.HandleUpdateConfig)
		r.Post("/api/v1/admin/simulate-login", a.HandleSimulateLogin)
	})
	return r
}

// fakeWHMCSAPI implements admin.WHMCSAPI for HandleSimulateLogin tests.
// Each method is a scripted return so tests can exercise success, lookup
// failure, partial outage (e.g. details errors), and the "no client found"
// branch independently.
type fakeWHMCSAPI struct {
	client       *whmcs.Client
	clientErr    error
	products     []whmcs.ClientProduct
	productsErr  error
	details      whmcs.ClientDetails
	detailsErr   error
	lastClientID string
}

func (f *fakeWHMCSAPI) GetClientByEmail(_ context.Context, _ string) (*whmcs.Client, error) {
	return f.client, f.clientErr
}
func (f *fakeWHMCSAPI) GetClientsProducts(_ context.Context, id string) ([]whmcs.ClientProduct, error) {
	f.lastClientID = id
	return f.products, f.productsErr
}
func (f *fakeWHMCSAPI) GetClientsDetails(_ context.Context, id string) (whmcs.ClientDetails, error) {
	f.lastClientID = id
	return f.details, f.detailsErr
}

func newSimulatorServer(cfg pluginrt.Config, api admin.WHMCSAPI) *admin.Server {
	return admin.NewServer(admin.Deps{
		ConfigFn:   func() pluginrt.Config { return cfg },
		APIFactory: func(_ pluginrt.Config) admin.WHMCSAPI { return api },
	})
}

func adminCfg() pluginrt.Config {
	return pluginrt.Config{
		WHMCSServerURL:      "https://billing.example",
		ClientID:            "cid",
		ClientSecret:        "secret",
		WHMCSAdminAPIID:     "api",
		WHMCSAdminAPISecret: "api-secret",
	}
}

func TestSimulateLogin_RequiresEmailOrClientID(t *testing.T) {
	s := newSimulatorServer(adminCfg(), &fakeWHMCSAPI{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/admin/simulate-login", strings.NewReader(`{}`))
	r.Header.Set("X-Continuum-User-Role", "admin")
	r.Header.Set("Content-Type", "application/json")
	mountRouter(s).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", w.Code)
	}
}

func TestSimulateLogin_AdminAPIUnconfigured_ReportsClearly(t *testing.T) {
	cfg := adminCfg()
	cfg.WHMCSAdminAPIID = ""
	cfg.WHMCSAdminAPISecret = ""
	s := newSimulatorServer(cfg, nil)
	r := httptest.NewRequest("POST", "/api/v1/admin/simulate-login",
		strings.NewReader(`{"client_id":"42"}`))
	r.Header.Set("X-Continuum-User-Role", "admin")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mountRouter(s).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["ok"] != false || resp["reason"] != "admin_api_required" {
		t.Errorf("response = %v", resp)
	}
}

func TestSimulateLogin_EmailLookup_NotFound(t *testing.T) {
	api := &fakeWHMCSAPI{client: nil}
	s := newSimulatorServer(adminCfg(), api)
	r := httptest.NewRequest("POST", "/api/v1/admin/simulate-login",
		strings.NewReader(`{"email":"unknown@example.com"}`))
	r.Header.Set("X-Continuum-User-Role", "admin")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mountRouter(s).ServeHTTP(w, r)
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["ok"] != false || resp["reason"] != "client_not_found" {
		t.Errorf("response = %v", resp)
	}
}

func TestSimulateLogin_GatePassesAndElevatesToAdmin(t *testing.T) {
	active := "Active"
	api := &fakeWHMCSAPI{
		client: &whmcs.Client{ID: "100", Email: "ada@example.com"},
		products: []whmcs.ClientProduct{
			{PID: 5, Name: "Basic", Status: &active},
			{PID: 12, Name: "Premium", Status: &active},
		},
		details: whmcs.ClientDetails{
			ID:           "100",
			Email:        "ada@example.com",
			CustomFields: map[string]string{"Discord ID": "ada#1234"},
		},
	}
	cfg := adminCfg()
	cfg.AllowedProductIDs = []string{"5"}
	cfg.ClaimRoleMapping = []pluginrt.ClaimRoleMap{
		{ProductID: "12", Role: "admin"},
	}
	cfg.FetchDiscordID = true
	s := newSimulatorServer(cfg, api)

	r := httptest.NewRequest("POST", "/api/v1/admin/simulate-login",
		strings.NewReader(`{"email":"ada@example.com"}`))
	r.Header.Set("X-Continuum-User-Role", "admin")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mountRouter(s).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["ok"] != true || resp["allowed"] != true || resp["role"] != "admin" {
		t.Errorf("response = %v", resp)
	}
	if resp["discord_id"] != "ada#1234" {
		t.Errorf("discord_id = %v", resp["discord_id"])
	}
}

func TestSimulateLogin_NoAllowedProducts_GateRejects(t *testing.T) {
	active := "Active"
	api := &fakeWHMCSAPI{
		client:   &whmcs.Client{ID: "200"},
		products: []whmcs.ClientProduct{{PID: 99, Name: "Other", Status: &active}},
	}
	cfg := adminCfg()
	cfg.AllowedProductIDs = []string{"5", "12"}
	s := newSimulatorServer(cfg, api)
	r := httptest.NewRequest("POST", "/api/v1/admin/simulate-login",
		strings.NewReader(`{"client_id":"200"}`))
	r.Header.Set("X-Continuum-User-Role", "admin")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mountRouter(s).ServeHTTP(w, r)
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["allowed"] != false {
		t.Errorf("allowed = %v, want false", resp["allowed"])
	}
}

func TestSimulateLogin_OverridesUseUnsavedRules(t *testing.T) {
	active := "Active"
	api := &fakeWHMCSAPI{
		client:   &whmcs.Client{ID: "300"},
		products: []whmcs.ClientProduct{{PID: 7, Name: "X", Status: &active}},
	}
	// Live config doesn't allow PID 7; the request body overrides to allow it.
	cfg := adminCfg()
	cfg.AllowedProductIDs = []string{"99"}
	s := newSimulatorServer(cfg, api)
	body := `{"client_id":"300","allowed_product_ids":[7]}`
	r := httptest.NewRequest("POST", "/api/v1/admin/simulate-login", strings.NewReader(body))
	r.Header.Set("X-Continuum-User-Role", "admin")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mountRouter(s).ServeHTTP(w, r)
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["allowed"] != true {
		t.Errorf("override didn't take: allowed = %v", resp["allowed"])
	}
}

func TestSimulateLogin_ClientDetailsErrorIsNonFatal(t *testing.T) {
	active := "Active"
	api := &fakeWHMCSAPI{
		client:     &whmcs.Client{ID: "400"},
		products:   []whmcs.ClientProduct{{PID: 5, Name: "X", Status: &active}},
		detailsErr: errFake,
	}
	cfg := adminCfg()
	cfg.AllowedProductIDs = []string{"5"}
	s := newSimulatorServer(cfg, api)
	r := httptest.NewRequest("POST", "/api/v1/admin/simulate-login",
		strings.NewReader(`{"client_id":"400"}`))
	r.Header.Set("X-Continuum-User-Role", "admin")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mountRouter(s).ServeHTTP(w, r)
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["allowed"] != true {
		t.Errorf("allowed = %v despite details outage", resp["allowed"])
	}
	if resp["client_details_error"] == "" || resp["client_details_error"] == nil {
		t.Errorf("expected client_details_error to be surfaced, got %v", resp)
	}
}

func TestWhoami_ReturnsRoleFromHeader(t *testing.T) {
	s := newAdminServer(pluginrt.Config{}, nil)
	r := httptest.NewRequest("GET", "/api/v1/admin/whoami", nil)
	r.Header.Set("X-Continuum-User-Id", "u-1")
	r.Header.Set("X-Continuum-User-Role", "admin")
	w := httptest.NewRecorder()
	mountRouter(s).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["role"] != "admin" || body["user_id"] != "u-1" {
		t.Errorf("body = %+v", body)
	}
}

func TestWhoami_AllowedWithoutAdminRole(t *testing.T) {
	// /whoami must succeed for non-admin users — the SPA uses it to decide
	// whether to render the "admin required" notice.
	s := newAdminServer(pluginrt.Config{}, nil)
	r := httptest.NewRequest("GET", "/api/v1/admin/whoami", nil)
	r.Header.Set("X-Continuum-User-Role", "user")
	w := httptest.NewRecorder()
	mountRouter(s).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("code = %d (want 200)", w.Code)
	}
}

func TestProducts_RejectsNonAdmin(t *testing.T) {
	s := newAdminServer(pluginrt.Config{}, []whmcs.Product{{PID: 1, Name: "A"}})
	r := httptest.NewRequest("GET", "/api/v1/admin/products", nil)
	r.Header.Set("X-Continuum-User-Role", "user")
	w := httptest.NewRecorder()
	mountRouter(s).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("non-admin code = %d, want 403", w.Code)
	}
}

func TestProducts_ReturnsListForAdmin(t *testing.T) {
	s := newAdminServer(pluginrt.Config{}, []whmcs.Product{{PID: 1, Name: "A"}})
	r := httptest.NewRequest("GET", "/api/v1/admin/products", nil)
	r.Header.Set("X-Continuum-User-Role", "admin")
	w := httptest.NewRecorder()
	mountRouter(s).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("admin code = %d (body=%s)", w.Code, w.Body.String())
	}
	var body struct {
		Products []whmcs.Product `json:"products"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Products) != 1 || body.Products[0].PID != 1 {
		t.Errorf("products = %+v", body)
	}
}

func TestProductsRefresh_RefetchesCache(t *testing.T) {
	s := newAdminServer(pluginrt.Config{}, []whmcs.Product{{PID: 1, Name: "A"}})
	r := httptest.NewRequest("POST", "/api/v1/admin/products/refresh", nil)
	r.Header.Set("X-Continuum-User-Role", "admin")
	w := httptest.NewRecorder()
	mountRouter(s).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
}

func TestProducts_NoCacheReturnsSetupPayload(t *testing.T) {
	a := admin.NewServer(admin.Deps{ConfigFn: func() pluginrt.Config { return pluginrt.Config{} }})
	r := httptest.NewRequest("GET", "/api/v1/admin/products", nil)
	r.Header.Set("X-Continuum-User-Role", "admin")
	w := httptest.NewRecorder()
	mountRouter(a).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", w.Code)
	}
	var body struct {
		Products   []whmcs.Product `json:"products"`
		Configured bool            `json:"configured"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Configured {
		t.Error("configured = true, want false")
	}
	if len(body.Products) != 0 {
		t.Errorf("products len = %d, want 0", len(body.Products))
	}
}

func TestConfigSummary_RedactsSecrets(t *testing.T) {
	cfg := pluginrt.Config{
		WHMCSServerURL: "https://x", ClientID: "cid", ClientSecret: "shh",
		AllowedProductIDs: []string{"1", "5"},
		WHMCSAdminAPIID:   "aid", WHMCSAdminAPISecret: "asec",
		FetchDiscordID: true, DiscordIDCustomField: "Discord ID",
		IconURLPath: "https://example.com/icon.svg",
		ClaimRoleMapping: []pluginrt.ClaimRoleMap{
			{ProductID: "5", Role: "admin"},
		},
	}
	s := newAdminServer(cfg, nil)
	r := httptest.NewRequest("GET", "/api/v1/admin/config-summary", nil)
	r.Header.Set("X-Continuum-User-Role", "admin")
	w := httptest.NewRecorder()
	mountRouter(s).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["client_secret"] != nil {
		t.Errorf("client_secret leaked: %+v", body)
	}
	if body["whmcs_admin_api_secret"] != nil {
		t.Errorf("whmcs_admin_api_secret leaked: %+v", body)
	}
	if body["has_client_secret"] != true {
		t.Errorf("has_client_secret = %v", body["has_client_secret"])
	}
	if body["has_whmcs_admin_api_secret"] != true {
		t.Errorf("has_whmcs_admin_api_secret = %v", body["has_whmcs_admin_api_secret"])
	}
	if body["client_id"] != "cid" {
		t.Errorf("client_id = %v", body["client_id"])
	}
	if body["icon_url_path"] != "https://example.com/icon.svg" {
		t.Errorf("icon_url_path = %v", body["icon_url_path"])
	}
	prods, _ := body["allowed_product_ids"].([]any)
	if len(prods) != 2 {
		t.Errorf("allowed_product_ids = %v", prods)
	}
	mapping, _ := body["claim_role_mapping"].([]any)
	if len(mapping) != 1 {
		t.Errorf("claim_role_mapping = %v", mapping)
	}
}
