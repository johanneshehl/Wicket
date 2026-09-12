package main

import (
	"net/http"
	"strconv"
)

// pathID2 reads a second numeric path value, e.g. {pid}.
func pathID2(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		return 0, userErr("err.badID")
	}
	return id, nil
}

// Admin actions that e-mail a link to a user.

func (a *App) apiUserInvite(w http.ResponseWriter, r *http.Request) {
	u := a.loadUser(w, r)
	if u == nil {
		return
	}
	if err := a.issueLink(u, "invite", a.notifyLang(), inviteTTL); err != nil {
		a.fail(w, r, err)
		return
	}
	a.event(r, "invite_sent", reqUser(r).Username, "", u.Username)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": tr(a.langFor(r), "mail.inviteSent", u.Email)})
}

func (a *App) apiUserResetLink(w http.ResponseWriter, r *http.Request) {
	u := a.loadUser(w, r)
	if u == nil {
		return
	}
	if err := a.issueLink(u, "reset", a.notifyLang(), adminResetTTL); err != nil {
		a.fail(w, r, err)
		return
	}
	a.event(r, "password_reset_requested", reqUser(r).Username, "", u.Username)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": tr(a.langFor(r), "mail.resetSent", u.Email)})
}
