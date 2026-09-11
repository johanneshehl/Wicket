package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Sign-in with Microsoft, GitHub and Google (OAuth 2.0 authorization code flow).
// The provider's verified email address is matched with the email of an existing
// Wicket user; external accounts never create users on their own. Users with 2FA
// still enter their code afterwards.

type OAuthProvider struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	Tenant       string `json:"tenant"` // Microsoft only
	Enabled      bool   `json:"enabled"`
}

var (
	oauthOrder = []string{"microsoft", "github", "google"}
	oauthNames = map[string]string{"microsoft": "Microsoft", "github": "GitHub", "google": "Google"}
)

// Tenant of personal Microsoft accounts; Microsoft verifies their email addresses.
const msaTenant = "9188040d-6c67-4c5b-b112-36a304b66dad"

const oauthCookie = "wicket_oauth"

type oauthState struct {
	Provider, RD, Verifier string
	Admin                  bool
	Expires                int64
}

type oauthButton struct{ ID, Name string }

var oauthHTTP = &http.Client{Timeout: 10 * time.Second}

var (
	errNoVerifiedEmail = errors.New("the provider returned no verified email address")
	errMSTenant        = errors.New("organisation accounts need a configured tenant")
)

type oauthError struct{ Code, Desc string }

func (e *oauthError) Error() string {
	if e.Desc == "" {
		return e.Code
	}
	d := strings.SplitN(strings.TrimSpace(e.Desc), "\n", 2)[0]
	if len(d) > 200 {
		d = d[:200]
	}
	return e.Code + ": " + d
}

// ---------------------------------------------------------------- configuration

func (a *App) oauthConfig() map[string]OAuthProvider {
	out := map[string]OAuthProvider{}
	if raw, ok, err := a.store.GetSetting("oauth"); err == nil && ok {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

func (a *App) saveOAuth(cfg map[string]OAuthProvider) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return a.store.SetSetting("oauth", string(b))
}

func (a *App) oauthButtons() []oauthButton {
	cfg := a.oauthConfig()
	var out []oauthButton
	for _, id := range oauthOrder {
		if cfg[id].Enabled {
			out = append(out, oauthButton{ID: id, Name: oauthNames[id]})
		}
	}
	return out
}

// redirectURI is always on the login host, so each provider needs exactly one registered URI.
func (a *App) redirectURI(p string) string {
	return a.loginBase(a.settings()) + "/oauth/" + p + "/callback"
}

type oauthEndpoint struct {
	auth, token, scope string
	pkce               bool
}

func msTenantFixed(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	return t != "" && t != "common" && t != "organizations" && t != "consumers"
}

func endpointsFor(p string, cfg OAuthProvider) oauthEndpoint {
	switch p {
	case "google":
		return oauthEndpoint{"https://accounts.google.com/o/oauth2/v2/auth", "https://oauth2.googleapis.com/token", "openid email profile", true}
	case "github":
		return oauthEndpoint{"https://github.com/login/oauth/authorize", "https://github.com/login/oauth/access_token", "read:user user:email", false}
	default: // microsoft
		tenant := strings.TrimSpace(cfg.Tenant)
		if tenant == "" {
			tenant = "common"
		}
		base := "https://login.microsoftonline.com/" + url.PathEscape(tenant) + "/oauth2/v2.0"
		return oauthEndpoint{base + "/authorize", base + "/token", "openid email profile", true}
	}
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (a *App) putOAuthState(id string, st oauthState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, v := range a.oauthStates {
		if v.Expires < now() {
			delete(a.oauthStates, k)
		}
	}
	a.oauthStates[id] = st
}

func (a *App) takeOAuthState(id string) (oauthState, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.oauthStates[id]
	delete(a.oauthStates, id)
	return st, ok && st.Expires >= now()
}

// ---------------------------------------------------------------- browser flow

func (a *App) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	p := r.PathValue("provider")
	cfg := a.oauthConfig()[p]
	if oauthNames[p] == "" || !cfg.Enabled {
		http.NotFound(w, r)
		return
	}
	state, verifier := newToken(), newToken()
	a.putOAuthState(state, oauthState{Provider: p, RD: validRedirect(r.URL.Query().Get("rd"), a.settings()),
		Verifier: verifier, Admin: a.isAdminHost(r), Expires: now() + 600})
	// set for the whole domain: the flow may start on the admin host and return to the login host
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: state, Path: "/", HttpOnly: true, Secure: isHTTPS(r),
		SameSite: http.SameSiteLaxMode, Domain: a.cookieDomainFor(r), MaxAge: 600})

	ep := endpointsFor(p, cfg)
	q := url.Values{"client_id": {cfg.ClientID}, "redirect_uri": {a.redirectURI(p)}, "response_type": {"code"},
		"scope": {ep.scope}, "state": {state}}
	if ep.pkce {
		q.Set("code_challenge", pkceChallenge(verifier))
		q.Set("code_challenge_method", "S256")
	}
	if p != "github" {
		q.Set("prompt", "select_account")
	}
	http.Redirect(w, r, ep.auth+"?"+q.Encode(), http.StatusFound)
}

