package rtorrent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thiagokokada/gtorrent/internal/domain"
	"github.com/thiagokokada/gtorrent/internal/rtorrent/xmlrpc"
)

// Service describes torrent operations used by the HTTP handlers.
type Service interface {
	List(ctx context.Context) ([]domain.Torrent, error)
	AddMagnet(ctx context.Context, magnet string) error
	AddTorrent(ctx context.Context, data []byte, filename string) error
	Remove(ctx context.Context, hash string, deleteData bool) error
	Start(ctx context.Context, hash string) error
	Stop(ctx context.Context, hash string) error
	Recheck(ctx context.Context, hash string) error
	GetSpeedLimits(ctx context.Context) (domain.SpeedLimits, error)
	SetSpeedLimits(ctx context.Context, limits domain.SpeedLimits) error
}

// Client is an rTorrent service backed by XML-RPC methods.
type Client struct {
	rpc      xmlrpc.Caller
	statusMu sync.RWMutex
	target   string
	status   domain.BackendStatus
}

func NewClient(rpc xmlrpc.Caller) *Client {
	return &Client{rpc: rpc}
}

func (c *Client) SetConnectionTarget(target string) {
	c.statusMu.Lock()
	c.target = strings.TrimSpace(target)
	c.statusMu.Unlock()
}

func (c *Client) BackendStatus() domain.BackendStatus {
	c.statusMu.RLock()
	defer c.statusMu.RUnlock()
	return c.status
}

func (c *Client) GetSpeedLimits(ctx context.Context) (domain.SpeedLimits, error) {
	downloadKiB, err := c.getSpeedLimitWithFallback(ctx, []rateReadMethod{
		{name: "throttle.global_down.max_rate", bytesToKiB: true},
		{name: "throttle.global_down.max_rate.get_kb"},
		{name: "get_download_rate"},
	})
	if err != nil {
		return domain.SpeedLimits{}, err
	}
	uploadKiB, err := c.getSpeedLimitWithFallback(ctx, []rateReadMethod{
		{name: "throttle.global_up.max_rate", bytesToKiB: true},
		{name: "throttle.global_up.max_rate.get_kb"},
		{name: "get_upload_rate"},
	})
	if err != nil {
		return domain.SpeedLimits{}, err
	}
	return domain.SpeedLimits{
		DownloadKiB: downloadKiB,
		UploadKiB:   uploadKiB,
	}, nil
}

func (c *Client) SetSpeedLimits(ctx context.Context, limits domain.SpeedLimits) error {
	if limits.DownloadKiB < 0 || limits.UploadKiB < 0 {
		return errors.New("speed limits must be non-negative")
	}

	if err := c.setSpeedLimitWithFallback(ctx, []rpcMethodCall{
		{name: "throttle.global_down.max_rate.set_kb", args: []any{"", limits.DownloadKiB}},
		{name: "throttle.global_down.max_rate.set", args: []any{"", limits.DownloadKiB}},
		{name: "set_download_rate", args: []any{limits.DownloadKiB}},
	}); err != nil {
		return err
	}
	if err := c.setSpeedLimitWithFallback(ctx, []rpcMethodCall{
		{name: "throttle.global_up.max_rate.set_kb", args: []any{"", limits.UploadKiB}},
		{name: "throttle.global_up.max_rate.set", args: []any{"", limits.UploadKiB}},
		{name: "set_upload_rate", args: []any{limits.UploadKiB}},
	}); err != nil {
		return err
	}

	slog.Info("speed limits updated", "download_kib_s", limits.DownloadKiB, "upload_kib_s", limits.UploadKiB)
	return nil
}

type rateReadMethod struct {
	name       string
	bytesToKiB bool
}

func (c *Client) getSpeedLimitWithFallback(ctx context.Context, methods []rateReadMethod) (int64, error) {
	var lastErr error
	for _, method := range methods {
		result, err := c.rpc.Call(ctx, method.name)
		if err == nil {
			value := asInt64(result)
			if method.bytesToKiB {
				value = bytesToKiB(value)
			}
			if value < 0 {
				return 0, nil
			}
			return value, nil
		}
		lastErr = err
	}
	return 0, fmt.Errorf("get speed limit failed: %w", lastErr)
}

func bytesToKiB(value int64) int64 {
	if value <= 0 {
		return 0
	}
	return (value + 512) / 1024
}

type rpcMethodCall struct {
	name string
	args []any
}

