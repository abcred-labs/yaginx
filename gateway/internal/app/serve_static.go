package app

// Serving the admin console's built assets.

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func spaStatic(directory string) http.Handler {
	return spaStaticWithCache(directory, "no-cache, no-store, must-revalidate", "public, max-age=31536000, immutable")
}

func spaStaticWithCache(directory, indexCacheControl, hashedAssetCacheControl string) http.Handler {
	files := http.FileServer(http.Dir(directory))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Cache-Control", indexCacheControl)
			files.ServeHTTP(w, r)
			return
		}
		relative := strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), string(filepath.Separator))
		candidate := filepath.Join(directory, relative)
		if _, err := os.Stat(candidate); err == nil {
			if strings.HasPrefix(strings.TrimPrefix(r.URL.Path, "/"), "assets/") {
				w.Header().Set("Cache-Control", hashedAssetCacheControl)
			} else if relative == "index.html" {
				w.Header().Set("Cache-Control", indexCacheControl)
			}
			files.ServeHTTP(w, r)
			return
		}
		index := r.Clone(r.Context())
		index.URL.Path = "/"
		w.Header().Set("Cache-Control", indexCacheControl)
		files.ServeHTTP(w, index)
	})
}
