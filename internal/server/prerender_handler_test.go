package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestComputeBaseHref(t *testing.T) {
	cases := []struct {
		name    string
		reqPath string
		want    string
	}{
		{"plugin root no trailing slash", "/admin", "./"},
		{"plugin root trailing slash", "/admin/", "../"},
		{"one level deep", "/admin/products", "../"},
		{"two levels deep trailing slash", "/admin/products/", "../../"},
		{"three levels deep file", "/admin/settings/foo/bar", "../../../"},
		{"root only", "/", "./"},
		{"empty", "", "./"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeBaseHref(tc.reqPath)
			if got != tc.want {
				t.Errorf("computeBaseHref(%q) = %q, want %q", tc.reqPath, got, tc.want)
			}
		})
	}
}

func TestHandleSPA_InjectsThemeAndBaseHref(t *testing.T) {
	spa := fakeFS{
		"/index.html": []byte(`<!doctype html><html><head><title>x</title></head><body></body></html>`),
	}
	s := New(Deps{SPAFiles: spa})

	r := httptest.NewRequest(http.MethodGet, "/admin/products", nil)
	r.Header.Set("X-Continuum-Theme", "dark")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `data-theme="dark"`) {
		t.Errorf("body does not contain data-theme: %s", body)
	}
	if !strings.Contains(body, `<base href="../">`) {
		t.Errorf("body does not contain base href: %s", body)
	}
}

func TestHandleSPA_ThemeFallsBackToQuery(t *testing.T) {
	spa := fakeFS{
		"/index.html": []byte(`<html><head></head><body></body></html>`),
	}
	s := New(Deps{SPAFiles: spa})

	r := httptest.NewRequest(http.MethodGet, "/admin?theme=midnight", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if !strings.Contains(w.Body.String(), `data-theme="midnight"`) {
		t.Errorf("expected theme from query, got: %s", w.Body.String())
	}
}

func TestHandleSPA_NoSPAReturns500(t *testing.T) {
	s := New(Deps{})
	r := httptest.NewRequest(http.MethodGet, "/admin", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	// /admin route is not registered when SPAFiles is nil, so we expect 404.
	if w.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", w.Code)
	}
}
