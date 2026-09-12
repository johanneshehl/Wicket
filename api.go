package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxSess
)

func reqUser(r *http.Request) *User       { u, _ := r.Context().Value(ctxUser).(*User); return u }
func reqSession(r *http.Request) *Session { s, _ := r.Context().Value(ctxSess).(*Session); return s }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// errKey answers with a translated error message.
func (a *App) errKey(w http.ResponseWriter, r *http.Request, status int, key string, args ...any) {
	writeErr(w, status, tr(a.langFor(r), key, args...))
}

// fail answers user errors with 400 and anything else with a logged 500.
func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ue userError
	if errors.As(err, &ue) {
		writeErr(w, http.StatusBadRequest, tr(a.langFor(r), ue.key, ue.args...))
		return
	}
	log.Printf("api: %v", err)
	a.errKey(w, r, http.StatusInternalServerError, "err.internal")
}

func readJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v); err != nil {
		return userErr("err.badRequest")
	}
	return nil
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, userErr("err.badID")
	}
	return id, nil
}

// adminAPI serves /api/* on the admin host for signed-in admins only.
// Mutating requests must carry X-Wicket: 1 – browsers cannot add that header cross-site without CORS.
func (a *App) adminAPI() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/me", a.apiMe)
	mux.HandleFunc("PUT /api/me/password", a.apiMePassword)
	mux.HandleFunc("POST /api/me/2fa", a.apiMe2FABegin)
	mux.HandleFunc("POST /api/me/2fa/confirm", a.apiMe2FAConfirm)
	mux.HandleFunc("POST /api/me/2fa/disable", a.apiMe2FADisable)
	mux.HandleFunc("POST /api/me/recovery", a.apiMeRecovery)

	mux.HandleFunc("GET /api/overview", a.apiOverview)
	mux.HandleFunc("GET /api/sites", a.apiSites)
	mux.HandleFunc("POST /api/sites", a.apiSiteCreate)
	mux.HandleFunc("PUT /api/sites/{id}", a.apiSiteUpdate)
	mux.HandleFunc("DELETE /api/sites/{id}", a.apiSiteDelete)
	mux.HandleFunc("GET /api/dns", a.apiDNS)
	a.registerExtAPI(mux)

	mux.HandleFunc("GET /api/users", a.apiUsers)
	mux.HandleFunc("POST /api/users", a.apiUserCreate)
	mux.HandleFunc("PUT /api/users/{id}", a.apiUserUpdate)
	mux.HandleFunc("DELETE /api/users/{id}", a.apiUserDelete)
	mux.HandleFunc("PUT /api/users/{id}/password", a.apiUserPassword)
	mux.HandleFunc("POST /api/users/{id}/reset-2fa", a.apiUserReset2FA)
	mux.HandleFunc("POST /api/users/{id}/logout", a.apiUserLogoutAll)
	mux.HandleFunc("GET /api/users/{id}/sessions", a.apiUserSessions)
	mux.HandleFunc("DELETE /api/sessions/{id}", a.apiSessionDelete)

	mux.HandleFunc("GET /api/events", a.apiEvents)
	mux.HandleFunc("GET /api/events.csv", a.apiEventsCSV)
	mux.HandleFunc("GET /api/settings", a.apiSettingsGet)
	mux.HandleFunc("PUT /api/settings", a.apiSettingsPut)
	mux.HandleFunc("GET /api/oauth", a.apiOAuthList)
	mux.HandleFunc("PUT /api/oauth/{provider}", a.apiOAuthSave)
	mux.HandleFunc("POST /api/oauth/{provider}/test", a.apiOAuthTest)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdminHost(r) {
			http.NotFound(w, r)
			return
		}
		sess, user := a.activeSession(r)
		if sess == nil || !canAdmin(user) {
			a.errKey(w, r, http.StatusUnauthorized, "err.notSignedIn")
			return
		}
		if a.needs2FASetup(user, nil) {
			writeErr(w, http.StatusForbidden, "2fa_setup_required")
			return
		}
		if r.Method != http.MethodGet && r.Header.Get("X-Wicket") != "1" {
			a.errKey(w, r, http.StatusForbidden, "err.rejected")
			return
		}
		// auditors read everything but may only change their own account
		if user.Role == "auditor" && r.Method != http.MethodGet && !strings.HasPrefix(r.URL.Path, "/api/me") {
			a.errKey(w, r, http.StatusForbidden, "err.readOnly")
			return
		}
		if now()-sess.LastSeen > 60 {
			_ = a.store.TouchSession(sess.ID)
			_ = a.store.TouchUser(user.ID)
		}
		ctx := context.WithValue(context.WithValue(r.Context(), ctxUser, user), ctxSess, sess)
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ---------------------------------------------------------------- event groups

