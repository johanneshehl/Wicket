package main

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

type page struct {
	Lang, Title, Error, RD, Target, TargetInitial, Username, CSRF, Kind, Site, Domain, Version string
	Tpl, StackClass, TargetName, TargetRest                                                    string
	Admin, Recovery, Forced, Preview                                                           bool
	RememberDays                                                                               int
	Sites                                                                                      []string
	More                                                                                       int
	Remaining                                                                                  string
	RemainingSec                                                                               int64
	Step                                                                                       int
	QR                                                                                         template.URL
	Secret, SecretGrouped                                                                      string
	Codes                                                                                      []string
	CodesText                                                                                  string
	CodesURL                                                                                   template.URL
	Continue                                                                                   string
	Providers                                                                                  []oauthButton
	CookieDomain, LoginHost, AdminHost, ImportLine                                             string
	Token, Info                                                                                string // reset / invitation links
	CanReset                                                                                   bool   // a mail server is configured
	Brand                                                                                      brandView
}

var (
	usernameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{3,32}$`)
	hostRE     = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,63}$`)
)

// splitHost turns "portainer.example.com" into "portainer" and ".example.com".
func splitHost(h string) (string, string) {
	if i := strings.Index(h, "."); i > 0 {
		return h[:i], h[i:]
	}
	return h, ""
}

func (a *App) basePage(w http.ResponseWriter, r *http.Request, titleKey string) *page {
	s := a.settings()
	p := &page{Lang: a.langFor(r), CSRF: a.csrfToken(w, r), Admin: a.isAdminHost(r), RememberDays: s.RememberDays,
		Version: version, Domain: rootDomain(s), Tpl: "centered"}
	p.Title = p.T(titleKey)
	p.Providers = a.oauthButtons()
	p.CanReset = a.mailEnabled()
	p.Brand = a.branding().view()
	if !p.Admin {
		p.Tpl = s.LoginTemplate
	}
	p.RD = validRedirect(r.FormValue("rd"), s)
	if p.RD != "" {
		if u, err := url.Parse(p.RD); err == nil {
			p.Target = hostOnly(u.Host)
			p.TargetInitial = strings.ToUpper(p.Target[:1])
		}
	}
	if p.Target != "" {
		p.TargetName, p.TargetRest = splitHost(p.Target)
	} else {
		p.TargetName, p.TargetRest = splitHost(p.Domain)
	}
	return p
}

