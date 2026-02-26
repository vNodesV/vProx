package ingest

import (
	"fmt"
	"os"
	"time"
)

// ---------------------------------------------------------------------------
// Poll-based archive watcher
// ---------------------------------------------------------------------------

// Watcher periodically calls IngestAll to pick up new archives.
// It uses a simple time.Ticker — no external dependencies.
type Watcher struct {
	ingester *Ingester
	interval time.Duration
	done     chan struct{}
}

// NewWatcher returns a Watcher that polls every intervalSec seconds.
func NewWatcher(ing *Ingester, intervalSec int) *Watcher {
	if intervalSec < 1 {
		intervalSec = 60
	}
	return &Watcher{
		ingester: ing,
		interval: time.Duration(intervalSec) * time.Second,
		done:     make(chan struct{}),
	}
}

// Start begins polling archivesDir on a background goroutine.
// It is non-blocking.
func (w *Watcher) Start() {
	go w.loop()
}

// Stop signals the watcher goroutine to exit and blocks until it does.
func (w *Watcher) Stop() {
	close(w.done)
}

func (w *Watcher) loop() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			n, err := w.ingester.IngestAll()
			if err != nil {
				fmt.Fprintf(os.Stderr, "vlog: watcher ingest error: %v\n", err)
				continue
			}
			if n > 0 {
				fmt.Fprintf(os.Stderr, "vlog: watcher ingested %d new archive(s)\n", n)
			}
		}
	}
}
