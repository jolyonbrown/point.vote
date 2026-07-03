package api

import (
	"fmt"
	"net/http"

	"github.com/jolyonbrown/point.vote/web"
)

// mountStatic serves the embedded UI. Pages and assets are read from the
// embed FS once at startup; they are small and immutable for the life of
// the process.
func mountStatic(mux *http.ServeMux) {
	routes := []struct {
		pattern     string
		file        string
		contentType string
	}{
		{"GET /{$}", "index.html", "text/html; charset=utf-8"},
		{"GET /r/{id}", "room.html", "text/html; charset=utf-8"},
		{"GET /app.js", "app.js", "text/javascript; charset=utf-8"},
		{"GET /style.css", "style.css", "text/css; charset=utf-8"},
		{"GET /llms.txt", "llms.txt", "text/plain; charset=utf-8"},
	}
	for _, rt := range routes {
		body, err := web.Files.ReadFile(rt.file)
		if err != nil {
			panic(fmt.Sprintf("embedded file %s: %v", rt.file, err))
		}
		contentType := rt.contentType
		mux.HandleFunc(rt.pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			w.Write(body)
		})
	}
}
