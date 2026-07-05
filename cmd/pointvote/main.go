// Command pointvote serves the point.vote planning-poker API and UI as one
// binary. It binds loopback by default; the Cloudflare Tunnel is the sole
// public ingress.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/jolyonbrown/point.vote/internal/api"
	"github.com/jolyonbrown/point.vote/internal/mcp"
	"github.com/jolyonbrown/point.vote/internal/room"
)

// appVersion is whatever the Go toolchain stamped from the git tag —
// v1.2.0 on a tagged build, a pseudo-version between tags, "dev" when
// there is nothing to go on.
func appVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	createLimit := flag.Int("create-limit", 0, "per-IP room creations per hour (0 = server default of 30); for self-hosted or batch use")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	svc := room.NewService(room.NewMemStore(), logger)
	go svc.RunGC(ctx, room.GCInterval)

	version := appVersion()
	srv := &http.Server{
		Addr: *addr,
		Handler: (&api.Server{
			Log: logger, Svc: svc, MCP: mcp.Handler(svc, logger),
			Version: version, CreatePerHour: *createLimit,
		}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	logger.Info("listening", "addr", *addr, "version", version)

	select {
	case err := <-errCh:
		logger.Error("server failed", "err", err)
		os.Exit(1)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "err", err)
	}
	logger.Info("stopped")
}
