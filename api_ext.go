package main

import (
	"net/http"
	"strings"
)

// Admin API for the features added in 1.3: groups, Docker targets, OIDC clients, mail server,
// notifications and integration snippets.

func (a *App) registerExtAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/groups", a.apiGroups)
	mux.HandleFunc("POST /api/groups", a.apiGroupCreate)
	mux.HandleFunc("PUT /api/groups/{id}", a.apiGroupUpdate)
	mux.HandleFunc("DELETE /api/groups/{id}", a.apiGroupDelete)

	mux.HandleFunc("POST /api/users/{id}/invite", a.apiUserInvite)
	mux.HandleFunc("POST /api/users/{id}/reset-link", a.apiUserResetLink)

	mux.HandleFunc("GET /api/containers", a.apiContainers)
	mux.HandleFunc("GET /api/integrations", a.apiIntegrations)

	mux.HandleFunc("GET /api/oidc", a.apiOIDCList)
	mux.HandleFunc("POST /api/oidc", a.apiOIDCCreate)
	mux.HandleFunc("PUT /api/oidc/{id}", a.apiOIDCUpdate)
	mux.HandleFunc("POST /api/oidc/{id}/secret", a.apiOIDCRotate)
	mux.HandleFunc("DELETE /api/oidc/{id}", a.apiOIDCDelete)

	mux.HandleFunc("GET /api/mail", a.apiMailGet)
	mux.HandleFunc("PUT /api/mail", a.apiMailPut)
	mux.HandleFunc("POST /api/mail/test", a.apiMailTest)

	mux.HandleFunc("GET /api/notify", a.apiNotifyGet)
	mux.HandleFunc("PUT /api/notify", a.apiNotifyPut)
	mux.HandleFunc("POST /api/notify/test", a.apiNotifyTest)
}

// ---------------------------------------------------------------- groups

type groupIn struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Members     []int64 `json:"members"`
}

func (a *App) apiGroups(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.ListGroups()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": list})
}

func (a *App) readGroup(w http.ResponseWriter, r *http.Request) (*Group, bool) {
	var in groupIn
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return nil, false
	}
	in.Name = strings.TrimSpace(in.Name)
	if !validGroupName(in.Name) {
		a.errKey(w, r, http.StatusBadRequest, "err.groupName")
		return nil, false
	}
	if in.Members == nil {
		in.Members = []int64{}
	}
	return &Group{Name: in.Name, Description: strings.TrimSpace(in.Description), Members: in.Members}, true
}

func (a *App) apiGroupCreate(w http.ResponseWriter, r *http.Request) {
	g, ok := a.readGroup(w, r)
	if !ok {
		return
	}
	id, err := a.store.CreateGroup(g)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			a.errKey(w, r, http.StatusConflict, "err.groupExists")
			return
		}
		a.fail(w, r, err)
		return
	}
	g.ID, g.CreatedAt = id, now()
	a.event(r, "group_created", reqUser(r).Username, "", g.Name)
	writeJSON(w, http.StatusCreated, g)
}

func (a *App) apiGroupUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	g, ok := a.readGroup(w, r)
	if !ok {
		return
	}
	g.ID = id
	if err := a.store.UpdateGroup(g); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			a.errKey(w, r, http.StatusConflict, "err.groupExists")
			return
		}
		a.fail(w, r, err)
		return
	}
	a.event(r, "group_updated", reqUser(r).Username, "", g.Name)
	writeJSON(w, http.StatusOK, g)
}

