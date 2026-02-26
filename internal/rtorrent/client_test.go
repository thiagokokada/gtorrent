package rtorrent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thiagokokada/gtorrent/internal/domain"
)

type mockRPC struct {
	calls []string
	fn    func(method string, args ...any) (any, error)
}

func (m *mockRPC) Call(_ context.Context, method string, args ...any) (any, error) {
	m.calls = append(m.calls, method)
	if m.fn == nil {
		return nil, nil
	}
	return m.fn(method, args...)
}

func TestListMapsRows(t *testing.T) {
	rpc := &mockRPC{
		fn: func(method string, _ ...any) (any, error) {
			if method != "d.multicall2" {
				t.Fatalf("unexpected method: %s", method)
			}
			return []any{
				[]any{"h1", "Ubuntu ISO", int64(100), int64(75), int64(10), int64(2), int64(1), int64(0), int64(1700000000), int64(0), int64(1500), int64(22), int64(8)},
				[]any{"h2", "Fedora", int64(100), int64(100), int64(0), int64(5), int64(1), int64(1), int64(0), int64(1700000100), int64(2500), int64(12), int64(40)},
			}, nil
		},
	}

	client := NewClient(rpc)
	items, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Progress != 0.75 || items[0].State != "downloading" {
		t.Fatalf("unexpected first torrent mapping: %+v", items[0])
	}
	if items[0].ETASeconds != 3 || items[0].Ratio != 1.5 || items[0].Peers != 22 || items[0].Seeds != 8 {
		t.Fatalf("unexpected first torrent extra mapping: %+v", items[0])
	}
	if !items[0].AddedAt.Equal(time.Unix(1700000000, 0).UTC()) {
		t.Fatalf("unexpected first torrent addedAt: %v", items[0].AddedAt)
	}
	if items[1].Progress != 1 || items[1].State != "seeding" {
		t.Fatalf("unexpected second torrent mapping: %+v", items[1])
	}
	if items[1].ETASeconds != 0 || items[1].Ratio != 2.5 || items[1].Peers != 12 || items[1].Seeds != 40 {
		t.Fatalf("unexpected second torrent extra mapping: %+v", items[1])
	}
	if !items[1].AddedAt.Equal(time.Unix(1700000100, 0).UTC()) {
		t.Fatalf("unexpected second torrent addedAt: %v", items[1].AddedAt)
	}
}

func TestAddMagnetFallback(t *testing.T) {
	rpc := &mockRPC{
		fn: func(method string, _ ...any) (any, error) {
			if method == "load.start" {
				return nil, errors.New("not supported")
			}
			return true, nil
		},
	}

	client := NewClient(rpc)
	if err := client.AddMagnet(context.Background(), "magnet:?xt=urn:btih:abc"); err != nil {
		t.Fatalf("AddMagnet() error = %v", err)
	}
	if len(rpc.calls) < 2 || rpc.calls[1] != "load.normal" {
		t.Fatalf("expected fallback to load.normal, calls=%v", rpc.calls)
	}
}

func TestRemoveCallsErase(t *testing.T) {
	rpc := &mockRPC{}
	client := NewClient(rpc)
	if err := client.Remove(context.Background(), "abc", false); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if len(rpc.calls) != 1 || rpc.calls[0] != "d.erase" {
		t.Fatalf("unexpected calls: %v", rpc.calls)
	}
}

