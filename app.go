package main

import (
	"crypto/subtle"
	"encoding/json"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Settings struct {
	CookieDomain     string `json:"cookieDomain"`
	LoginHost        string `json:"loginHost"`
	AdminHost        string `json:"adminHost"`
	LockAttempts     int    `json:"lockAttempts"`
	LockWindowSec    int    `json:"lockWindowSec"`
	LockDurationSec  int    `json:"lockDurationSec"`
	SessionHours     int    `json:"sessionHours"`
	RememberDays     int    `json:"rememberDays"`
	EnforceAdmin2FA  bool   `json:"enforceAdmin2fa"`
	LogRetentionDays int    `json:"logRetentionDays"`
	Language         string `json:"language"`      // "auto" or one of languages
	LoginTemplate    string `json:"loginTemplate"` // one of loginTemplates
}

func defaultSettings(cfg Config) Settings {
	return Settings{
		CookieDomain: cfg.CookieDomain, LoginHost: cfg.LoginHost, AdminHost: cfg.AdminHost,
		LockAttempts: 5, LockWindowSec: 120, LockDurationSec: 900,
		SessionHours: 12, RememberDays: 30, EnforceAdmin2FA: true, LogRetentionDays: 90,
		Language: "en", LoginTemplate: "centered",
	}
}

type App struct {
	cfg     Config
	store   *Store
	tmpl    *template.Template
	limiter *Limiter

	mu          sync.Mutex
	settingsV   Settings
	setupCode   string // set while no user exists
	setupPhase  int    // 1 = create admin, 2 = configure domain, 0 = done
	pendingTOTP map[int64]string
	deniedSeen  map[string]int64
	oauthStates map[string]oauthState

	caddyMu sync.Mutex
}

func NewApp(cfg Config, st *Store) (*App, error) {
	tmpl, err := template.ParseFS(webFS, "web/templates/*.html")
	if err != nil {
		return nil, err
	}
	a := &App{cfg: cfg, store: st, tmpl: tmpl, limiter: NewLimiter(), pendingTOTP: map[int64]string{}, deniedSeen: map[string]int64{}, oauthStates: map[string]oauthState{}}

	s := defaultSettings(cfg)
	raw, ok, err := st.GetSetting("settings")
	if err != nil {
		return nil, err
	}
	if ok {
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return nil, err
		}
	}
	// environment fills gaps (first start, or values never set in the UI)
	if s.CookieDomain == "" {
		s.CookieDomain = cfg.CookieDomain
	}
	if s.LoginHost == "" {
		s.LoginHost = cfg.LoginHost
	}
	if s.AdminHost == "" {
		s.AdminHost = cfg.AdminHost
	}
	// settings stored by older versions have no language or template yet
	if s.Language != "auto" && !supportedLang(s.Language) {
		s.Language = "en"
	}
	if !validTemplate(s.LoginTemplate) {
		s.LoginTemplate = "centered"
	}
	if err := a.saveSettings(s); err != nil {
		return nil, err
	}

	n, err := st.CountUsers()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		a.setupCode = randomString(4, codeAlphabet) + "-" + randomString(4, codeAlphabet)
		a.setupPhase = 1
		log.Printf("┌────────────────────────────────────────────────┐")
		log.Printf("│  Wicket first-run setup – setup code: %s  │", a.setupCode)
		log.Printf("└────────────────────────────────────────────────┘")
	}
	if err := a.syncCaddy(); err != nil {
		log.Printf("caddy: %v", err)
	}
	return a, nil
}

func (a *App) settings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settingsV
}

func (a *App) saveSettings(s Settings) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := a.store.SetSetting("settings", string(b)); err != nil {
		return err
	}
	a.mu.Lock()
	a.settingsV = s
	a.mu.Unlock()
	return nil
}

func (a *App) phase() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.setupPhase
}

func (a *App) setPhase(p int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.setupPhase = p
	if p != 1 {
		a.setupCode = ""
	}
}

// ---------------------------------------------------------------- routing

