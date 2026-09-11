package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Wicket owns the files in CaddyDir:
//   00-wicket.caddy        named snippet "(wicket)" with the forward_auth block
//   01-wicket-hosts.caddy  login + admin host → Wicket itself
//   site-<domain>.caddy    one block per managed site: import wicket + reverse_proxy
//
// If the Caddyfile is writable, Wicket also keeps it in shape:
//   - the import of CaddyDir stays at the top (snippets must be defined before use)
//   - site blocks of unmanaged sites get "import wicket" and their basic_auth is commented out
//   - both changes are undone when the site is removed from Wicket

const (
	managedHeader  = "# managed by wicket - do not edit, changes are overwritten\n"
	importMarker   = "import wicket # added by wicket"
	disabledPrefix = "# disabled by wicket: "
	importComment  = "# Wicket - managed snippets, must stay above all site blocks"
)

func managedFile(name string) bool {
	return strings.HasSuffix(name, ".caddy") &&
		(strings.HasPrefix(name, "00-wicket") || strings.HasPrefix(name, "01-wicket") || strings.HasPrefix(name, "site-"))
}

func siteFileName(domain string) string {
	return "site-" + strings.ReplaceAll(domain, "*", "_") + ".caddy"
}

func (a *App) importLine() string {
	return "import " + filepath.ToSlash(filepath.Join(a.cfg.CaddyDir, "*.caddy"))
}

func (a *App) caddyDirOK() bool {
	fi, err := os.Stat(a.cfg.CaddyDir)
	return err == nil && fi.IsDir()
}

