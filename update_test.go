package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSemver(t *testing.T) {
	a, _ := parseSemver("v1.9.0")
	b, _ := parseSemver("1.10.0")
	if !a.less(b) || b.less(a) || a.less(a) {
		t.Fatal("version order")
	}
	for _, bad := range []string{"dev", "1.2", "1.2.3-rc1", "v1.x.0"} {
		if _, ok := parseSemver(bad); ok {
			t.Fatalf("%q should not parse", bad)
		}
	}
	if envOn("off") || envOn("FALSE") || !envOn("") || !envOn("on") {
		t.Fatal("envOn")
	}
}

func TestUpdateCheck(t *testing.T) {
	releases := `[
		{"tag_name":"v2.0.0","draft":true},
		{"tag_name":"v1.6.0-rc1","prerelease":true},
		{"tag_name":"v1.5.0","body":"Fixes <!-- wicket:required -->","html_url":"https://example.com/1.5.0"},
		{"tag_name":"v1.4.0","body":"<!-- wicket:min-version 1.3.5 -->"}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/releases" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(releases))
	}))
	defer srv.Close()
	st, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := version
	defer func() { version = old }()
	upd.loaded, upd.state = false, updateState{}
	a := &App{cfg: Config{UpdateCheck: true, UpdateAPI: srv.URL, UpdateRepo: "o/r"}, store: st}

	version = "1.4.2"
	if err := a.checkUpdates(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := a.updateState()
	if s.Latest != "1.5.0" || s.MinVersion != "1.5.0" || s.Notes != "Fixes" || s.URL != "https://example.com/1.5.0" {
		t.Fatalf("state: %+v", s)
	}
	if available, locked := a.updateStatus(s); !available || !locked {
		t.Fatal("1.4.2 must see a required update")
	}
	version = "1.5.0"
	if available, locked := a.updateStatus(s); available || locked {
		t.Fatal("1.5.0 is current")
	}
	version = "dev"
	if _, locked := a.updateStatus(s); locked {
		t.Fatal("development builds are never locked")
	}
	version = "1.4.2"
	a.cfg.UpdateCheck = false
	if _, locked := a.updateStatus(s); locked {
		t.Fatal("WICKET_UPDATE_CHECK=off must lift the lock")
	}

	// a minimum above every published release is capped at the latest release
	releases = `[{"tag_name":"v1.4.0","body":"<!-- wicket:min-version 9.9.9 -->"}]`
	a.cfg.UpdateCheck = true
	s.ETag = ""
	a.saveUpdateState(s)
	if err := a.checkUpdates(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := a.updateState(); s.MinVersion != "1.4.0" || s.Latest != "1.4.0" {
		t.Fatalf("capped minimum: %+v", s)
	}
}
