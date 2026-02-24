package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thiagokokada/gtorrent/internal/domain"
)

const maxTorrentUploadBytes = 16 << 20 // 16 MiB

var (
	streamPollInterval      = 4 * time.Second
	streamKeepaliveInterval = 20 * time.Second
)

//go:embed static/*
var staticFS embed.FS

//go:embed templates/*.gohtml
var templateFS embed.FS

// Service defines application operations required by the UI.
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
	templates  *template.Template
	streamStop chan struct{}
	streamOnce sync.Once
}

type viewParams struct {
	Query    string
	Filter   string
	Sort     string
	Dir      string
	Selected string
}

type flashMessage struct {
	Kind    string
	Message string
}

type torrentRow struct {
	Hash          string
	Name          string
	State         string
	StateClass    string
	Running       bool
	Active        bool
	AddedAt       string
	ProgressPct   int
	ProgressValue string
	ETA           string
	Ratio         string
	Peers         string
	Seeds         string
	DownRate      string
	UpRate        string
	Size          string
}

type dashboardView struct {
	Params        viewParams
	Flash         flashMessage
	Torrents      []torrentRow
	VisibleCount  int
	TotalDownRate string
	TotalUpRate   string
	StreamURL     string
}

func New(svc Service) (*Server, error) {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("load static fs: %w", err)
	}

	tpls, err := template.ParseFS(templateFS, "templates/*.gohtml")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	return &Server{
		svc:        svc,
		staticRoot: http.FileServer(http.FS(sub)),
		templates:  tpls,
		streamStop: make(chan struct{}),
	}, nil
}

func (s *Server) ShutdownStreams() {
	s.streamOnce.Do(func() {
		close(s.streamStop)
	})
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ui", s.handleUIPage)
	mux.HandleFunc("/ui/dashboard", s.handleUIDashboard)
	mux.HandleFunc("/ui/torrents", s.handleUIAddTorrent)
	mux.HandleFunc("/ui/torrents/", s.handleUITorrentAction)
	mux.HandleFunc("/ui/stream", s.handleUIStream)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/", s.handleStatic)
	return loggingMiddleware(mux)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleUIPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeMethodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	http.ServeFileFS(w, r, staticFS, "static/index.html")
}

func (s *Server) handleUIDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	params := parseViewParams(r.URL.Query())
	s.renderDashboard(w, r.Context(), params, flashMessage{})
}

func (s *Server) handleUIAddTorrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	if err := r.ParseMultipartForm(maxTorrentUploadBytes); err != nil {
		s.renderDashboard(w, r.Context(), defaultViewParams(), flashMessage{Kind: "error", Message: "invalid multipart payload"})
		return
	}

	params := parseViewParams(r.Form)
	magnet := strings.TrimSpace(r.FormValue("magnet"))

	if magnet != "" {
		if err := s.svc.AddMagnet(r.Context(), magnet); err != nil {
			s.renderDashboard(w, r.Context(), params, flashMessage{Kind: "error", Message: err.Error()})
			return
		}
		s.renderDashboard(w, r.Context(), params, flashMessage{Kind: "ok", Message: "Torrent added"})
		return
	}

	file, hdr, err := r.FormFile("torrent")
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			s.renderDashboard(w, r.Context(), params, flashMessage{Kind: "error", Message: "provide a magnet link or a .torrent file"})
			return
		}
		s.renderDashboard(w, r.Context(), params, flashMessage{Kind: "error", Message: "invalid torrent file"})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxTorrentUploadBytes+1))
	if err != nil {
		s.renderDashboard(w, r.Context(), params, flashMessage{Kind: "error", Message: "failed to read torrent file"})
		return
	}
	if len(data) == 0 {
		s.renderDashboard(w, r.Context(), params, flashMessage{Kind: "error", Message: "torrent file is empty"})
		return
	}
	if len(data) > maxTorrentUploadBytes {
		s.renderDashboard(w, r.Context(), params, flashMessage{Kind: "error", Message: "torrent file is too large"})
		return
	}

	if err := s.svc.AddTorrent(r.Context(), data, hdr.Filename); err != nil {
		s.renderDashboard(w, r.Context(), params, flashMessage{Kind: "error", Message: err.Error()})
		return
	}

	s.renderDashboard(w, r.Context(), params, flashMessage{Kind: "ok", Message: "Torrent added"})
}

