// Package ui serves the KNOTT web console.
//
// The built single-page app is compiled into the binary with go:embed, so a
// KNOTT release is one self-contained executable with no static-file directory
// to deploy. A directory on disk still wins when FRONTEND_PATH points at one,
// which keeps `vite build` → refresh workflows fast during development.
package ui

import (
	"bytes"
	"embed"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed all:dist
var embedded embed.FS

// Assets returns the console's file system and whether it holds a real build.
// A placeholder is embedded when the console has not been built, so the Go
// packages always compile; the boolean reports false in that case.
func Assets() (fs.FS, bool) {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "assets"); err != nil {
		return sub, false
	}
	return sub, true
}

// Handler returns an http.Handler serving the console with SPA fallback —
// unknown paths resolve to index.html so client-side routes survive a reload —
// along with a label describing where the files came from. dir, when it holds a
// build, takes precedence over the embedded one. A nil handler means no console
// is available.
func Handler(dir string) (http.Handler, string) {
	if dir != "" {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			if _, err := os.Stat(filepath.Join(dir, "index.html")); err == nil {
				return spa(http.Dir(dir)), dir
			}
		}
	}
	assets, ok := Assets()
	if !ok {
		return nil, ""
	}
	return spa(http.FS(assets)), "embedded"
}

func spa(root http.FileSystem) http.Handler {
	fileServer := http.FileServer(root)
	started := time.Now()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := strings.TrimPrefix(path(r), "/")
		if upath == "" {
			upath = "index.html"
		}
		if f, err := root.Open(upath); err == nil {
			f.Close()
			// Vite emits content-hashed filenames under /assets — cache hard.
			if strings.HasPrefix(upath, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		index, err := root.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer index.Close()
		body, err := io.ReadAll(index)
		if err != nil {
			http.Error(w, "console unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "index.html", started, bytes.NewReader(body))
	})
}

// path returns the cleaned request path, guarding against traversal attempts
// before the value is handed to the file system.
func path(r *http.Request) string {
	p := r.URL.Path
	if strings.Contains(p, "..") {
		return "/"
	}
	return p
}
