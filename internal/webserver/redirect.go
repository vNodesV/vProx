package webserver

import (
	"fmt"
	"net/http"
)

// NewRedirectHandler returns an http.Handler that issues a permanent 301
// redirect from HTTP to HTTPS for every request, preserving the original
// path and query string.
//
// The target scheme is always "https". The host is taken from the incoming
// request's Host header (or r.Host), so the same handler works for all vhosts
// on the listener without per-vhost configuration.
func NewRedirectHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := fmt.Sprintf("https://%s%s", r.Host, r.RequestURI)
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})
}
