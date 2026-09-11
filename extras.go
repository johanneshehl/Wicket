package main

import "net/http"

// withExtras adds endpoints that live outside the page router (and outside the setup gate).
func (a *App) withExtras(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" && r.Method == http.MethodGet {
			a.handleMetrics(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