func TestRemoveWithDeleteDataDeletesResolvedPath(t *testing.T) {
	tmpDir := t.TempDir()
	dataPath := filepath.Join(tmpDir, "payload")
	if err := os.MkdirAll(dataPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataPath, "piece.bin"), []byte("abc"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	rpc := &mockRPC{
		fn: func(method string, _ ...any) (any, error) {
			switch method {
			case "d.base_path":
				return dataPath, nil
			case "d.stop", "d.close", "d.erase":
				return nil, nil
			default:
				t.Fatalf("unexpected method: %s", method)
			}
			return nil, nil
		},
	}

	client := NewClient(rpc)
	if err := client.Remove(context.Background(), "abc", true); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Stat(dataPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected data path to be removed, statErr=%v", err)
	}
	if len(rpc.calls) != 4 {
		t.Fatalf("unexpected calls: %v", rpc.calls)
	}
	if rpc.calls[0] != "d.base_path" || rpc.calls[1] != "d.stop" || rpc.calls[2] != "d.close" || rpc.calls[3] != "d.erase" {
		t.Fatalf("unexpected calls order: %v", rpc.calls)
	}
}

func TestRemoveWithDeleteDataReturnsPartialErrorWhenDataPathLookupFails(t *testing.T) {
	rpc := &mockRPC{
		fn: func(method string, _ ...any) (any, error) {
			switch method {
			case "d.base_path", "d.base_filename":
				return nil, errors.New("unsupported")
			case "d.stop", "d.close", "d.erase":
				return nil, nil
			default:
				t.Fatalf("unexpected method: %s", method)
			}
			return nil, nil
		},
	}

	client := NewClient(rpc)
	err := client.Remove(context.Background(), "abc", true)
	if err == nil {
		t.Fatalf("expected Remove() error")
	}
	if !strings.Contains(err.Error(), "torrent removed but failed to determine data path") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rpc.calls) != 5 {
		t.Fatalf("unexpected calls: %v", rpc.calls)
	}
	if rpc.calls[len(rpc.calls)-1] != "d.erase" {
		t.Fatalf("expected erase to run even when data path lookup fails, calls=%v", rpc.calls)
	}
}