func (a *App) apiGroupDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	name := ""
	if list, err := a.store.ListGroups(); err == nil {
		for _, g := range list {
			if g.ID == id {
				name = g.Name
			}
		}
	}
	if name == "" {
		a.errKey(w, r, http.StatusNotFound, "err.groupNotFound")
		return
	}
	if err := a.store.DeleteGroup(id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.event(r, "group_deleted", reqUser(r).Username, "", name)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------- integrations

func (a *App) apiIntegrations(w http.ResponseWriter, r *http.Request) {
	s := a.settings()
	login := a.loginBase(s)
	auth := a.cfg.AuthAddr
	traefik := "http:\n  middlewares:\n    wicket:\n      forwardAuth:\n        address: http://" + auth + "/verify\n" +
		"        trustForwardHeader: true\n        authResponseHeaders: [Remote-User, Remote-Role, Remote-Email, Remote-Groups]\n"
	nginx := "location = /_wicket {\n    internal;\n    proxy_pass http://" + auth + "/verify?mode=nginx;\n" +
		"    proxy_pass_request_body off;\n    proxy_set_header Content-Length \"\";\n" +
		"    proxy_set_header X-Original-URL $scheme://$http_host$request_uri;\n    proxy_set_header X-Forwarded-For $remote_addr;\n}\n\n" +
		"location / {\n    auth_request /_wicket;\n    auth_request_set $wicket_user $upstream_http_remote_user;\n" +
		"    auth_request_set $wicket_location $upstream_http_x_wicket_location;\n    proxy_set_header Remote-User $wicket_user;\n" +
		"    error_page 401 = @wicket_login;\n    proxy_pass http://127.0.0.1:8080;\n}\n\n" +
		"location @wicket_login {\n    return 302 $wicket_location;\n}\n"
	_, _, docker := a.dockerClient()
	writeJSON(w, http.StatusOK, map[string]any{
		"loginUrl": login,
		"metrics":  map[string]any{"path": "/metrics", "tokenSet": a.cfg.MetricsToken != ""},
		"docker":   docker,
		"traefik":  traefik,
		"nginx":    nginx,
		"oidc":     map[string]string{"issuer": a.oidcIssuer(), "discovery": a.oidcIssuer() + "/.well-known/openid-configuration"},
	})
}

// ---------------------------------------------------------------- OIDC clients

type oidcIn struct {
	Name         string   `json:"name"`
	RedirectURIs []string `json:"redirectUris"`
	Public       bool     `json:"public"`
	Access       string   `json:"access"`
	Users        []int64  `json:"users"`
	Groups       []int64  `json:"groups"`
}

func (in *oidcIn) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if len(in.Name) < 2 || len(in.Name) > 48 {
		return userErr("err.oidcName")
	}
	uris := []string{}
	for _, u := range in.RedirectURIs {
		if u = strings.TrimSpace(u); u == "" {
			continue
		}
		if !validRedirectURI(u) {
			return userErr("err.oidcRedirect", u)
		}
		uris = append(uris, u)
	}
	if len(uris) == 0 {
		return userErr("err.oidcRedirectMissing")
	}
	in.RedirectURIs = uris
	switch in.Access {
	case "all", "admins", "users":
	default:
		return userErr("err.access")
	}
	if in.Access != "users" || in.Users == nil {
		in.Users = []int64{}
	}
	if in.Access != "users" || in.Groups == nil {
		in.Groups = []int64{}
	}
	return nil
}

func publicClient(c *OIDCClient) OIDCClient {
	v := *c
	v.SecretHash = ""
	return v
}

func (a *App) apiOIDCList(w http.ResponseWriter, r *http.Request) {
	list, err := a.oidcClients()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	out := []OIDCClient{}
	for _, c := range list {
		out = append(out, publicClient(c))
	}
	iss := a.oidcIssuer()
	writeJSON(w, http.StatusOK, map[string]any{"clients": out, "issuer": iss, "discovery": iss + "/.well-known/openid-configuration"})
}

func (a *App) apiOIDCCreate(w http.ResponseWriter, r *http.Request) {
	var in oidcIn
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := in.validate(); err != nil {
		a.fail(w, r, err)
		return
	}
	list, err := a.oidcClients()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	c := &OIDCClient{ID: newOIDCClientID(in.Name, list), Name: in.Name, Public: in.Public, RedirectURIs: in.RedirectURIs,
		Access: in.Access, Users: in.Users, Groups: in.Groups, CreatedAt: now()}
	secret := ""
	if !c.Public {
		secret = newToken()
		c.SecretHash = sha(secret)
	}
	if err := a.saveOIDCClients(append(list, c)); err != nil {
		a.fail(w, r, err)
		return
	}
	a.event(r, "oidc_client_created", reqUser(r).Username, "", c.Name)
	writeJSON(w, http.StatusCreated, map[string]any{"client": publicClient(c), "secret": secret})
}

func (a *App) findOIDC(w http.ResponseWriter, r *http.Request) ([]*OIDCClient, *OIDCClient, bool) {
	list, err := a.oidcClients()
	if err != nil {
		a.fail(w, r, err)
		return nil, nil, false
	}
	for _, c := range list {
		if c.ID == r.PathValue("id") {
			return list, c, true
		}
	}
	a.errKey(w, r, http.StatusNotFound, "err.oidcNotFound")
	return nil, nil, false
}

func (a *App) apiOIDCUpdate(w http.ResponseWriter, r *http.Request) {
	list, c, ok := a.findOIDC(w, r)
	if !ok {
		return
	}
	var in oidcIn
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := in.validate(); err != nil {
		a.fail(w, r, err)
		return
	}
	c.Name, c.RedirectURIs, c.Access, c.Users, c.Groups = in.Name, in.RedirectURIs, in.Access, in.Users, in.Groups
	secret := ""
	if c.Public != in.Public {
		c.Public = in.Public
		c.SecretHash = ""
		if !c.Public {
			secret = newToken()
			c.SecretHash = sha(secret)
		}
	}
	if err := a.saveOIDCClients(list); err != nil {
		a.fail(w, r, err)
		return
	}
	a.event(r, "oidc_client_updated", reqUser(r).Username, "", c.Name)
	writeJSON(w, http.StatusOK, map[string]any{"client": publicClient(c), "secret": secret})
}

