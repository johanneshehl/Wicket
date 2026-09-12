package main

import (
	"fmt"
	"html"
	"log"
	"net/http"
	"strings"
)

// Password reset and invitations. Mails go through the operator's own SMTP server; without a
// configured mail server both flows are switched off.

const (
	resetTTL       = 3600       // link from "forgot password"
	adminResetTTL  = 24 * 3600  // link an admin sends
	inviteTTL      = 7 * 86400
)

func (a *App) loginHome(p *page) string {
	if p.Admin {
		return "/login"
	}
	return "/"
}

func (a *App) mailEnabled() bool { return a.smtpConfig().Configured() }

// sendAccountMail sends an invitation or reset link to the user. purpose: "invite" | "reset".
func (a *App) sendAccountMail(u *User, purpose, link, lang string, ttl int64) error {
	site := rootDomain(a.settings())
	if site == "" {
		site = "Wicket"
	}
	var validity string
	if ttl >= 86400 {
		validity = tr(lang, "mail.validDays", ttl/86400)
	} else {
		validity = tr(lang, "mail.validHours", ttl/3600)
	}
	subject := tr(lang, "mail."+purpose+"Subject", site)
	text := tr(lang, "mail."+purpose+"Body", u.Username, link, validity)
	button := tr(lang, "mail."+purpose+"Button")
	var body strings.Builder
	for _, para := range strings.Split(text, "\n\n") {
		if strings.TrimSpace(para) == link {
			fmt.Fprintf(&body, `<p><a href="%s" style="display:inline-block;padding:10px 18px;background:#111;color:#fff;border-radius:6px;text-decoration:none">%s</a></p>`,
				html.EscapeString(link), html.EscapeString(button))
			continue
		}
		fmt.Fprintf(&body, "<p>%s</p>", strings.ReplaceAll(html.EscapeString(para), "\n", "<br>"))
	}
	htmlBody := `<div style="font-family:system-ui,sans-serif;font-size:15px;line-height:1.5;color:#111;max-width:520px">` + body.String() + `</div>`
	return sendMail(a.smtpConfig(), Mail{To: u.Email, Subject: subject, Text: text, HTML: htmlBody})
}

// issueLink creates a token and mails it; the link always points at the login host.
func (a *App) issueLink(u *User, purpose, lang string, ttl int64) error {
	if u.Email == "" {
		return userErr("err.userNoEmail")
	}
	if !a.mailEnabled() {
		return userErr("err.smtpMissing")
	}
	token, err := a.store.CreateUserToken(u.ID, purpose, ttl)
	if err != nil {
		return err
	}
	link := a.loginBase(a.settings()) + "/" + purpose + "/" + token
	if err := a.sendAccountMail(u, purpose, link, lang, ttl); err != nil {
		return userErr("err.mailTest", err.Error())
	}
	return nil
}

// ---------------------------------------------------------------- "forgot password"

func (a *App) handleResetPage(w http.ResponseWriter, r *http.Request) {
	if !a.mailEnabled() {
		http.NotFound(w, r)
		return
	}
	p := a.basePage(w, r, "reset.title")
	p.Kind, p.StackClass = "reset-request", "wide"
	a.render(w, http.StatusOK, "reset.html", p)
}

func (a *App) handleResetPost(w http.ResponseWriter, r *http.Request) {
	if !a.mailEnabled() {
		http.NotFound(w, r)
		return
	}
	p := a.basePage(w, r, "reset.title")
	p.StackClass = "wide"
	if !checkCSRF(r) {
		p.Kind, p.Error = "reset-request", p.T("err.csrf")
		a.render(w, http.StatusBadRequest, "reset.html", p)
		return
	}
	// at most 5 requests per IP in 15 minutes; the answer is the same either way
	key := "reset:" + clientIP(r)
	_, limited := a.limiter.Locked(key)
	if !limited {
		a.limiter.Fail(key, 5, 900, 900)
		login := strings.TrimSpace(r.FormValue("login"))
		u, _ := a.store.UserByName(login)
		if u == nil && strings.Contains(login, "@") {
			u, _ = a.store.UserByEmail(login)
		}
		if u != nil && !u.Disabled && u.Email != "" {
			lang := a.langFor(r)
			go func(u *User) {
				if err := a.issueLink(u, "reset", lang, resetTTL); err != nil {
					log.Printf("reset mail for %s: %v", u.Username, err)
				}
			}(u)
			a.event(r, "password_reset_requested", u.Username, "", "")
		}
	}
	p.Kind = "reset-sent"
	a.render(w, http.StatusOK, "reset.html", p)
}

// ---------------------------------------------------------------- link pages (reset + invite)

func (a *App) tokenPage(purpose string) (http.HandlerFunc, http.HandlerFunc) {
	title := "reset.newTitle"
	if purpose == "invite" {
		title = "invite.title"
	}
	get := func(w http.ResponseWriter, r *http.Request) {
		p := a.basePage(w, r, title)
		p.StackClass = "wide"
		u, err := a.store.PeekUserToken(r.PathValue("token"), purpose)
		if err != nil || u == nil {
			p.Kind = "reset-invalid"
			a.render(w, http.StatusGone, "reset.html", p)
			return
		}
		p.Kind, p.Username, p.Token = purpose+"-form", u.Username, r.PathValue("token")
		a.render(w, http.StatusOK, "reset.html", p)
	}
	post := func(w http.ResponseWriter, r *http.Request) {
		p := a.basePage(w, r, title)
		p.StackClass = "wide"
		token := r.PathValue("token")
		u, err := a.store.PeekUserToken(token, purpose)
		if err != nil || u == nil {
			p.Kind = "reset-invalid"
			a.render(w, http.StatusGone, "reset.html", p)
			return
		}
		p.Kind, p.Username, p.Token = purpose+"-form", u.Username, token
		pw := r.FormValue("password")
		switch {
		case !checkCSRF(r):
			p.Error = p.T("err.csrf")
		case len(pw) < 10:
			p.Error = p.T("err.password10")
		case pw != r.FormValue("password2"):
			p.Error = p.T("err.passwordMatch")
		}
		if p.Error != "" {
			a.render(w, http.StatusBadRequest, "reset.html", p)
			return
		}
		if used, err := a.store.UseUserToken(token, purpose); err != nil || used == nil {
			p.Kind = "reset-invalid"
			a.render(w, http.StatusGone, "reset.html", p)
			return
		}
		if err := a.store.SetPassword(u.ID, hashPassword(pw)); err != nil {
			internalError(w, err)
			return
		}
		_ = a.store.DeleteUserSessions(u.ID) // a reset signs out every device
		kind := "password_reset"
		if purpose == "invite" {
			kind = "invite_accepted"
		}
		a.event(r, kind, u.Username, "", "")
		p.Kind = "reset-done"
		a.render(w, http.StatusOK, "reset.html", p)
	}
	return get, post
}