var kindGroups = map[string][]string{
	"ok":   {"login_ok", "mfa_enabled", "recovery_used", "logout", "oidc_login", "password_reset", "password_reset_requested", "invite_accepted"},
	"fail": {"login_fail_password", "login_fail_user", "mfa_fail", "denied", "blocked"},
	"lock": {"locked"},
	"admin": {"user_created", "user_updated", "user_deleted", "password_set", "mfa_reset", "sessions_revoked", "site_created", "site_updated", "site_deleted", "settings_changed",
		"group_created", "group_updated", "group_deleted", "oidc_client_created", "oidc_client_updated", "oidc_client_deleted", "invite_sent"},
}

var failKinds = []string{"login_fail_password", "login_fail_user", "mfa_fail"}

func loginKinds() []string {
	var out []string
	for _, g := range []string{"ok", "fail", "lock"} {
		out = append(out, kindGroups[g]...)
	}
	return out
}

// ---------------------------------------------------------------- overview

func (a *App) apiOverview(w http.ResponseWriter, r *http.Request) {
	sites, err := a.store.ListSites()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if sites == nil {
		sites = []*Site{}
	}
	enabled := 0
	for _, s := range sites {
		if s.Enabled {
			enabled++
		}
	}
	sessions, _ := a.store.CountActiveSessions()
	t := now()
	fails, _ := a.store.CountEvents(failKinds, t-86400, t+1)
	prev, _ := a.store.CountEvents(failKinds, t-2*86400, t-86400)
	events, _, err := a.store.ListEvents(EventQuery{Kinds: loginKinds(), Limit: 6})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sites":  sites,
		"events": events,
		"stats": map[string]int{
			"sites": enabled, "sessions": sessions, "fails24h": fails, "failsPrev": prev, "locked": a.limiter.LockedCount(),
		},
	})
}

// ---------------------------------------------------------------- sites

var (
	domainRE = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,63}$`)
	targetRE = regexp.MustCompile(`^(https?://)?[A-Za-z0-9.-]+:[0-9]{1,5}$`)
	bypassRE = regexp.MustCompile(`^/[A-Za-z0-9._~/-]*\*?$`)
)

func (a *App) validateSite(in *Site) error {
	in.Domain = strings.ToLower(strings.TrimSpace(in.Domain))
	in.Target = strings.TrimSpace(in.Target)
	if !domainRE.MatchString(in.Domain) {
		return userErr("err.siteDomain")
	}
	s := a.settings()
	if in.Domain == s.LoginHost || in.Domain == s.AdminHost {
		return userErr("err.siteOwnHost")
	}
	// the Caddyfile already has a block for this domain: protect that one instead of adding a second
	if in.Managed && a.caddyfileHasSite(in.Domain) {
		in.Managed = false
	}
	if in.Managed {
		if strings.HasPrefix(in.Domain, "*.") {
			return userErr("err.wildcard")
		}
		if !targetRE.MatchString(in.Target) {
			return userErr("err.target")
		}
	}
	switch in.Access {
	case "all", "admins", "users":
	default:
		return userErr("err.access")
	}
	if in.Access != "users" || in.Users == nil {
		in.Users = []int64{}
	}
	bypass := []string{}
	for _, b := range in.Bypass {
		if b = strings.TrimSpace(b); b == "" {
			continue
		}
		if !bypassRE.MatchString(b) {
			return userErr("err.bypass", b)
		}
		bypass = append(bypass, b)
	}
	in.Bypass = bypass
	if in.Access != "users" || in.Groups == nil {
		in.Groups = []int64{}
	}
	allow, err := normalizeIPRules(in.AllowIPs)
	if err != nil {
		return userErr("err.ipRule", err.Error())
	}
	deny, err := normalizeIPRules(in.DenyIPs)
	if err != nil {
		return userErr("err.ipRule", err.Error())
	}
	in.AllowIPs, in.DenyIPs = allow, deny
	if err := between(in.MaxSessionHours, 0, 720, "f.maxSession"); err != nil {
		return err
	}
	return nil
}

