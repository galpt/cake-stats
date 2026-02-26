package main

import (
	_ "embed"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Version is overridden at build time via -ldflags "-X main.Version=vX.Y.Z"
var Version = "dev"

// indexHTML is the embedded single-file web UI.
//
//go:embed index.html
var indexHTML string

func main() {
	// ── CLI flags ────────────────────────────────────────────────────────
	var (
		host     = flag.String("host", "0.0.0.0", "Bind address for the web interface")
		port     = flag.Int("port", 11112, "TCP port for the web interface")
		interval = flag.Duration("interval", time.Second, "How often to poll 'tc -s qdisc'")
		showVer  = flag.Bool("version", false, "Print version and exit")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "cake-stats %s\n\n", Version)
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\nOptions:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVer {
		fmt.Printf("cake-stats %s\n", Version)
		os.Exit(0)
	}

	addr := fmt.Sprintf("%s:%d", *host, *port)

	log.SetFlags(log.LstdFlags)
	log.SetPrefix("[cake-stats] ")
	log.Printf("cake-stats %s  –  poll interval %s", Version, *interval)

	// ── Context wired to OS signals ──────────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := NewServer(addr, *interval)
	if err := srv.Run(ctx); err != nil {
		log.Fatalf("fatal: %v", err)
	}

	log.Println("Shutdown complete.")
}
