package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/admin"
	pluginrt "github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/runtime"
	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/whmcs"
)

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
		ConfigFn:     func() pluginrt.Config { return cfg },
		ProductCache: cache,
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
	})
	return r
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

func TestProducts_NoCacheReturns503(t *testing.T) {
	a := admin.NewServer(admin.Deps{ConfigFn: func() pluginrt.Config { return pluginrt.Config{} }})
	r := httptest.NewRequest("GET", "/api/v1/admin/products", nil)
	r.Header.Set("X-Continuum-User-Role", "admin")
	w := httptest.NewRecorder()
	mountRouter(a).ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d, want 503", w.Code)
	}
}

func TestConfigSummary_RedactsSecrets(t *testing.T) {
	cfg := pluginrt.Config{
		WHMCSServerURL: "https://x", ClientID: "cid", ClientSecret: "shh",
		AllowedProductIDs: []string{"1", "5"},
		WHMCSAdminAPIID:   "aid", WHMCSAdminAPISecret: "asec",
		FetchDiscordID: true, DiscordIDCustomField: "Discord ID",
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
	prods, _ := body["allowed_product_ids"].([]any)
	if len(prods) != 2 {
		t.Errorf("allowed_product_ids = %v", prods)
	}
	mapping, _ := body["claim_role_mapping"].([]any)
	if len(mapping) != 1 {
		t.Errorf("claim_role_mapping = %v", mapping)
	}
}