func (a *App) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	p := r.PathValue("provider")
	cfg := a.oauthConfig()[p]
	if oauthNames[p] == "" || !cfg.Enabled {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	stateID := q.Get("state")
	st, ok := a.takeOAuthState(stateID)
	c, cerr := r.Cookie(oauthCookie)
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: "", Path: "/", HttpOnly: true, Secure: isHTTPS(r),
		SameSite: http.SameSiteLaxMode, Domain: a.cookieDomainFor(r), MaxAge: -1})
	if !ok || cerr != nil || stateID == "" || c.Value != stateID || st.Provider != p {
		a.oauthFailRedirect(w, r, st, "state", p)
		return
	}
	key := "ip:" + clientIP(r)
	if rem, locked := a.limiter.Locked(key); locked {
		a.renderLocked(w, a.basePage(w, r, "title.locked"), rem)
		return
	}
	if e := q.Get("error"); e != "" {
		log.Printf("oauth %s: %s %s", p, e, q.Get("error_description"))
		a.oauthFailRedirect(w, r, st, "failed", p)
		return
	}
	emails, err := a.oauthEmails(r.Context(), p, cfg, q.Get("code"), st.Verifier)
	if err != nil {
		code := "failed"
		switch {
		case errors.Is(err, errNoVerifiedEmail):
			code = "email"
		case errors.Is(err, errMSTenant):
			code = "tenant"
		}
		log.Printf("oauth %s: %v", p, err)
		a.oauthFailRedirect(w, r, st, code, p)
		return
	}
	var u *User
	for _, e := range emails {
		if found, _ := a.store.UserByEmail(e); found != nil && !found.Disabled {
			u = found
			break
		}
	}
	site := ""
	if rdURL, err := url.Parse(st.RD); err == nil && st.RD != "" {
		site = hostOnly(rdURL.Host)
	}
	if u == nil {
		a.event(r, "login_fail_user", emails[0], site, p)
		if rem, locked := a.lockIfNeeded(r, key, site); locked {
			a.renderLocked(w, a.basePage(w, r, "title.locked"), rem)
			return
		}
		a.oauthFailRedirect(w, r, st, "nouser", p)
		return
	}
	if st.Admin && u.Role != "admin" {
		a.event(r, "denied", u.Username, "", "not-admin")
		a.oauthFailRedirect(w, r, st, "noadmin", p)
		return
	}
	a.limiter.Reset(key)

	s := a.settings()
	base := ""
	if st.Admin && s.AdminHost != "" {
		base = "https://" + s.AdminHost
	}
	if u.TOTPEnabled {
		if _, err := a.newSession(w, r, u, "pending", false, false); err != nil {
			internalError(w, err)
			return
		}
		http.Redirect(w, r, base+"/2fa?rd="+url.QueryEscape(st.RD), http.StatusSeeOther)
		return
	}
	if _, err := a.newSession(w, r, u, "active", false, false); err != nil {
		internalError(w, err)
		return
	}
	a.event(r, "login_ok", u.Username, site, p)
	_ = a.store.TouchUser(u.ID)
	target := "/"
	switch {
	case a.needs2FASetup(u, nil):
		target = base + "/setup-2fa?rd=" + url.QueryEscape(st.RD)
	case st.Admin:
		target = base + "/"
	case st.RD != "":
		target = st.RD
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// oauthFailRedirect sends the visitor back to the right login page with a short error code.
func (a *App) oauthFailRedirect(w http.ResponseWriter, r *http.Request, st oauthState, code, provider string) {
	s := a.settings()
	base := "/"
	if st.Admin && s.AdminHost != "" {
		base = "https://" + s.AdminHost + "/login"
	} else if lb := a.loginBase(s); lb != "" {
		base = lb + "/"
	}
	q := url.Values{"e": {code}}
	if oauthNames[provider] != "" {
		q.Set("p", provider)
	}
	if st.RD != "" {
		q.Set("rd", st.RD)
	}
	http.Redirect(w, r, base+"?"+q.Encode(), http.StatusSeeOther)
}

// oauthErrorText turns the error code from oauthFailRedirect into a message (unknown codes are ignored).
func (a *App) oauthErrorText(p *page, code, provider string) string {
	name := oauthNames[provider]
	switch code {
	case "state":
		return p.T("err.oauthState")
	case "failed":
		return p.T("err.oauthFailed", name)
	case "email":
		return p.T("err.oauthEmail", name)
	case "tenant":
		return p.T("err.oauthTenant")
	case "nouser":
		return p.T("err.oauthNoUser")
	case "noadmin":
		return p.T("err.noAdminAccess")
	}
	return ""
}

// ---------------------------------------------------------------- provider calls

func (a *App) exchangeCode(ctx context.Context, p string, cfg OAuthProvider, code, verifier string) (map[string]any, error) {
	ep := endpointsFor(p, cfg)
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {a.redirectURI(p)},
		"client_id": {cfg.ClientID}, "client_secret": {cfg.ClientSecret}}
	if ep.pkce && verifier != "" {
		form.Set("code_verifier", verifier)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.token, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := oauthHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out := map[string]any{}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("token endpoint returned %d without JSON", resp.StatusCode)
	}
	if e, _ := out["error"].(string); e != "" {
		desc, _ := out["error_description"].(string)
		return out, &oauthError{Code: e, Desc: desc}
	}
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}
	return out, nil
}