func (a *App) apiSites(w http.ResponseWriter, r *http.Request) {
	sites, err := a.store.ListSites()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if sites == nil {
		sites = []*Site{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sites": sites, "caddy": a.caddyStatus(), "authAddr": a.cfg.AuthAddr})
}

func (a *App) apiSiteCreate(w http.ResponseWriter, r *http.Request) {
	var in Site
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.validateSite(&in); err != nil {
		a.fail(w, r, err)
		return
	}
	id, err := a.store.CreateSite(&in)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			a.errKey(w, r, http.StatusConflict, "err.domainExists")
			return
		}
		a.fail(w, r, err)
		return
	}
	if err := a.syncCaddy(); err != nil {
		_ = a.store.DeleteSite(id)
		a.errKey(w, r, http.StatusBadRequest, "err.caddyRejected", err.Error())
		return
	}
	a.event(r, "site_created", reqUser(r).Username, in.Domain, "")
	site, _ := a.store.SiteByID(id)
	writeJSON(w, http.StatusCreated, site)
}

func (a *App) apiSiteUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	old, err := a.store.SiteByID(id)
	if err != nil || old == nil {
		a.errKey(w, r, http.StatusNotFound, "err.siteNotFound")
		return
	}
	var in Site
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	in.ID = id
	if err := a.validateSite(&in); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.store.UpdateSite(&in); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			a.errKey(w, r, http.StatusConflict, "err.domainExists")
			return
		}
		a.fail(w, r, err)
		return
	}
	if err := a.syncCaddy(); err != nil {
		_ = a.store.UpdateSite(old)
		a.errKey(w, r, http.StatusBadRequest, "err.caddyRejected", err.Error())
		return
	}
	a.event(r, "site_updated", reqUser(r).Username, in.Domain, "")
	site, _ := a.store.SiteByID(id)
	writeJSON(w, http.StatusOK, site)
}

func (a *App) apiSiteDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	old, err := a.store.SiteByID(id)
	if err != nil || old == nil {
		a.errKey(w, r, http.StatusNotFound, "err.siteNotFound")
		return
	}
	if err := a.store.DeleteSite(id); err != nil {
		a.fail(w, r, err)
		return
	}
	warning := ""
	if err := a.syncCaddy(); err != nil {
		warning = tr(a.langFor(r), "warn.caddyReload", err.Error())
	}
	a.event(r, "site_deleted", reqUser(r).Username, old.Domain, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "warning": warning})
}

func (a *App) apiDNS(w http.ResponseWriter, r *http.Request) {
	raw := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("domain")))
	domain := strings.TrimPrefix(raw, "*.")
	if !hostRE.MatchString(domain) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "reason": "invalid"})
		return
	}
	caddyBlock := a.caddyfileHasSite(raw)
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupHost(ctx, domain)
	if err != nil || len(ips) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "reason": "missing", "caddyBlock": caddyBlock})
		return
	}
	s := a.settings()
	ref := s.LoginHost
	if ref == "" {
		ref = s.AdminHost
	}
	var server []string
	if ref != "" {
		server, _ = net.DefaultResolver.LookupHost(ctx, ref)
	}
	match := false
	for _, ip := range ips {
		for _, sip := range server {
			if ip == sip {
				match = true
			}
		}
	}
	reason := "match"
	if !match {
		reason = "other"
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": match, "reason": reason, "ips": ips, "serverIps": server, "caddyBlock": caddyBlock})
}

// ---------------------------------------------------------------- users

type userView struct {
	*User
	Sessions     int     `json:"sessions"`
	RecoveryLeft int     `json:"recoveryLeft"`
	Groups       []int64 `json:"groups"`
}

func (a *App) apiUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.store.ListUsers()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	counts, err := a.store.SessionCounts()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	groups, err := a.store.ListGroups()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	memberOf := map[int64][]int64{}
	for _, g := range groups {
		for _, uid := range g.Members {
			memberOf[uid] = append(memberOf[uid], g.ID)
		}
	}
	out := []userView{}
	for _, u := range users {
		left, _ := a.store.RecoveryLeft(u.ID)
		gids := memberOf[u.ID]
		if gids == nil {
			gids = []int64{}
		}
		out = append(out, userView{User: u, Sessions: counts[u.ID], RecoveryLeft: left, Groups: gids})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out, "me": reqUser(r).ID})
}