func (a *App) render(w http.ResponseWriter, status int, name string, p *page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Language", p.Lang)
	w.WriteHeader(status)
	if err := a.tmpl.ExecuteTemplate(w, name, p); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func tpl(p *page, name string) string {
	if p.Admin {
		return "admin_" + name + ".html"
	}
	return "site_" + name + ".html"
}

func (a *App) loginPath(r *http.Request) string {
	if a.isAdminHost(r) {
		return "/login"
	}
	return "/"
}

func (a *App) continueTarget(r *http.Request, rd string) string {
	if a.isAdminHost(r) || rd == "" {
		return "/"
	}
	return rd
}

func internalError(w http.ResponseWriter, err error) {
	log.Printf("error: %v", err)
	http.Error(w, "Internal error", http.StatusInternalServerError)
}

func (a *App) siteChips() ([]string, int) {
	sites, err := a.store.ListSites()
	if err != nil {
		return nil, 0
	}
	var names []string
	for _, s := range sites {
		if !s.Enabled {
			continue
		}
		label := strings.TrimPrefix(s.Domain, "*.")
		if i := strings.Index(label, "."); i > 0 {
			label = label[:i]
		}
		names = append(names, label)
	}
	if len(names) > 4 {
		return names[:4], len(names) - 4
	}
	return names, 0
}

// ---------------------------------------------------------------- login

func (a *App) handleRoot(w http.ResponseWriter, r *http.Request) {
	if !a.isAdminHost(r) {
		a.handleLoginPage(w, r)
		return
	}
	if a.phase() == 2 {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}
	sess, user := a.activeSession(r)
	if sess == nil || !canAdmin(user) {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if a.needs2FASetup(user, nil) {
		http.Redirect(w, r, "/setup-2fa", http.StatusFound)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFileFS(w, r, webFS, "web/static/admin.html")
}

func (a *App) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	p := a.basePage(w, r, "title.signin")
	if code := r.URL.Query().Get("e"); code != "" {
		p.Error = a.oauthErrorText(p, code, r.URL.Query().Get("p"))
	}
	// reauth=1: a site with a shorter session limit wants a fresh sign-in, so show the form anyway
	if sess, user := a.activeSession(r); sess != nil && r.URL.Query().Get("reauth") == "" {
		switch {
		case p.Admin && canAdmin(user):
			http.Redirect(w, r, "/", http.StatusFound)
			return
		case p.Admin:
			p.Error = p.T("err.signedInNoAdmin", user.Username)
		case p.RD != "":
			http.Redirect(w, r, p.RD, http.StatusFound)
			return
		default:
			p.Kind = "signedin"
			p.Title = p.T("title.signedin")
			p.StackClass = "wide"
			p.Username = user.Username
			a.render(w, http.StatusOK, "site_notice.html", p)
			return
		}
	}
	if rem, locked := a.limiter.Locked("ip:" + clientIP(r)); locked {
		a.renderLocked(w, p, rem)
		return
	}
	a.renderLogin(w, http.StatusOK, p)
}

func (a *App) renderLogin(w http.ResponseWriter, status int, p *page) {
	if p.Admin {
		p.Sites, p.More = a.siteChips()
		a.render(w, status, "admin_login.html", p)
		return
	}
	a.render(w, status, "site_login.html", p)
}

func (a *App) renderLocked(w http.ResponseWriter, p *page, remaining int64) {
	p.Kind = "locked"
	p.Title = p.T("title.locked")
	p.StackClass = "wide"
	p.RemainingSec = remaining
	p.Remaining = fmt.Sprintf("%02d:%02d", remaining/60, remaining%60)
	if p.Admin {
		p.Tpl = "centered"
	}
	a.render(w, http.StatusTooManyRequests, "site_notice.html", p)
}

func (a *App) lockIfNeeded(r *http.Request, key, site string) (int64, bool) {
	s := a.settings()
	if !a.limiter.Fail(key, s.LockAttempts, s.LockWindowSec, s.LockDurationSec) {
		return 0, false
	}
	a.event(r, "locked", "", site, fmt.Sprintf("lock:%d:%d", s.LockAttempts, s.LockDurationSec/60))
	rem, _ := a.limiter.Locked(key)
	return rem, true
}

func (a *App) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	p := a.basePage(w, r, "title.signin")
	key := "ip:" + clientIP(r)
	if rem, locked := a.limiter.Locked(key); locked {
		a.renderLocked(w, p, rem)
		return
	}
	p.Username = strings.TrimSpace(r.FormValue("username"))
	if !checkCSRF(r) {
		p.Error = p.T("err.csrf")
		a.renderLogin(w, http.StatusBadRequest, p)
		return
	}
	password := r.FormValue("password")
	remember := r.FormValue("remember") != ""

	u, err := a.store.UserByName(p.Username)
	if err != nil {
		internalError(w, err)
		return
	}
	ok := false
	if u != nil && !u.Disabled {
		ok = checkPassword(u.PasswordHash, password)
	} else {
		checkPassword(dummyHash, password)
	}
	if !ok {
		countMetric("wicket_logins_total", "failure")
		kind := "login_fail_password"
		if u == nil {
			kind = "login_fail_user"
		}
		a.event(r, kind, p.Username, p.Target, "")
		if rem, locked := a.lockIfNeeded(r, key, p.Target); locked {
			a.renderLocked(w, p, rem)
			return
		}
		p.Error = p.T("err.credentials")
		a.renderLogin(w, http.StatusUnauthorized, p)
		return
	}
	countMetric("wicket_logins_total", "success")
	if p.Admin && !canAdmin(u) {
		a.event(r, "denied", u.Username, hostOnly(r.Host), "not-admin")
		p.Error = p.T("err.noAdminAccess")
		a.renderLogin(w, http.StatusForbidden, p)
		return
	}
	a.limiter.Reset(key)

	if u.TOTPEnabled {
		if _, err := a.newSession(w, r, u, "pending", false, remember); err != nil {
			internalError(w, err)
			return
		}
		http.Redirect(w, r, "/2fa?rd="+url.QueryEscape(p.RD), http.StatusSeeOther)
		return
	}
	if _, err := a.newSession(w, r, u, "active", false, remember); err != nil {
		internalError(w, err)
		return
	}
	a.event(r, "login_ok", u.Username, p.Target, "")
	_ = a.store.TouchUser(u.ID)
	a.afterLogin(w, r, u, p.RD)
}

func (a *App) afterLogin(w http.ResponseWriter, r *http.Request, u *User, rd string) {
	if a.needs2FASetup(u, nil) {
		http.Redirect(w, r, "/setup-2fa?rd="+url.QueryEscape(rd), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, a.continueTarget(r, rd), http.StatusSeeOther)
}

// ---------------------------------------------------------------- second factor

func (a *App) handle2FAPage(w http.ResponseWriter, r *http.Request) {
	p := a.basePage(w, r, "title.confirm")
	sess, user := a.readSession(r)
	if sess == nil || sess.State != "pending" {
		http.Redirect(w, r, a.loginPath(r)+"?rd="+url.QueryEscape(p.RD), http.StatusFound)
		return
	}
	p.Username = user.Username
	p.StackClass = "wide"
	p.Recovery = r.URL.Query().Get("recovery") == "1"
	a.render(w, http.StatusOK, tpl(p, "2fa"), p)
}

func (a *App) handle2FAPost(w http.ResponseWriter, r *http.Request) {
	p := a.basePage(w, r, "title.confirm")
	sess, user := a.readSession(r)
	if sess == nil || sess.State != "pending" {
		http.Redirect(w, r, a.loginPath(r)+"?rd="+url.QueryEscape(p.RD), http.StatusSeeOther)
		return
	}
	p.Username = user.Username
	p.StackClass = "wide"
	p.Recovery = r.FormValue("mode") == "recovery"
	if !checkCSRF(r) {
		p.Error = p.T("err.csrf")
		a.render(w, http.StatusBadRequest, tpl(p, "2fa"), p)
		return
	}
	key := "ip:" + clientIP(r)
	if rem, locked := a.limiter.Locked(key); locked {
		_ = a.store.DeleteSession(sess.ID)
		a.renderLocked(w, p, rem)
		return
	}

	ok := false
	if p.Recovery {
		used, err := a.store.UseRecoveryCode(user.ID, sha(normalizeCode(r.FormValue("recovery"))))
		if err != nil {
			internalError(w, err)
			return
		}
		if ok = used; ok {
			a.event(r, "recovery_used", user.Username, p.Target, "")
		}
	} else if step := verifyTOTP(user.TOTPSecret, r.FormValue("code"), user.TOTPLastStep); step > 0 {
		ok = true
		_ = a.store.SetTOTPStep(user.ID, step)
	}
	if !ok {
		a.event(r, "mfa_fail", user.Username, p.Target, "")
		if rem, locked := a.lockIfNeeded(r, key, p.Target); locked {
			_ = a.store.DeleteSession(sess.ID)
			a.renderLocked(w, p, rem)
			return
		}
		p.Error = p.T("err.code")
		a.render(w, http.StatusUnauthorized, tpl(p, "2fa"), p)
		return
	}
	a.limiter.Reset(key)
	if err := a.activateSession(w, r, sess, true); err != nil {
		internalError(w, err)
		return
	}
	a.event(r, "login_ok", user.Username, p.Target, "2fa")
	_ = a.store.TouchUser(user.ID)
	a.afterLogin(w, r, user, p.RD)
}

// ---------------------------------------------------------------- logout, notices, preview

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sess, user := a.readSession(r); sess != nil {
		_ = a.store.DeleteSession(sess.ID)
		if sess.State == "active" {
			a.event(r, "logout", user.Username, "", "")
		}
	}
	a.clearSessionCookie(w, r)
	http.Redirect(w, r, a.loginPath(r), http.StatusFound)
}

func (a *App) handleDenied(w http.ResponseWriter, r *http.Request) {
	p := a.basePage(w, r, "title.denied")
	sess, user := a.activeSession(r)
	if sess == nil {
		http.Redirect(w, r, a.loginPath(r), http.StatusFound)
		return
	}
	p.Kind = "denied"
	p.StackClass = "wide"
	p.Username = user.Username
	p.Site = p.T("notice.thisSite")
	if site := hostOnly(r.URL.Query().Get("site")); hostRE.MatchString(site) {
		p.Site = site
		p.Target = site
		p.TargetInitial = strings.ToUpper(site[:1])
		p.TargetName, p.TargetRest = splitHost(site)
	}
	a.render(w, http.StatusForbidden, "site_notice.html", p)
}

// handlePreview shows a login template with sample data (admins only, on the admin host).
func (a *App) handlePreview(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("tpl")
	if !validTemplate(name) {
		http.NotFound(w, r)
		return
	}
	if sess, user := a.activeSession(r); !a.isAdminHost(r) || sess == nil || user.Role != "admin" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	p := a.basePage(w, r, "title.signin")
	p.Admin, p.Preview, p.Tpl = false, true, name
	d := p.Domain
	if d == "" {
		d = "example.com"
	}
	p.Target = "app." + d
	p.TargetInitial = "A"
	p.TargetName, p.TargetRest = splitHost(p.Target)
	a.render(w, http.StatusOK, "site_login.html", p)
}

// ---------------------------------------------------------------- 2FA enrollment

func (a *App) pendingSecret(uid int64) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s, ok := a.pendingTOTP[uid]; ok {
		return s
	}
	s := newTOTPSecret()
	a.pendingTOTP[uid] = s
	return s
}

