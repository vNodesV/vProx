package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/vNodesV/vProx/internal/fleet"
	fleetcfg "github.com/vNodesV/vProx/internal/fleet/config"
	vopscfg "github.com/vNodesV/vProx/internal/vops/config"
	"github.com/vNodesV/vProx/internal/vops/db"
	"github.com/vNodesV/vProx/internal/vops/ingest"
	"github.com/vNodesV/vProx/internal/vops/intel"
	"github.com/vNodesV/vProx/internal/vops/web"
)

// runVOpsCmd handles: vprox vops [args...]
// It delegates to the same logic as cmd/vops/main.go.
// For the full CLI experience (one-shot flags, etc.), use the standalone vops binary.
// This subcommand supports: start, stop, restart, status.
func runVOpsCmd(home string, args []string) {
	sub := "start" // default
	if len(args) > 0 {
		sub = args[0]
	}

	switch sub {
	case "start":
		code := vopsStart(home, false)
		os.Exit(code)
	case "stop":
		code := vopsServiceCommand("stop")
		os.Exit(code)
	case "restart":
		code := vopsServiceCommand("restart")
		os.Exit(code)
	case "status":
		code := vopsStatus(home)
		os.Exit(code)
	case "ingest":
		code := vopsAPICall(home, http.MethodPost, "/api/v1/ingest")
		os.Exit(code)
	case "accounts":
		code := vopsAPICall(home, http.MethodGet, "/api/v1/accounts?per_page=20")
		os.Exit(code)
	case "threats":
		code := vopsAPICall(home, http.MethodGet, "/api/v1/threats")
		os.Exit(code)
	case "cache":
		code := vopsAPICall(home, http.MethodGet, "/api/v1/intel/cache/stats")
		os.Exit(code)
	default:
		fmt.Fprintf(os.Stderr, "vprox vops: unknown subcommand %q\n", sub)
		fmt.Fprintln(os.Stderr, "Usage: vprox vops [start|stop|restart|ingest|accounts|threats|cache|status]")
		os.Exit(1)
	}
}

// vopsAPICall makes a one-shot HTTP request to the vOps API and prints the response body.
// It reads the port from the vops config located at vopsConfigPath(home).
func vopsAPICall(home, method, apiPath string) int {
	cfgPath := vopsConfigPath(home)
	cfg, err := vopscfg.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vops: config error: %v\n", err)
		return 1
	}

	url := fmt.Sprintf("http://127.0.0.1:%d%s", cfg.VOps.Port, apiPath)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vops: build request: %v\n", err)
		return 1
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vops: request failed: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vops: read response: %v\n", err)
		return 1
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "vops: server returned %s: %s\n", resp.Status, string(body))
		return 1
	}

	fmt.Print(string(body))
	return 0
}

// vopsStart starts the vOps server (foreground). When quiet is true, startup
// banners are suppressed (used by --with-vops integrated mode).
func vopsStart(home string, quiet bool) int {
	cfgPath := vopsConfigPath(home)
	cfg, err := vopscfg.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vops: config error: %v\n", err)
		return 1
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "vops: config error: %v\n", err)
		return 1
	}

	if !quiet {
		fmt.Fprintf(os.Stdout, "vOps starting on :%d\n", cfg.VOps.Port)
	}

	database, err := db.Open(cfg.VOps.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vops: database error: %v\n", err)
		return 1
	}
	defer database.Close()

	ingester := ingest.New(database, cfg.VOps.ArchivesDir)
	processed, err := ingester.IngestAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "vops: initial ingest error: %v\n", err)
		return 1
	}
	if !quiet && processed > 0 {
		fmt.Fprintf(os.Stdout, "vops: ingested %d archive(s) at startup\n", processed)
	}

	var enricher *intel.Enricher
	enricher = intel.NewEnricher(cfg.VOps.Intel, database)
	enricher.Start()

	var watcher *ingest.Watcher
	if cfg.VOps.WatchIntervalSec > 0 {
		watcher = ingest.NewWatcher(ingester, cfg.VOps.WatchIntervalSec)
		watcher.Start()
	}

	// Fleet module
	var fleetSvc *fleet.Service
	{
		defs := fleetcfg.FleetDefaults{
			User:    cfg.VOps.Push.Defaults.User,
			KeyPath: cfg.VOps.Push.Defaults.KeyPath,
		}
		runtimeCfg, err := fleetcfg.LoadRuntimeConfig(home, defs, cfg.VOps.Push.ChainsDir, cfg.VOps.Push.InfraDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "vops: fleet config warning: %v\n", err)
		} else if len(runtimeCfg.VMs) > 0 {
			svc, err := fleet.NewEmpty(cfg.VOps.Push.DBPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "vops: fleet db error: %v\n", err)
			} else {
				svc.SetConfig(runtimeCfg)
				fleetSvc = svc
				defer svc.Close()
				go svc.StartPolling(context.Background(), time.Duration(cfg.VOps.Push.PollIntervalSec)*time.Second)
			}
		}
	}

	server, err := web.New(database, enricher, ingester, cfg, fleetSvc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vops: web server error: %v\n", err)
		return 1
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()

	if !quiet {
		fmt.Fprintf(os.Stdout, "vOps listening on :%d\n", cfg.VOps.Port)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	select {
	case sig := <-sigCh:
		if !quiet {
			fmt.Fprintf(os.Stdout, "\nvOps received %s, shutting down...\n", sig)
		}
	case err := <-serverErr:
		if err != nil {
			fmt.Fprintf(os.Stderr, "vops: server error: %v\n", err)
		}
	}

	if watcher != nil {
		watcher.Stop()
	}
	if enricher != nil {
		enricher.Stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "vops: shutdown error: %v\n", err)
	}

	if !quiet {
		fmt.Fprintln(os.Stdout, "vOps stopped.")
	}
	return 0
}

