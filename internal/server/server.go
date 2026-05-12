// Package server constructs the chi-based HTTP handler exposed by the
// plugin's http_routes.v1 capability. It wires:
//
//	GET  /api/v1/health           public  (status check)
//	GET  /api/v1/admin/whoami     authenticated  (returns role headers)
//	*    /api/v1/admin/*          admin-only  (handled by internal/admin)
//	GET  /assets/*                public  (logo + SPA bundle)
//	GET  /admin and /admin/*      authenticated  (SPA HTML; theme-injected)
package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/admin"
)

// Deps holds the dependencies this package needs from main.go. All fields are
// optional — nil means "not yet wired", which keeps the router safe to call
// before Configure runs.
type Deps struct {
	Admin      *admin.Server
	AssetsFS   http.FileSystem
	SPAHandler http.HandlerFunc
}

type Server struct {
	deps Deps
}

func New(d Deps) *Server { return &Server{deps: d} }

// Handler returns the composed chi router.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/api/v1/health", s.handleHealth)

	if s.deps.Admin != nil {
		// /whoami is intentionally NOT gated on admin — the SPA needs it to
		// render a friendly "admin required" notice for regular users.
		r.Get("/api/v1/admin/whoami", s.deps.Admin.HandleWhoami)
		r.Group(func(r chi.Router) {
			r.Use(s.deps.Admin.RequireAdmin)
			r.Get("/api/v1/admin/products", s.deps.Admin.HandleProducts)
			r.Post("/api/v1/admin/products/refresh", s.deps.Admin.HandleProductsRefresh)
			r.Get("/api/v1/admin/config-summary", s.deps.Admin.HandleConfigSummary)
		})
	}

	if s.deps.AssetsFS != nil {
		r.Get("/assets/*", http.FileServer(s.deps.AssetsFS).ServeHTTP)
	}
	if s.deps.SPAHandler != nil {
		r.Get("/admin", s.deps.SPAHandler)
		r.Get("/admin/*", s.deps.SPAHandler)
	}

	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}
