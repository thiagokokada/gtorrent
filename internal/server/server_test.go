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
	listFn           func(context.Context) ([]domain.Torrent, error)
	addMagnetFn      func(context.Context, string) error
	addFileFn        func(context.Context, []byte, string) error
	removeFn         func(context.Context, string, bool) error
	startFn          func(context.Context, string) error
	stopFn           func(context.Context, string) error
	recheckFn        func(context.Context, string) error
	setSpeedLimitsFn func(context.Context, domain.SpeedLimits) error
	getSpeedLimitsFn func(context.Context) (domain.SpeedLimits, error)
	backendStatusFn  func() domain.BackendStatus
}

type testFlusher struct{}

func (testFlusher) Flush() {}

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

func (m *mockService) SetSpeedLimits(ctx context.Context, limits domain.SpeedLimits) error {
	if m.setSpeedLimitsFn == nil {
		return nil
	}
	return m.setSpeedLimitsFn(ctx, limits)
}

func (m *mockService) BackendStatus() domain.BackendStatus {
	if m.backendStatusFn == nil {
		return domain.BackendStatus{}
	}
	return m.backendStatusFn()
}

func (m *mockService) GetSpeedLimits(ctx context.Context) (domain.SpeedLimits, error) {
	if m.getSpeedLimitsFn == nil {
		return domain.SpeedLimits{}, nil
	}
	return m.getSpeedLimitsFn(ctx)
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
	if !strings.Contains(body, `id="add-btn" class="primary" disabled`) {
		t.Fatalf("expected add button disabled by default in add dialog, body=%s", body)
	}
	if !strings.Contains(body, "data-hash=\"abc\"") || !strings.Contains(body, `hx-get="/ui/dashboard?dir=desc&amp;filter=all&amp;selected=abc&amp;sort=addedAt"`) {
		t.Fatalf("expected selectable row URL, body=%s", body)
	}
}

