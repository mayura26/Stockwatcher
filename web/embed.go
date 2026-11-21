package web

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"strings"
)

//go:embed index.html manifest.webmanifest sw.js css js fonts vendor pwa-192.png pwa-512.png apple-touch-180.png badge.svg badge-96.png
var FS embed.FS

func init() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	_ = mime.AddExtensionType(".woff2", "font/woff2")
	_ = mime.AddExtensionType(".svg", "image/svg+xml")
}

// Handler serves the blotter. The shell, service worker, and icons are not cached by the browser
// for long, so a Coolify deploy shows up on the next open.
func Handler() http.Handler {
	fileServer := http.FileServer(http.FS(FS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/" || p == "/index.html" || p == "/sw.js" || p == "/manifest.webmanifest" || strings.HasPrefix(p, "/js/") || strings.HasPrefix(p, "/css/") || isIcon(p) {
			w.Header().Set("Cache-Control", "no-cache")
		} else if !strings.HasPrefix(p, "/api/") {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func isIcon(p string) bool {
	switch p {
	case "/pwa-192.png", "/pwa-512.png", "/apple-touch-180.png", "/badge-96.png", "/badge.svg":
		return true
	default:
		return false
	}
}

// Read is used by tests that want to confirm a file was embedded.
func Read(name string) ([]byte, error) {
	return fs.ReadFile(FS, name)
}
