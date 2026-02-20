package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gtorrent/internal/domain"
)

type mockService struct {
	listFn      func(context.Context) ([]domain.Torrent, error)
	addMagnetFn func(context.Context, string) error
	addFileFn   func(context.Context, []byte, string) error
	removeFn    func(context.Context, string, bool) error
	startFn     func(context.Context, string) error
	stopFn      func(context.Context, string) error
}

func (m *mockService) List(ctx context.Context) ([]domain.Torrent, error) {
	return m.listFn(ctx)
}
func (m *mockService) AddMagnet(ctx context.Context, magnet string) error {
	return m.addMagnetFn(ctx, magnet)
}
func (m *mockService) AddTorrent(ctx context.Context, data []byte, filename string) error {
	return m.addFileFn(ctx, data, filename)
}
func (m *mockService) Remove(ctx context.Context, hash string, deleteData bool) error {
	return m.removeFn(ctx, hash, deleteData)
}
func (m *mockService) Start(ctx context.Context, hash string) error {
	return m.startFn(ctx, hash)
}
func (m *mockService) Stop(ctx context.Context, hash string) error {
	return m.stopFn(ctx, hash)
}

func TestListEndpoint(t *testing.T) {
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return []domain.Torrent{{Hash: "a", Name: "n"}}, nil
		},
		addMagnetFn: func(context.Context, string) error { return nil },
		addFileFn:   func(context.Context, []byte, string) error { return nil },
		removeFn:    func(context.Context, string, bool) error { return nil },
		startFn:     func(context.Context, string) error { return nil },
		stopFn:      func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/torrents", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var payload struct {
		Torrents []domain.Torrent `json:"torrents"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if len(payload.Torrents) != 1 || payload.Torrents[0].Hash != "a" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestAddMagnetJSON(t *testing.T) {
	called := false
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) { return nil, nil },
		addMagnetFn: func(_ context.Context, magnet string) error {
			called = true
			if magnet == "" {
				t.Fatalf("empty magnet")
			}
			return nil
		},
		addFileFn: func(context.Context, []byte, string) error { return nil },
		removeFn:  func(context.Context, string, bool) error { return nil },
		startFn:   func(context.Context, string) error { return nil },
		stopFn:    func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/torrents", strings.NewReader(`{"magnet":"magnet:?xt=urn:btih:abc"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatalf("expected AddMagnet call")
	}
}

func TestAddMultipartTorrent(t *testing.T) {
	called := false
	s, err := New(&mockService{
		listFn:      func(context.Context) ([]domain.Torrent, error) { return nil, nil },
		addMagnetFn: func(context.Context, string) error { return nil },
		addFileFn: func(_ context.Context, data []byte, filename string) error {
			called = true
			if len(data) == 0 || filename == "" {
				t.Fatalf("invalid file args")
			}
			return nil
		},
		removeFn: func(context.Context, string, bool) error { return nil },
		startFn:  func(context.Context, string) error { return nil },
		stopFn:   func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("torrent", "a.torrent")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	_, _ = part.Write([]byte("dummy torrent bytes"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/torrents", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatalf("expected AddTorrent call")
	}
}

func TestDeleteTorrent(t *testing.T) {
	called := false
	s, err := New(&mockService{
		listFn:      func(context.Context) ([]domain.Torrent, error) { return nil, nil },
		addMagnetFn: func(context.Context, string) error { return nil },
		addFileFn:   func(context.Context, []byte, string) error { return nil },
		removeFn: func(_ context.Context, hash string, deleteData bool) error {
			called = true
			if hash != "abc" || !deleteData {
				t.Fatalf("unexpected args hash=%q deleteData=%v", hash, deleteData)
			}
			return nil
		},
		startFn: func(context.Context, string) error { return nil },
		stopFn:  func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/torrents/abc?deleteData=true", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatalf("expected Remove call")
	}
}

func TestStartTorrent(t *testing.T) {
	called := false
	s, err := New(&mockService{
		listFn:      func(context.Context) ([]domain.Torrent, error) { return nil, nil },
		addMagnetFn: func(context.Context, string) error { return nil },
		addFileFn:   func(context.Context, []byte, string) error { return nil },
		removeFn:    func(context.Context, string, bool) error { return nil },
		startFn: func(_ context.Context, hash string) error {
			called = true
			if hash != "abc" {
				t.Fatalf("unexpected hash=%q", hash)
			}
			return nil
		},
		stopFn: func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/torrents/abc/start", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatalf("expected Start call")
	}
}

func TestStopTorrent(t *testing.T) {
	called := false
	s, err := New(&mockService{
		listFn:      func(context.Context) ([]domain.Torrent, error) { return nil, nil },
		addMagnetFn: func(context.Context, string) error { return nil },
		addFileFn:   func(context.Context, []byte, string) error { return nil },
		removeFn:    func(context.Context, string, bool) error { return nil },
		startFn:     func(context.Context, string) error { return nil },
		stopFn: func(_ context.Context, hash string) error {
			called = true
			if hash != "abc" {
				t.Fatalf("unexpected hash=%q", hash)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/torrents/abc/stop", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatalf("expected Stop call")
	}
}
