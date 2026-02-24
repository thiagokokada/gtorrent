package server

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/thiagokokada/gtorrent/internal/domain"
)

type mockService struct {
	listFn          func(context.Context) ([]domain.Torrent, error)
	addMagnetFn     func(context.Context, string) error
	addFileFn       func(context.Context, []byte, string) error
	removeFn        func(context.Context, string, bool) error
	startFn         func(context.Context, string) error
	stopFn          func(context.Context, string) error
	recheckFn       func(context.Context, string) error
	backendStatusFn func() domain.BackendStatus
}

func (m *mockService) List(ctx context.Context) ([]domain.Torrent, error) {
	if m.listFn == nil {
		return nil, nil
	}
	return m.listFn(ctx)
}

func (m *mockService) AddMagnet(ctx context.Context, magnet string) error {
	if m.addMagnetFn == nil {
		return nil
	}
	return m.addMagnetFn(ctx, magnet)
}

func (m *mockService) AddTorrent(ctx context.Context, data []byte, filename string) error {
	if m.addFileFn == nil {
		return nil
	}
	return m.addFileFn(ctx, data, filename)
}

func (m *mockService) Remove(ctx context.Context, hash string, deleteData bool) error {
	if m.removeFn == nil {
		return nil
	}
	return m.removeFn(ctx, hash, deleteData)
}

func (m *mockService) Start(ctx context.Context, hash string) error {
	if m.startFn == nil {
		return nil
	}
	return m.startFn(ctx, hash)
}

func (m *mockService) Stop(ctx context.Context, hash string) error {
	if m.stopFn == nil {
		return nil
	}
	return m.stopFn(ctx, hash)
}

func (m *mockService) Recheck(ctx context.Context, hash string) error {
	if m.recheckFn == nil {
		return nil
	}
	return m.recheckFn(ctx, hash)
}

func (m *mockService) BackendStatus() domain.BackendStatus {
	if m.backendStatusFn == nil {
		return domain.BackendStatus{}
	}
	return m.backendStatusFn()
}

func TestDashboardEndpointRendersTorrentRows(t *testing.T) {
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return []domain.Torrent{{Hash: "abc", Name: "Ubuntu ISO", State: "downloading", Progress: 0.5}}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ui/dashboard", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Ubuntu ISO") {
		t.Fatalf("expected torrent row, body=%s", body)
	}
	if !strings.Contains(body, "id=\"toggle-selected\"") {
		t.Fatalf("expected top-bar action buttons, body=%s", body)
	}
	if !strings.Contains(body, "data-hash=\"abc\"") || !strings.Contains(body, "data-running=\"1\"") {
		t.Fatalf("expected selectable running row, body=%s", body)
	}
}

func TestUIPageHasCacheBustedUIScript(t *testing.T) {
	s, err := New(&mockService{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ui", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	matched, err := regexp.MatchString(`src="/ui\.js\?v=[0-9a-f]{16}"`, body)
	if err != nil {
		t.Fatalf("regexp error = %v", err)
	}
	if !matched {
		t.Fatalf("expected cache-busted ui.js script tag, body=%s", body)
	}
}

func TestAddTorrentMagnet(t *testing.T) {
	called := false
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return nil, nil
		},
		addMagnetFn: func(_ context.Context, magnet string) error {
			called = true
			if magnet == "" {
				t.Fatalf("empty magnet")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("magnet", "magnet:?xt=urn:btih:test")
	_ = writer.WriteField("filter", "all")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/ui/torrents", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatalf("expected AddMagnet call")
	}
	if !strings.Contains(rr.Body.String(), "Torrent added") {
		t.Fatalf("expected success flash, body=%s", rr.Body.String())
	}
}

func TestTorrentActionRemove(t *testing.T) {
	called := false
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return []domain.Torrent{{Hash: "abc", Name: "Ubuntu ISO"}}, nil
		},
		removeFn: func(_ context.Context, hash string, deleteData bool) error {
			called = true
			if hash != "abc" {
				t.Fatalf("unexpected hash %q", hash)
			}
			if deleteData {
				t.Fatalf("expected deleteData=false")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/ui/torrents/abc/remove", strings.NewReader("filter=all&sort=addedAt&dir=desc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatalf("expected Remove call")
	}
	if !strings.Contains(rr.Body.String(), "Torrent removed") {
		t.Fatalf("expected success flash, body=%s", rr.Body.String())
	}
}

func TestUIStreamEndpoint(t *testing.T) {
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return []domain.Torrent{{Hash: "abc", Name: "Ubuntu ISO", State: "downloading"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/ui/stream", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content-type = %q", got)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "event: stats") {
		t.Fatalf("expected stats event, body=%s", body)
	}
	if !strings.Contains(body, "event: table") {
		t.Fatalf("expected table event, body=%s", body)
	}
	if strings.Contains(body, "event: backend-status") {
		t.Fatalf("unexpected backend-status event in ui stream, body=%s", body)
	}
}

func TestUIBackendStatusStreamEndpoint(t *testing.T) {
	s, err := New(&mockService{
		backendStatusFn: func() domain.BackendStatus {
			return domain.BackendStatus{
				Kind:    "error",
				Message: "Error talking to rTorrent (/run/rtorrent/rpc.sock): dial unix socket: no such file or directory",
			}
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/ui/backend-status/stream", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content-type = %q", got)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "event: status") {
		t.Fatalf("expected status event, body=%s", body)
	}
	if !strings.Contains(body, "\"kind\":\"error\"") {
		t.Fatalf("expected status kind payload, body=%s", body)
	}
	if !strings.Contains(body, "/run/rtorrent/rpc.sock") {
		t.Fatalf("expected connection target in payload, body=%s", body)
	}
}

func TestAPIRoutesRemoved(t *testing.T) {
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return []domain.Torrent{{Hash: "abc"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/torrents", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
}

func TestDashboardRendersBackendStatus(t *testing.T) {
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return nil, nil
		},
		backendStatusFn: func() domain.BackendStatus {
			return domain.BackendStatus{
				Kind:    "ok",
				Message: "Connected successfully to rTorrent: /run/rtorrent/rpc.sock",
			}
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ui/dashboard", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "data-backend-status-kind=\"ok\"") {
		t.Fatalf("expected backend status kind in stats, body=%s", body)
	}
	if !strings.Contains(body, "Connected successfully to rTorrent: /run/rtorrent/rpc.sock") {
		t.Fatalf("expected backend status message in stats, body=%s", body)
	}
}
