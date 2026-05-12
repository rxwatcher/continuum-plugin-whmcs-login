// Package server constructs the chi-based HTTP handler exposed by the
// plugin's http_routes.v1 capability. Phase 7 wires only the /api/v1/health
// stub; later phases compose in the admin API and SPA.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Deps holds the dependencies this package needs from main.go. Fields are
// optional — nil means "not yet configured", which lets us mount partial
// surfaces without crashing the plugin before Configure runs.
type Deps struct {
	// Filled in by later phases (Phase 10 mounts AdminHandler; Phase 14
	// mounts the SPA assets/HTML).
	AdminHandler http.Handler
	AssetsFS     http.FileSystem
	SPAHandler   http.HandlerFunc
}

type Server struct {
	deps Deps
}

func New(d Deps) *Server { return &Server{deps: d} }

// Handler builds the chi router. Routes:
//
//	GET  /api/v1/health           public  (status check)
//	*    /api/v1/admin/*          authenticated, role-gated per-route
//	GET  /assets/*                public  (logo + SPA bundle assets the SPA loads pre-auth)
//	GET  /admin and /admin/*      authenticated (SPA HTML)
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/api/v1/health", s.handleHealth)

	if s.deps.AdminHandler != nil {
		r.Mount("/api/v1/admin", s.deps.AdminHandler)
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
