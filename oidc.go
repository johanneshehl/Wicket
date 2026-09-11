package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Wicket as OpenID Connect provider: applications (GitLab, Grafana, Portainer, ...) redirect their
// users to Wicket and get a signed ID token back. Authorization code flow with optional PKCE,
// ES256 signatures, clients configured by the admin.

type OIDCClient struct {
	ID           string   `json:"id"` // client_id
	Name         string   `json:"name"`
	SecretHash   string   `json:"secretHash,omitempty"`
	Public       bool     `json:"public"` // no secret, PKCE required
	RedirectURIs []string `json:"redirectUris"`
	Access       string   `json:"access"` // all | admins | users
	Users        []int64  `json:"users"`
	Groups       []int64  `json:"groups"`
	CreatedAt    int64    `json:"createdAt"`
}

type oidcCode struct {
	ClientID, RedirectURI, Nonce, Challenge, Scope string
	UserID, AuthTime, Expires                      int64
}

type oidcToken struct {
	UserID   int64
	ClientID string
	Scope    string
	Expires  int64
}

type oidcProvider struct {
	mu     sync.Mutex
	key    *ecdsa.PrivateKey
	kid    string
	codes  map[string]oidcCode  // sha(code) -> grant
	tokens map[string]oidcToken // sha(access token) -> grant
}

var oidcState = &oidcProvider{codes: map[string]oidcCode{}, tokens: map[string]oidcToken{}}

const (
	oidcCodeTTL  = 60
	oidcTokenTTL = 3600
)

var b64url = base64.RawURLEncoding

// signingKey loads (or creates once) the ES256 key in the data directory.
func (a *App) oidcKey() (*ecdsa.PrivateKey, string, error) {
	oidcState.mu.Lock()
	defer oidcState.mu.Unlock()
	if oidcState.key != nil {
		return oidcState.key, oidcState.kid, nil
	}
	path := filepath.Join(a.cfg.DataDir, "oidc-signing-key.pem")
	var key *ecdsa.PrivateKey
	if b, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(b)
		if block == nil {
			return nil, "", errors.New("oidc key: invalid PEM")
		}
		k, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, "", err
		}
		key = k
	} else {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, "", err
		}
		der, err := x509.MarshalECPrivateKey(k)
		if err != nil {
			return nil, "", err
		}
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600); err != nil {
			return nil, "", err
		}
		key = k
	}
	sum := sha256.Sum256(append(pad32(key.X.Bytes()), pad32(key.Y.Bytes())...))
	oidcState.key, oidcState.kid = key, b64url.EncodeToString(sum[:])[:16]
	return oidcState.key, oidcState.kid, nil
}

func pad32(b []byte) []byte {
	if len(b) >= 32 {
		return b
	}
	return append(make([]byte, 32-len(b)), b...)
}

func (a *App) signJWT(claims map[string]any) (string, error) {
	key, kid, err := a.oidcKey()
	if err != nil {
		return "", err
	}
	header, _ := json.Marshal(map[string]string{"alg": "ES256", "typ": "JWT", "kid": kid})
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := b64url.EncodeToString(header) + "." + b64url.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", err
	}
	sig := append(pad32(r.Bytes()), pad32(s.Bytes())...)
	return signing + "." + b64url.EncodeToString(sig), nil
}

func (a *App) oidcIssuer() string {
	s := a.settings()
	if s.LoginHost != "" {
		return "https://" + s.LoginHost
	}
	return "https://" + s.AdminHost
}

// ---------------------------------------------------------------- clients (stored as JSON setting)

