package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gtorrent/internal/config"
	"gtorrent/internal/rtorrent"
	"gtorrent/internal/rtorrent/transport"
	httptransport "gtorrent/internal/rtorrent/transport/http"
	"gtorrent/internal/rtorrent/transport/scgi"
	"gtorrent/internal/rtorrent/xmlrpc"
	"gtorrent/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	transport, err := buildTransport(cfg)
	if err != nil {
		log.Fatalf("transport setup failed: %v", err)
	}

	rpc := xmlrpc.NewClient(transport)
	svc := rtorrent.NewClient(rpc)
	srv, err := server.New(svc)
	if err != nil {
		log.Fatalf("server setup failed: %v", err)
	}

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("gTorrent listening on %s", cfg.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
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
