// aid — aios chain daemon.
//
// PHASE 0.5 SIMPLIFICATION: a custom Go HTTP service implements the marketplace
// state machine (services, requests, escrow, finalization). Real signatures,
// real persistence (bbolt), real event streaming (SSE). The architectural
// concepts — messages, events, escrow, finalization — match the eventual
// Cosmos SDK design 1:1. Phase 1 swaps this service for a real CometBFT chain
// with `x/aiservice`; the proto messages in /proto/aiservice/v1/ are the
// future-state design and remain authoritative.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/aios/aios/internal/api"
	"github.com/aios/aios/internal/state"
)

func main() {
	homeDir := flag.String("home", envOr("AID_HOME", defaultHome()), "Data directory")
	listenAddr := flag.String("listen", envOr("LISTEN", ":26657"), "HTTP listen address")
	logLevel := flag.String("log-level", envOr("LOG_LEVEL", "info"), "Log level")
	devKeyring := flag.Bool("dev-keyring", envBool("DEV_KEYRING", true), "Create alice/bob dev keys on first boot")
	flag.Parse()

	logger, err := newLogger(*logLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger:", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	if err := os.MkdirAll(*homeDir, 0o755); err != nil {
		logger.Fatal("create home dir", zap.Error(err))
	}

	st, err := state.Open(filepath.Join(*homeDir, "chain.db"), logger)
	if err != nil {
		logger.Fatal("open state", zap.Error(err))
	}
	defer st.Close()

	if *devKeyring {
		if err := st.EnsureDevKeyring(filepath.Join(*homeDir, "keys.json")); err != nil {
			logger.Fatal("ensure dev keyring", zap.Error(err))
		}
	}

	srv := api.NewServer(st, logger)
	httpSrv := &http.Server{
		Addr:        *listenAddr,
		Handler:     srv.Handler(),
		ReadTimeout: 30 * time.Second,
		// SSE responses use unbounded write timeouts via per-request hijack;
		// global WriteTimeout interferes, so we keep it generous.
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go handleSignals(cancel, logger)

	go func() {
		<-ctx.Done()
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	// Block producer: ticks once per second, sweeps expired requests,
	// finalizes anything past its challenge window. Real CometBFT runs every
	// ~6s; we go faster for demo snappiness.
	go state.RunBlockProducer(ctx, st, 1*time.Second, logger)

	logger.Info("aid starting",
		zap.String("home", *homeDir),
		zap.String("listen", *listenAddr),
		zap.String("chain_id", st.ChainID()))

	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal("http server", zap.Error(err))
	}
	logger.Info("aid stopped")
}

func defaultHome() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".aid")
	}
	return "./.aid"
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "TRUE"
}

func newLogger(level string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	if err := cfg.Level.UnmarshalText([]byte(level)); err != nil {
		return nil, err
	}
	return cfg.Build()
}

func handleSignals(cancel context.CancelFunc, logger *zap.Logger) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	logger.Info("signal received", zap.String("signal", sig.String()))
	cancel()
}