func (c *Client) setSpeedLimitWithFallback(ctx context.Context, methods []rpcMethodCall) error {
	var lastErr error
	for _, method := range methods {
		_, err := c.rpc.Call(ctx, method.name, method.args...)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("set speed limit failed: %w", lastErr)
}

func (c *Client) List(ctx context.Context) ([]domain.Torrent, error) {
	start := time.Now()
	result, err := c.rpc.Call(ctx, "d.multicall2",
		"",
		"main",
		"d.hash=",
		"d.name=",
		"d.size_bytes=",
		"d.completed_bytes=",
		"d.down.rate=",
		"d.up.rate=",
		"d.is_active=",
		"d.complete=",
		"d.custom=tm_loaded",
		"d.timestamp.started=",
		"d.ratio=",
		"d.peers_connected=",
		"d.peers_complete=",
	)
	if err != nil {
		c.setBackendStatus("error", fmt.Sprintf("Error talking to rTorrent (%s): %v", c.connectionTarget(), err))
		slog.Error("rtorrent list update failed", "duration", time.Since(start).Round(time.Millisecond), "error", err)
		return nil, err
	}

	outer, ok := result.([]any)
	if !ok {
		err := fmt.Errorf("unexpected multicall result type %T", result)
		c.setBackendStatus("error", fmt.Sprintf("Error talking to rTorrent (%s): %v", c.connectionTarget(), err))
		slog.Error("rtorrent list update failed", "duration", time.Since(start).Round(time.Millisecond), "error", err)
		return nil, err
	}

	out := make([]domain.Torrent, 0, len(outer))
	for _, row := range outer {
		cols, ok := row.([]any)
		if !ok || len(cols) < 8 {
			continue
		}

		size := asInt64(getCol(cols, 2))
		done := asInt64(getCol(cols, 3))
		progress := 0.0
		if size > 0 {
			progress = float64(done) / float64(size)
		}
		if progress < 0 {
			progress = 0
		}
		if progress > 1 {
			progress = 1
		}
		progress = math.Round(progress*1000) / 1000

		active := asBool(getCol(cols, 6))
		complete := asBool(getCol(cols, 7))
		state := "stopped"
		switch {
		case complete && active:
			state = "seeding"
		case complete:
			state = "complete"
		case active:
			state = "downloading"
		}

		downRate := asInt64(getCol(cols, 4))
		upRate := asInt64(getCol(cols, 5))
		addedAt := pickAddedAt(asInt64(getCol(cols, 8)), asInt64(getCol(cols, 9)))
		ratio := asRatio(getCol(cols, 10))
		eta := estimateETASeconds(size, done, downRate)

		out = append(out, domain.Torrent{
			Hash:       asString(getCol(cols, 0)),
			Name:       asString(getCol(cols, 1)),
			SizeBytes:  size,
			DoneBytes:  done,
			Progress:   progress,
			State:      state,
			DownRate:   downRate,
			UpRate:     upRate,
			AddedAt:    addedAt,
			ETASeconds: eta,
			Ratio:      ratio,
			Peers:      asInt64(getCol(cols, 11)),
			Seeds:      asInt64(getCol(cols, 12)),
		})
	}
	c.setBackendStatus("ok", fmt.Sprintf("Connected successfully to rTorrent: %s", c.connectionTarget()))
	slog.Debug("rtorrent list update", "torrents", len(out), "duration", time.Since(start).Round(time.Millisecond))
	return out, nil
}

func (c *Client) setBackendStatus(kind, message string) {
	c.statusMu.Lock()
	c.status = domain.BackendStatus{
		Kind:    kind,
		Message: strings.TrimSpace(message),
	}
	c.statusMu.Unlock()
}

func (c *Client) connectionTarget() string {
	c.statusMu.RLock()
	target := c.target
	c.statusMu.RUnlock()
	if target != "" {
		return target
	}
	return "unknown endpoint"
}

func (c *Client) AddMagnet(ctx context.Context, magnet string) error {
	magnet = strings.TrimSpace(magnet)
	if magnet == "" {
		slog.Warn("add magnet rejected", "reason", "empty magnet")
		return errors.New("magnet is required")
	}
	if !strings.HasPrefix(magnet, "magnet:") {
		slog.Warn("add magnet rejected", "reason", "invalid magnet uri")
		return errors.New("invalid magnet URI")
	}

	slog.Info("adding magnet")
	methods := []string{"load.start", "load.normal"}
	var lastErr error
	for _, method := range methods {
		_, err := c.rpc.Call(ctx, method, "", magnet)
		if err == nil {
			slog.Info("magnet added", "method", method)
			return nil
		}
		slog.Debug("add magnet method failed", "method", method, "error", err)
		lastErr = err
	}
	err := fmt.Errorf("add magnet failed: %w", lastErr)
	slog.Error("add magnet failed", "error", err)
	return err
}

func (c *Client) AddTorrent(ctx context.Context, data []byte, _ string) error {
	if len(data) == 0 {
		slog.Warn("add torrent rejected", "reason", "empty payload")
		return errors.New("torrent payload is empty")
	}

	slog.Info("adding torrent file", "bytes", len(data))
	methods := []string{"load.raw_start", "load.raw", "load.start"}
	var lastErr error
	for _, method := range methods {
		_, err := c.rpc.Call(ctx, method, "", data)
		if err == nil {
			slog.Info("torrent file added", "method", method)
			return nil
		}
		slog.Debug("add torrent method failed", "method", method, "error", err)
		lastErr = err
	}
	err := fmt.Errorf("add torrent failed: %w", lastErr)
	slog.Error("add torrent failed", "error", err)
	return err
}

func (c *Client) Remove(ctx context.Context, hash string, deleteData bool) error {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		slog.Warn("remove torrent rejected", "reason", "empty hash")
		return errors.New("hash is required")
	}

	slog.Info("removing torrent", "hash", hash, "delete_data", deleteData)
	if deleteData {
		_, _ = c.rpc.Call(ctx, "d.stop", hash)
		_, _ = c.rpc.Call(ctx, "d.close", hash)
	}
	_, err := c.rpc.Call(ctx, "d.erase", hash)
	if err != nil {
		wrapped := fmt.Errorf("remove torrent: %w", err)
		slog.Error("remove torrent failed", "hash", hash, "error", wrapped)
		return wrapped
	}
	slog.Info("torrent removed", "hash", hash)
	return nil
}