func TestDashboardRendersFilterAndSortURLs(t *testing.T) {
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return []domain.Torrent{{Hash: "abc", Name: "Ubuntu ISO", State: "downloading"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ui/dashboard?filter=all&sort=addedAt&dir=desc&selected=abc", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, `hx-get="/ui/dashboard?dir=desc&amp;filter=downloading&amp;selected=abc&amp;sort=addedAt"`) {
		t.Fatalf("expected downloading filter URL preserving params, body=%s", body)
	}
	if !strings.Contains(body, `hx-get="/ui/dashboard?dir=asc&amp;filter=all&amp;selected=abc&amp;sort=addedAt"`) {
		t.Fatalf("expected active sort toggle URL, body=%s", body)
	}
	if !strings.Contains(body, `hx-get="/ui/dashboard?dir=asc&amp;filter=all&amp;selected=abc&amp;sort=name"`) {
		t.Fatalf("expected sort URL default direction for name, body=%s", body)
	}
}

func TestDashboardRendersSpeedLimitInputs(t *testing.T) {
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return nil, nil
		},
		getSpeedLimitsFn: func(context.Context) (domain.SpeedLimits, error) {
			return domain.SpeedLimits{DownloadKiB: 2048, UploadKiB: 512}, nil
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
	if !strings.Contains(body, `id="download-limit-kib"`) || !strings.Contains(body, `value="2048"`) {
		t.Fatalf("expected download speed limit input value, body=%s", body)
	}
	if !strings.Contains(body, `id="upload-limit-kib"`) || !strings.Contains(body, `value="512"`) {
		t.Fatalf("expected upload speed limit input value, body=%s", body)
	}
}

func TestDashboardRendersSelectedActionButtons(t *testing.T) {
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return []domain.Torrent{
				{Hash: "abc", Name: "Ubuntu ISO", State: "downloading"},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ui/dashboard?selected=abc", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, `id="toggle-selected" class="secondary"`) || !strings.Contains(body, `hx-post="/ui/torrents/abc/stop"`) {
		t.Fatalf("expected selected running toggle button, body=%s", body)
	}
	if !strings.Contains(body, `hx-post="/ui/torrents/abc/recheck"`) {
		t.Fatalf("expected selected recheck button, body=%s", body)
	}
	if !strings.Contains(body, `hx-post="/ui/torrents/abc/remove"`) {
		t.Fatalf("expected selected remove button, body=%s", body)
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

func TestSetSpeedLimits(t *testing.T) {
	called := false
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return nil, nil
		},
		setSpeedLimitsFn: func(_ context.Context, limits domain.SpeedLimits) error {
			called = true
			if limits.DownloadKiB != 2048 || limits.UploadKiB != 512 {
				t.Fatalf("unexpected speed limits: %+v", limits)
			}
			return nil
		},
		getSpeedLimitsFn: func(context.Context) (domain.SpeedLimits, error) {
			return domain.SpeedLimits{DownloadKiB: 2048, UploadKiB: 512}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/ui/speed-limits", strings.NewReader("downloadLimitKiB=2048&uploadLimitKiB=512&filter=all&sort=addedAt&dir=desc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatalf("expected SetSpeedLimits call")
	}
	if !strings.Contains(rr.Body.String(), "Speed limits updated") {
		t.Fatalf("expected success flash, body=%s", rr.Body.String())
	}
}

func TestSetSpeedLimitsRejectsInvalidInput(t *testing.T) {
	called := false
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return nil, nil
		},
		setSpeedLimitsFn: func(_ context.Context, _ domain.SpeedLimits) error {
			called = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/ui/speed-limits", strings.NewReader("downloadLimitKiB=-1&uploadLimitKiB=64"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if called {
		t.Fatalf("did not expect SetSpeedLimits call")
	}
	if !strings.Contains(rr.Body.String(), "download limit must be a non-negative integer") {
		t.Fatalf("expected validation error, body=%s", rr.Body.String())
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
	if strings.Contains(body, "event: status") {
		t.Fatalf("unexpected status event for unchanged steady-state stream, body=%s", body)
	}
	if strings.Contains(body, "event: backend-status") {
		t.Fatalf("unexpected backend-status event in ui stream, body=%s", body)
	}
}

func TestWriteLiveUpdateDeduplicatesStatusEvent(t *testing.T) {
	status := domain.BackendStatus{
		Kind:    "error",
		Message: "Error talking to rTorrent (/run/rtorrent/rpc.sock): dial unix socket: no such file or directory",
	}
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return []domain.Torrent{{Hash: "abc", Name: "Ubuntu ISO", State: "downloading"}}, nil
		},
		backendStatusFn: func() domain.BackendStatus {
			return status
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var body strings.Builder
	lastKind := ""
	lastMessage := ""

	if err := s.writeLiveUpdate(&body, testFlusher{}, context.Background(), defaultViewParams(), &lastKind, &lastMessage); err != nil {
		t.Fatalf("first writeLiveUpdate() error = %v", err)
	}
	if err := s.writeLiveUpdate(&body, testFlusher{}, context.Background(), defaultViewParams(), &lastKind, &lastMessage); err != nil {
		t.Fatalf("second writeLiveUpdate() error = %v", err)
	}

	out := body.String()
	if count := strings.Count(out, "event: status"); count != 1 {
		t.Fatalf("expected 1 status event for unchanged status, got %d, body=%s", count, out)
	}

	status = domain.BackendStatus{
		Kind:    "error",
		Message: "Error talking to rTorrent (/run/rtorrent/rpc.sock): dial unix socket: connection refused",
	}
	if err := s.writeLiveUpdate(&body, testFlusher{}, context.Background(), defaultViewParams(), &lastKind, &lastMessage); err != nil {
		t.Fatalf("third writeLiveUpdate() error = %v", err)
	}

	out = body.String()
	if count := strings.Count(out, "event: status"); count != 2 {
		t.Fatalf("expected 2 status events after status change, got %d, body=%s", count, out)
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

func TestDashboardHidesSteadyBackendOKStatus(t *testing.T) {
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
	if !strings.Contains(body, `id="form-message"`) {
		t.Fatalf("expected status bar in dashboard, body=%s", body)
	}
	if !strings.Contains(body, "message-info is-hidden") {
		t.Fatalf("expected steady backend ok status to be hidden, body=%s", body)
	}
	if strings.Contains(body, "Connected successfully to rTorrent: /run/rtorrent/rpc.sock") {
		t.Fatalf("expected no steady backend ok message in status bar, body=%s", body)
	}
}