func (a *App) dropPendingSecret(uid int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.pendingTOTP, uid)
}

func (a *App) fillTOTP(p *page, u *User, secret string) error {
	issuer := "Wicket"
	if d := rootDomain(a.settings()); d != "" {
		issuer = "Wicket " + d
	}
	png, err := qrcode.Encode(totpURI(secret, u.Username, issuer), qrcode.Medium, 320)
	if err != nil {
		return err
	}
	p.QR = template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png))
	p.Secret = secret
	p.SecretGrouped = groupSecret(secret)
	return nil
}

// enableTOTP turns on 2FA and returns fresh recovery codes (shown exactly once).
func (a *App) enableTOTP(r *http.Request, u *User, sess *Session, secret string, step int64) ([]string, error) {
	if err := a.store.EnableTOTP(u.ID, secret, step); err != nil {
		return nil, err
	}
	codes, err := a.newRecoveryCodes(u.ID)
	if err != nil {
		return nil, err
	}
	if sess != nil {
		_ = a.store.SetSessionMFA(sess.ID)
	}
	a.dropPendingSecret(u.ID)
	a.event(r, "mfa_enabled", u.Username, "", "")
	return codes, nil
}

func (a *App) newRecoveryCodes(uid int64) ([]string, error) {
	codes := newRecoveryCodes()
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = sha(c)
	}
	return codes, a.store.ReplaceRecoveryCodes(uid, hashes)
}

