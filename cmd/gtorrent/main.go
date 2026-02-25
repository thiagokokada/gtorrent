package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/lmittmann/tint"

	"github.com/thiagokokada/gtorrent/internal/config"
	"github.com/thiagokokada/gtorrent/internal/rtorrent"
	"github.com/thiagokokada/gtorrent/internal/rtorrent/transport"
	httptransport "github.com/thiagokokada/gtorrent/internal/rtorrent/transport/http"
	"github.com/thiagokokada/gtorrent/internal/rtorrent/transport/scgi"
	"github.com/thiagokokada/gtorrent/internal/rtorrent/xmlrpc"
	"github.com/thiagokokada/gtorrent/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid config", "error", err)
		os.Exit(1)
	}
	configureLogger(cfg.Verbose)

	transport, err := buildTransport(cfg)
	if err != nil {
		slog.Error("transport setup failed", "error", err)
		os.Exit(1)
	}

	rpc := xmlrpc.NewClient(transport)
	svc := rtorrent.NewClient(rpc)
	svc.SetConnectionTarget(rtorrentTarget(cfg))
	srv, err := server.New(svc)
	if err != nil {
		slog.Error("server setup failed", "error", err)
		os.Exit(1)
	}

	serverCtx, cancelServerCtx := context.WithCancel(context.Background())
	defer cancelServerCtx()

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		// WriteTimeout must be unset for long-lived SSE responses.
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return serverCtx
		},
	}

	go func() {
		slog.Info("gtorrent listening", "address", cfg.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	if cfg.OpenBrowser {
		url := browserURL(cfg.ListenAddr)
		if err := openBrowser(url); err != nil {
			slog.Warn("failed to open browser", "url", url, "error", err)
		} else {
			slog.Info("opened browser", "url", url)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	cancelServerCtx()
	srv.ShutdownStreams()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
		if closeErr := httpServer.Close(); closeErr != nil && closeErr != http.ErrServerClosed {
			slog.Error("force close failed", "error", closeErr)
		}
	}
}

func configureLogger(verbose bool) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	noColor := false
	if v, ok := os.LookupEnv("NO_COLOR"); ok && v != "" {
		noColor = true
	}
	logger := slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		Level:   level,
		NoColor: noColor,
	}))
	slog.SetDefault(logger)
}

func buildTransport(cfg config.Config) (transport.Caller, error) {
	switch cfg.Mode {
	case config.ModeUnix:
		return &scgi.Client{SocketPath: cfg.UnixSocket}, nil
	case config.ModeHTTP:
		return &httptransport.Client{URL: cfg.HTTPURL, User: cfg.HTTPUser, Pass: cfg.HTTPPass}, nil
	default:
		return nil, fmt.Errorf("invalid mode %q", cfg.Mode)
	}
}

func rtorrentTarget(cfg config.Config) string {
	switch cfg.Mode {
	case config.ModeUnix:
		return cfg.UnixSocket
	case config.ModeHTTP:
		return cfg.HTTPURL
	default:
		return ""
	}
}

func browserURL(listenAddr string) string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		if len(listenAddr) > 0 && listenAddr[0] == ':' {
			return "http://127.0.0.1" + listenAddr
		}
		return "http://" + listenAddr
	}

	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s", net.JoinHostPort(host, port))
}

func openBrowser(url string) error {
	name, args, err := browserCommand(runtime.GOOS, url)
	if err != nil {
		return err
	}
	return exec.Command(name, args...).Start()
}

func browserCommand(goos, url string) (string, []string, error) {
	switch goos {
	case "darwin":
		return "open", []string{url}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}, nil
	case "linux":
		return "xdg-open", []string{url}, nil
	default:
		return "", nil, fmt.Errorf("browser opening unsupported on %s", goos)
	}
}