func (a *App) apiUserCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username, Email, Role, Password string
		Invite                          bool // no password: the user chooses one via an e-mailed link
	}
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	in.Email = strings.TrimSpace(in.Email)
	if in.Invite {
		switch {
		case !a.mailEnabled():
			a.errKey(w, r, http.StatusBadRequest, "err.smtpMissing")
			return
		case !strings.Contains(in.Email, "@"):
			a.errKey(w, r, http.StatusBadRequest, "err.userNoEmail")
			return
		}
		in.Password = newToken() // unusable until the invitation is accepted
	}
	switch {
	case !usernameRE.MatchString(in.Username):
		a.errKey(w, r, http.StatusBadRequest, "err.username")
		return
	case !validRole(in.Role):
		a.errKey(w, r, http.StatusBadRequest, "err.role")
		return
	case len(in.Password) < 10:
		a.errKey(w, r, http.StatusBadRequest, "err.password10")
		return
	}
	id, err := a.store.CreateUser(&User{Username: in.Username, Email: strings.TrimSpace(in.Email), Role: in.Role, PasswordHash: hashPassword(in.Password)})
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			a.errKey(w, r, http.StatusConflict, "err.userExists")
			return
		}
		a.fail(w, r, err)
		return
	}
	a.event(r, "user_created", reqUser(r).Username, "", in.Username)
	u, _ := a.store.UserByID(id)
	if in.Invite && u != nil {
		if err := a.issueLink(u, "invite", a.notifyLang(), inviteTTL); err != nil {
			// the user exists; the admin can resend the invitation from the user panel
			writeJSON(w, http.StatusCreated, map[string]any{"user": u, "inviteError": msgFor(a.langFor(r), err)})
			return
		}
		a.event(r, "invite_sent", reqUser(r).Username, "", u.Username)
	}
	writeJSON(w, http.StatusCreated, u)
}

func (a *App) loadUser(w http.ResponseWriter, r *http.Request) *User {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, r, err)
		return nil
	}
	u, err := a.store.UserByID(id)
	if err != nil || u == nil {
		a.errKey(w, r, http.StatusNotFound, "err.userNotFound")
		return nil
	}
	return u
}

func (a *App) lastAdmin(u *User) bool {
	if u.Role != "admin" || u.Disabled {
		return false
	}
	n, err := a.store.CountAdmins()
	return err == nil && n <= 1
}