func (s *Server) handleUITorrentAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderDashboard(w, r.Context(), defaultViewParams(), flashMessage{Kind: "error", Message: "invalid form payload"})
		return
	}
	params := parseViewParams(r.Form)

	rawPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/ui/torrents/")
	rawPath = strings.Trim(rawPath, "/")
	parts := strings.Split(rawPath, "/")
	if len(parts) != 2 || parts[0] == "" {
		s.renderDashboard(w, r.Context(), params, flashMessage{Kind: "error", Message: "invalid torrent action"})
		return
	}

	hash := parts[0]
	action := parts[1]

	var err error
	message := ""
	switch action {
	case "start":
		err = s.svc.Start(r.Context(), hash)
		message = "Torrent started"
	case "stop":
		err = s.svc.Stop(r.Context(), hash)
		message = "Torrent stopped"
	case "recheck":
		err = s.svc.Recheck(r.Context(), hash)
		message = "Torrent recheck requested"
	case "remove":
		err = s.svc.Remove(r.Context(), hash, false)
		message = "Torrent removed"
	default:
		s.renderDashboard(w, r.Context(), params, flashMessage{Kind: "error", Message: "unknown action"})
		return
	}

	if err != nil {
		s.renderDashboard(w, r.Context(), params, flashMessage{Kind: "error", Message: err.Error()})
		return
	}
	s.renderDashboard(w, r.Context(), params, flashMessage{Kind: "ok", Message: message})
}

