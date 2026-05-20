// indexerd — chain event indexer for the aios marketplace (Phase 0.5).
//
// Subscribes to the chain's /events SSE, maintains a SQLite mirror of
// services + requests + stats, exposes a tiny REST API for the frontend.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/aios/indexer/internal/api"
	"github.com/aios/indexer/internal/store"
	"github.com/aios/indexer/internal/sub"
)

func main() {
	chainURL := flag.String("chain", envOr("CHAIN_URL", "http://chain:26657"), "Chain HTTP URL")
	dbPath := flag.String("db", envOr("DB_PATH", "/data/indexer.db"), "SQLite database path")
	listenAddr := flag.String("listen", envOr("LISTEN", ":8081"), "HTTP listen address")
	logLevel := flag.String("log-level", envOr("LOG_LEVEL", "info"), "Log level")
	flag.Parse()

	logger, err := newLogger(*logLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger:", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	st, err := store.Open(*dbPath, logger)
	if err != nil {
		logger.Fatal("open store", zap.Error(err))
	}
	defer st.Close()

	if err := st.Migrate(); err != nil {
		logger.Fatal("migrate", zap.Error(err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go handleSignals(cancel, logger)
	go sub.Run(ctx, *chainURL, st, logger)

	srv := api.NewServer(st, logger)
	httpSrv := &http.Server{
		Addr:         *listenAddr,
		Handler:      srv.Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	logger.Info("indexerd listening", zap.String("addr", *listenAddr))
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal("http", zap.Error(err))
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
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
