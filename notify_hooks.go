package main

import (
	"encoding/json"
	"log"
)

// Configuration of the operator's mail server and notification channels (settings table, JSON).

func (a *App) smtpConfig() SMTPConfig {
	var c SMTPConfig
	if raw, ok, err := a.store.GetSetting("smtp"); err == nil && ok {
		_ = json.Unmarshal([]byte(raw), &c)
	}
	if c.Security == "" {
		c.Security = "starttls"
	}
	return c
}

func (a *App) saveSMTP(c SMTPConfig) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return a.store.SetSetting("smtp", string(b))
}

func (a *App) notifyChannels() []NotifyChannel {
	out := []NotifyChannel{}
	if raw, ok, err := a.store.GetSetting("notify"); err == nil && ok {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

func (a *App) saveNotifyChannels(list []NotifyChannel) error {
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return a.store.SetSetting("notify", string(b))
}

// notifyLang: notifications follow the configured language, "auto" falls back to English.
func (a *App) notifyLang() string {
	if l := a.settings().Language; supportedLang(l) {
		return l
	}
	return "en"
}

var changeKinds = map[string]bool{}

func init() {
	for _, k := range kindGroups["admin"] {
		changeKinds[k] = true
	}
}

// afterEvent turns audit events into notifications. It runs in its own goroutine.
func (a *App) afterEvent(e Event) {
	channels := a.notifyChannels()
	if len(channels) == 0 {
		return
	}
	lang := a.notifyLang()
	s := a.settings()
	link := ""
	if s.AdminHost != "" {
		link = "https://" + s.AdminHost + "/#/log"
	}
	var list []Notification
	switch {
	case e.Kind == "login_ok":
		if u, err := a.store.UserByName(e.Username); err == nil && canAdmin(u) {
			list = append(list, Notification{Event: "admin_signin", Title: tr(lang, "notify.adminTitle", e.Username),
				Message: tr(lang, "notify.adminBody", e.Username, e.IP, e.UA), Link: link})
		}
		if a.isNewDevice(e) {
			list = append(list, Notification{Event: "new_device", Title: tr(lang, "notify.deviceTitle", e.Username),
				Message: tr(lang, "notify.deviceBody", e.Username, e.IP, e.UA), Link: link})
		}
	case e.Kind == "locked":
		list = append(list, Notification{Event: "ip_locked", Title: tr(lang, "notify.lockedTitle", e.IP),
			Message: tr(lang, "notify.lockedBody", e.IP, e.Site), Link: link})
	case changeKinds[e.Kind]:
		list = append(list, Notification{Event: "settings_changed", Title: tr(lang, "notify.changeTitle"),
			Message: tr(lang, "notify.changeBody", e.Username, e.Kind, e.Site+e.Detail), Link: link})
	}
	smtpCfg := a.smtpConfig()
	for _, n := range list {
		notifyAll(channels, smtpCfg, n)
	}
}

// isNewDevice: the first successful sign-in of this user from this IP address.
func (a *App) isNewDevice(e Event) bool {
	var n int
	err := a.store.db.QueryRow(`select count(*) from events where kind = 'login_ok' and username = ? and ip = ?`, e.Username, e.IP).Scan(&n)
	if err != nil {
		log.Printf("notify: %v", err)
		return false
	}
	return n <= 1
}
