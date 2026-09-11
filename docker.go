package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// Optional Docker integration: list running containers and their published ports, so the
// admin UI can offer them as targets. Read-only; point WICKET_DOCKER_HOST at a socket proxy
// (e.g. tcp://127.0.0.1:2375) if you do not want to give Wicket the Docker socket itself.

type ContainerTarget struct {
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	State   string   `json:"state"`
	Network string   `json:"network"`
	Targets []string `json:"targets"`
}

func (a *App) dockerClient() (*http.Client, string, bool) {
	h := a.cfg.DockerHost
	switch {
	case strings.HasPrefix(h, "unix://"):
		path := strings.TrimPrefix(h, "unix://")
		if _, err := os.Stat(path); err != nil {
			return nil, "", false
		}
		tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		}}
		return &http.Client{Transport: tr, Timeout: 5 * time.Second}, "http://docker", true
	case strings.HasPrefix(h, "tcp://"):
		return &http.Client{Timeout: 5 * time.Second}, "http://" + strings.TrimPrefix(h, "tcp://"), true
	case strings.HasPrefix(h, "http://"), strings.HasPrefix(h, "https://"):
		return &http.Client{Timeout: 5 * time.Second}, strings.TrimSuffix(h, "/"), true
	}
	return nil, "", false
}

func (a *App) listContainers() ([]ContainerTarget, error) {
	client, base, ok := a.dockerClient()
	if !ok {
		return nil, fmt.Errorf("docker not configured")
	}
	resp, err := client.Get(base + "/containers/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker API: %s", resp.Status)
	}
	var raw []struct {
		Names      []string
		Image      string
		State      string
		HostConfig struct{ NetworkMode string }
		Ports      []struct {
			IP          string
			PrivatePort int
			PublicPort  int
			Type        string
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := []ContainerTarget{}
	for _, c := range raw {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		ct := ContainerTarget{Name: name, Image: c.Image, State: c.State, Network: c.HostConfig.NetworkMode, Targets: []string{}}
		seen := map[string]bool{}
		for _, p := range c.Ports {
			if p.PublicPort == 0 || p.Type != "tcp" {
				continue
			}
			ip := p.IP
			if ip == "" || ip == "0.0.0.0" || ip == "::" {
				ip = "127.0.0.1"
			}
			if strings.Contains(ip, ":") {
				continue // IPv6 duplicate of an IPv4 binding
			}
			t := fmt.Sprintf("%s:%d", ip, p.PublicPort)
			if !seen[t] {
				seen[t] = true
				ct.Targets = append(ct.Targets, t)
			}
		}
		sort.Strings(ct.Targets)
		out = append(out, ct)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (a *App) apiContainers(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := a.dockerClient(); !ok {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "containers": []ContainerTarget{}})
		return
	}
	list, err := a.listContainers()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "error": err.Error(), "containers": []ContainerTarget{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "containers": list})
}