func (s *Server) handleUIStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	params := parseViewParams(r.URL.Query())

	ctx := r.Context()
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()
	go func() {
		select {
		case <-s.streamStop:
			streamCancel()
		case <-ctx.Done():
		}
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	if _, err := io.WriteString(w, "retry: 3000\n\n"); err != nil {
		return
	}

	if err := s.writeLiveUpdate(w, flusher, streamCtx, params); err != nil {
		return
	}

	pollTicker := time.NewTicker(streamPollInterval)
	defer pollTicker.Stop()

	keepaliveTicker := time.NewTicker(streamKeepaliveInterval)
	defer keepaliveTicker.Stop()

	for {
		select {
		case <-streamCtx.Done():
			return
		case <-pollTicker.C:
			if err := s.writeLiveUpdate(w, flusher, streamCtx, params); err != nil {
				return
			}
		case <-keepaliveTicker.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) writeLiveUpdate(w io.Writer, flusher http.Flusher, ctx context.Context, params viewParams) error {
	view, err := s.buildDashboardView(ctx, params)
	if err != nil {
		fallback := dashboardView{
			Params:        params,
			VisibleCount:  0,
			TotalDownRate: "0 B/s",
			TotalUpRate:   "0 B/s",
			StreamURL:     streamURLForParams(params),
		}
		statsHTML, renderErr := s.renderTemplateToString("stats", fallback)
		if renderErr != nil {
			return renderErr
		}
		tableHTML, renderErr := s.renderTemplateToString("table", fallback)
		if renderErr != nil {
			return renderErr
		}
		if writeErr := writeSSEHTML(w, flusher, "stats", statsHTML); writeErr != nil {
			return writeErr
		}
		if writeErr := writeSSEHTML(w, flusher, "table", tableHTML); writeErr != nil {
			return writeErr
		}
		if writeErr := writeSSEText(w, flusher, "backend-error", err.Error()); writeErr != nil {
			return writeErr
		}
		return nil
	}

	statsHTML, err := s.renderTemplateToString("stats", view)
	if err != nil {
		return err
	}
	tableHTML, err := s.renderTemplateToString("table", view)
	if err != nil {
		return err
	}
	if err := writeSSEHTML(w, flusher, "stats", statsHTML); err != nil {
		return err
	}
	if err := writeSSEHTML(w, flusher, "table", tableHTML); err != nil {
		return err
	}
	if err := writeSSEText(w, flusher, "backend-ok", "ok"); err != nil {
		return err
	}
	return nil
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeMethodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/ui/") || strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}

	if r.URL.Path == "/" {
		http.ServeFileFS(w, r, staticFS, "static/index.html")
		return
	}
	s.staticRoot.ServeHTTP(w, r)
}

func (s *Server) renderDashboard(w http.ResponseWriter, ctx context.Context, params viewParams, flash flashMessage) {
	view, err := s.buildDashboardView(ctx, params)
	if err != nil {
		view = dashboardView{
			Params:        params,
			VisibleCount:  0,
			TotalDownRate: "0 B/s",
			TotalUpRate:   "0 B/s",
			StreamURL:     streamURLForParams(params),
		}
		if flash.Message == "" {
			flash = flashMessage{Kind: "error", Message: err.Error()}
		}
	}
	view.Flash = flash
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "dashboard", view); err != nil {
		slog.Error("render dashboard failed", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

func (s *Server) buildDashboardView(ctx context.Context, params viewParams) (dashboardView, error) {
	items, err := s.svc.List(ctx)
	if err != nil {
		return dashboardView{}, err
	}

	filtered := filterTorrents(items, params)
	sortTorrents(filtered, params)

	rows := make([]torrentRow, 0, len(filtered))
	var downTotal int64
	var upTotal int64

	for _, item := range filtered {
		state := normalizeState(item.State)
		progress := clampProgress(item.Progress)
		rows = append(rows, torrentRow{
			Hash:          item.Hash,
			Name:          torrentDisplayName(item),
			State:         state,
			StateClass:    state,
			Running:       state == "downloading" || state == "seeding",
			Active:        params.Selected != "" && item.Hash == params.Selected,
			AddedAt:       formatAdded(item.AddedAt),
			ProgressPct:   int(math.Round(progress * 100)),
			ProgressValue: strconv.FormatFloat(progress, 'f', 4, 64),
			ETA:           formatETA(item.ETASeconds),
			Ratio:         formatRatio(item.Ratio),
			Peers:         strconv.FormatInt(item.Peers, 10),
			Seeds:         strconv.FormatInt(item.Seeds, 10),
			DownRate:      formatRate(item.DownRate),
			UpRate:        formatRate(item.UpRate),
			Size:          fmt.Sprintf("%s / %s", formatBytes(item.DoneBytes), formatBytes(item.SizeBytes)),
		})
		downTotal += item.DownRate
		upTotal += item.UpRate
	}

	return dashboardView{
		Params:        params,
		Torrents:      rows,
		VisibleCount:  len(rows),
		TotalDownRate: formatRate(downTotal),
		TotalUpRate:   formatRate(upTotal),
		StreamURL:     streamURLForParams(params),
	}, nil
}

func filterTorrents(items []domain.Torrent, params viewParams) []domain.Torrent {
	query := strings.ToLower(strings.TrimSpace(params.Query))
	filtered := make([]domain.Torrent, 0, len(items))
	for _, item := range items {
		state := normalizeState(item.State)
		if params.Filter != "all" && state != params.Filter {
			continue
		}
		if query != "" {
			name := strings.ToLower(item.Name)
			hash := strings.ToLower(item.Hash)
			if !strings.Contains(name, query) && !strings.Contains(hash, query) {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func sortTorrents(items []domain.Torrent, params viewParams) {
	ascending := params.Dir == "asc"
	sort.SliceStable(items, func(i, j int) bool {
		a := items[i]
		b := items[j]

		cmp := 0
		switch params.Sort {
		case "name":
			cmp = strings.Compare(strings.ToLower(torrentDisplayName(a)), strings.ToLower(torrentDisplayName(b)))
		case "state":
			cmp = strings.Compare(normalizeState(a.State), normalizeState(b.State))
		case "progress":
			cmp = compareFloat(a.Progress, b.Progress)
		case "etaSeconds":
			av := etaSortValue(a.ETASeconds)
			bv := etaSortValue(b.ETASeconds)
			cmp = compareInt64(av, bv)
		case "ratio":
			cmp = compareFloat(a.Ratio, b.Ratio)
		case "peers":
			cmp = compareInt64(a.Peers, b.Peers)
		case "seeds":
			cmp = compareInt64(a.Seeds, b.Seeds)
		case "downRate":
			cmp = compareInt64(a.DownRate, b.DownRate)
		case "upRate":
			cmp = compareInt64(a.UpRate, b.UpRate)
		case "sizeBytes":
			cmp = compareInt64(a.SizeBytes, b.SizeBytes)
		case "addedAt":
			fallthrough
		default:
			cmp = compareTime(a.AddedAt, b.AddedAt)
		}

		if ascending {
			return cmp < 0
		}
		return cmp > 0
	})
}

func compareInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func compareFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func compareTime(a, b time.Time) int {
	switch {
	case a.Before(b):
		return -1
	case a.After(b):
		return 1
	default:
		return 0
	}
}

func parseViewParams(values url.Values) viewParams {
	params := defaultViewParams()
	if values == nil {
		return params
	}
	if v := strings.TrimSpace(values.Get("q")); v != "" {
		params.Query = v
	}
	if v := strings.TrimSpace(values.Get("filter")); isValidFilter(v) {
		params.Filter = v
	}
	if v := strings.TrimSpace(values.Get("sort")); isValidSort(v) {
		params.Sort = v
	}
	if v := strings.TrimSpace(values.Get("dir")); v == "asc" || v == "desc" {
		params.Dir = v
	}
	if v := strings.TrimSpace(values.Get("selected")); v != "" {
		params.Selected = v
	}
	return params
}

func defaultViewParams() viewParams {
	return viewParams{Filter: "all", Sort: "addedAt", Dir: "desc"}
}

func isValidFilter(v string) bool {
	switch v {
	case "all", "downloading", "seeding", "complete", "stopped", "unknown":
		return true
	default:
		return false
	}
}

func isValidSort(v string) bool {
	switch v {
	case "addedAt", "name", "state", "progress", "etaSeconds", "ratio", "peers", "seeds", "downRate", "upRate", "sizeBytes":
		return true
	default:
		return false
	}
}

func streamURLForParams(params viewParams) string {
	values := url.Values{}
	if params.Query != "" {
		values.Set("q", params.Query)
	}
	if params.Filter != "" {
		values.Set("filter", params.Filter)
	}
	if params.Sort != "" {
		values.Set("sort", params.Sort)
	}
	if params.Dir != "" {
		values.Set("dir", params.Dir)
	}
	if params.Selected != "" {
		values.Set("selected", params.Selected)
	}
	encoded := values.Encode()
	if encoded == "" {
		return "/ui/stream"
	}
	return "/ui/stream?" + encoded
}

func normalizeState(raw string) string {
	state := strings.ToLower(strings.TrimSpace(raw))
	switch state {
	case "downloading", "seeding", "complete", "stopped":
		return state
	default:
		return "unknown"
	}
}

func etaSortValue(v int64) int64 {
	if v < 0 {
		return math.MaxInt64
	}
	return v
}

func clampProgress(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func torrentDisplayName(t domain.Torrent) string {
	if strings.TrimSpace(t.Name) != "" {
		return t.Name
	}
	if strings.TrimSpace(t.Hash) != "" {
		return t.Hash
	}
	return "(unknown)"
}

func formatAdded(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format("2006-01-02 15:04")
}

func formatRatio(ratio float64) string {
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 {
		return "0.00"
	}
	return fmt.Sprintf("%.2f", ratio)
}

func formatETA(seconds int64) string {
	if seconds < 0 {
		return "inf"
	}
	if seconds == 0 {
		return "Done"
	}
	d := seconds / 86400
	h := (seconds % 86400) / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	if d > 0 {
		return fmt.Sprintf("%dd %dh", d, h)
	}
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func formatRate(bytesPerSecond int64) string {
	return formatBytes(bytesPerSecond) + "/s"
}

func formatBytes(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	value := float64(bytes)
	idx := 0
	for value >= 1024 && idx < len(units)-1 {
		value /= 1024
		idx++
	}
	if idx <= 1 {
		return fmt.Sprintf("%.0f %s", value, units[idx])
	}
	return fmt.Sprintf("%.1f %s", value, units[idx])
}

func (s *Server) renderTemplateToString(name string, data any) (string, error) {
	var b strings.Builder
	if err := s.templates.ExecuteTemplate(&b, name, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

func writeSSEHTML(w io.Writer, flusher http.Flusher, event, html string) error {
	if event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
			return err
		}
	}
	for _, line := range strings.Split(html, "\n") {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func writeSSEText(w io.Writer, flusher http.Flusher, event, text string) error {
	if event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
			return err
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return err
	}
	flusher.Flush()
	return nil
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

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
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
