package main

import (
	"net/http"
	"net/url"
	"path"
	"strings"
)

// forwardedTarget reads the original request from the proxy headers.
// Caddy and Traefik send X-Forwarded-Host/-Uri/-Proto; nginx (auth_request) is configured to send X-Original-URL.
func forwardedTarget(r *http.Request) (host, uri, proto string) {
	if raw := r.Header.Get("X-Original-URL"); raw != "" {
		if u, err := url.Parse(raw); err == nil && u.Host != "" {
			proto = "https"
			if u.Scheme == "http" {
				proto = "http"
			}
			return hostOnly(u.Host), u.RequestURI(), proto
		}
	}
	host = hostOnly(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = hostOnly(r.Host)
	}
	uri = r.Header.Get("X-Forwarded-Uri")
	if uri == "" {
		uri = "/"
	}
	proto = r.Header.Get("X-Forwarded-Proto")
	if proto != "http" {
		proto = "https"
	}
	return host, uri, proto
}

// handleVerify is called by the reverse proxy before every request to a protected site.
// 2xx lets the request through. Caddy and Traefik pass any other response to the browser (a redirect to
// the login page); nginx's auth_request only understands 401/403, so in that mode Wicket answers 401 and
// puts the login URL into X-Wicket-Location.
func (a *App) handleVerify(w http.ResponseWriter, r *http.Request) {
	s := a.settings()
	lang := a.langFor(r)
	nginx := r.URL.Query().Get("mode") == "nginx" || r.Header.Get("X-Original-URL") != ""
	host, uri, proto := forwardedTarget(r)
	if host == s.LoginHost || host == s.AdminHost {
		http.NotFound(w, r)
		return
	}

	site, err := a.store.SiteForHost(host)
	if err != nil {
		http.Error(w, tr(lang, "verify.internal"), http.StatusInternalServerError)
		return
	}
	if site == nil {
		countMetric("wicket_verify_total", "unknown")
		http.Error(w, tr(lang, "verify.unknown"), http.StatusForbidden)
		return
	}
	ip := clientIP(r)
	if ipMatch(site.DenyIPs, ip) {
		countMetric("wicket_verify_total", "blocked")
		a.logThrottled(r, "blocked", "", host, ip)
		http.Error(w, tr(lang, "verify.blocked"), http.StatusForbidden)
		return
	}
	if !site.Enabled || bypassed(site.Bypass, uri) || ipMatch(site.AllowIPs, ip) {
		countMetric("wicket_verify_total", "bypass")
		w.WriteHeader(http.StatusOK)
		return
	}

	login := a.loginBase(s)
	original := proto + "://" + host + uri
	send := func(status int, target, result string) {
		countMetric("wicket_verify_total", result)
		if nginx {
			if status == http.StatusFound {
				status = http.StatusUnauthorized
			}
			w.Header().Set("X-Wicket-Location", target)
			http.Error(w, http.StatusText(status), status)
			return
		}
		http.Redirect(w, r, target, http.StatusFound)
	}

	sess, user := a.activeSession(r)
	if sess == nil {
		send(http.StatusFound, login+"/?rd="+url.QueryEscape(original), "redirect")
		return
	}
	if site.MaxSessionHours > 0 && now()-sess.CreatedAt > int64(site.MaxSessionHours)*3600 {
		send(http.StatusFound, login+"/?reauth=1&rd="+url.QueryEscape(original), "reauth")
		return
	}
	if a.needs2FASetup(user, site) {
		send(http.StatusFound, login+"/setup-2fa?rd="+url.QueryEscape(original), "redirect")
		return
	}
	if !a.allowed(site, user) {
		a.logThrottled(r, "denied", user.Username, host, "")
		send(http.StatusForbidden, login+"/denied?site="+url.QueryEscape(host), "denied")
		return
	}
	if now()-sess.LastSeen > 60 {
		_ = a.store.TouchSession(sess.ID)
		_ = a.store.TouchUser(user.ID)
	}
	countMetric("wicket_verify_total", "allow")
	w.Header().Set("Remote-User", user.Username)
	w.Header().Set("Remote-Role", user.Role)
	if user.Email != "" {
		w.Header().Set("Remote-Email", user.Email)
	}
	if _, names, err := a.store.UserGroups(user.ID); err == nil && len(names) > 0 {
		w.Header().Set("Remote-Groups", strings.Join(names, ","))
	}
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

// allowed: admins everywhere; otherwise the site's rule (all users, admins only, or selected users and groups).
func (a *App) allowed(site *Site, u *User) bool {
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
		return a.store.inAnyGroup(u.ID, site.Groups)
	}
	return false
}

// logThrottled records denials and blocks at most every 5 minutes per user/IP and site
// (assets of a page would otherwise flood the log).
func (a *App) logThrottled(r *http.Request, kind, username, host, detail string) {
	key := kind + "|" + username + "|" + host + "|" + detail
	a.mu.Lock()
	last := a.deniedSeen[key]
	if now()-last < 300 {
		a.mu.Unlock()
		return
	}
	a.deniedSeen[key] = now()
	a.mu.Unlock()
	a.event(r, kind, username, host, detail)
}

// logDenied is kept for callers outside verify.
func (a *App) logDenied(r *http.Request, username, host string) {
	a.logThrottled(r, "denied", username, host, "")
}