func TestStartCallsOpenThenStart(t *testing.T) {
	rpc := &mockRPC{
		fn: func(method string, _ ...any) (any, error) {
			if method == "d.open" {
				return nil, nil
			}
			if method == "d.start" {
				return true, nil
			}
			return nil, nil
		},
	}
	client := NewClient(rpc)
	if err := client.Start(context.Background(), "abc"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if len(rpc.calls) < 2 || rpc.calls[0] != "d.open" || rpc.calls[1] != "d.start" {
		t.Fatalf("unexpected calls: %v", rpc.calls)
	}
}

func TestStopCallsStop(t *testing.T) {
	rpc := &mockRPC{
		fn: func(method string, _ ...any) (any, error) {
			if method == "d.stop" {
				return true, nil
			}
			return nil, nil
		},
	}
	client := NewClient(rpc)
	if err := client.Stop(context.Background(), "abc"); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if len(rpc.calls) < 1 || rpc.calls[0] != "d.stop" {
		t.Fatalf("unexpected calls: %v", rpc.calls)
	}
}

func TestRecheckCallsCheckHash(t *testing.T) {
	rpc := &mockRPC{
		fn: func(method string, _ ...any) (any, error) {
			if method == "d.check_hash" {
				return true, nil
			}
			return nil, nil
		},
	}
	client := NewClient(rpc)
	if err := client.Recheck(context.Background(), "abc"); err != nil {
		t.Fatalf("Recheck() error = %v", err)
	}
	if len(rpc.calls) < 1 || rpc.calls[0] != "d.check_hash" {
		t.Fatalf("unexpected calls: %v", rpc.calls)
	}
}

func TestBackendStatusTransitionsFromErrorToConnected(t *testing.T) {
	callCount := 0
	rpc := &mockRPC{
		fn: func(method string, _ ...any) (any, error) {
			if method != "d.multicall2" {
				return nil, nil
			}
			callCount++
			if callCount == 1 {
				return nil, errors.New("dial unix /run/rtorrent/rpc.sock: no such file")
			}
			return []any{
				[]any{"h1", "Ubuntu ISO", int64(100), int64(100), int64(0), int64(0), int64(1), int64(1), int64(1700000000), int64(0), int64(1000), int64(0), int64(0)},
			}, nil
		},
	}

	client := NewClient(rpc)
	client.SetConnectionTarget("/run/rtorrent/rpc.sock")

	if _, err := client.List(context.Background()); err == nil {
		t.Fatalf("expected first List() call to fail")
	}
	status := client.BackendStatus()
	if status.Kind != "error" {
		t.Fatalf("expected backend status error, got %+v", status)
	}
	if !strings.Contains(status.Message, "/run/rtorrent/rpc.sock") {
		t.Fatalf("expected backend status target in message, got %q", status.Message)
	}

	if _, err := client.List(context.Background()); err != nil {
		t.Fatalf("expected second List() call to succeed, err=%v", err)
	}
	status = client.BackendStatus()
	if status.Kind != "ok" {
		t.Fatalf("expected backend status ok, got %+v", status)
	}
	if !strings.Contains(status.Message, "Connected successfully to rTorrent: /run/rtorrent/rpc.sock") {
		t.Fatalf("unexpected backend success message: %q", status.Message)
	}
}

func TestSetSpeedLimitsUsesPrimaryMethods(t *testing.T) {
	rpc := &mockRPC{
		fn: func(method string, args ...any) (any, error) {
			if len(args) != 2 {
				t.Fatalf("expected two arguments, got %d", len(args))
			}
			target, ok := args[0].(string)
			if !ok {
				t.Fatalf("expected string target argument, got %T", args[0])
			}
			if target != "" {
				t.Fatalf("expected empty target argument, got %q", target)
			}
			value, ok := args[1].(int64)
			if !ok {
				t.Fatalf("expected int64 argument, got %T", args[1])
			}

			switch method {
			case "throttle.global_down.max_rate.set_kb":
				if value != 2048 {
					t.Fatalf("unexpected download limit value: %d", value)
				}
				return nil, nil
			case "throttle.global_up.max_rate.set_kb":
				if value != 512 {
					t.Fatalf("unexpected upload limit value: %d", value)
				}
				return nil, nil
			default:
				t.Fatalf("unexpected method: %s", method)
			}
			return nil, nil
		},
	}

	client := NewClient(rpc)
	err := client.SetSpeedLimits(context.Background(), domain.SpeedLimits{DownloadKiB: 2048, UploadKiB: 512})
	if err != nil {
		t.Fatalf("SetSpeedLimits() error = %v", err)
	}
	if len(rpc.calls) != 2 || rpc.calls[0] != "throttle.global_down.max_rate.set_kb" || rpc.calls[1] != "throttle.global_up.max_rate.set_kb" {
		t.Fatalf("unexpected calls: %v", rpc.calls)
	}
}

func TestSetSpeedLimitsFallsBackToLegacyMethods(t *testing.T) {
	rpc := &mockRPC{
		fn: func(method string, _ ...any) (any, error) {
			switch method {
			case "throttle.global_down.max_rate.set_kb", "throttle.global_up.max_rate.set_kb":
				return nil, errors.New("unsupported")
			case "throttle.global_down.max_rate.set", "throttle.global_up.max_rate.set":
				return nil, nil
			default:
				t.Fatalf("unexpected method: %s", method)
			}
			return nil, nil
		},
	}

	client := NewClient(rpc)
	err := client.SetSpeedLimits(context.Background(), domain.SpeedLimits{DownloadKiB: 128, UploadKiB: 64})
	if err != nil {
		t.Fatalf("SetSpeedLimits() error = %v", err)
	}
	if len(rpc.calls) != 4 ||
		rpc.calls[0] != "throttle.global_down.max_rate.set_kb" ||
		rpc.calls[1] != "throttle.global_down.max_rate.set" ||
		rpc.calls[2] != "throttle.global_up.max_rate.set_kb" ||
		rpc.calls[3] != "throttle.global_up.max_rate.set" {
		t.Fatalf("unexpected calls: %v", rpc.calls)
	}
}

func TestSetSpeedLimitsFallsBackToLegacyMethodsWhenSetVariantsMissing(t *testing.T) {
	rpc := &mockRPC{
		fn: func(method string, _ ...any) (any, error) {
			switch method {
			case "throttle.global_down.max_rate.set_kb", "throttle.global_up.max_rate.set_kb":
				return nil, errors.New("unsupported")
			case "throttle.global_down.max_rate.set", "throttle.global_up.max_rate.set":
				return nil, errors.New("unsupported")
			case "set_download_rate", "set_upload_rate":
				return nil, nil
			default:
				t.Fatalf("unexpected method: %s", method)
			}
			return nil, nil
		},
	}

	client := NewClient(rpc)
	err := client.SetSpeedLimits(context.Background(), domain.SpeedLimits{DownloadKiB: 128, UploadKiB: 64})
	if err != nil {
		t.Fatalf("SetSpeedLimits() error = %v", err)
	}
	if len(rpc.calls) != 6 ||
		rpc.calls[0] != "throttle.global_down.max_rate.set_kb" ||
		rpc.calls[1] != "throttle.global_down.max_rate.set" ||
		rpc.calls[2] != "set_download_rate" ||
		rpc.calls[3] != "throttle.global_up.max_rate.set_kb" ||
		rpc.calls[4] != "throttle.global_up.max_rate.set" ||
		rpc.calls[5] != "set_upload_rate" {
		t.Fatalf("unexpected calls: %v", rpc.calls)
	}
}

func TestSetSpeedLimitsRejectsNegativeValues(t *testing.T) {
	rpc := &mockRPC{}
	client := NewClient(rpc)

	err := client.SetSpeedLimits(context.Background(), domain.SpeedLimits{DownloadKiB: -1, UploadKiB: 32})
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(rpc.calls) != 0 {
		t.Fatalf("expected no rpc calls, got %v", rpc.calls)
	}
}

func TestGetSpeedLimitsUsesPrimaryMethods(t *testing.T) {
	rpc := &mockRPC{
		fn: func(method string, args ...any) (any, error) {
			if len(args) != 0 {
				t.Fatalf("expected no arguments, got %d", len(args))
			}
			switch method {
			case "throttle.global_down.max_rate":
				return int64(102400), nil
			case "throttle.global_up.max_rate":
				return int64(1024), nil
			default:
				t.Fatalf("unexpected method: %s", method)
			}
			return nil, nil
		},
	}

	client := NewClient(rpc)
	limits, err := client.GetSpeedLimits(context.Background())
	if err != nil {
		t.Fatalf("GetSpeedLimits() error = %v", err)
	}
	if limits.DownloadKiB != 100 || limits.UploadKiB != 1 {
		t.Fatalf("unexpected limits: %+v", limits)
	}
	if len(rpc.calls) != 2 || rpc.calls[0] != "throttle.global_down.max_rate" || rpc.calls[1] != "throttle.global_up.max_rate" {
		t.Fatalf("unexpected calls: %v", rpc.calls)
	}
}

func TestGetSpeedLimitsFallsBackToLegacyMethods(t *testing.T) {
	rpc := &mockRPC{
		fn: func(method string, _ ...any) (any, error) {
			switch method {
			case "throttle.global_down.max_rate", "throttle.global_up.max_rate":
				return nil, errors.New("unsupported")
			case "throttle.global_down.max_rate.get_kb", "throttle.global_up.max_rate.get_kb":
				return nil, errors.New("unsupported")
			case "get_download_rate":
				return int64(300), nil
			case "get_upload_rate":
				return int64(45), nil
			default:
				t.Fatalf("unexpected method: %s", method)
			}
			return nil, nil
		},
	}

	client := NewClient(rpc)
	limits, err := client.GetSpeedLimits(context.Background())
	if err != nil {
		t.Fatalf("GetSpeedLimits() error = %v", err)
	}
	if limits.DownloadKiB != 300 || limits.UploadKiB != 45 {
		t.Fatalf("unexpected limits: %+v", limits)
	}
	if len(rpc.calls) != 6 ||
		rpc.calls[0] != "throttle.global_down.max_rate" ||
		rpc.calls[1] != "throttle.global_down.max_rate.get_kb" ||
		rpc.calls[2] != "get_download_rate" ||
		rpc.calls[3] != "throttle.global_up.max_rate" ||
		rpc.calls[4] != "throttle.global_up.max_rate.get_kb" ||
		rpc.calls[5] != "get_upload_rate" {
		t.Fatalf("unexpected calls: %v", rpc.calls)
	}
}