func (a *App) apiOIDCRotate(w http.ResponseWriter, r *http.Request) {
	list, c, ok := a.findOIDC(w, r)
	if !ok {
		return
	}
	if c.Public {
		a.errKey(w, r, http.StatusBadRequest, "err.oidcPublic")
		return
	}
	secret := newToken()
	c.SecretHash = sha(secret)
	if err := a.saveOIDCClients(list); err != nil {
		a.fail(w, r, err)
		return
	}
	a.event(r, "oidc_client_updated", reqUser(r).Username, "", c.Name+":secret")
	writeJSON(w, http.StatusOK, map[string]any{"secret": secret})
}

func (a *App) apiOIDCDelete(w http.ResponseWriter, r *http.Request) {
	list, c, ok := a.findOIDC(w, r)
	if !ok {
		return
	}
	out := []*OIDCClient{}
	for _, x := range list {
		if x.ID != c.ID {
			out = append(out, x)
		}
	}
	if err := a.saveOIDCClients(out); err != nil {
		a.fail(w, r, err)
		return
	}
	a.event(r, "oidc_client_deleted", reqUser(r).Username, "", c.Name)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------- mail server

func (a *App) apiMailGet(w http.ResponseWriter, r *http.Request) {
	c := a.smtpConfig()
	has := c.Password != ""
	c.Password = ""
	writeJSON(w, http.StatusOK, map[string]any{"smtp": c, "hasPassword": has})
}

func (a *App) apiMailPut(w http.ResponseWriter, r *http.Request) {
	var in SMTPConfig
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	in.Host, in.From, in.Username = strings.TrimSpace(in.Host), strings.TrimSpace(in.From), strings.TrimSpace(in.Username)
	in.FromName = strings.TrimSpace(in.FromName)
	if in.Password == "" {
		in.Password = a.smtpConfig().Password // empty field keeps the stored password
	}
	if in.Host != "" {
		if err := validSMTP(in); err != nil {
			a.errKey(w, r, http.StatusBadRequest, "err.smtp", err.Error())
			return
		}
	}
	if err := a.saveSMTP(in); err != nil {
		a.fail(w, r, err)
		return
	}
	a.event(r, "settings_changed", reqUser(r).Username, "", "smtp")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) apiMailTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		To string `json:"to"`
	}
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	c := a.smtpConfig()
	if !c.Configured() {
		a.errKey(w, r, http.StatusBadRequest, "err.smtpMissing")
		return
	}
	to := strings.TrimSpace(in.To)
	if to == "" {
		to = reqUser(r).Email
	}
	if !strings.Contains(to, "@") {
		a.errKey(w, r, http.StatusBadRequest, "err.mailTo")
		return
	}
	lang := a.langFor(r)
	if err := sendMail(c, Mail{To: to, Subject: tr(lang, "mail.testSubject"), Text: tr(lang, "mail.testBody")}); err != nil {
		a.errKey(w, r, http.StatusBadRequest, "err.mailTest", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": tr(lang, "mail.testSent", to)})
}

// ---------------------------------------------------------------- notifications

func (a *App) apiNotifyGet(w http.ResponseWriter, r *http.Request) {
	type view struct {
		NotifyChannel
		HasToken bool `json:"hasToken"`
	}
	out := []view{}
	for _, c := range a.notifyChannels() {
		has := c.Token != ""
		c.Token = ""
		out = append(out, view{c, has})
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": out, "events": notifyEvents, "mailConfigured": a.smtpConfig().Configured()})
}

func (a *App) apiNotifyPut(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Channels []NotifyChannel `json:"channels"`
	}
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	old := map[string]NotifyChannel{}
	for _, c := range a.notifyChannels() {
		old[c.ID] = c
	}
	out := []NotifyChannel{}
	for _, c := range in.Channels {
		if c.ID == "" {
			c.ID = newToken()[:12]
		}
		if c.Token == "" {
			c.Token = old[c.ID].Token
		}
		if err := validNotifyChannel(&c); err != nil {
			a.errKey(w, r, http.StatusBadRequest, "err.notifyChannel", c.Name, err.Error())
			return
		}
		out = append(out, c)
	}
	if err := a.saveNotifyChannels(out); err != nil {
		a.fail(w, r, err)
		return
	}
	a.event(r, "settings_changed", reqUser(r).Username, "", "notify")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) apiNotifyTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
	}
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	for _, c := range a.notifyChannels() {
		if c.ID != in.ID {
			continue
		}
		lang := a.langFor(r)
		n := Notification{Event: "test", Title: tr(lang, "notify.testTitle"), Message: tr(lang, "notify.testBody")}
		if err := deliver(c, a.smtpConfig(), n); err != nil {
			a.errKey(w, r, http.StatusBadRequest, "err.notifyTest", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": tr(lang, "notify.testSent", c.Name)})
		return
	}
	a.errKey(w, r, http.StatusNotFound, "err.notifyNotFound")
}
