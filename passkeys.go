package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// Passkeys (WebAuthn). The relying party is the main domain, so one passkey works on the login host
// and on the admin host. A passkey sign-in is phishing-resistant and counts as the second factor.

const passkeySchema = `
create table if not exists passkeys (
	id integer primary key,
	user_id integer not null references users(id) on delete cascade,
	cred_id text unique not null,
	name text not null default '',
	credential text not null,
	created_at integer not null,
	last_used integer not null default 0
);
create table if not exists user_handles (
	user_id integer primary key references users(id) on delete cascade,
	handle text unique not null
);
`

var passkeyOnce sync.Once

func (st *Store) ensurePasskeys() {
	passkeyOnce.Do(func() {
		if _, err := st.db.Exec(passkeySchema); err != nil {
			log.Printf("passkey schema: %v", err)
		}
	})
}

type Passkey struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"createdAt"`
	LastUsed  int64  `json:"lastUsed"`
}

var b64raw = base64.RawURLEncoding

// userHandle is a random, stable WebAuthn user id (never the database id or the name).
func (st *Store) userHandle(uid int64) ([]byte, error) {
	st.ensurePasskeys()
	var h string
	err := st.db.QueryRow(`select handle from user_handles where user_id = ?`, uid).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		h = b64raw.EncodeToString(b)
		if _, err := st.db.Exec(`insert into user_handles (user_id, handle) values (?, ?)`, uid, h); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return b64raw.DecodeString(h)
}

func (st *Store) userByHandle(handle []byte) (*User, error) {
	st.ensurePasskeys()
	var uid int64
	err := st.db.QueryRow(`select user_id from user_handles where handle = ?`, b64raw.EncodeToString(handle)).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return st.UserByID(uid)
}

func (st *Store) passkeyCredentials(uid int64) ([]webauthn.Credential, error) {
	st.ensurePasskeys()
	rows, err := st.db.Query(`select credential from passkeys where user_id = ?`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []webauthn.Credential
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var c webauthn.Credential
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (st *Store) ListPasskeys(uid int64) ([]Passkey, error) {
	st.ensurePasskeys()
	rows, err := st.db.Query(`select id, name, created_at, last_used from passkeys where user_id = ? order by created_at`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Passkey{}
	for rows.Next() {
		var p Passkey
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.LastUsed); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (st *Store) HasPasskeys(uid int64) bool {
	st.ensurePasskeys()
	var n int
	_ = st.db.QueryRow(`select count(*) from passkeys where user_id = ?`, uid).Scan(&n)
	return n > 0
}

func (st *Store) AddPasskey(uid int64, name string, c *webauthn.Credential) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = st.db.Exec(`insert into passkeys (user_id, cred_id, name, credential, created_at) values (?, ?, ?, ?, ?)`,
		uid, b64raw.EncodeToString(c.ID), name, string(raw), now())
	return err
}

// touchPasskey stores the updated credential (sign counter, flags) after a sign-in.
func (st *Store) touchPasskey(c *webauthn.Credential) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = st.db.Exec(`update passkeys set credential = ?, last_used = ? where cred_id = ?`, string(raw), now(), b64raw.EncodeToString(c.ID))
	return err
}

func (st *Store) DeletePasskey(uid, id int64) (bool, error) {
	st.ensurePasskeys()
	res, err := st.db.Exec(`delete from passkeys where id = ? and user_id = ?`, id, uid)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ---------------------------------------------------------------- WebAuthn glue

type waUser struct {
	u      *User
	handle []byte
	creds  []webauthn.Credential
}

func (w *waUser) WebAuthnID() []byte                         { return w.handle }
func (w *waUser) WebAuthnName() string                       { return w.u.Username }
func (w *waUser) WebAuthnDisplayName() string                { return w.u.Username }
func (w *waUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }

func (a *App) loadWAUser(u *User) (*waUser, error) {
	h, err := a.store.userHandle(u.ID)
	if err != nil {
		return nil, err
	}
	creds, err := a.store.passkeyCredentials(u.ID)
	if err != nil {
		return nil, err
	}
	return &waUser{u: u, handle: h, creds: creds}, nil
}

func (a *App) webAuthn() (*webauthn.WebAuthn, error) {
	s := a.settings()
	rp := rootDomain(s)
	var origins []string
	for _, h := range []string{s.LoginHost, s.AdminHost} {
		if h != "" {
			origins = append(origins, "https://"+h)
		}
	}
	if rp == "" || len(origins) == 0 {
		return nil, userErr("err.passkeySetup")
	}
	return webauthn.New(&webauthn.Config{RPID: rp, RPDisplayName: "Wicket", RPOrigins: origins})
}

// Ceremony state lives on the server, referenced by a short-lived cookie.
const passkeyCookie = "wicket_pk"

type pkCeremony struct {
	data    webauthn.SessionData
	userID  int64 // registration only
	expires int64
}

var pkCeremonies = struct {
	sync.Mutex
	m map[string]pkCeremony
}{m: map[string]pkCeremony{}}

func putCeremony(w http.ResponseWriter, r *http.Request, c pkCeremony) {
	id := newToken()
	c.expires = now() + 300
	pkCeremonies.Lock()
	for k, v := range pkCeremonies.m {
		if v.expires < now() {
			delete(pkCeremonies.m, k)
		}
	}
	pkCeremonies.m[id] = c
	pkCeremonies.Unlock()
	http.SetCookie(w, &http.Cookie{Name: passkeyCookie, Value: id, Path: "/passkey/", HttpOnly: true, Secure: isHTTPS(r),
		SameSite: http.SameSiteStrictMode, MaxAge: 300})
}

func takeCeremony(r *http.Request) (pkCeremony, bool) {
	c, err := r.Cookie(passkeyCookie)
	if err != nil {
		return pkCeremony{}, false
	}
	pkCeremonies.Lock()
	defer pkCeremonies.Unlock()
	cer, ok := pkCeremonies.m[c.Value]
	delete(pkCeremonies.m, c.Value)
	return cer, ok && cer.expires >= now()
}

// JSON endpoints called by passkey.js; the custom header blocks cross-site form posts.
func (a *App) passkeyGuard(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-Wicket") != "1" {
		a.errKey(w, r, http.StatusForbidden, "err.rejected")
		return false
	}
	return true
}

func (a *App) handlePasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if !a.passkeyGuard(w, r) {
		return
	}
	sess, user := a.activeSession(r)
	if sess == nil {
		a.errKey(w, r, http.StatusUnauthorized, "err.notSignedIn")
		return
	}
	wa, err := a.webAuthn()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	wu, err := a.loadWAUser(user)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	exclude := make([]protocol.CredentialDescriptor, 0, len(wu.creds))
	for _, c := range wu.creds {
		exclude = append(exclude, c.Descriptor())
	}
	opts, data, err := wa.BeginRegistration(wu,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(exclude))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	putCeremony(w, r, pkCeremony{data: *data, userID: user.ID})
	writeJSON(w, http.StatusOK, opts)
}

func (a *App) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if !a.passkeyGuard(w, r) {
		return
	}
	sess, user := a.activeSession(r)
	cer, ok := takeCeremony(r)
	if sess == nil || !ok || cer.userID != user.ID {
		a.errKey(w, r, http.StatusBadRequest, "err.passkey", "session expired")
		return
	}
	wa, err := a.webAuthn()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	wu, err := a.loadWAUser(user)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	cred, err := wa.FinishRegistration(wu, cer.data, r)
	if err != nil {
		a.errKey(w, r, http.StatusBadRequest, "err.passkey", err.Error())
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = passkeyName(r.UserAgent())
	}
	if len(name) > 48 {
		name = name[:48]
	}
	if err := a.store.AddPasskey(user.ID, name, cred); err != nil {
		a.fail(w, r, err)
		return
	}
	a.event(r, "passkey_added", user.Username, "", name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": tr(a.langFor(r), "passkey.added")})
}

// passkeyName suggests a readable name from the browser ("Chrome on Windows").
func passkeyName(ua string) string {
	os := ""
	switch {
	case strings.Contains(ua, "iPhone"):
		os = "iPhone"
	case strings.Contains(ua, "iPad"):
		os = "iPad"
	case strings.Contains(ua, "Android"):
		os = "Android"
	case strings.Contains(ua, "Windows"):
		os = "Windows"
	case strings.Contains(ua, "Mac OS"):
		os = "Mac"
	case strings.Contains(ua, "Linux"):
		os = "Linux"
	}
	if os == "" {
		return "Passkey"
	}
	return "Passkey · " + os
}

func (a *App) handlePasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if !a.passkeyGuard(w, r) {
		return
	}
	if rem, locked := a.limiter.Locked("ip:" + clientIP(r)); locked {
		a.errKey(w, r, http.StatusTooManyRequests, "err.passkeyLocked", (rem+59)/60)
		return
	}
	wa, err := a.webAuthn()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	opts, data, err := wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	putCeremony(w, r, pkCeremony{data: *data})
	writeJSON(w, http.StatusOK, opts)
}

func (a *App) handlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if !a.passkeyGuard(w, r) {
		return
	}
	key := "ip:" + clientIP(r)
	if rem, locked := a.limiter.Locked(key); locked {
		a.errKey(w, r, http.StatusTooManyRequests, "err.passkeyLocked", (rem+59)/60)
		return
	}
	cer, ok := takeCeremony(r)
	if !ok {
		a.errKey(w, r, http.StatusBadRequest, "err.passkey", "session expired")
		return
	}
	wa, err := a.webAuthn()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	handler := func(rawID, handle []byte) (webauthn.User, error) {
		u, err := a.store.userByHandle(handle)
		if err != nil || u == nil || u.Disabled {
			return nil, errors.New("unknown passkey")
		}
		return a.loadWAUser(u)
	}
	wu, cred, err := wa.FinishPasskeyLogin(handler, cer.data, r)
	if err != nil {
		countMetric("wicket_logins_total", "failure")
		a.event(r, "passkey_fail", "", "", "")
		a.lockIfNeeded(r, key, "")
		a.errKey(w, r, http.StatusUnauthorized, "err.passkey", err.Error())
		return
	}
	user := wu.(*waUser).u
	admin := a.isAdminHost(r)
	if admin && !canAdmin(user) {
		a.event(r, "denied", user.Username, hostOnly(r.Host), "not-admin")
		a.errKey(w, r, http.StatusForbidden, "err.noAdminAccess")
		return
	}
	_ = a.store.touchPasskey(cred)
	a.limiter.Reset(key)
	if _, err := a.newSession(w, r, user, "active", true, false); err != nil {
		a.fail(w, r, err)
		return
	}
	countMetric("wicket_logins_total", "success")
	rd := validRedirect(r.URL.Query().Get("rd"), a.settings())
	site := ""
	if rd != "" {
		if i := strings.Index(rd, "://"); i >= 0 {
			site = hostOnly(strings.SplitN(rd[i+3:], "/", 2)[0])
		}
	}
	a.event(r, "login_ok", user.Username, site, "passkey")
	_ = a.store.TouchUser(user.ID)
	writeJSON(w, http.StatusOK, map[string]string{"redirect": a.continueTarget(r, rd)})
}

// ---------------------------------------------------------------- admin API

func (a *App) apiMyPasskeys(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.ListPasskeys(reqUser(r).ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": list})
}

func (a *App) apiMyPasskeyDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	u := reqUser(r)
	if ok, err := a.store.DeletePasskey(u.ID, id); err != nil || !ok {
		a.errKey(w, r, http.StatusNotFound, "err.passkeyNotFound")
		return
	}
	a.event(r, "passkey_removed", u.Username, "", "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) apiUserPasskeys(w http.ResponseWriter, r *http.Request) {
	u := a.loadUser(w, r)
	if u == nil {
		return
	}
	list, err := a.store.ListPasskeys(u.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": list})
}

func (a *App) apiUserPasskeyDelete(w http.ResponseWriter, r *http.Request) {
	u := a.loadUser(w, r)
	if u == nil {
		return
	}
	pid, err := pathID2(r, "pid")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if ok, err := a.store.DeletePasskey(u.ID, pid); err != nil || !ok {
		a.errKey(w, r, http.StatusNotFound, "err.passkeyNotFound")
		return
	}
	a.event(r, "passkey_removed", reqUser(r).Username, "", u.Username)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
