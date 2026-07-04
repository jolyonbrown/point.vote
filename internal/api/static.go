package api

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"github.com/jolyonbrown/point.vote/web"
)

// mountStatic serves the embedded UI. Pages and assets are read from the
// embed FS once at startup; they are small and immutable for the life of
// the process. HTML pages get the {{version}} placeholder substituted so
// the footer states what is actually running.
func (s *Server) mountStatic(mux *http.ServeMux) {
	routes := []struct {
		pattern     string
		file        string
		contentType string
		cache       bool
	}{
		{"GET /{$}", "index.html", "text/html; charset=utf-8", false},
		{"GET /r/{id}", "room.html", "text/html; charset=utf-8", false},
		{"GET /app.js", "app.js", "text/javascript; charset=utf-8", true},
		{"GET /style.css", "style.css", "text/css; charset=utf-8", true},
		{"GET /llms.txt", "llms.txt", "text/plain; charset=utf-8", false},
		{"GET /openapi.yaml", "openapi.yaml", "application/yaml; charset=utf-8", false},
		{"GET /skill", "skill.md", "text/markdown; charset=utf-8", false},
	}
	for _, rt := range routes {
		body, err := web.Files.ReadFile(rt.file)
		if err != nil {
			panic(fmt.Sprintf("embedded file %s: %v", rt.file, err))
		}
		if strings.HasSuffix(rt.file, ".html") {
			body = bytes.ReplaceAll(body, []byte("{{version}}"), []byte(s.Version))
		}
		contentType, cache := rt.contentType, rt.cache
		mux.HandleFunc(rt.pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			if cache {
				// Assets are tiny but the origin is a Pi behind Cloudflare;
				// give the edge something to hold onto. HTML stays
				// uncached so deploys show up promptly.
				w.Header().Set("Cache-Control", "public, max-age=300")
			}
			w.Write(body)
		})
	}
}
