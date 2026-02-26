package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/vNodesV/vProx/internal/vlog/config"
	"github.com/vNodesV/vProx/internal/vlog/db"
	"github.com/vNodesV/vProx/internal/vlog/ingest"
	"github.com/vNodesV/vProx/internal/vlog/intel"
	"github.com/vNodesV/vProx/internal/vlog/web"
)

const version = "0.1.0"

// ---------------------------------------------------------------------------
// Help / usage
// ---------------------------------------------------------------------------

func printHelp() {
	fmt.Print(`vLog v` + version + ` — vProx log archive analyzer

Usage:
  vlog <command> [flags]

Commands:
  start    Start vLog server (foreground)
  ingest   Run one-shot archive ingest and exit
  status   Show database stats and exit

Flags:
  --home PATH      Override $VPROX_HOME (default: ~/.vProx)
  --config PATH    Override config file path
  --port PORT      Override listen port (default: 8889)
  --quiet          Suppress stdout output
  --version        Print version and exit
  --help, -h       Show this help

Examples:
  vlog start
  vlog start --port 9000
  vlog ingest --home /opt/vprox
  vlog status
`)
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	os.Exit(run())
}

func run() int {
	// -- Flags ---------------------------------------------------------------
	var (
		homeFlag    string
		configFlag  string
		portFlag    int
		quietFlag   bool
		versionFlag bool
		helpFlag    bool
	)

	fs := flag.NewFlagSet("vlog", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&homeFlag, "home", "", "Override $VPROX_HOME")
	fs.StringVar(&configFlag, "config", "", "Override config file path")
	fs.IntVar(&portFlag, "port", 0, "Override listen port")
	fs.BoolVar(&quietFlag, "quiet", false, "Suppress stdout output")
	fs.BoolVar(&versionFlag, "version", false, "Print version and exit")
	fs.BoolVar(&helpFlag, "help", false, "Show this help")
	fs.BoolVar(&helpFlag, "h", false, "Show this help")

	// Suppress default usage — we provide our own.
	fs.Usage = func() {}

	if err := fs.Parse(os.Args[1:]); err != nil {
		printHelp()
		return 1
	}

	if helpFlag {
		printHelp()
		return 0
	}
	if versionFlag {
		fmt.Println("vLog " + version)
		return 0
	}

	// -- Command dispatch ----------------------------------------------------
	cmd := fs.Arg(0)

	switch cmd {
	case "":
		printHelp()
		return 0
	case "start":
		return cmdStart(homeFlag, configFlag, portFlag, quietFlag)
	case "ingest":
		return cmdIngest(homeFlag, configFlag, quietFlag)
	case "status":
		return cmdStatus(homeFlag, configFlag)
	default:
		fmt.Fprintf(os.Stderr, "vlog: unknown command %q\n\n", cmd)
		printHelp()
		return 1
	}
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

// resolveHome returns the vProx home directory.
// Priority: --home flag > $VPROX_HOME > ~/.vProx.
func resolveHome(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return config.FindHome()
}

// loadConfig resolves home, loads config, and applies flag overrides.
func loadConfig(homeFlag, configFlag string, portFlag int) (cfg config.Config, home string, err error) {
	home = resolveHome(homeFlag)

	cfgPath := configFlag
	if cfgPath == "" {
		cfgPath = filepath.Join(home, "config", "vlog.toml")
	}

	cfg, err = config.Load(cfgPath)
	if err != nil {
		return cfg, home, err
	}

	// Apply flag overrides.
	if portFlag > 0 {
		cfg.VLog.Port = portFlag
	}

	if err := cfg.Validate(); err != nil {
		return cfg, home, err
	}
	return cfg, home, nil
}

// ---------------------------------------------------------------------------
// vlog start
// ---------------------------------------------------------------------------

func cmdStart(homeFlag, configFlag string, portFlag int, quiet bool) int {
	cfg, home, err := loadConfig(homeFlag, configFlag, portFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vlog: config error: %v\n", err)
		return 1
	}

	if !quiet {
		intelAbuseIPDB := boolYesNo(cfg.VLog.Intel.Keys.AbuseIPDB != "")
		intelVirusTotal := boolYesNo(cfg.VLog.Intel.Keys.VirusTotal != "")
		intelShodan := boolYesNo(cfg.VLog.Intel.Keys.Shodan != "")
		fmt.Fprintf(os.Stdout, "vLog %s\n", version)
		fmt.Fprintf(os.Stdout, "  home:     %s\n", home)
		fmt.Fprintf(os.Stdout, "  db:       %s\n", cfg.VLog.DBPath)
		fmt.Fprintf(os.Stdout, "  archives: %s\n", cfg.VLog.ArchivesDir)
		fmt.Fprintf(os.Stdout, "  port:     :%d\n", cfg.VLog.Port)
		fmt.Fprintf(os.Stdout, "  intel:    abuseipdb=%s virustotal=%s shodan=%s\n",
			intelAbuseIPDB, intelVirusTotal, intelShodan)
		fmt.Fprintln(os.Stdout)
	}

	// 5. Open database.
	database, err := db.Open(cfg.VLog.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vlog: database error: %v\n", err)
		return 1
	}
	defer database.Close()

	// 6. Create ingester.
	ingester := ingest.New(database, cfg.VLog.ArchivesDir)

	// 7. Run initial ingest (sync, before watcher).
	processed, err := ingester.IngestAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "vlog: initial ingest error: %v\n", err)
		return 1
	}
	if !quiet && processed > 0 {
		fmt.Fprintf(os.Stdout, "Ingested %d archive(s) at startup\n", processed)
	}

	// 8. Create and start enricher.
	enricher := intel.NewEnricher(cfg.VLog.Intel, database)
	enricher.Start()

	// 9. Create and start watcher (if configured).
	var watcher *ingest.Watcher
	if cfg.VLog.WatchIntervalSec > 0 {
		watcher = ingest.NewWatcher(ingester, cfg.VLog.WatchIntervalSec)
		watcher.Start()
	}

	// 10. Create and start web server.
	server, err := web.New(database, enricher, ingester, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vlog: web server error: %v\n", err)
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
		fmt.Fprintf(os.Stdout, "Listening on :%d\n", cfg.VLog.Port)
	}

	// 11. Wait for OS signal or server error.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	select {
	case sig := <-sigCh:
		if !quiet {
			fmt.Fprintf(os.Stdout, "\nReceived %s, shutting down...\n", sig)
		}
	case err := <-serverErr:
		if err != nil {
			fmt.Fprintf(os.Stderr, "vlog: server error: %v\n", err)
		}
	}

	// 12. Graceful shutdown.
	if watcher != nil {
		watcher.Stop()
	}
	enricher.Stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "vlog: shutdown error: %v\n", err)
	}

	database.Close()

	if !quiet {
		fmt.Fprintln(os.Stdout, "vLog stopped.")
	}
	return 0
}

