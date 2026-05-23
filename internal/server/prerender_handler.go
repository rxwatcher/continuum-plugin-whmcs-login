package server

import (
	"bytes"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// htmlTagRE matches the opening <html> tag (with or without attributes),
// case-insensitive. We replace it wholesale to inject data-theme — the SPA's
// index.html deliberately avoids other <html> attributes to make this safe.
var htmlTagRE = regexp.MustCompile(`(?i)<html(\s[^>]*)?>`)

// headTagRE matches the opening <head> tag. We append a <base href="…"> so
// Vite's relative asset URLs resolve to the plugin root regardless of the
// document URL.
var headTagRE = regexp.MustCompile(`(?i)<head(\s[^>]*)?>`)

// computeBaseHref returns the relative path needed in <base href="…"> so
// that the SPA's relative asset URLs (./assets/foo.js, emitted by Vite)
// resolve to the plugin's root regardless of the document URL. The
// algorithm counts directory segments past the root to figure out how many
// "../" segments are needed.
//
//	/admin                  → "./"
//	/admin/                 → "../"
//	/admin/products         → "../"
//	/admin/products/        → "../../"
//	/admin/settings/foo/bar → "../../../"
func computeBaseHref(reqPath string) string {
	lastSlash := strings.LastIndex(reqPath, "/")
	if lastSlash < 0 {
		return "./"
	}
	dir := reqPath[:lastSlash+1]
	depth := strings.Count(dir, "/") - 1
	if depth <= 0 {
		return "./"
	}
	return strings.Repeat("../", depth)
}

// handleSPA serves index.html with `data-theme="<theme>"` injected on the
// <html> element and a `<base href>` tag injected in <head>. The theme is
// read from the host-injected X-Silo-Theme header (preferred) or the
// ?theme= query string (fallback for direct nav).
//
// Cache-Control: no-store — the theme varies per request.
func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if s.deps.SPAFiles == nil {
		http.Error(w, "spa not embedded", http.StatusInternalServerError)
		return
	}
	f, err := s.deps.SPAFiles.Open("/index.html")
	if err != nil {
		f, err = s.deps.SPAFiles.Open("index.html")
	}
	if err != nil {
		http.Error(w, "spa missing", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	raw, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "spa read", http.StatusInternalServerError)
		return
	}

	theme := r.Header.Get("X-Silo-Theme")
	if theme == "" {
		theme = r.URL.Query().Get("theme")
	}
	if theme == "" {
		theme = "default"
	}
	safeTheme := strings.ReplaceAll(theme, `"`, "&quot;")

	out := htmlTagRE.ReplaceAll(raw, []byte(`<html data-theme="`+safeTheme+`">`))

	baseHref := computeBaseHref(r.URL.Path)
	out = headTagRE.ReplaceAllFunc(out, func(m []byte) []byte {
		return append(append([]byte{}, m...), []byte(`<base href="`+baseHref+`">`)...)
	})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(bytes.TrimSpace(out))
}
