package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Update check and updater.
//
// Wicket asks GitHub for the releases of its repository (30 s after start, then twice a day) and
// tells admins about newer versions. A release can require an update: its notes contain
// "<!-- wicket:required -->" (every older version must update) or "<!-- wicket:min-version 1.4.0 -->"
// (versions below 1.4.0 must update). While a required update is pending, the admin API is locked
// except for the update itself; sign-ins, forward_auth and OIDC keep working. WICKET_UPDATE_CHECK=off
// turns the check and the lock off. Updates are installed by a Watchtower instance through its HTTP API.

const updateInterval = 12 * time.Hour

var (
	requiredRE = regexp.MustCompile(`<!--\s*wicket:required\s*-->`)
	minVerRE   = regexp.MustCompile(`<!--\s*wicket:min-version\s+v?([0-9]+\.[0-9]+\.[0-9]+)\s*-->`)
	commentRE  = regexp.MustCompile(`(?s)<!--.*?-->`)
)

type ghRelease struct {
	TagName     string `json:"tag_name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
}

// updateState is kept in settings key "update_state", so a known required update survives a restart.
type updateState struct {
	CheckedAt   int64  `json:"checkedAt"`
	Error       string `json:"error,omitempty"`
	Latest      string `json:"latest"`
	URL         string `json:"url"`
	Notes       string `json:"notes"`
	PublishedAt string `json:"publishedAt"`
	MinVersion  string `json:"minVersion"`
	ETag        string `json:"etag,omitempty"`
	Notified    string `json:"notified,omitempty"` // version the notification channels were told about
	Disabled    bool   `json:"disabled"`           // automatic check turned off in the admin interface
}

// updateRun describes an update started from the admin interface.
type updateRun struct {
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt"`
	Target     string `json:"target"`
	Backup     string `json:"backup"`
	Error      string `json:"error,omitempty"`
}

var upd struct {
	mu     sync.Mutex
	loaded bool
	state  updateState
	run    updateRun
}

// envOn reads a switch variable: everything except off, false, no and 0 means on.
func envOn(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "off", "false", "no", "0":
		return false
	}
	return true
}

// ---------------------------------------------------------------- versions

type semver [3]int

func parseSemver(s string) (semver, bool) {
	var v semver
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func (v semver) less(o semver) bool {
	for i := range v {
		if v[i] != o[i] {
			return v[i] < o[i]
		}
	}
	return false
}

func (v semver) String() string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// ---------------------------------------------------------------- state

func (a *App) updateState() updateState {
	upd.mu.Lock()
	defer upd.mu.Unlock()
	if !upd.loaded {
		if raw, ok, err := a.store.GetSetting("update_state"); err == nil && ok {
			_ = json.Unmarshal([]byte(raw), &upd.state)
		}
		upd.loaded = true
	}
	return upd.state
}

func (a *App) saveUpdateState(s updateState) {
	upd.mu.Lock()
	upd.state, upd.loaded = s, true
	upd.mu.Unlock()
	if b, err := json.Marshal(s); err == nil {
		_ = a.store.SetSetting("update_state", string(b))
	}
}

// updateStatus tells whether a newer release exists and whether this version must update.
// Development builds (no release version) are never compared.
func (a *App) updateStatus(s updateState) (available, locked bool) {
	cur, ok := parseSemver(version)
	if !a.cfg.UpdateCheck || !ok {
		return false, false
	}
	if l, ok := parseSemver(s.Latest); ok && cur.less(l) {
		available = true
	}
	if m, ok := parseSemver(s.MinVersion); ok && cur.less(m) {
		locked = true
	}
	return available, locked
}

func (a *App) updateLocked() bool {
	_, locked := a.updateStatus(a.updateState())
	return locked
}

func cleanNotes(s string) string {
	s = strings.TrimSpace(commentRE.ReplaceAllString(s, ""))
	if r := []rune(s); len(r) > 4000 {
		s = string(r[:4000]) + " …"
	}
	return s
}

// ---------------------------------------------------------------- check

func (a *App) checkUpdates(ctx context.Context) error {
	s := a.updateState()
	fail := func(err error) error {
		s.Error = err.Error()
		a.saveUpdateState(s)
		return err
	}
	u := strings.TrimRight(a.cfg.UpdateAPI, "/") + "/repos/" + a.cfg.UpdateRepo + "/releases?per_page=30"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Wicket/"+version)
	if s.ETag != "" && s.Latest != "" {
		req.Header.Set("If-None-Match", s.ETag)
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	s.CheckedAt = now()
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotModified:
		s.Error = ""
		a.saveUpdateState(s)
		a.notifyUpdate()
		return nil
	case http.StatusOK:
	default:
		return fail(fmt.Errorf("GitHub answered %s", resp.Status))
	}
	var list []ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&list); err != nil {
		return fail(fmt.Errorf("unreadable answer from GitHub"))
	}

	var latest, min semver
	var latestRel *ghRelease
	for i := range list {
		r := &list[i]
		v, ok := parseSemver(r.TagName)
		if !ok || r.Draft || r.Prerelease {
			continue
		}
		if latestRel == nil || latest.less(v) {
			latest, latestRel = v, r
		}
		if requiredRE.MatchString(r.Body) && min.less(v) {
			min = v
		}
		if m := minVerRE.FindStringSubmatch(r.Body); m != nil {
			if mv, ok := parseSemver(m[1]); ok && min.less(mv) {
				min = mv
			}
		}
	}
	// a minimum above every published version could never be met
	if latestRel != nil && latest.less(min) {
		min = latest
	}

	s.Error, s.ETag = "", resp.Header.Get("ETag")
	s.Latest, s.URL, s.PublishedAt, s.Notes, s.MinVersion = "", "", "", "", ""
	if latestRel != nil {
		s.Latest, s.URL, s.PublishedAt = latest.String(), latestRel.HTMLURL, latestRel.PublishedAt
		s.Notes = cleanNotes(latestRel.Body)
	}
	if min != (semver{}) {
		s.MinVersion = min.String()
	}
	a.saveUpdateState(s)
	a.notifyUpdate()
	return nil
}

