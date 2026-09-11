package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Wicket owns the files in CaddyDir. The Caddyfile has to contain `import <CaddyDir>/*.caddy`:
//   00-wicket.caddy        named snippet "(wicket)" with the forward_auth block
//   01-wicket-hosts.caddy  login + admin host → Wicket itself
//   site-<domain>.caddy    one block per managed site: import wicket + reverse_proxy
// Unmanaged sites keep their own Caddy block and just add `import wicket`.

const managedHeader = "# managed by wicket - do not edit, changes are overwritten\n"

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

func (a *App) caddyFiles() (map[string]string, error) {
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
	sites, err := a.store.ListSites()
	if err != nil {
		return nil, err
	}
	for _, st := range sites {
		if st.Managed && st.Target != "" {
			files[siteFileName(st.Domain)] = managedHeader + fmt.Sprintf("%s {\n\timport wicket\n\treverse_proxy %s\n}\n", st.Domain, st.Target)
		}
	}
	return files, nil
}

// syncCaddy writes the managed files and reloads Caddy; on failure the previous files are restored.
func (a *App) syncCaddy() error {
	a.caddyMu.Lock()
	defer a.caddyMu.Unlock()
	if !a.caddyDirOK() {
		return nil
	}
	want, err := a.caddyFiles()
	if err != nil {
		return err
	}
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
	changed := false
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
		_ = a.reloadCaddy()
		return err
	}
	return nil
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

type CaddyStatus struct {
	Enabled    bool   `json:"enabled"`
	Writable   bool   `json:"writable"`
	Reachable  bool   `json:"reachable"`
	Imported   bool   `json:"imported"`
	Dir        string `json:"dir"`
	Admin      string `json:"admin"`
	ImportLine string `json:"importLine"`
}

func (a *App) caddyStatus() CaddyStatus {
	st := CaddyStatus{Dir: a.cfg.CaddyDir, Admin: a.cfg.CaddyAdmin, ImportLine: a.importLine(), Enabled: a.caddyDirOK()}
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