func (a *App) Routes() http.Handler {
	static, _ := fs.Sub(webFS, "web/static")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /verify", a.handleVerify)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))

	mux.HandleFunc("GET /{$}", a.handleRoot)
	mux.HandleFunc("GET /login", a.handleLoginPage)
	mux.HandleFunc("POST /login", a.handleLoginPost)
	mux.HandleFunc("GET /2fa", a.handle2FAPage)
	mux.HandleFunc("POST /2fa", a.handle2FAPost)
	mux.HandleFunc("GET /logout", a.handleLogout)
	mux.HandleFunc("GET /denied", a.handleDenied)
	mux.HandleFunc("GET /setup-2fa", a.handleSetup2FAPage)
	mux.HandleFunc("POST /setup-2fa", a.handleSetup2FAPost)
	mux.HandleFunc("GET /setup", a.handleSetupPage)
	mux.HandleFunc("POST /setup", a.handleSetupPost)
	mux.HandleFunc("GET /preview/{tpl}", a.handlePreview)
	mux.HandleFunc("GET /oauth/{provider}/start", a.handleOAuthStart)
	mux.HandleFunc("GET /oauth/{provider}/callback", a.handleOAuthCallback)
	// password reset and invitations (only active with a configured mail server)
	mux.HandleFunc("GET /reset", a.handleResetPage)
	mux.HandleFunc("POST /reset", a.handleResetPost)
	resetGet, resetPost := a.tokenPage("reset")
	mux.HandleFunc("GET /reset/{token}", resetGet)
	mux.HandleFunc("POST /reset/{token}", resetPost)
	inviteGet, invitePost := a.tokenPage("invite")
	mux.HandleFunc("GET /invite/{token}", inviteGet)
	mux.HandleFunc("POST /invite/{token}", invitePost)
	// passkeys (JSON endpoints used by passkey.js)
	mux.HandleFunc("POST /passkey/register/begin", a.handlePasskeyRegisterBegin)
	mux.HandleFunc("POST /passkey/register/finish", a.handlePasskeyRegisterFinish)
	mux.HandleFunc("POST /passkey/login/begin", a.handlePasskeyLoginBegin)
	mux.HandleFunc("POST /passkey/login/finish", a.handlePasskeyLoginFinish)
	// Wicket as OpenID Connect provider for other applications
	mux.HandleFunc("GET /.well-known/openid-configuration", a.handleOIDCDiscovery)
	mux.HandleFunc("GET /oidc/jwks", a.handleOIDCJWKS)
	mux.HandleFunc("GET /oidc/authorize", a.handleOIDCAuthorize)
	mux.HandleFunc("POST /oidc/token", a.handleOIDCToken)
	mux.HandleFunc("GET /oidc/userinfo", a.handleOIDCUserinfo)
	mux.HandleFunc("POST /oidc/userinfo", a.handleOIDCUserinfo)
	mux.Handle("/api/", a.adminAPI())

	return securityHeaders(a.setupGate(mux))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		next.ServeHTTP(w, r)
	})
}

// setupGate sends every page to /setup until the first admin exists.
func (a *App) setupGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.phase() == 1 {
			p := r.URL.Path
			if p != "/setup" && p != "/healthz" && p != "/verify" && !strings.HasPrefix(p, "/static/") {
				http.Redirect(w, r, "/setup", http.StatusFound)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------- request helpers

func hostOnly(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		return strings.ToLower(host)
	}
	return strings.ToLower(strings.TrimSpace(h))
}

func (a *App) isAdminHost(r *http.Request) bool {
	s := a.settings()
	return s.AdminHost == "" || hostOnly(r.Host) == s.AdminHost
}

// clientIP trusts X-Forwarded-For only from a loopback peer (Caddy on the same host),
// and Caddy replaces untrusted incoming values with the real client address.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || trustedProxy(ip)) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}
	return host
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func rootDomain(s Settings) string { return strings.TrimPrefix(s.CookieDomain, ".") }

func withinDomain(host, root string) bool {
	return root != "" && (host == root || strings.HasSuffix(host, "."+root))
}

// validRedirect only allows targets inside the cookie domain (no open redirects).
func validRedirect(raw string, s Settings) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return ""
	}
	if !withinDomain(hostOnly(u.Host), rootDomain(s)) {
		return ""
	}
	return u.String()
}

func (a *App) loginBase(s Settings) string {
	switch {
	case s.LoginHost != "":
		return "https://" + s.LoginHost
	case s.AdminHost != "":
		return "https://" + s.AdminHost
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ---------------------------------------------------------------- sessions & cookies

const (
	sessionCookie = "wicket_session"
	csrfCookie    = "wicket_csrf"
)

func (a *App) cookieDomainFor(r *http.Request) string {
	root := rootDomain(a.settings())
	if withinDomain(hostOnly(r.Host), root) {
		return root
	}
	return "" // e.g. local testing via IP: host-only cookie
}

func (a *App) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, persistent bool) {
	c := &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: isHTTPS(r),
		SameSite: http.SameSiteLaxMode, Domain: a.cookieDomainFor(r)}
	if persistent {
		c.MaxAge = a.settings().RememberDays * 86400
	}
	http.SetCookie(w, c)
}