func (a *App) apiUserUpdate(w http.ResponseWriter, r *http.Request) {
	u := a.loadUser(w, r)
	if u == nil {
		return
	}
	var in struct {
		Email    string
		Role     string
		Disabled bool
		Groups   *[]int64 // nil = keep memberships
	}
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	if !validRole(in.Role) {
		a.errKey(w, r, http.StatusBadRequest, "err.role")
		return
	}
	demote := in.Role != "admin" || in.Disabled
	if demote && u.ID == reqUser(r).ID {
		a.errKey(w, r, http.StatusBadRequest, "err.selfDemote")
		return
	}
	if demote && a.lastAdmin(u) {
		a.errKey(w, r, http.StatusBadRequest, "err.lastAdmin")
		return
	}
	u.Email, u.Role, u.Disabled = strings.TrimSpace(in.Email), in.Role, in.Disabled
	if err := a.store.UpdateUser(u); err != nil {
		a.fail(w, r, err)
		return
	}
	if u.Disabled {
		_ = a.store.DeleteUserSessions(u.ID)
	}
	if in.Groups != nil {
		if err := a.store.SetUserGroups(u.ID, *in.Groups); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	a.event(r, "user_updated", reqUser(r).Username, "", u.Username)
	writeJSON(w, http.StatusOK, u)
}

func (a *App) apiUserDelete(w http.ResponseWriter, r *http.Request) {
	u := a.loadUser(w, r)
	if u == nil {
		return
	}
	if u.ID == reqUser(r).ID {
		a.errKey(w, r, http.StatusBadRequest, "err.selfDelete")
		return
	}
	if a.lastAdmin(u) {
		a.errKey(w, r, http.StatusBadRequest, "err.lastAdmin")
		return
	}
	if err := a.store.DeleteUser(u.ID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.event(r, "user_deleted", reqUser(r).Username, "", u.Username)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) apiUserPassword(w http.ResponseWriter, r *http.Request) {
	u := a.loadUser(w, r)
	if u == nil {
		return
	}
	var in struct{ Password string }
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	if len(in.Password) < 10 {
		a.errKey(w, r, http.StatusBadRequest, "err.password10")
		return
	}
	if err := a.store.SetPassword(u.ID, hashPassword(in.Password)); err != nil {
		a.fail(w, r, err)
		return
	}
	if u.ID != reqUser(r).ID {
		_ = a.store.DeleteUserSessions(u.ID)
	}
	a.event(r, "password_set", reqUser(r).Username, "", u.Username)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) apiUserReset2FA(w http.ResponseWriter, r *http.Request) {
	u := a.loadUser(w, r)
	if u == nil {
		return
	}
	if u.ID == reqUser(r).ID {
		a.errKey(w, r, http.StatusBadRequest, "err.selfReset2fa")
		return
	}
	if err := a.store.DisableTOTP(u.ID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.event(r, "mfa_reset", reqUser(r).Username, "", u.Username)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) apiUserLogoutAll(w http.ResponseWriter, r *http.Request) {
	u := a.loadUser(w, r)
	if u == nil {
		return
	}
	if err := a.store.DeleteUserSessions(u.ID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.event(r, "sessions_revoked", reqUser(r).Username, "", u.Username)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true, "self": u.ID == reqUser(r).ID})
}

func (a *App) apiUserSessions(w http.ResponseWriter, r *http.Request) {
	u := a.loadUser(w, r)
	if u == nil {
		return
	}
	sessions, err := a.store.ListUserSessions(u.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	type view struct {
		*Session
		Current bool `json:"current"`
	}
	out := []view{}
	cur := reqSession(r).ID
	for _, s := range sessions {
		out = append(out, view{Session: s, Current: s.ID == cur})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (a *App) apiSessionDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.store.DeleteSession(id); err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------- events

func eventQuery(r *http.Request) EventQuery {
	q := r.URL.Query()
	eq := EventQuery{Kinds: kindGroups[q.Get("kind")], Site: q.Get("site"), Limit: 50}
	ranges := map[string]int64{"24h": 86400, "7d": 7 * 86400, "30d": 30 * 86400, "90d": 90 * 86400}
	if d, ok := ranges[q.Get("range")]; ok {
		eq.Since = now() - d
	} else {
		eq.Since = now() - 86400
	}
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 1 {
		eq.Offset = (p - 1) * eq.Limit
	}
	return eq
}

func (a *App) apiEvents(w http.ResponseWriter, r *http.Request) {
	q := eventQuery(r)
	items, total, err := a.store.ListEvents(q)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "pageSize": q.Limit})
}

func (a *App) apiEventsCSV(w http.ResponseWriter, r *http.Request) {
	q := eventQuery(r)
	q.Limit, q.Offset = 100000, 0
	items, _, err := a.store.ListEvents(q)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	lang := a.langFor(r)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+tr(lang, "csv.filename")+`"`)
	w.Write([]byte("\xef\xbb\xbf")) // BOM so Excel detects UTF-8
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	_ = cw.Write(strings.Split(tr(lang, "csv.header"), ";"))
	for _, e := range items {
		_ = cw.Write([]string{time.Unix(e.At, 0).Format("2006-01-02 15:04:05"), e.Kind, e.Username, e.Site, e.IP, e.UA, e.Detail})
	}
	cw.Flush()
}

// ---------------------------------------------------------------- settings

func (a *App) apiSettingsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"settings": a.settings(), "caddy": a.caddyStatus(), "version": version, "authAddr": a.cfg.AuthAddr, "templates": loginTemplates, "languages": languages})
}

func between(v, lo, hi int, name string) error {
	if v < lo || v > hi {
		return userErr("err.between", trKey(name), lo, hi)
	}
	return nil
}