// setup2FAPage keeps the enrollment card centered: the wide card does not fit the
// side-panel templates, so only the light/dark choice of the template is kept.
func (a *App) setup2FAPage(w http.ResponseWriter, r *http.Request) *page {
	p := a.basePage(w, r, "title.setup2fa")
	if p.Tpl != "light" {
		p.Tpl = "centered"
	}
	p.StackClass = "xwide"
	return p
}

func (a *App) handleSetup2FAPage(w http.ResponseWriter, r *http.Request) {
	p := a.setup2FAPage(w, r)
	sess, user := a.activeSession(r)
	if sess == nil {
		http.Redirect(w, r, a.loginPath(r)+"?rd="+url.QueryEscape(p.RD), http.StatusFound)
		return
	}
	p.Continue = a.continueTarget(r, p.RD)
	if user.TOTPEnabled {
		http.Redirect(w, r, p.Continue, http.StatusFound)
		return
	}
	p.Username = user.Username
	p.Forced = a.needs2FASetup(user, nil) || p.RD != ""
	p.Step = 1
	if err := a.fillTOTP(p, user, a.pendingSecret(user.ID)); err != nil {
		internalError(w, err)
		return
	}
	a.render(w, http.StatusOK, "setup_2fa.html", p)
}

func (a *App) handleSetup2FAPost(w http.ResponseWriter, r *http.Request) {
	p := a.setup2FAPage(w, r)
	sess, user := a.activeSession(r)
	if sess == nil {
		http.Redirect(w, r, a.loginPath(r), http.StatusSeeOther)
		return
	}
	p.Continue = a.continueTarget(r, p.RD)
	p.Username = user.Username
	p.Forced = a.needs2FASetup(user, nil) || p.RD != ""
	secret := a.pendingSecret(user.ID)
	step := int64(0)
	if checkCSRF(r) {
		step = verifyTOTP(secret, r.FormValue("code"), 0)
	}
	if step == 0 {
		p.Step = 1
		p.Error = p.T("err.codeClock")
		if err := a.fillTOTP(p, user, secret); err != nil {
			internalError(w, err)
			return
		}
		a.render(w, http.StatusBadRequest, "setup_2fa.html", p)
		return
	}
	codes, err := a.enableTOTP(r, user, sess, secret, step)
	if err != nil {
		internalError(w, err)
		return
	}
	p.Step = 2
	p.Codes = codes
	p.CodesText = strings.Join(codes, "\n")
	p.CodesURL = template.URL("data:text/plain;charset=utf-8," + url.PathEscape(p.T("setup2fa.fileTitle", user.Username)+"\n\n"+p.CodesText+"\n"))
	a.render(w, http.StatusOK, "setup_2fa.html", p)
}