func (a *App) oidcClients() ([]*OIDCClient, error) {
	raw, ok, err := a.store.GetSetting("oidc_clients")
	if err != nil || !ok {
		return []*OIDCClient{}, err
	}
	out := []*OIDCClient{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *App) saveOIDCClients(list []*OIDCClient) error {
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return a.store.SetSetting("oidc_clients", string(b))
}

func (a *App) oidcClient(id string) *OIDCClient {
	list, err := a.oidcClients()
	if err != nil {
		return nil
	}
	for _, c := range list {
		if c.ID == id {
			return c
		}
	}
	return nil
}

func validRedirectURI(u string) bool {
	p, err := url.Parse(u)
	if err != nil || p.Fragment != "" || p.Host == "" {
		return false
	}
	if p.Scheme == "https" {
		return true
	}
	// plain http only for local development callbacks
	return p.Scheme == "http" && (p.Hostname() == "localhost" || p.Hostname() == "127.0.0.1")
}

func (c *OIDCClient) redirectAllowed(u string) bool {
	for _, r := range c.RedirectURIs {
		if r == u {
			return true
		}
	}
	return false
}

func (a *App) oidcAllowed(c *OIDCClient, u *User) bool {
	if u.Role == "admin" {
		return true
	}
	switch c.Access {
	case "all":
		return true
	case "users":
		for _, id := range c.Users {
			if id == u.ID {
				return true
			}
		}
		return a.store.inAnyGroup(u.ID, c.Groups)
	}
	return false
}

func (a *App) userClaims(u *User, scope string) map[string]any {
	claims := map[string]any{"sub": strconv.FormatInt(u.ID, 10)}
	scopes := " " + scope + " "
	if strings.Contains(scopes, " profile ") {
		claims["preferred_username"] = u.Username
		claims["name"] = u.Username
		claims["role"] = u.Role
	}
	if strings.Contains(scopes, " email ") && u.Email != "" {
		claims["email"] = u.Email
		claims["email_verified"] = false
	}
	if strings.Contains(scopes, " groups ") {
		_, names, _ := a.store.UserGroups(u.ID)
		claims["groups"] = names
	}
	return claims
}

// ---------------------------------------------------------------- endpoints

func (a *App) handleOIDCDiscovery(w http.ResponseWriter, r *http.Request) {
	iss := a.oidcIssuer()
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                iss,
		"authorization_endpoint":                iss + "/oidc/authorize",
		"token_endpoint":                        iss + "/oidc/token",
		"userinfo_endpoint":                     iss + "/oidc/userinfo",
		"jwks_uri":                              iss + "/oidc/jwks",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"ES256"},
		"scopes_supported":                      []string{"openid", "profile", "email", "groups"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post", "none"},
		"code_challenge_methods_supported":      []string{"S256"},
		"claims_supported":                      []string{"sub", "preferred_username", "name", "email", "email_verified", "groups", "role"},
	})
}

func (a *App) handleOIDCJWKS(w http.ResponseWriter, r *http.Request) {
	key, kid, err := a.oidcKey()
	if err != nil {
		http.Error(w, "key unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": []map[string]string{{
		"kty": "EC", "crv": "P-256", "use": "sig", "alg": "ES256", "kid": kid,
		"x": b64url.EncodeToString(pad32(key.X.Bytes())), "y": b64url.EncodeToString(pad32(key.Y.Bytes())),
	}}})
}

func redirectWith(w http.ResponseWriter, r *http.Request, target string, params url.Values) {
	u, _ := url.Parse(target)
	q := u.Query()
	for k, v := range params {
		q[k] = v
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (a *App) handleOIDCAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c := a.oidcClient(q.Get("client_id"))
	redirect := q.Get("redirect_uri")
	// never redirect to an unverified URI: show the error instead
	if c == nil || !c.redirectAllowed(redirect) {
		http.Error(w, "Wicket: unknown client or redirect_uri not registered", http.StatusBadRequest)
		return
	}
	state := q.Get("state")
	fail := func(code, desc string) {
		redirectWith(w, r, redirect, url.Values{"error": {code}, "error_description": {desc}, "state": {state}})
	}
	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "only the authorization code flow is supported")
		return
	}
	scope := q.Get("scope")
	if !strings.Contains(" "+scope+" ", " openid ") {
		fail("invalid_scope", "scope must include openid")
		return
	}
	challenge := q.Get("code_challenge")
	if challenge != "" && q.Get("code_challenge_method") != "S256" {
		fail("invalid_request", "only S256 code challenges are supported")
		return
	}
	if c.Public && challenge == "" {
		fail("invalid_request", "public clients must use PKCE")
		return
	}

	self := a.oidcIssuer() + r.URL.RequestURI()
	sess, user := a.activeSession(r)
	if sess == nil {
		http.Redirect(w, r, a.oidcIssuer()+"/?rd="+url.QueryEscape(self), http.StatusFound)
		return
	}
	if a.needs2FASetup(user, nil) {
		http.Redirect(w, r, a.oidcIssuer()+"/setup-2fa?rd="+url.QueryEscape(self), http.StatusFound)
		return
	}
	if !a.oidcAllowed(c, user) {
		a.event(r, "denied", user.Username, "oidc:"+c.Name, "")
		fail("access_denied", "user is not allowed to use this application")
		return
	}

	code := newToken()
	oidcState.mu.Lock()
	oidcState.codes[sha(code)] = oidcCode{ClientID: c.ID, RedirectURI: redirect, Nonce: q.Get("nonce"), Challenge: challenge,
		Scope: scope, UserID: user.ID, AuthTime: sess.CreatedAt, Expires: now() + oidcCodeTTL}
	oidcState.mu.Unlock()
	a.event(r, "oidc_login", user.Username, "oidc:"+c.Name, "")
	redirectWith(w, r, redirect, url.Values{"code": {code}, "state": {state}})
}