func (a *App) caddyfileWritable() bool {
	f, err := os.OpenFile(a.cfg.Caddyfile, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

func (a *App) caddyFiles(sites []*Site) map[string]string {
	s := a.settings()
	files := map[string]string{
		"00-wicket.caddy": managedHeader + fmt.Sprintf("(wicket) {\n\tforward_auth %s {\n\t\turi /verify\n\t\tcopy_headers Remote-User Remote-Role\n\t}\n}\n", a.cfg.AuthAddr),
	}
	var hosts []string
	for _, h := range []string{s.LoginHost, s.AdminHost} {
		if h != "" && (len(hosts) == 0 || hosts[0] != h) {
			hosts = append(hosts, h)
		}
	}
	if len(hosts) > 0 {
		files["01-wicket-hosts.caddy"] = managedHeader + fmt.Sprintf("%s {\n\treverse_proxy %s\n}\n", strings.Join(hosts, ", "), a.cfg.AuthAddr)
	}
	for _, st := range sites {
		if st.Managed && st.Target != "" {
			files[siteFileName(st.Domain)] = managedHeader + fmt.Sprintf("%s {\n\timport wicket\n\treverse_proxy %s\n}\n", st.Domain, st.Target)
		}
	}
	return files
}

// syncCaddy writes the managed files (and the Caddyfile, if writable) and reloads Caddy.
// On failure everything is restored to the previous state.
func (a *App) syncCaddy() error {
	a.caddyMu.Lock()
	defer a.caddyMu.Unlock()
	if !a.caddyDirOK() {
		return nil
	}
	sites, err := a.store.ListSites()
	if err != nil {
		return err
	}
	want := a.caddyFiles(sites)
	dir := a.cfg.CaddyDir
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	old := map[string][]byte{}
	for _, e := range entries {
		if !e.IsDir() && managedFile(e.Name()) {
			if b, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil {
				old[e.Name()] = b
			}
		}
	}

	oldCF, cfErr := os.ReadFile(a.cfg.Caddyfile)
	newCF := oldCF
	if cfErr == nil && a.caddyfileWritable() {
		protect := map[string]bool{}
		for _, st := range sites {
			if !st.Managed {
				protect[st.Domain] = true
			}
		}
		newCF = []byte(rewriteCaddyfile(string(oldCF), protect, a.importLine()))
	}
	cfChanged := !bytes.Equal(oldCF, newCF)

	changed := cfChanged
	for name, content := range want {
		if prev, ok := old[name]; !ok || !bytes.Equal(prev, []byte(content)) {
			changed = true
		}
	}
	for name := range old {
		if _, ok := want[name]; !ok {
			changed = true
		}
	}
	if !changed {
		return nil
	}

	if cfChanged {
		a.backupCaddyfile(oldCF)
		// in place (not rename): keeps the inode, owner and permissions of the operator's file
		if err := os.WriteFile(a.cfg.Caddyfile, newCF, 0o644); err != nil {
			return fmt.Errorf("Caddyfile nicht schreibbar: %w", err)
		}
	}
	for name, content := range want {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	for name := range old {
		if _, ok := want[name]; !ok {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
	if err := a.reloadCaddy(); err != nil {
		for name := range want {
			if _, existed := old[name]; !existed {
				_ = os.Remove(filepath.Join(dir, name))
			}
		}
		for name, b := range old {
			_ = os.WriteFile(filepath.Join(dir, name), b, 0o644)
		}
		if cfChanged {
			_ = os.WriteFile(a.cfg.Caddyfile, oldCF, 0o644)
		}
		_ = a.reloadCaddy()
		return err
	}
	return nil
}

// backupCaddyfile keeps the operator's original Caddyfile once, before Wicket touches it the first time.
func (a *App) backupCaddyfile(content []byte) {
	path := filepath.Join(a.cfg.DataDir, "Caddyfile.before-wicket")
	if _, err := os.Stat(path); err == nil {
		return
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		log.Printf("caddy: backup: %v", err)
	}
}

func (a *App) reloadCaddy() error {
	cf, err := os.ReadFile(a.cfg.Caddyfile)
	if err != nil {
		return fmt.Errorf("Caddyfile nicht lesbar: %w", err)
	}
	if !bytes.Contains(cf, []byte(a.importLine())) {
		return fmt.Errorf("das Caddyfile enthält noch nicht die Zeile %q", a.importLine())
	}
	req, err := http.NewRequest(http.MethodPost, a.cfg.CaddyAdmin+"/load", bytes.NewReader(cf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/caddyfile")
	client := http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Caddy nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4000))
		var e struct{ Error string }
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return fmt.Errorf("%s", e.Error)
		}
		return fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	return nil
}

// caddyfileHasSite reports whether the Caddyfile already defines a site block for domain.
func (a *App) caddyfileHasSite(domain string) bool {
	b, err := os.ReadFile(a.cfg.Caddyfile)
	if err != nil {
		return false
	}
	return blockFor(parseBlocks(strings.Split(string(b), "\n")), domain) >= 0
}

// ---------------------------------------------------------------- Caddyfile editing

type cfBlock struct {
	start, end int
	hosts      []string // empty for the global options block and snippets
	global     bool
}

// stripComment returns the trimmed line without a trailing "# comment".
func stripComment(line string) string {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "#") {
		return ""
	}
	for _, sep := range []string{" #", "\t#"} {
		if i := strings.Index(t, sep); i >= 0 {
			t = strings.TrimSpace(t[:i])
		}
	}
	return t
}

func normalizeHost(a string) string {
	a = strings.ToLower(strings.TrimSpace(a))
	a = strings.TrimPrefix(strings.TrimPrefix(a, "https://"), "http://")
	if i := strings.Index(a, "/"); i >= 0 {
		a = a[:i]
	}
	if h, _, err := net.SplitHostPort(a); err == nil {
		a = h
	}
	return a
}

// parseBlocks finds the top-level blocks of a Caddyfile (good enough for hand-written files:
// braces inside quoted strings are not special-cased).
func parseBlocks(lines []string) []cfBlock {
	var out []cfBlock
	depth := 0
	for i, l := range lines {
		t := stripComment(l)
		d := strings.Count(t, "{") - strings.Count(t, "}")
		prev := depth
		if prev == 0 && d > 0 && strings.HasSuffix(t, "{") {
			addr := strings.TrimSpace(strings.TrimSuffix(t, "{"))
			b := cfBlock{start: i, end: -1, global: addr == ""}
			if addr != "" && !strings.HasPrefix(addr, "(") {
				for _, h := range strings.FieldsFunc(addr, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
					b.hosts = append(b.hosts, normalizeHost(h))
				}
			}
			out = append(out, b)
		}
		depth += d
		if prev > 0 && depth <= 0 {
			depth = 0
			if n := len(out); n > 0 && out[n-1].end == -1 {
				out[n-1].end = i
			}
		}
	}
	return out
}

func blockFor(blocks []cfBlock, domain string) int {
	for i, b := range blocks {
		if b.end < 0 {
			continue
		}
		for _, h := range b.hosts {
			if h == domain {
				return i
			}
		}
	}
	return -1
}

func leadingWS(l string) string { return l[:len(l)-len(strings.TrimLeft(l, " \t"))] }

func splice(lines []string, from, to int, repl []string) []string {
	out := make([]string, 0, len(lines)-(to-from)+len(repl))
	out = append(out, lines[:from]...)
	out = append(out, repl...)
	return append(out, lines[to:]...)
}

// rewriteCaddyfile puts the Wicket import at the top and protects the blocks of the given domains.
// It is idempotent and reverses its own changes for domains that are no longer protected.
func rewriteCaddyfile(content string, protect map[string]bool, importLine string) string {
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		if t := strings.TrimSpace(l); t == importLine || t == importComment {
			continue
		}
		lines = append(lines, l)
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}

	// 1. the import goes first (after a global options block, which Caddy requires to be first)
	at := 0
	if blocks := parseBlocks(lines); len(blocks) > 0 && blocks[0].global && blocks[0].end >= 0 {
		at = blocks[0].end + 1
	}
	head := []string{importComment, importLine, ""}
	if at > 0 {
		head = append([]string{""}, head...)
	}
	lines = splice(lines, at, at, head)

	// 2. per site block, bottom-up so indexes stay valid
	blocks := parseBlocks(lines)
	for bi := len(blocks) - 1; bi >= 0; bi-- {
		b := blocks[bi]
		if len(b.hosts) == 0 || b.end < 0 {
			continue
		}
		wanted := false
		for _, h := range b.hosts {
			wanted = wanted || protect[h]
		}
		body := lines[b.start+1 : b.end]
		var nb []string
		if wanted {
			has := false
			for _, l := range body {
				if t := stripComment(l); t == "import wicket" {
					has = true
				}
			}
			if !has {
				nb = append(nb, "\t"+importMarker)
			}
			inAuth, depth := false, 0
			for _, l := range body {
				t := stripComment(l)
				if !inAuth && (strings.HasPrefix(t, "basic_auth") || strings.HasPrefix(t, "basicauth")) {
					inAuth, depth = true, 0
				}
				if inAuth {
					depth += strings.Count(t, "{") - strings.Count(t, "}")
					nb = append(nb, leadingWS(l)+disabledPrefix+l)
					if depth <= 0 {
						inAuth = false
					}
					continue
				}
				nb = append(nb, l)
			}
		} else {
			for _, l := range body {
				if strings.TrimSpace(l) == importMarker {
					continue
				}
				if i := strings.Index(l, disabledPrefix); i >= 0 && strings.TrimSpace(l[:i]) == "" {
					nb = append(nb, l[i+len(disabledPrefix):])
					continue
				}
				nb = append(nb, l)
			}
		}
		lines = splice(lines, b.start+1, b.end, nb)
	}
	return strings.Join(lines, "\n") + "\n"
}

// ---------------------------------------------------------------- status

type CaddyStatus struct {
	Enabled           bool   `json:"enabled"`
	Writable          bool   `json:"writable"`
	CaddyfileWritable bool   `json:"caddyfileWritable"`
	Reachable         bool   `json:"reachable"`
	Imported          bool   `json:"imported"`
	Dir               string `json:"dir"`
	Admin             string `json:"admin"`
	ImportLine        string `json:"importLine"`
}

func (a *App) caddyStatus() CaddyStatus {
	st := CaddyStatus{Dir: a.cfg.CaddyDir, Admin: a.cfg.CaddyAdmin, ImportLine: a.importLine(), Enabled: a.caddyDirOK(),
		CaddyfileWritable: a.caddyfileWritable()}
	if st.Enabled {
		if f, err := os.CreateTemp(a.cfg.CaddyDir, ".probe-*"); err == nil {
			st.Writable = true
			name := f.Name()
			f.Close()
			_ = os.Remove(name)
		}
	}
	if cf, err := os.ReadFile(a.cfg.Caddyfile); err == nil {
		st.Imported = bytes.Contains(cf, []byte(st.ImportLine))
	}
	client := http.Client{Timeout: 2 * time.Second}
	if resp, err := client.Get(a.cfg.CaddyAdmin + "/config/"); err == nil {
		resp.Body.Close()
		st.Reachable = resp.StatusCode == http.StatusOK
	}
	return st
}
