package main

import (
	"bytes"
	"net/http"
	"regexp"
	"strings"
)

// Serving under a sub-path (e.g. https://example.org/dudle/) behind a reverse
// proxy that forwards the path unchanged. The prefix is stripped on the way in
// and added to generated links, redirects and cookie paths on the way out.

// normalizeBasePath turns "dudle", "/dudle/" etc. into "/dudle"; "" and "/" mean no prefix.
func normalizeBasePath(p string) string {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p == "" {
		return ""
	}
	return "/" + p
}

// SetBasePath makes the server answer under the given path prefix.
func (s *Server) SetBasePath(p string) { s.basePath = normalizeBasePath(p) }

// withBasePath strips the prefix from incoming requests and prefixes
// redirects. Requests outside the prefix get a 404.
func (s *Server) withBasePath(next http.Handler) http.Handler {
	if s.basePath == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == s.basePath:
			http.Redirect(w, r, s.basePath+"/", http.StatusMovedPermanently)
		case strings.HasPrefix(r.URL.Path, s.basePath+"/"):
			w = &prefixWriter{ResponseWriter: w, prefix: s.basePath}
			http.StripPrefix(s.basePath, next).ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// prefixWriter adds the prefix to root-relative Location headers.
type prefixWriter struct {
	http.ResponseWriter
	prefix string
}

func (p *prefixWriter) WriteHeader(code int) {
	h := p.Header()
	if loc := h.Get("Location"); strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, "//") {
		h.Set("Location", p.prefix+loc)
	}
	p.ResponseWriter.WriteHeader(code)
}

var rootAttr = regexp.MustCompile(`((?:href|action|src)=")/([^/])`)

// prefixHTML rewrites root-relative href/action/src attributes.
func (s *Server) prefixHTML(b []byte) []byte {
	if s.basePath == "" {
		return b
	}
	b = rootAttr.ReplaceAll(b, []byte("${1}"+s.basePath+"/${2}"))
	// href="/" and action="/" (the bare root)
	for _, a := range []string{"href", "action", "src"} {
		b = bytes.ReplaceAll(b, []byte(a+`="/"`), []byte(a+`="`+s.basePath+`/"`))
	}
	return b
}
