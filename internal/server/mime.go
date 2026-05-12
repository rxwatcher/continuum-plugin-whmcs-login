package server

import (
	"net/http"
	"strings"
	"time"
)

// zeroModTime is used when serving embed.FS content via http.ServeContent.
// embed.FS files report a zero modtime; we surface that explicitly so we
// don't accidentally synthesise a current time and break caching tests.
func zeroModTime() time.Time { return time.Time{} }

// setContentTypeByExt sets Content-Type for the small set of static-asset
// extensions the plugin actually ships. http.ServeContent does this
// automatically for files served via http.FileSystem, but the fallback
// path (embed.FS via fs.ReadFile) bypasses that helper.
func setContentTypeByExt(w http.ResponseWriter, path string) {
	switch {
	case strings.HasSuffix(path, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	case strings.HasSuffix(path, ".png"):
		w.Header().Set("Content-Type", "image/png")
	case strings.HasSuffix(path, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(path, ".js"):
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case strings.HasSuffix(path, ".json"):
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
}