// notifyUpdate tells the notification channels once per new version.
func (a *App) notifyUpdate() {
	s := a.updateState()
	available, locked := a.updateStatus(s)
	if !available || s.Notified == s.Latest {
		return
	}
	s.Notified = s.Latest
	a.saveUpdateState(s)
	lang := a.notifyLang()
	title := tr(lang, "update.notifyTitle", s.Latest)
	if locked {
		title = tr(lang, "update.notifyTitleRequired", s.MinVersion)
	}
	link := ""
	if h := a.settings().AdminHost; h != "" {
		link = "https://" + h + "/#/settings/updates"
	}
	n := Notification{Event: "update_available", Title: title, Message: tr(lang, "update.notifyBody", version), Link: link}
	smtp := a.smtpConfig()
	for _, c := range a.notifyChannels() {
		if c.Enabled && c.wants("update_available") {
			go func(c NotifyChannel) {
				if err := deliver(c, smtp, n); err != nil {
					log.Printf("notify %s: %v", c.Name, err)
				}
			}(c)
		}
	}
}

func (a *App) updateLoop() {
	if !a.cfg.UpdateCheck {
		return
	}
	time.Sleep(30 * time.Second)
	for {
		if !a.updateState().Disabled {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := a.checkUpdates(ctx); err != nil {
				log.Printf("update check: %v", err)
			}
			cancel()
		}
		time.Sleep(updateInterval)
	}
}

// ---------------------------------------------------------------- install

// Backup writes a consistent copy of the database.
func (s *Store) Backup(path string) error {
	_, err := s.db.Exec("VACUUM INTO '" + strings.ReplaceAll(path, "'", "''") + "'")
	return err
}

