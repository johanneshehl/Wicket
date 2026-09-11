package main

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"math/big"
	"strings"
	"testing"
)

func TestIPRules(t *testing.T) {
	got, err := normalizeIPRules([]string{" 10.0.0.0/8 ", "203.0.113.7", "", "2001:db8::/32", "192.168.1.5/32"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.0.0.0/8", "203.0.113.7", "2001:db8::/32", "192.168.1.5"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("normalize: got %v, want %v", got, want)
	}
	for _, bad := range []string{"10.0.0.0/33", "not-an-ip", "300.1.1.1"} {
		if _, err := normalizeIPRules([]string{bad}); err == nil {
			t.Fatalf("%q should be rejected", bad)
		}
	}
	cases := map[string]bool{"10.1.2.3": true, "203.0.113.7": true, "203.0.113.8": false, "2001:db8::1": true, "2001:db9::1": false, "garbage": false}
	for ip, want := range cases {
		if ipMatch(got, ip) != want {
			t.Fatalf("ipMatch(%s) = %v, want %v", ip, !want, want)
		}
	}
}

func TestTrustedProxies(t *testing.T) {
	if err := setTrustedProxies("172.16.0.0/12, 10.0.0.5"); err != nil {
		t.Fatal(err)
	}
	defer setTrustedProxies("")
	if len(trustedProxies) != 2 {
		t.Fatalf("parsed %d proxies", len(trustedProxies))
	}
	if err := setTrustedProxies("nope"); err == nil {
		t.Fatal("invalid proxy accepted")
	}
}

func TestOIDCTokenSignature(t *testing.T) {
	a := &App{cfg: Config{DataDir: t.TempDir()}}
	oidcState.key = nil
	tok, err := a.signJWT(map[string]any{"sub": "1", "aud": "app"})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %s", tok)
	}
	sig, err := b64url.DecodeString(parts[2])
	if err != nil || len(sig) != 64 {
		t.Fatalf("bad signature encoding: %v (%d bytes)", err, len(sig))
	}
	key, _, _ := a.oidcKey()
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(&key.PublicKey, digest[:], r, s) {
		t.Fatal("signature does not verify with the published key")
	}
	// the key is persisted and reused
	oidcState.key = nil
	key2, _, err := a.oidcKey()
	if err != nil || key2.X.Cmp(key.X) != 0 {
		t.Fatal("signing key not reloaded from the data directory")
	}
}

func TestOIDCHelpers(t *testing.T) {
	existing := []*OIDCClient{{ID: "grafana"}, {ID: "grafana-2"}}
	if id := newOIDCClientID("Grafana", existing); id != "grafana-3" {
		t.Fatalf("client id: %s", id)
	}
	if id := newOIDCClientID("My App!", nil); id != "my-app" {
		t.Fatalf("client id: %s", id)
	}
	good := []string{"https://app.example.com/callback", "http://localhost:3000/cb"}
	bad := []string{"http://app.example.com/cb", "https://app.example.com/cb#x", "javascript:alert(1)", "/relative"}
	for _, u := range good {
		if !validRedirectURI(u) {
			t.Fatalf("%s should be allowed", u)
		}
	}
	for _, u := range bad {
		if validRedirectURI(u) {
			t.Fatalf("%s should be rejected", u)
		}
	}
}

func TestMailMessage(t *testing.T) {
	c := SMTPConfig{Host: "mail.example.com", Port: 587, Security: "starttls", From: "wicket@example.com", FromName: "Wicket"}
	msg := string(buildMessage(c, Mail{To: "alex@example.com", Subject: "Grüße", Text: "Hallo\nWelt", HTML: "<p>Hallo</p>"}))
	for _, want := range []string{"To: alex@example.com\r\n", "Subject: =?utf-8?q?Gr=C3=BC=C3=9Fe?=", "multipart/alternative", "text/html", "Message-ID: <"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message misses %q:\n%s", want, msg)
		}
	}
	if err := validSMTP(SMTPConfig{Host: "x", Port: 25, Security: "ssl", From: "a@b"}); err == nil {
		t.Fatal("unknown security accepted")
	}
}

func TestNotifyChannelValidation(t *testing.T) {
	c := NotifyChannel{Name: "ops", Type: "webhook", Target: "https://hooks.example.com/x", Events: []string{"ip_locked", "bogus"}, Enabled: true}
	if err := validNotifyChannel(&c); err != nil {
		t.Fatal(err)
	}
	if len(c.Events) != 1 || !c.wants("ip_locked") || c.wants("new_device") {
		t.Fatalf("events not filtered: %v", c.Events)
	}
	for _, bad := range []NotifyChannel{
		{Type: "email", Target: "no-address"},
		{Type: "ntfy", Target: "ftp://x"},
		{Type: "sms", Target: "123"},
	} {
		if err := validNotifyChannel(&bad); err == nil {
			t.Fatalf("channel %+v should be rejected", bad)
		}
	}
}
