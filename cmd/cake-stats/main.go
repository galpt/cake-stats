package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/galpt/cake-stats/pkg/log"
	"github.com/galpt/cake-stats/pkg/server"
	"github.com/rs/zerolog"
)

// Version is overridden at build-time.
var Version = "dev"

func main() {
	host := flag.String("host", "0.0.0.0", "bind address for web interface")
	port := flag.Int("port", 11112, "TCP port for web interface")
	interval := flag.Duration("interval", 100*time.Millisecond, "poll interval for tc")
	histCap := flag.Int("history", 300, "samples to retain per interface")
	showVer := flag.Bool("version", false, "print version and exit")

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
	log.Logger = log.Logger.Level(zerolog.InfoLevel).With().Str("version", Version).Logger()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := server.New(addr, *interval, *histCap)
	if err := srv.Run(ctx, addr); err != nil {
		log.Logger.Fatal().Err(err).Msg("fatal")
	}
	log.Logger.Info().Msg("shutdown complete")
}
