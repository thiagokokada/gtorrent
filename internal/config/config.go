package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
)

const (
	ModeUnix = "unix"
	ModeHTTP = "http"
)

// Config controls server listen and rTorrent connectivity settings.
type Config struct {
	ListenAddr  string
	OpenBrowser bool
	Verbose     bool
	Mode        string
	UnixSocket  string
	HTTPURL     string
	HTTPUser    string
	HTTPPass    string
}

func Load() (Config, error) {
	cfg := Config{}

	flag.StringVar(&cfg.ListenAddr, "listen", envOrDefault("GTORRENT_LISTEN", ":8080"), "HTTP listen address")
	flag.BoolVar(&cfg.OpenBrowser, "open-browser", envBoolOrDefault("GTORRENT_OPEN_BROWSER", true), "Open UI in default browser on startup")
	disableOpenBrowser := flag.Bool("no-open-browser", false, "Disable opening UI in browser on startup")
	flag.BoolVar(&cfg.Verbose, "verbose", envBoolOrDefault("GTORRENT_VERBOSE", false), "Enable debug logging")
	flag.StringVar(&cfg.Mode, "rtorrent-mode", envOrDefault("GTORRENT_MODE", ModeUnix), "rTorrent connection mode: unix or http")
	flag.StringVar(&cfg.UnixSocket, "rtorrent-socket", envOrDefault("GTORRENT_UNIX_SOCKET", ""), "Path to rTorrent unix socket")
	flag.StringVar(&cfg.HTTPURL, "rtorrent-http-url", envOrDefault("GTORRENT_HTTP_URL", ""), "HTTP XML-RPC endpoint URL")
	flag.StringVar(&cfg.HTTPUser, "rtorrent-http-user", envOrDefault("GTORRENT_HTTP_USER", ""), "HTTP basic auth user")
	flag.StringVar(&cfg.HTTPPass, "rtorrent-http-pass", envOrDefault("GTORRENT_HTTP_PASS", ""), "HTTP basic auth password")

	flag.Parse()
	if *disableOpenBrowser {
		cfg.OpenBrowser = false
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	switch c.Mode {
	case ModeUnix:
		if c.UnixSocket == "" {
			return errors.New("rtorrent unix mode requires --rtorrent-socket or GTORRENT_UNIX_SOCKET")
		}
	case ModeHTTP:
		if c.HTTPURL == "" {
			return errors.New("rtorrent http mode requires --rtorrent-http-url or GTORRENT_HTTP_URL")
		}
	default:
		return fmt.Errorf("invalid rtorrent mode %q: expected unix or http", c.Mode)
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func envBoolOrDefault(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return parsed
}