// startVOpsInBackground starts the vOps server in a goroutine and returns a
// shutdown function. Used by --with-vops to run vOps alongside the proxy.
// Returns (shutdownFunc, error). Caller must call shutdownFunc on exit.
func startVOpsInBackground(home string) (func(), error) {
	cfgPath := vopsConfigPath(home)
	cfg, err := vopscfg.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("vops config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("vops config: %w", err)
	}

	database, err := db.Open(cfg.VOps.DBPath)
	if err != nil {
		return nil, fmt.Errorf("vops database: %w", err)
	}

	ingester := ingest.New(database, cfg.VOps.ArchivesDir)
	if _, err := ingester.IngestAll(); err != nil {
		database.Close()
		return nil, fmt.Errorf("vops ingest: %w", err)
	}

	var enricher *intel.Enricher
	enricher = intel.NewEnricher(cfg.VOps.Intel, database)
	enricher.Start()

	var watcher *ingest.Watcher
	if cfg.VOps.WatchIntervalSec > 0 {
		watcher = ingest.NewWatcher(ingester, cfg.VOps.WatchIntervalSec)
		watcher.Start()
	}

	// Fleet module
	var fleetSvc *fleet.Service
	{
		defs := fleetcfg.FleetDefaults{
			User:    cfg.VOps.Push.Defaults.User,
			KeyPath: cfg.VOps.Push.Defaults.KeyPath,
		}
		runtimeCfg, err := fleetcfg.LoadRuntimeConfig(home, defs, cfg.VOps.Push.ChainsDir, cfg.VOps.Push.InfraDir)
		if err != nil {
			// Non-fatal: fleet is optional
			fmt.Fprintf(os.Stderr, "vops: fleet config warning: %v\n", err)
		} else if len(runtimeCfg.VMs) > 0 {
			svc, err := fleet.NewEmpty(cfg.VOps.Push.DBPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "vops: fleet db error: %v\n", err)
			} else {
				svc.SetConfig(runtimeCfg)
				fleetSvc = svc
				go svc.StartPolling(context.Background(), time.Duration(cfg.VOps.Push.PollIntervalSec)*time.Second)
			}
		}
	}

	server, err := web.New(database, enricher, ingester, cfg, fleetSvc)
	if err != nil {
		if enricher != nil {
			enricher.Stop()
		}
		if watcher != nil {
			watcher.Stop()
		}
		database.Close()
		return nil, fmt.Errorf("vops web server: %w", err)
	}

	go func() {
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "vops: server error: %v\n", err)
		}
	}()

	fmt.Fprintf(os.Stdout, "  vOps:     :%d (integrated)\n", cfg.VOps.Port)

	// Return cleanup function
	shutdown := func() {
		if watcher != nil {
			watcher.Stop()
		}
		if enricher != nil {
			enricher.Stop()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		if fleetSvc != nil {
			fleetSvc.Close()
		}
		database.Close()
	}

	return shutdown, nil
}

// vopsConfigPath returns the canonical vops.toml path, trying new layout first.
func vopsConfigPath(home string) string {
	newPath := filepath.Join(home, "config", "vops", "vops.toml")
	if _, err := os.Stat(newPath); err == nil {
		return newPath
	}
	// Legacy fallback: config/vops.toml
	oldPath := filepath.Join(home, "config", "vops.toml")
	if _, err := os.Stat(oldPath); err == nil {
		return oldPath
	}
	return newPath // neither exists → Load() returns defaults
}

// vopsStatus prints basic vOps service status.
func vopsStatus(home string) int {
	cfgPath := vopsConfigPath(home)
	cfg, err := vopscfg.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vops: config error: %v\n", err)
		return 1
	}

	database, err := db.Open(cfg.VOps.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vops: database error: %v\n", err)
		return 1
	}
	defer database.Close()

	stats, err := database.Stats()
	if err != nil {
		fmt.Fprintf(os.Stderr, "vops: stats error: %v\n", err)
		return 1
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(os.Stdout, "vOps status — %s\n\n", ts)
	fmt.Fprintf(os.Stdout, "  Archives ingested:  %d\n", stats["total_archives"])
	fmt.Fprintf(os.Stdout, "  IP accounts:        %d\n", stats["total_ips"])
	fmt.Fprintf(os.Stdout, "  Request events:     %d\n", stats["total_requests"])
	fmt.Fprintf(os.Stdout, "  Flagged IPs:        %d\n", stats["flagged_ips"])
	return 0
}

// vopsServiceCommand runs sudo service vOps <action>.
func vopsServiceCommand(action string) int {
	cmd := sudoServiceCommand("vOps", action)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "vops: service %s: %v\n", action, err)
		return 1
	}
	return 0
}

// sudoServiceCommand builds an exec.Cmd for "sudo service <name> <action>".
func sudoServiceCommand(name, action string) *exec.Cmd {
	sudo, err := exec.LookPath("sudo")
	if err == nil {
		return exec.Command(sudo, "service", name, action)
	}
	return exec.Command("service", name, action)
}

// vopsConfigExists returns true when a vops.toml exists at the expected location.
func vopsConfigExists(home string) bool {
	p := vopsConfigPath(home)
	_, err := os.Stat(p)
	return err == nil
}
