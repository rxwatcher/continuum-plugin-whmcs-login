// Package server constructs the chi-based HTTP handler exposed by the
// plugin's http_routes.v1 capability. Routes:
//
//	GET  /api/v1/health          public  (status check)
//	GET  /api/v1/admin/whoami    authenticated (identity headers)
//	*    /api/v1/admin/*         admin-only (handled by internal/admin)
//	GET  /assets/*               public  (logo + SPA bundle assets)
//	GET  /admin and /admin/*     authenticated (SPA HTML; theme-injected)
//
// The SPA's own JS/CSS assets live under web/dist/assets/ at build time;
// they are served from the same /assets/* mount as the logo. A short-circuit
// in the assets handler tries the SPA dist first and falls back to the
// plugin's static-assets embed.FS (logo) if no SPA file matches.
package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/RXWatcher/silo-plugin-whmcs-login/internal/admin"
)

// Deps holds the dependencies main.go provides. All fields are optional —
// nil disables the corresponding surface so the router stays safe before
// Configure has run.
type Deps struct {
	Admin      *admin.Server
	SPAFiles   http.FileSystem // built SPA, rooted at dist/
	StaticFS   embed.FS        // plugin static assets, rooted at <package>/assets
	StaticRoot string          // subdir of StaticFS to serve (e.g. "assets")
}

type Server struct {
	deps Deps
}

func New(d Deps) *Server { return &Server{deps: d} }

// Handler builds the chi router.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/api/v1/health", s.handleHealth)

	if s.deps.Admin != nil {
		// /whoami is intentionally NOT admin-gated — the SPA needs it to
		// render a friendly "admin required" notice for regular users.
		r.Get("/api/v1/admin/whoami", s.deps.Admin.HandleWhoami)
		r.Group(func(r chi.Router) {
			r.Use(s.deps.Admin.RequireAdmin)
			r.Get("/api/v1/admin/products", s.deps.Admin.HandleProducts)
			r.Post("/api/v1/admin/products/refresh", s.deps.Admin.HandleProductsRefresh)
			r.Get("/api/v1/admin/config-summary", s.deps.Admin.HandleConfigSummary)
			r.Patch("/api/v1/admin/config", s.deps.Admin.HandleUpdateConfig)
			r.Post("/api/v1/admin/simulate-login", s.deps.Admin.HandleSimulateLogin)
		})
	}

	r.Get("/assets/*", s.handleAssets)

	if s.deps.SPAFiles != nil {
		r.Get("/admin", s.handleSPA)
		r.Get("/admin/*", s.handleSPA)
	}

	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// handleAssets serves files under /assets/*. Order of resolution:
//
//  1. The built SPA's dist/assets/* (the bundled JS/CSS Vite emits).
//  2. The plugin's static assets embed (logo + future static files).
//
// 404 if neither matches.
func (s *Server) handleAssets(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/assets/")
	if rel == "" {
		http.NotFound(w, r)
		return
	}

	// 1. Try the SPA's dist/assets/* first (Vite-emitted bundles).
	if s.deps.SPAFiles != nil {
		if f, err := s.deps.SPAFiles.Open("/assets/" + rel); err == nil {
			defer f.Close()
			http.ServeContent(w, r, rel, zeroModTime(), f.(http.File))
			return
		}
	}

	// 2. Fall back to the plugin's static-assets embed (logo et al).
	if s.deps.StaticRoot != "" {
		data, err := fs.ReadFile(s.deps.StaticFS, s.deps.StaticRoot+"/"+rel)
		if err == nil {
			setContentTypeByExt(w, rel)
			_, _ = w.Write(data)
			return
		}
	}

	http.NotFound(w, r)
}