func oauthError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

func (a *App) handleOIDCToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "malformed body")
		return
	}
	if r.PostForm.Get("grant_type") != "authorization_code" {
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "only authorization_code")
		return
	}
	id, secret, basic := r.BasicAuth()
	if !basic {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	} else {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	}
	c := a.oidcClient(id)
	if c == nil || (!c.Public && subtle.ConstantTimeCompare([]byte(sha(secret)), []byte(c.SecretHash)) != 1) {
		w.Header().Set("WWW-Authenticate", `Basic realm="wicket"`)
		oauthError(w, http.StatusUnauthorized, "invalid_client", "unknown client or wrong secret")
		return
	}

	h := sha(r.PostForm.Get("code"))
	oidcState.mu.Lock()
	grant, ok := oidcState.codes[h]
	delete(oidcState.codes, h) // single use
	oidcState.mu.Unlock()
	if !ok || grant.Expires < now() || grant.ClientID != c.ID || grant.RedirectURI != r.PostForm.Get("redirect_uri") {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "code is invalid, expired or already used")
		return
	}
	if grant.Challenge != "" {
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if subtle.ConstantTimeCompare([]byte(b64url.EncodeToString(sum[:])), []byte(grant.Challenge)) != 1 {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
			return
		}
	}
	u, err := a.store.UserByID(grant.UserID)
	if err != nil || u == nil || u.Disabled {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "user no longer exists")
		return
	}

	t := now()
	claims := a.userClaims(u, grant.Scope)
	claims["iss"] = a.oidcIssuer()
	claims["aud"] = c.ID
	claims["iat"] = t
	claims["exp"] = t + oidcTokenTTL
	claims["auth_time"] = grant.AuthTime
	if grant.Nonce != "" {
		claims["nonce"] = grant.Nonce
	}
	idToken, err := a.signJWT(claims)
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "signing failed")
		return
	}
	access := newToken()
	oidcState.mu.Lock()
	oidcState.tokens[sha(access)] = oidcToken{UserID: u.ID, ClientID: c.ID, Scope: grant.Scope, Expires: t + oidcTokenTTL}
	oidcState.mu.Unlock()

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": access, "token_type": "Bearer", "expires_in": oidcTokenTTL, "id_token": idToken, "scope": grant.Scope,
	})
}

func (a *App) handleOIDCUserinfo(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	oidcState.mu.Lock()
	tok, ok := oidcState.tokens[sha(strings.TrimPrefix(auth, "Bearer "))]
	oidcState.mu.Unlock()
	if !ok || tok.Expires < now() {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	u, err := a.store.UserByID(tok.UserID)
	if err != nil || u == nil || u.Disabled {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, a.userClaims(u, tok.Scope))
}

// cleanupOIDC drops expired codes and tokens (called from the janitor).
func cleanupOIDC() {
	oidcState.mu.Lock()
	defer oidcState.mu.Unlock()
	t := now()
	for k, v := range oidcState.codes {
		if v.Expires < t {
			delete(oidcState.codes, k)
		}
	}
	for k, v := range oidcState.tokens {
		if v.Expires < t {
			delete(oidcState.tokens, k)
		}
	}
}

// newOIDCClientID returns a readable, unique client id derived from the name.
func newOIDCClientID(name string, existing []*OIDCClient) string {
	base := strings.Trim(strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 32
		}
		return '-'
	}, name), "-")
	if base == "" {
		base = "app"
	}
	id := base
	for i := 2; ; i++ {
		taken := false
		for _, c := range existing {
			if c.ID == id {
				taken = true
			}
		}
		if !taken {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
}

var _ = time.Second
