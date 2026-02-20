package rtorrent

import (
	"context"
	"errors"
	"testing"
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
				[]any{"h1", "Ubuntu ISO", int64(100), int64(75), int64(10), int64(2), int64(1), int64(0)},
				[]any{"h2", "Fedora", int64(100), int64(100), int64(0), int64(5), int64(1), int64(1)},
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
	if items[1].Progress != 1 || items[1].State != "seeding" {
		t.Fatalf("unexpected second torrent mapping: %+v", items[1])
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
