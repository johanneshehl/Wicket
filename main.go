package main

import (
	"context"
	"embed"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

//go:embed web
var webFS embed.FS

// Set at build time: -ldflags "-X main.version=1.2.3"
var version = "dev"

type Config struct {
	Listen       string
	DataDir      string
	CookieDomain string
	LoginHost    string
	AdminHost    string
	AuthAddr     string // address Caddy uses to reach Wicket
	CaddyAdmin   string
	CaddyDir     string
	Caddyfile    string
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func loadConfig() Config {
	return Config{
		Listen:       env("WICKET_LISTEN", ":9091"),
		DataDir:      env("WICKET_DATA", "/data"),
		CookieDomain: strings.ToLower(env("WICKET_COOKIE_DOMAIN", "")),
		LoginHost:    strings.ToLower(env("WICKET_LOGIN_HOST", "")),
		AdminHost:    strings.ToLower(env("WICKET_ADMIN_HOST", "")),
		AuthAddr:     env("WICKET_AUTH_ADDR", "127.0.0.1:9091"),
		CaddyAdmin:   env("WICKET_CADDY_ADMIN", "http://127.0.0.1:2019"),
		CaddyDir:     env("WICKET_CADDY_DIR", "/etc/caddy/wicket"),
		Caddyfile:    env("WICKET_CADDYFILE", "/etc/caddy/Caddyfile"),
	}
}

func main() {
	cfg := loadConfig()
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck(cfg))
	}

	store, err := OpenStore(cfg.DataDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	app, err := NewApp(cfg, store)
	if err != nil {
		log.Fatalf("init: %v", err)
	}

	srv := &http.Server{Addr: cfg.Listen, Handler: app.Routes(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("wicket %s listening on %s", version, cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	go app.janitor()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// healthcheck is the container HEALTHCHECK (the image has no shell or curl).
func healthcheck(cfg Config) int {
	addr := cfg.Listen
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