func getJSON(ctx context.Context, u, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Wicket")
	resp, err := oauthHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned %d", u, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

func decodeJWTClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	claims := map[string]any{}
	if len(parts) != 3 {
		return claims
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err == nil {
		_ = json.Unmarshal(raw, &claims)
	}
	return claims
}

// oauthEmails returns the verified email addresses of the signed-in provider account.
func (a *App) oauthEmails(ctx context.Context, p string, cfg OAuthProvider, code, verifier string) ([]string, error) {
	tok, err := a.exchangeCode(ctx, p, cfg, code, verifier)
	if err != nil {
		return nil, err
	}
	access, _ := tok["access_token"].(string)
	switch p {
	case "google":
		var ui struct {
			Email    string `json:"email"`
			Verified bool   `json:"email_verified"`
		}
		if err := getJSON(ctx, "https://openidconnect.googleapis.com/v1/userinfo", access, &ui); err != nil {
			return nil, err
		}
		if ui.Email == "" || !ui.Verified {
			return nil, errNoVerifiedEmail
		}
		return []string{ui.Email}, nil
	case "github":
		var list []struct {
			Email    string `json:"email"`
			Verified bool   `json:"verified"`
			Primary  bool   `json:"primary"`
		}
		if err := getJSON(ctx, "https://api.github.com/user/emails", access, &list); err != nil {
			return nil, err
		}
		var out []string
		for _, e := range list {
			if e.Verified && e.Primary {
				out = append([]string{e.Email}, out...)
			} else if e.Verified {
				out = append(out, e.Email)
			}
		}
		if len(out) == 0 {
			return nil, errNoVerifiedEmail
		}
		return out, nil
	default: // microsoft: the ID token comes straight from the token endpoint over TLS
		idToken, _ := tok["id_token"].(string)
		claims := decodeJWTClaims(idToken)
		tid, _ := claims["tid"].(string)
		// Without a fixed tenant anyone could create an organisation and put any address into its
		// email claim, so only personal accounts (verified by Microsoft) are accepted then.
		if !msTenantFixed(cfg.Tenant) && tid != msaTenant {
			return nil, errMSTenant
		}
		email, _ := claims["email"].(string)
		if email == "" {
			email, _ = claims["preferred_username"].(string)
		}
		if !strings.Contains(email, "@") {
			return nil, errNoVerifiedEmail
		}
		return []string{email}, nil
	}
}

// testOAuth sends a deliberately invalid code: a provider that answers "invalid code"
// has accepted the client credentials, anything else means the configuration is wrong.
func (a *App) testOAuth(ctx context.Context, p string, cfg OAuthProvider) error {
	name := oauthNames[p]
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return userErr("err.oauthConfig")
	}
	if a.loginBase(a.settings()) == "" {
		return userErr("err.oauthNoHost")
	}
	_, err := a.exchangeCode(ctx, p, cfg, "wicket-connection-test", "")
	var oe *oauthError
	if errors.As(err, &oe) {
		if oe.Code == "invalid_grant" || oe.Code == "bad_verification_code" {
			return nil
		}
		return userErr("err.oauthTest", name, oe.Error())
	}
	if err != nil {
		return userErr("err.oauthTest", name, err.Error())
	}
	return userErr("err.oauthTest", name, "unexpected answer")
}

