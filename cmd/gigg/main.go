// Command gigg is the gigg gig board server.
//
//	gigg serve         run the server (configuration from GIGG_* environment variables)
//	gigg healthcheck   exit 0 if the local server answers /healthz (for container health checks)
//	gigg admin ...     first setup and recovery (see gigg admin help)
//	gigg version       print the version
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // the image is scratch: recurrence schedules need the zone database built in

	"github.com/0x2alx/gigg/internal/config"
	"github.com/0x2alx/gigg/internal/server"
	"github.com/0x2alx/gigg/internal/store"
	"github.com/0x2alx/gigg/internal/version"
	"github.com/0x2alx/gigg/web"
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "serve":
		os.Exit(serve())
	case "healthcheck":
		os.Exit(healthcheck())
	case "admin":
		os.Exit(admin(os.Args[2:]))
	case "version", "--version", "-v":
		fmt.Println(version.Version)
	default:
		fmt.Fprintln(os.Stderr, "usage: gigg serve | healthcheck | admin ... | version")
		os.Exit(2)
	}
}

func serve() int {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load()
	if err != nil {
		log.Error("configuration", "err", err)
		return 2
	}
	if cfg.PublicURL == nil {
		log.Warn("GIGG_PUBLIC_URL is not set: only localhost host names are accepted")
	}
	store.DefaultTZ = cfg.DefaultTZ
	store.StaleRunAfter = time.Duration(cfg.StaleRunHours) * time.Hour
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	st, err := store.Open(ctx, cfg.DataDir, log)
	if err != nil {
		log.Error("database", "err", err)
		return 1
	}
	defer st.Close()
	log.Info("starting", "version", version.Version, "data", cfg.DataDir)
	if err := server.New(cfg, st, log, web.Dist()).Run(ctx); err != nil {
		log.Error("server", "err", err)
		return 1
	}
	log.Info("stopped")
	return 0
}

// healthcheck asks this container's own server for /healthz on loopback. It prefers a plain-HTTP
// extra listener; otherwise it uses the main listener (skipping certificate checks: it is talking
// to itself on 127.0.0.1, and the certificate names the public host).
func healthcheck() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	l := cfg.Listeners[0]
	for _, x := range cfg.Listeners[1:] {
		l = x
		break
	}
	_, port, _ := net.SplitHostPort(l.Addr)
	scheme := "http"
	tr := &http.Transport{}
	if l.TLS() {
		scheme = "https"
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // loopback self-check
	}
	c := &http.Client{Timeout: 4 * time.Second, Transport: tr}
	resp, err := c.Get(scheme + "://127.0.0.1:" + port + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthz:", resp.Status)
		return 1
	}
	return 0
}
