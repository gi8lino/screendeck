package handler

import (
	"io/fs"
	"net/http"
)

// Frontend serves static frontend assets for GET and HEAD requests.
func Frontend(appFS fs.FS) http.Handler {
	frontend := http.FileServer(http.FS(appFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		frontend.ServeHTTP(w, r)
	})
}
