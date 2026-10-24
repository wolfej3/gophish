package controllers

import (
	"net"
	"net/http"

	"github.com/gorilla/handlers"
)

// trustedProxyHeaders applies handlers.ProxyHeaders (which rewrites
// r.RemoteAddr from X-Forwarded-For / X-Real-IP) only when the direct peer is
// one of the trusted proxy networks. Requests from any other peer pass
// through untouched, so an attacker can't spoof their IP to bypass the rate
// limiter or poison the audit log and campaign results.
func trustedProxyHeaders(trusted []*net.IPNet, next http.Handler) http.Handler {
	if len(trusted) == 0 {
		return next
	}
	withProxy := handlers.ProxyHeaders(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if ip := net.ParseIP(host); ip != nil {
			for _, network := range trusted {
				if network.Contains(ip) {
					withProxy.ServeHTTP(w, r)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