func (a *App) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: isHTTPS(r),
		SameSite: http.SameSiteLaxMode, Domain: a.cookieDomainFor(r), MaxAge: -1})
}

func (a *App) readSession(r *http.Request) (*Session, *User) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil, nil
	}
	s, err := a.store.SessionByTokenHash(sha(c.Value))
	if err != nil || s == nil || s.ExpiresAt < now() {
		return nil, nil
	}
	u, err := a.store.UserByID(s.UserID)
	if err != nil || u == nil || u.Disabled {
		return nil, nil
	}
	return s, u
}

func (a *App) activeSession(r *http.Request) (*Session, *User) {
	s, u := a.readSession(r)
	if s == nil || s.State != "active" {
		return nil, nil
	}
	return s, u
}

func (a *App) sessionExpiry(state string, remember bool) int64 {
	s := a.settings()
	switch {
	case state == "pending":
		return now() + 600
	case remember:
		return now() + int64(s.RememberDays)*86400
	}
	return now() + int64(s.SessionHours)*3600
}

func (a *App) newSession(w http.ResponseWriter, r *http.Request, u *User, state string, mfa, remember bool) (*Session, error) {
	token := newToken()
	sess := &Session{UserID: u.ID, State: state, MFA: mfa, Remember: remember, ExpiresAt: a.sessionExpiry(state, remember),
		IP: clientIP(r), UA: truncate(r.UserAgent(), 300)}
	id, err := a.store.CreateSession(sess, sha(token))
	if err != nil {
		return nil, err
	}
	sess.ID = id
	a.setSessionCookie(w, r, token, remember && state == "active")
	return sess, nil
}

// activateSession promotes a session with a fresh token (prevents session fixation).
func (a *App) activateSession(w http.ResponseWriter, r *http.Request, sess *Session, mfa bool) error {
	token := newToken()
	if err := a.store.ActivateSession(sess.ID, sha(token), mfa, a.sessionExpiry("active", sess.Remember)); err != nil {
		return err
	}
	a.setSessionCookie(w, r, token, sess.Remember)
	return nil
}

func (a *App) csrfToken(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(csrfCookie); err == nil && len(c.Value) >= 32 {
		return c.Value
	}
	t := newToken()
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Value: t, Path: "/", HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode})
	return t
}

func checkCSRF(r *http.Request) bool {
	c, err := r.Cookie(csrfCookie)
	if err != nil || c.Value == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(r.FormValue("csrf"))) == 1
}

// needs2FASetup: user must enroll TOTP before continuing.
func (a *App) needs2FASetup(u *User, site *Site) bool {
	// a passkey is a second factor on its own
	if u.TOTPEnabled || a.store.HasPasskeys(u.ID) {
		return false
	}
	if a.settings().EnforceAdmin2FA && canAdmin(u) {
		return true
	}
	return site != nil && site.Require2FA
}

// event records an audit log entry. detail is a language-neutral code the admin UI translates
// ("2fa", "setup", "lock:5:15", ...) or a plain value such as a user name.
func (a *App) event(r *http.Request, kind, username, site, detail string) {
	e := Event{At: now(), Kind: kind, Username: username, Site: site, IP: clientIP(r), UA: truncate(r.UserAgent(), 300), Detail: detail}
	if err := a.store.AddEvent(e); err != nil {
		log.Printf("event: %v", err)
	}
	go a.afterEvent(e)
}

func (a *App) janitor() {
	for {
		s := a.settings()
		if err := a.store.CleanupSessions(); err != nil {
			log.Printf("janitor: %v", err)
		}
		if err := a.store.CleanupEvents(now() - int64(s.LogRetentionDays)*86400); err != nil {
			log.Printf("janitor: %v", err)
		}
		a.limiter.Cleanup(s.LockWindowSec)
		a.mu.Lock()
		for k, t := range a.deniedSeen {
			if t < now()-600 {
				delete(a.deniedSeen, k)
			}
		}
		a.mu.Unlock()
		cleanupOIDC()
		a.store.CleanupUserTokens()
		time.Sleep(30 * time.Minute)
	}
}
