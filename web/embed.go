// Package web embeds the built SPA + static plugin assets (logo).
//
// The embed sits in web/ rather than internal/server because //go:embed is
// constrained to the package directory tree, and dist/ lives here.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var dist embed.FS

// FS returns the SPA file system rooted at dist/.
func FS() http.FileSystem {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic("web: " + err.Error())
	}
	return http.FS(sub)
}