func (a *App) apiSettingsPut(w http.ResponseWriter, r *http.Request) {
	var in Settings
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	in.CookieDomain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(in.CookieDomain)), ".")
	in.LoginHost = strings.ToLower(strings.TrimSpace(in.LoginHost))
	in.AdminHost = strings.ToLower(strings.TrimSpace(in.AdminHost))
	if err := validateHosts(in.CookieDomain, in.LoginHost, in.AdminHost); err != nil {
		a.fail(w, r, err)
		return
	}
	in.CookieDomain = "." + in.CookieDomain
	if in.Language != "auto" && !supportedLang(in.Language) {
		a.errKey(w, r, http.StatusBadRequest, "err.language")
		return
	}
	if !validTemplate(in.LoginTemplate) {
		a.errKey(w, r, http.StatusBadRequest, "err.template")
		return
	}
	for _, err := range []error{
		between(in.LockAttempts, 3, 50, "f.lockAttempts"),
		between(in.LockWindowSec, 30, 3600, "f.lockWindow"),
		between(in.LockDurationSec, 60, 86400, "f.lockDuration"),
		between(in.SessionHours, 1, 720, "f.sessionHours"),
		between(in.RememberDays, 1, 365, "f.rememberDays"),
		between(in.LogRetentionDays, 7, 3650, "f.retention"),
	} {
		if err != nil {
			a.fail(w, r, err)
			return
		}
	}
	old := a.settings()
	if err := a.saveSettings(in); err != nil {
		a.fail(w, r, err)
		return
	}
	relogin := in.CookieDomain != old.CookieDomain
	if relogin {
		_ = a.store.DeleteAllSessions()
	}
	caddyErr := ""
	if in.LoginHost != old.LoginHost || in.AdminHost != old.AdminHost {
		if err := a.syncCaddy(); err != nil {
			caddyErr = err.Error()
		}
	}
	a.event(r, "settings_changed", reqUser(r).Username, "", "")
	writeJSON(w, http.StatusOK, map[string]any{"settings": in, "relogin": relogin, "caddyError": caddyErr, "lang": a.langFor(r)})
}

// ---------------------------------------------------------------- my account

func (a *App) apiMe(w http.ResponseWriter, r *http.Request) {
	u := reqUser(r)
	left, _ := a.store.RecoveryLeft(u.ID)
	s := a.settings()
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "recoveryLeft": left, "enforce2fa": s.EnforceAdmin2FA,
		"domain": rootDomain(s), "version": version, "lang": a.langFor(r)})
}

func (a *App) apiMePassword(w http.ResponseWriter, r *http.Request) {
	u := reqUser(r)
	var in struct{ Current, New string }
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	if !checkPassword(u.PasswordHash, in.Current) {
		a.errKey(w, r, http.StatusBadRequest, "err.currentPassword")
		return
	}
	if len(in.New) < 10 {
		a.errKey(w, r, http.StatusBadRequest, "err.newPassword10")
		return
	}
	if err := a.store.SetPassword(u.ID, hashPassword(in.New)); err != nil {
		a.fail(w, r, err)
		return
	}
	a.event(r, "password_set", u.Username, "", u.Username)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) apiMe2FABegin(w http.ResponseWriter, r *http.Request) {
	u := reqUser(r)
	if u.TOTPEnabled {
		a.errKey(w, r, http.StatusBadRequest, "err.2faActive")
		return
	}
	p := &page{}
	if err := a.fillTOTP(p, u, a.pendingSecret(u.ID)); err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"qr": string(p.QR), "secret": p.Secret, "secretGrouped": p.SecretGrouped})
}

func (a *App) apiMe2FAConfirm(w http.ResponseWriter, r *http.Request) {
	u := reqUser(r)
	var in struct{ Code string }
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	secret := a.pendingSecret(u.ID)
	step := verifyTOTP(secret, in.Code, 0)
	if step == 0 {
		a.errKey(w, r, http.StatusBadRequest, "err.codeClock")
		return
	}
	codes, err := a.enableTOTP(r, u, reqSession(r), secret, step)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"codes": codes})
}

func (a *App) apiMeRecovery(w http.ResponseWriter, r *http.Request) {
	u := reqUser(r)
	var in struct{ Password string }
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	if !checkPassword(u.PasswordHash, in.Password) {
		a.errKey(w, r, http.StatusBadRequest, "err.passwordWrong")
		return
	}
	if !u.TOTPEnabled {
		a.errKey(w, r, http.StatusBadRequest, "err.2faInactive")
		return
	}
	codes, err := a.newRecoveryCodes(u.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"codes": codes})
}

func (a *App) apiMe2FADisable(w http.ResponseWriter, r *http.Request) {
	u := reqUser(r)
	var in struct{ Password string }
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	if !checkPassword(u.PasswordHash, in.Password) {
		a.errKey(w, r, http.StatusBadRequest, "err.passwordWrong")
		return
	}
	if a.settings().EnforceAdmin2FA {
		a.errKey(w, r, http.StatusBadRequest, "err.2faEnforced")
		return
	}
	if err := a.store.DisableTOTP(u.ID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.event(r, "mfa_reset", u.Username, "", u.Username)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