// ---------------------------------------------------------------- first-run setup

func validateHosts(cookieDomain, loginHost, adminHost string) error {
	root := strings.TrimPrefix(cookieDomain, ".")
	if !hostRE.MatchString(root) {
		return userErr("err.domain")
	}
	for _, h := range []string{loginHost, adminHost} {
		if !hostRE.MatchString(h) || !withinDomain(h, root) {
			return userErr("err.subdomain", h, root)
		}
	}
	if loginHost == adminHost {
		return userErr("err.hostsDiffer")
	}
	return nil
}

func (a *App) fillSetupDefaults(p *page) {
	s := a.settings()
	p.CookieDomain = rootDomain(s)
	p.LoginHost = s.LoginHost
	p.AdminHost = s.AdminHost
	p.ImportLine = a.importLine()
}

func (a *App) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	p := a.basePage(w, r, "title.setup")
	switch a.phase() {
	case 1:
		p.Step = 1
	case 2:
		if sess, user := a.activeSession(r); sess == nil || user.Role != "admin" {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		p.Step = 2
		a.fillSetupDefaults(p)
	default:
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	a.render(w, http.StatusOK, "setup.html", p)
}

func (a *App) handleSetupPost(w http.ResponseWriter, r *http.Request) {
	p := a.basePage(w, r, "title.setup")
	p.Step = a.phase()
	if p.Step == 0 {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	fail := func(key string, args ...any) {
		p.Error = p.T(key, args...)
		a.render(w, http.StatusBadRequest, "setup.html", p)
	}
	if !checkCSRF(r) {
		if p.Step == 2 {
			a.fillSetupDefaults(p)
		}
		fail("err.csrf")
		return
	}

	if p.Step == 1 {
		p.Username = strings.TrimSpace(r.FormValue("username"))
		key := "ip:" + clientIP(r)
		if rem, locked := a.limiter.Locked(key); locked {
			a.renderLocked(w, p, rem)
			return
		}
		a.mu.Lock()
		code := a.setupCode
		a.mu.Unlock()
		if subtle.ConstantTimeCompare([]byte(normalizeCode(r.FormValue("code"))), []byte(code)) != 1 {
			a.lockIfNeeded(r, key, "")
			fail("err.setupCode")
			return
		}
		pw := r.FormValue("password")
		switch {
		case !usernameRE.MatchString(p.Username):
			fail("err.username")
			return
		case len(pw) < 10:
			fail("err.password10")
			return
		case pw != r.FormValue("password2"):
			fail("err.passwordMatch")
			return
		}
		id, err := a.store.CreateUser(&User{Username: p.Username, Role: "admin", PasswordHash: hashPassword(pw)})
		if err != nil {
			internalError(w, err)
			return
		}
		u, err := a.store.UserByID(id)
		if err != nil || u == nil {
			internalError(w, fmt.Errorf("load new admin: %v", err))
			return
		}
		if _, err := a.newSession(w, r, u, "active", false, false); err != nil {
			internalError(w, err)
			return
		}
		a.event(r, "user_created", u.Username, "", "setup")
		a.setPhase(2)
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}

	sess, user := a.activeSession(r)
	if sess == nil || user.Role != "admin" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	p.CookieDomain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(r.FormValue("cookieDomain"))), ".")
	p.LoginHost = strings.ToLower(strings.TrimSpace(r.FormValue("loginHost")))
	p.AdminHost = strings.ToLower(strings.TrimSpace(r.FormValue("adminHost")))
	p.ImportLine = a.importLine()
	if err := validateHosts(p.CookieDomain, p.LoginHost, p.AdminHost); err != nil {
		p.Error = msgFor(p.Lang, err)
		a.render(w, http.StatusBadRequest, "setup.html", p)
		return
	}
	s := a.settings()
	s.CookieDomain = "." + p.CookieDomain
	s.LoginHost = p.LoginHost
	s.AdminHost = p.AdminHost
	if err := a.saveSettings(s); err != nil {
		internalError(w, err)
		return
	}
	if err := a.syncCaddy(); err != nil {
		log.Printf("caddy: %v", err)
	}
	// replace a host-only setup cookie with one valid for the whole domain
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: isHTTPS(r)})
	if err := a.activateSession(w, r, sess, sess.MFA); err != nil {
		internalError(w, err)
		return
	}
	a.setPhase(0)
	a.event(r, "settings_changed", user.Username, "", "setup-done")
	http.Redirect(w, r, "/setup-2fa", http.StatusSeeOther)
}
