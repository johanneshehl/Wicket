package main

import (
	"net/http"
	"net/url"
	"path"
	"strings"
)

// handleVerify is called by Caddy's forward_auth before every request to a protected site.
// 2xx lets the request through; any other response (redirect to the login page) is sent to the browser.
func (a *App) handleVerify(w http.ResponseWriter, r *http.Request) {
	s := a.settings()
	host := hostOnly(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = hostOnly(r.Host)
	}
	if host == s.LoginHost || host == s.AdminHost {
		http.NotFound(w, r)
		return
	}
	uri := r.Header.Get("X-Forwarded-Uri")
	if uri == "" {
		uri = "/"
	}
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto != "http" {
		proto = "https"
	}

	site, err := a.store.SiteForHost(host)
	if err != nil {
		http.Error(w, "Wicket: interner Fehler", http.StatusInternalServerError)
		return
	}
	if site == nil {
		http.Error(w, "Wicket: diese Domain ist nicht eingerichtet", http.StatusForbidden)
		return
	}
	if !site.Enabled || bypassed(site.Bypass, uri) {
		w.WriteHeader(http.StatusOK)
		return
	}

	login := a.loginBase(s)
	original := proto + "://" + host + uri
	sess, user := a.activeSession(r)
	if sess == nil {
		http.Redirect(w, r, login+"/?rd="+url.QueryEscape(original), http.StatusFound)
		return
	}
	if a.needs2FASetup(user, site) {
		http.Redirect(w, r, login+"/setup-2fa?rd="+url.QueryEscape(original), http.StatusFound)
		return
	}
	if !allowed(site, user) {
		a.logDenied(r, user.Username, host)
		http.Redirect(w, r, login+"/denied?site="+url.QueryEscape(host), http.StatusFound)
		return
	}
	if now()-sess.LastSeen > 60 {
		_ = a.store.TouchSession(sess.ID)
		_ = a.store.TouchUser(user.ID)
	}
	w.Header().Set("Remote-User", user.Username)
	w.Header().Set("Remote-Role", user.Role)
	w.WriteHeader(http.StatusOK)
}

// bypassed matches the cleaned request path against exact paths or "prefix*" patterns.
// Cleaning first means "/healthz/../admin" cannot sneak past as "/healthz".
func bypassed(patterns []string, uri string) bool {
	if len(patterns) == 0 {
		return false
	}
	p := uri
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if u, err := url.PathUnescape(p); err == nil {
		p = u
	}
	p = path.Clean("/" + p)
	for _, pat := range patterns {
		if strings.HasSuffix(pat, "*") {
			if strings.HasPrefix(p, strings.TrimSuffix(pat, "*")) {
				return true
			}
		} else if p == path.Clean(pat) {
			return true
		}
	}
	return false
}

func allowed(site *Site, u *User) bool {
	if u.Role == "admin" {
		return true
	}
	switch site.Access {
	case "all":
		return true
	case "users":
		for _, id := range site.Users {
			if id == u.ID {
				return true
			}
		}
	}
	return false
}

// logDenied records a denial at most every 5 minutes per user and site (assets would flood the log).
func (a *App) logDenied(r *http.Request, username, host string) {
	key := username + "|" + host
	a.mu.Lock()
	last := a.deniedSeen[key]
	if now()-last < 300 {
		a.mu.Unlock()
		return
	}
	a.deniedSeen[key] = now()
	a.mu.Unlock()
	a.event(r, "denied", username, host, "")
}