// backupBeforeUpdate copies the database into the data directory and keeps the newest three copies.
func (a *App) backupBeforeUpdate(target string) (string, error) {
	name := fmt.Sprintf("wicket-before-%s-%s.db", target, time.Now().UTC().Format("20060102-150405"))
	if err := a.store.Backup(filepath.Join(a.cfg.DataDir, name)); err != nil {
		return "", err
	}
	old, _ := filepath.Glob(filepath.Join(a.cfg.DataDir, "wicket-before-*.db"))
	type file struct {
		path string
		mod  time.Time
	}
	var files []file
	for _, p := range old {
		if fi, err := os.Stat(p); err == nil {
			files = append(files, file{p, fi.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	for i := 3; i < len(files); i++ {
		_ = os.Remove(files[i].path)
	}
	return name, nil
}

func (a *App) updateMethod() string {
	if a.cfg.UpdateURL != "" && a.cfg.UpdateToken != "" {
		return "watchtower"
	}
	return "manual"
}

// triggerUpdate asks Watchtower to install the new image. If it works, Watchtower replaces this
// container and the request never returns; otherwise the result is kept for the admin interface.
func (a *App) triggerUpdate() {
	time.Sleep(300 * time.Millisecond) // let the answer reach the browser first
	msg := ""
	req, err := http.NewRequest(http.MethodPost, a.cfg.UpdateURL, nil)
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+a.cfg.UpdateToken)
		resp, err := (&http.Client{Timeout: 15 * time.Minute}).Do(req)
		if err != nil {
			msg = err.Error()
		} else {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				msg = "updater answered " + resp.Status
			}
		}
	} else {
		msg = err.Error()
	}
	upd.mu.Lock()
	upd.run.FinishedAt, upd.run.Error = now(), msg
	upd.mu.Unlock()
	if msg != "" {
		log.Printf("update: %s", msg)
	}
}

// ---------------------------------------------------------------- admin API

func (a *App) updateView(r *http.Request) map[string]any {
	s := a.updateState()
	available, locked := a.updateStatus(s)
	upd.mu.Lock()
	run := upd.run
	upd.mu.Unlock()
	_, release := parseSemver(version)
	method := a.updateMethod()
	owner := strings.ToLower(strings.SplitN(a.cfg.UpdateRepo, "/", 2)[0])
	return map[string]any{
		"current": version, "dev": !release, "latest": s.Latest, "available": available, "locked": locked,
		"minVersion": s.MinVersion, "notes": s.Notes, "url": s.URL, "publishedAt": s.PublishedAt,
		"checkedAt": s.CheckedAt, "error": s.Error, "check": a.cfg.UpdateCheck && !s.Disabled, "envDisabled": !a.cfg.UpdateCheck,
		"method": method, "canApply": method == "watchtower" && reqUser(r).Role == "admin", "run": run,
		"image": "ghcr.io/" + owner + "/wicket",
	}
}

func (a *App) apiUpdateGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.updateView(r))
}

func (a *App) apiUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if !a.cfg.UpdateCheck {
		a.errKey(w, r, http.StatusBadRequest, "err.updateEnvOff")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	_ = a.checkUpdates(ctx) // a failed check is reported in the view
	writeJSON(w, http.StatusOK, a.updateView(r))
}

func (a *App) apiUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Check bool `json:"check"`
	}
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	if !a.cfg.UpdateCheck {
		a.errKey(w, r, http.StatusBadRequest, "err.updateEnvOff")
		return
	}
	s := a.updateState()
	if _, locked := a.updateStatus(s); locked && !in.Check {
		a.errKey(w, r, http.StatusConflict, "err.updateLockedConfig")
		return
	}
	s.Disabled = !in.Check
	a.saveUpdateState(s)
	a.event(r, "settings_changed", reqUser(r).Username, "", "updates")
	writeJSON(w, http.StatusOK, a.updateView(r))
}

func (a *App) apiUpdateApply(w http.ResponseWriter, r *http.Request) {
	if a.updateMethod() != "watchtower" {
		a.errKey(w, r, http.StatusBadRequest, "err.updateNoMethod")
		return
	}
	s := a.updateState()
	if available, _ := a.updateStatus(s); !available {
		a.errKey(w, r, http.StatusBadRequest, "err.updateNone")
		return
	}
	upd.mu.Lock()
	running := upd.run.StartedAt != 0 && upd.run.FinishedAt == 0 && now()-upd.run.StartedAt < 900
	run := upd.run
	upd.mu.Unlock()
	if running {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "run": run})
		return
	}
	name, err := a.backupBeforeUpdate(s.Latest)
	if err != nil {
		a.errKey(w, r, http.StatusInternalServerError, "err.updateBackup", err.Error())
		return
	}
	run = updateRun{StartedAt: now(), Target: s.Latest, Backup: name}
	upd.mu.Lock()
	upd.run = run
	upd.mu.Unlock()
	a.event(r, "update_started", reqUser(r).Username, "", version+" → "+s.Latest)
	go a.triggerUpdate()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "run": run})
}