// ---------------------------------------------------------------- admin API

type oauthView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ClientID    string `json:"clientId"`
	Tenant      string `json:"tenant"`
	RedirectURI string `json:"redirectUri"`
	HasSecret   bool   `json:"hasSecret"`
	Enabled     bool   `json:"enabled"`
}

func (a *App) apiOAuthList(w http.ResponseWriter, r *http.Request) {
	cfg := a.oauthConfig()
	out := []oauthView{}
	for _, id := range oauthOrder {
		c := cfg[id]
		out = append(out, oauthView{ID: id, Name: oauthNames[id], ClientID: c.ClientID, Tenant: c.Tenant,
			RedirectURI: a.redirectURI(id), HasSecret: c.ClientSecret != "", Enabled: c.Enabled})
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": out})
}

// oauthInput reads a provider configuration; an empty secret keeps the stored one.
func (a *App) oauthInput(r *http.Request, id string) (OAuthProvider, error) {
	var in OAuthProvider
	if err := readJSON(r, &in); err != nil {
		return in, err
	}
	in.ClientID = strings.TrimSpace(in.ClientID)
	in.Tenant = strings.TrimSpace(in.Tenant)
	if in.ClientSecret == "" {
		in.ClientSecret = a.oauthConfig()[id].ClientSecret
	}
	if id != "microsoft" {
		in.Tenant = ""
	}
	return in, nil
}

func (a *App) apiOAuthSave(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("provider")
	if oauthNames[id] == "" {
		a.errKey(w, r, http.StatusNotFound, "err.oauthProvider")
		return
	}
	in, err := a.oauthInput(r, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var testErr error
	if in.Enabled {
		if testErr = a.testOAuth(r.Context(), id, in); testErr != nil {
			in.Enabled = false // keep the data, but never switch on a provider that does not work
		}
	}
	cfg := a.oauthConfig()
	cfg[id] = in
	if err := a.saveOAuth(cfg); err != nil {
		a.fail(w, r, err)
		return
	}
	a.event(r, "settings_changed", reqUser(r).Username, "", "oauth:"+id)
	if testErr != nil {
		a.fail(w, r, testErr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": in.Enabled})
}

func (a *App) apiOAuthTest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("provider")
	if oauthNames[id] == "" {
		a.errKey(w, r, http.StatusNotFound, "err.oauthProvider")
		return
	}
	in, err := a.oauthInput(r, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.testOAuth(r.Context(), id, in); err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": tr(a.langFor(r), "oauth.testOk", oauthNames[id])})
}
