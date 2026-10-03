package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/benjamincottle/gymrouter/internal/api"
	"github.com/benjamincottle/gymrouter/internal/config"
	"github.com/benjamincottle/gymrouter/internal/engine"
	"github.com/benjamincottle/gymrouter/internal/gyms"
	"github.com/benjamincottle/gymrouter/internal/tfnsw"
)

const defaultConfig = "/etc/gymrouter/config.toml"

func configFlag(fs *flag.FlagSet) *string {
	def := os.Getenv("GYMROUTER_CONFIG")
	if def == "" {
		def = defaultConfig
	}
	return fs.String("config", def, "config file (env GYMROUTER_CONFIG)")
}

// serve runs the HTTP server. Secrets come from the environment only.
func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := configFlag(fs)
	_ = fs.Parse(args)
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	key := os.Getenv("TFNSW_API_KEY")
	if key == "" {
		return errors.New("TFNSW_API_KEY is not set")
	}
	auth, err := api.NewAuth(os.Getenv("GYMROUTER_TOKEN"))
	if err != nil {
		return fmt.Errorf("GYMROUTER_TOKEN: %w", err)
	}
	// Don't leave secrets in the environment of anything we might start later.
	_ = os.Unsetenv("GYMROUTER_TOKEN")

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	eng := engine.New(cfg, sydney, tfnsw.NewClient(key), log, gyms.AllLines())
	log.Info("starting", "known_gyms", len(gyms.Known()), "preloaded_lines", len(gyms.AllLines()))
	if err := eng.Start(ctx); err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           api.New(eng, auth, log, webFS()).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("listening", "addr", cfg.Server.Listen)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// setupLink prints the link that registers a device. The token goes in the URL fragment, which
// browsers never send to the server.
func setupLink(args []string) error {
	fs := flag.NewFlagSet("setup-link", flag.ExitOnError)
	cfgPath := configFlag(fs)
	_ = fs.Parse(args)
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if cfg.Server.PublicURL == "" {
		return errors.New("server.public_url is not set in the config")
	}
	tok := os.Getenv("GYMROUTER_TOKEN")
	if _, err := api.NewAuth(tok); err != nil {
		return fmt.Errorf("GYMROUTER_TOKEN: %w", err)
	}
	fmt.Fprintln(os.Stderr, "Open this link on the device to set it up. Anyone with it can use the app; don't share it.")
	fmt.Printf("%s/#setup=%s\n", strings.TrimRight(cfg.Server.PublicURL, "/"), tok)
	return nil
}

func newToken([]string) error {
	t, err := api.NewToken()
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Store this as GYMROUTER_TOKEN (e.g. in Ansible Vault). Rotating it signs out every device.")
	fmt.Println(t)
	return nil
}

func checkConfig(args []string) error {
	fs := flag.NewFlagSet("check-config", flag.ExitOnError)
	cfgPath := configFlag(fs)
	_ = fs.Parse(args)
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	fmt.Printf("listen %s, data %s, public url %q\n", cfg.Server.Listen, cfg.Server.DataDir, cfg.Server.PublicURL)
	if cfg.Data.AutoDownload {
		fmt.Printf("fetches itself: street network from %s; basemap with %q\n", cfg.Data.WalkSource, cfg.Data.PMTilesBin)
	} else {
		fmt.Println("automatic downloads of the street network and basemap are off")
	}
	fmt.Println("known gyms:")
	for _, g := range gyms.Known() {
		fmt.Printf("  %-12s %-28s %s\n", g.ID, g.Name, strings.Join(g.LineSet.Strings(), ", "))
	}
	fmt.Printf("preloaded realtime feeds: ")
	for _, f := range tfnsw.FeedsFor(gyms.AllLines()) {
		fmt.Printf("%s ", f.Name)
	}
	fmt.Println()
	return nil
}

// healthcheck probes the local server's /healthz (the container image has no shell or curl).
// It exits non-zero unless the server answers 200.
func healthcheck(args []string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ExitOnError)
	cfgPath := configFlag(fs)
	_ = fs.Parse(args)
	addr := ":8080"
	if cfg, err := config.Load(*cfgPath); err == nil {
		addr = cfg.Server.Listen
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("listen address %q: %w", addr, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz: HTTP %d", resp.StatusCode)
	}
	return nil
}