// ---------------------------------------------------------------------------
// vlog ingest
// ---------------------------------------------------------------------------

func cmdIngest(homeFlag, configFlag string, quiet bool) int {
	cfg, _, err := loadConfig(homeFlag, configFlag, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vlog: config error: %v\n", err)
		return 1
	}

	database, err := db.Open(cfg.VLog.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vlog: database error: %v\n", err)
		return 1
	}
	defer database.Close()

	ingester := ingest.New(database, cfg.VLog.ArchivesDir)

	processed, err := ingester.IngestAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "vlog: ingest error: %v\n", err)
		return 1
	}

	if !quiet {
		fmt.Fprintf(os.Stdout, "Ingested %d archive(s)\n", processed)
	}
	return 0
}

// ---------------------------------------------------------------------------
// vlog status
// ---------------------------------------------------------------------------

func cmdStatus(homeFlag, configFlag string) int {
	cfg, _, err := loadConfig(homeFlag, configFlag, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vlog: config error: %v\n", err)
		return 1
	}

	database, err := db.Open(cfg.VLog.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vlog: database error: %v\n", err)
		return 1
	}
	defer database.Close()

	stats, err := database.Stats()
	if err != nil {
		fmt.Fprintf(os.Stderr, "vlog: stats error: %v\n", err)
		return 1
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(os.Stdout, "vLog status — %s\n\n", ts)
	fmt.Fprintf(os.Stdout, "  Archives ingested:  %s\n", fmtInt(stats["total_archives"]))
	fmt.Fprintf(os.Stdout, "  IP accounts:        %s\n", fmtInt(stats["total_ips"]))
	fmt.Fprintf(os.Stdout, "  Request events:     %s\n", fmtInt(stats["total_requests"]))
	fmt.Fprintf(os.Stdout, "  Rate-limit events:  %s\n", fmtInt(stats["total_ratelimit_events"]))
	fmt.Fprintf(os.Stdout, "  Flagged IPs:        %s\n", fmtInt(stats["flagged_ips"]))

	return 0
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// boolYesNo returns "yes" or "no" for a boolean value.
func boolYesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// fmtInt formats an int64 as a string.
func fmtInt(n int64) string {
	return strconv.FormatInt(n, 10)
}
