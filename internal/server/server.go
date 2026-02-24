package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/thiagokokada/gtorrent/internal/domain"
)

const maxTorrentUploadBytes = 16 << 20 // 16 MiB

//go:embed static/*
var staticFS embed.FS

// Service defines application operations required by the API.
type Service interface {
	List(ctx context.Context) ([]domain.Torrent, error)
	AddMagnet(ctx context.Context, magnet string) error
	AddTorrent(ctx context.Context, data []byte, filename string) error
	Remove(ctx context.Context, hash string, deleteData bool) error
	Start(ctx context.Context, hash string) error
	Stop(ctx context.Context, hash string) error
	Recheck(ctx context.Context, hash string) error
}

type Server struct {
	svc        Service
	staticRoot http.Handler
}

func New(svc Service) (*Server, error) {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("load static fs: %w", err)
	}
	return &Server{svc: svc, staticRoot: http.FileServer(http.FS(sub))}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/torrents", s.handleTorrents)
	mux.HandleFunc("/api/torrents/", s.handleTorrentByHash)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/", s.handleStatic)
	return loggingMiddleware(mux)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTorrents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListTorrents(w, r)
	case http.MethodPost:
		s.handleAddTorrent(w, r)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleTorrentByHash(w http.ResponseWriter, r *http.Request) {
	rawPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/api/torrents/")
	rawPath = strings.Trim(rawPath, "/")
	if rawPath == "" || rawPath == "." {
		writeError(w, http.StatusBadRequest, "torrent hash is required")
		return
	}

	parts := strings.Split(rawPath, "/")
	hash := parts[0]
	if hash == "" || hash == "." {
		writeError(w, http.StatusBadRequest, "torrent hash is required")
		return
	}

	if len(parts) == 1 {
		if r.Method != http.MethodDelete {
			writeMethodNotAllowed(w, http.MethodDelete)
			return
		}
		deleteData := strings.EqualFold(r.URL.Query().Get("deleteData"), "true")
		if err := s.svc.Remove(r.Context(), hash, deleteData); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	if len(parts) != 2 || r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	switch parts[1] {
	case "start":
		if err := s.svc.Start(r.Context(), hash); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	case "stop":
		if err := s.svc.Stop(r.Context(), hash); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	case "recheck":
		if err := s.svc.Recheck(r.Context(), hash); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	default:
		http.NotFound(w, r)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleListTorrents(w http.ResponseWriter, r *http.Request) {
	items, err := s.svc.List(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"torrents": items})
}

func (s *Server) handleAddTorrent(w http.ResponseWriter, r *http.Request) {
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	switch {
	case strings.HasPrefix(contentType, "application/json"):
		s.handleAddJSON(w, r)
	case strings.HasPrefix(contentType, "multipart/form-data"):
		s.handleAddMultipart(w, r)
	case strings.HasPrefix(contentType, "application/x-www-form-urlencoded") || contentType == "":
		s.handleAddForm(w, r)
	default:
		writeError(w, http.StatusUnsupportedMediaType, "content type not supported")
	}
}

func (s *Server) handleAddJSON(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct {
		Magnet string `json:"magnet"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json payload")
		return
	}
	if req.Magnet == "" {
		writeError(w, http.StatusBadRequest, "magnet is required")
		return
	}
	if err := s.svc.AddMagnet(r.Context(), req.Magnet); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleAddForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form payload")
		return
	}
	magnet := strings.TrimSpace(r.Form.Get("magnet"))
	if magnet == "" {
		writeError(w, http.StatusBadRequest, "magnet is required")
		return
	}
	if err := s.svc.AddMagnet(r.Context(), magnet); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleAddMultipart(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxTorrentUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart payload")
		return
	}

	magnet := strings.TrimSpace(r.FormValue("magnet"))
	if magnet != "" {
		if err := s.svc.AddMagnet(r.Context(), magnet); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
		return
	}

	file, hdr, err := r.FormFile("torrent")
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			writeError(w, http.StatusBadRequest, "either magnet or torrent file is required")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid torrent file")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxTorrentUploadBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read torrent file")
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "torrent file is empty")
		return
	}
	if len(data) > maxTorrentUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "torrent file is too large")
		return
	}
	if err := s.svc.AddTorrent(r.Context(), data, hdr.Filename); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeMethodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}

	if r.URL.Path == "/" {
		http.ServeFileFS(w, r, staticFS, "static/index.html")
		return
	}
	s.staticRoot.ServeHTTP(w, r)
}

func writeMethodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	size   int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	n, err := r.ResponseWriter.Write(p)
	r.size += n
	return n, err
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		duration := time.Since(start).Round(time.Millisecond)

		switch {
		case rec.status >= http.StatusInternalServerError:
			slog.Error("http request", "method", r.Method, "path", r.URL.RequestURI(), "remote", r.RemoteAddr, "status", rec.status, "bytes", rec.size, "duration", duration)
		case rec.status >= http.StatusBadRequest:
			slog.Warn("http request", "method", r.Method, "path", r.URL.RequestURI(), "remote", r.RemoteAddr, "status", rec.status, "bytes", rec.size, "duration", duration)
		default:
			slog.Debug("http request", "method", r.Method, "path", r.URL.RequestURI(), "remote", r.RemoteAddr, "status", rec.status, "bytes", rec.size, "duration", duration)
		}
	})
}