func (c *Client) Start(ctx context.Context, hash string) error {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		slog.Warn("start torrent rejected", "reason", "empty hash")
		return errors.New("hash is required")
	}

	slog.Info("starting torrent", "hash", hash)
	_, _ = c.rpc.Call(ctx, "d.open", hash)
	_, startErr := c.rpc.Call(ctx, "d.start", hash)
	if startErr == nil {
		slog.Info("torrent started", "hash", hash, "method", "d.start")
		return nil
	}
	if _, resumeErr := c.rpc.Call(ctx, "d.resume", hash); resumeErr == nil {
		slog.Info("torrent started", "hash", hash, "method", "d.resume")
		return nil
	}
	err := fmt.Errorf("start torrent %s failed: %w", hash, startErr)
	slog.Error("start torrent failed", "hash", hash, "error", err)
	return err
}

func (c *Client) Stop(ctx context.Context, hash string) error {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		slog.Warn("stop torrent rejected", "reason", "empty hash")
		return errors.New("hash is required")
	}

	slog.Info("stopping torrent", "hash", hash)
	_, stopErr := c.rpc.Call(ctx, "d.stop", hash)
	if stopErr == nil {
		slog.Info("torrent stopped", "hash", hash, "method", "d.stop")
		return nil
	}
	if _, err := c.rpc.Call(ctx, "d.pause", hash); err == nil {
		slog.Info("torrent stopped", "hash", hash, "method", "d.pause")
		return nil
	}
	err := fmt.Errorf("stop torrent %s failed: %w", hash, stopErr)
	slog.Error("stop torrent failed", "hash", hash, "error", err)
	return err
}

func (c *Client) Recheck(ctx context.Context, hash string) error {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		slog.Warn("recheck torrent rejected", "reason", "empty hash")
		return errors.New("hash is required")
	}

	slog.Info("rechecking torrent", "hash", hash)
	var firstErr error
	for _, method := range []string{"d.check_hash", "d.check_hash="} {
		_, err := c.rpc.Call(ctx, method, hash)
		if err == nil {
			slog.Info("torrent recheck started", "hash", hash, "method", method)
			return nil
		}
		slog.Debug("recheck method failed", "hash", hash, "method", method, "error", err)
		if firstErr == nil {
			firstErr = err
		}
	}

	err := fmt.Errorf("recheck torrent %s failed: %w", hash, firstErr)
	slog.Error("recheck torrent failed", "hash", hash, "error", err)
	return err
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func asInt64(v any) int64 {
	switch t := v.(type) {
	case int:
		return int64(t)
	case int64:
		return t
	case float64:
		return int64(t)
	case bool:
		if t {
			return 1
		}
		return 0
	case string:
		if t == "" {
			return 0
		}
		var n int64
		_, _ = fmt.Sscan(t, &n)
		return n
	default:
		return 0
	}
}

func asRatio(v any) float64 {
	switch t := v.(type) {
	case int:
		return float64(t) / 1000
	case int64:
		return float64(t) / 1000
	case float64:
		if t > 100 {
			return t / 1000
		}
		return t
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0
		}
		if strings.ContainsAny(s, ".eE") {
			f, err := strconv.ParseFloat(s, 64)
			if err == nil {
				return f
			}
			return 0
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0
		}
		return float64(n) / 1000
	default:
		return 0
	}
}

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int:
		return t != 0
	case int64:
		return t != 0
	case float64:
		return t != 0
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "1" || s == "true" || s == "yes"
	default:
		return false
	}
}

func getCol(cols []any, idx int) any {
	if idx < 0 || idx >= len(cols) {
		return nil
	}
	return cols[idx]
}

func pickAddedAt(loaded, started int64) time.Time {
	ts := normalizeUnixSeconds(loaded)
	if ts <= 0 {
		ts = normalizeUnixSeconds(started)
	}
	if ts <= 0 {
		return time.Time{}
	}
	return time.Unix(ts, 0).UTC()
}

func normalizeUnixSeconds(ts int64) int64 {
	if ts <= 0 {
		return 0
	}
	// Some deployments expose milliseconds; normalize to seconds.
	if ts > 1_000_000_000_000 {
		return ts / 1000
	}
	return ts
}

func estimateETASeconds(size, done, downRate int64) int64 {
	remaining := size - done
	if remaining <= 0 {
		return 0
	}
	if downRate <= 0 {
		return -1
	}
	return int64(math.Ceil(float64(remaining) / float64(downRate)))
}
