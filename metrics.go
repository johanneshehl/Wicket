package main

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// Prometheus text exposition without a client library: a handful of counters and gauges.

type counterSet struct {
	mu sync.Mutex
	v  map[string]uint64
}

var metricCounters = &counterSet{v: map[string]uint64{}}

var metricHelp = map[string]string{
	"wicket_verify_total": "forward_auth decisions by result.",
	"wicket_logins_total": "Password sign-in attempts by result.",
	"wicket_mfa_total":    "Second-factor attempts by result.",
}

func init() {
	// pre-create the series so they exist (with 0) before the first event
	for _, r := range []string{"allow", "bypass", "redirect", "reauth", "denied", "blocked", "unknown"} {
		metricCounters.v[metricKey("wicket_verify_total", r)] = 0
	}
	for _, r := range []string{"success", "failure"} {
		metricCounters.v[metricKey("wicket_logins_total", r)] = 0
		metricCounters.v[metricKey("wicket_mfa_total", r)] = 0
	}
}

func metricKey(name, result string) string { return fmt.Sprintf(`%s{result="%s"}`, name, result) }

func countMetric(name, result string) {
	metricCounters.mu.Lock()
	metricCounters.v[metricKey(name, result)]++
	metricCounters.mu.Unlock()
}

// handleMetrics serves /metrics. With WICKET_METRICS_TOKEN set, a bearer token is required;
// without it, the endpoint is only answered when not reached through the public login/admin hosts.
func (a *App) handleMetrics(w http.ResponseWriter, r *http.Request) {
	s := a.settings()
	if tok := a.cfg.MetricsToken; tok != "" {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+tok)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	} else if h := hostOnly(r.Host); h == s.LoginHost || h == s.AdminHost {
		http.NotFound(w, r)
		return
	}

	var b strings.Builder
	gauge := func(name, help string, v int) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", name, help, name, name, v)
	}
	fmt.Fprintf(&b, "# HELP wicket_build_info Build information.\n# TYPE wicket_build_info gauge\nwicket_build_info{version=%q} 1\n", version)
	users, _ := a.store.CountUsers()
	sessions, _ := a.store.CountActiveSessions()
	sites, _ := a.store.ListSites()
	gauge("wicket_users", "Number of users.", users)
	gauge("wicket_sites", "Number of protected sites.", len(sites))
	gauge("wicket_active_sessions", "Number of active sessions.", sessions)
	gauge("wicket_locked_ips", "IP addresses currently locked by brute-force protection.", a.limiter.LockedCount())
	available, _ := a.updateStatus(a.updateState())
	gauge("wicket_update_available", "1 if a newer Wicket release is available.", map[bool]int{true: 1}[available])

	metricCounters.mu.Lock()
	keys := make([]string, 0, len(metricCounters.v))
	for k := range metricCounters.v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	last := ""
	for _, k := range keys {
		name := k[:strings.Index(k, "{")]
		if name != last {
			fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s counter\n", name, metricHelp[name], name)
			last = name
		}
		fmt.Fprintf(&b, "%s %d\n", k, metricCounters.v[k])
	}
	metricCounters.mu.Unlock()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(b.String()))
}
