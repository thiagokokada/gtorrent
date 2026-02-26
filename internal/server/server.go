package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
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
	GetSpeedLimits(ctx context.Context) (domain.SpeedLimits, error)
	SetSpeedLimits(ctx context.Context, limits domain.SpeedLimits) error
}

type Server struct {
	svc           Service
	staticRoot    http.Handler
	templates     *template.Template
	indexTemplate *template.Template
	uiJSVersion   string
	streamStop    chan struct{}
	streamOnce    sync.Once
}

type viewParams struct {
	Query    string
	Filter   string
	Sort     string
	Dir      string
	Selected string
	Cols     string
}

type flashMessage struct {
	Kind              string
	Message           string
	OpenAddDialog     bool
	OpenColumnsDialog bool
	OpenRemoveDialog  bool
	AddFormError      string
	AddFormMagnet     string
}

type torrentRow struct {
	Hash          string
	Name          string
	State         string
	StateClass    string
	Running       bool
	Active        bool
	SelectURL     string
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

type fileListColumn struct {
	Key      string
	Label    string
	MinWidth int
	Sortable bool
}

type dashboardView struct {
	Params            viewParams
	Torrents          []torrentRow
	Columns           []fileListColumn
	VisibleColumns    map[string]bool
	VisibleColCount   int
	HasSelected       bool
	SelectedHash      string
	SelectedName      string
	SelectedRunning   bool
	DownloadLimitKiB  int64
	UploadLimitKiB    int64
	StatusKind        string
	StatusMessage     string
	VisibleCount      int
	TotalDownRate     string
	TotalUpRate       string
	StreamURL         string
	DashboardURL      string
	FilterURLs        map[string]string
	SortURLs          map[string]string
	OpenAddDialog     bool
	OpenColumnsDialog bool
	OpenRemoveDialog  bool
	AddFormError      string
	AddFormMagnet     string
	SwapOOB           bool
}

type indexView struct {
	UIJSVersion  string
	DashboardURL string
}

type dashboardFragments struct {
	ViewState bool
	Stats     bool
	Controls  bool
	AddDialog bool
	Status    bool
	FileList  bool
}

var (
	// Fragment presets for HTMX responses. Keep these aligned with AGENTS.md "UI Fragment Contract".
	// - Add validation errors: add dialog
	// - Speed limits: controls + status
	// - View navigation: file-list + controls + stats + view-state
	// - Torrent actions: all
	fragmentsAll = dashboardFragments{
		ViewState: true,
		Stats:     true,
		Controls:  true,
		AddDialog: true,
		Status:    true,
		FileList:  true,
	}
	fragmentsViewNavigation = dashboardFragments{
		ViewState: true,
		Stats:     true,
		Controls:  true,
		FileList:  true,
	}
	fragmentsAddDialogOnly = dashboardFragments{
		AddDialog: true,
	}
	fragmentsControlsOnly = dashboardFragments{
		Controls: true,
	}
	fragmentsControlsAndStatus = dashboardFragments{
		Controls: true,
		Status:   true,
	}
	fileListColumns = []fileListColumn{
		{Key: "name", Label: "Name", MinWidth: 220, Sortable: true},
		{Key: "hash", Label: "Hash", MinWidth: 180, Sortable: false},
		{Key: "state", Label: "State", MinWidth: 90, Sortable: true},
		{Key: "addedAt", Label: "Added", MinWidth: 110, Sortable: true},
		{Key: "progress", Label: "Done", MinWidth: 140, Sortable: true},
		{Key: "etaSeconds", Label: "ETA", MinWidth: 90, Sortable: true},
		{Key: "ratio", Label: "Ratio", MinWidth: 80, Sortable: true},
		{Key: "peers", Label: "Peers", MinWidth: 70, Sortable: true},
		{Key: "seeds", Label: "Seeds", MinWidth: 70, Sortable: true},
		{Key: "downRate", Label: "Down", MinWidth: 95, Sortable: true},
		{Key: "upRate", Label: "Up", MinWidth: 90, Sortable: true},
		{Key: "sizeBytes", Label: "Size", MinWidth: 110, Sortable: true},
	}
	fileListColumnSet          = buildColumnSet(fileListColumns)
	fileListSortableColumnKeys = buildSortableColumnKeys(fileListColumns)
	fileListSortableColumnSet  = buildColumnSetFromKeys(fileListSortableColumnKeys)
)

func New(svc Service) (*Server, error) {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("load static fs: %w", err)
	}

	tpls, err := template.ParseFS(templateFS, "templates/*.gohtml")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	indexTpl, err := template.ParseFS(staticFS, "static/index.html")
	if err != nil {
		return nil, fmt.Errorf("parse index template: %w", err)
	}

	uiJSVersion, err := staticHash("static/ui.js")
	if err != nil {
		return nil, fmt.Errorf("hash ui.js: %w", err)
	}

	return &Server{
		svc:           svc,
		staticRoot:    http.FileServer(http.FS(sub)),
		templates:     tpls,
		indexTemplate: indexTpl,
		uiJSVersion:   uiJSVersion,
		streamStop:    make(chan struct{}),
	}, nil
}

func (s *Server) ShutdownStreams() {
	s.streamOnce.Do(func() {
		close(s.streamStop)
	})
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/_empty", s.handleUIEmpty)
	mux.HandleFunc("/ui", s.handleUIPage)
	mux.HandleFunc("/ui/dashboard", s.handleUIDashboard)
	mux.HandleFunc("/ui/torrents", s.handleUIAddTorrent)
	mux.HandleFunc("/ui/speed-limits", s.handleUISpeedLimits)
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
	s.renderIndex(w, r)
}

func (s *Server) handleUIEmpty(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "status", dashboardView{}); err != nil {
		slog.Error("render status placeholder failed", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

func (s *Server) handleUIDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	params := parseViewParams(r.URL.Query())
	fragments := fragmentsViewNavigation
	flash := flashMessage{}
	switch strings.TrimSpace(r.Header.Get("HX-Trigger")) {
	case "cancel-add":
		fragments = fragmentsAddDialogOnly
	case "open-add":
		fragments = fragmentsAddDialogOnly
		flash.OpenAddDialog = true
	case "cancel-columns":
		fragments = fragmentsControlsOnly
	case "open-columns":
		fragments = fragmentsControlsOnly
		flash.OpenColumnsDialog = true
	case "cancel-remove":
		fragments = fragmentsControlsOnly
	case "open-remove":
		fragments = fragmentsControlsOnly
		flash.OpenRemoveDialog = true
	}
	s.renderDashboardResponse(w, r, r.Context(), params, flash, fragments)
}

func (s *Server) handleUIAddTorrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	if err := r.ParseMultipartForm(maxTorrentUploadBytes); err != nil {
		s.renderDashboardResponse(w, r, r.Context(), defaultViewParams(), flashMessage{AddFormError: "invalid multipart payload"}, fragmentsAddDialogOnly)
		return
	}

	params := parseViewParams(r.Form)
	magnet := strings.TrimSpace(r.FormValue("magnet"))

	if magnet != "" {
		if err := s.svc.AddMagnet(r.Context(), magnet); err != nil {
			s.renderDashboardResponse(w, r, r.Context(), params, flashMessage{AddFormError: err.Error(), AddFormMagnet: magnet}, fragmentsAddDialogOnly)
			return
		}
		s.renderDashboardResponse(w, r, r.Context(), params, flashMessage{Kind: "ok", Message: "Torrent added"}, fragmentsAll)
		return
	}

	file, hdr, err := r.FormFile("torrent")
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			s.renderDashboardResponse(w, r, r.Context(), params, flashMessage{AddFormError: "provide a magnet link or a .torrent file"}, fragmentsAddDialogOnly)
			return
		}
		s.renderDashboardResponse(w, r, r.Context(), params, flashMessage{AddFormError: "invalid torrent file"}, fragmentsAddDialogOnly)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxTorrentUploadBytes+1))
	if err != nil {
		s.renderDashboardResponse(w, r, r.Context(), params, flashMessage{AddFormError: "failed to read torrent file"}, fragmentsAddDialogOnly)
		return
	}
	if len(data) == 0 {
		s.renderDashboardResponse(w, r, r.Context(), params, flashMessage{AddFormError: "torrent file is empty"}, fragmentsAddDialogOnly)
		return
	}
	if len(data) > maxTorrentUploadBytes {
		s.renderDashboardResponse(w, r, r.Context(), params, flashMessage{AddFormError: "torrent file is too large"}, fragmentsAddDialogOnly)
		return
	}

	if err := s.svc.AddTorrent(r.Context(), data, hdr.Filename); err != nil {
		s.renderDashboardResponse(w, r, r.Context(), params, flashMessage{AddFormError: err.Error()}, fragmentsAddDialogOnly)
		return
	}

	s.renderDashboardResponse(w, r, r.Context(), params, flashMessage{Kind: "ok", Message: "Torrent added"}, fragmentsAll)
}

func (s *Server) handleUISpeedLimits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderDashboardResponse(w, r, r.Context(), defaultViewParams(), flashMessage{Kind: "error", Message: "invalid form payload"}, fragmentsControlsAndStatus)
		return
	}

	params := parseViewParams(r.Form)
	limits, err := parseSpeedLimits(r.Form)
	if err != nil {
		s.renderDashboardResponse(w, r, r.Context(), params, flashMessage{Kind: "error", Message: err.Error()}, fragmentsControlsAndStatus)
		return
	}

	if err := s.svc.SetSpeedLimits(r.Context(), limits); err != nil {
		s.renderDashboardResponse(w, r, r.Context(), params, flashMessage{Kind: "error", Message: err.Error()}, fragmentsControlsAndStatus)
		return
	}

	s.renderDashboardResponse(w, r, r.Context(), params, flashMessage{Kind: "ok", Message: "Speed limits updated"}, fragmentsControlsAndStatus)
}

func (s *Server) handleUITorrentAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderDashboardResponse(w, r, r.Context(), defaultViewParams(), flashMessage{Kind: "error", Message: "invalid form payload"}, fragmentsAll)
		return
	}
	params := parseViewParams(r.Form)

	rawPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/ui/torrents/")
	rawPath = strings.Trim(rawPath, "/")
	parts := strings.Split(rawPath, "/")
	if len(parts) != 2 || parts[0] == "" {
		s.renderDashboardResponse(w, r, r.Context(), params, flashMessage{Kind: "error", Message: "invalid torrent action"}, fragmentsAll)
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
		deleteData := parseBoolFormValue(r.Form.Get("deleteData"))
		err = s.svc.Remove(r.Context(), hash, deleteData)
		if deleteData {
			message = "Torrent removed and data deleted"
		} else {
			message = "Torrent removed"
		}
	default:
		s.renderDashboardResponse(w, r, r.Context(), params, flashMessage{Kind: "error", Message: "unknown action"}, fragmentsAll)
		return
	}

	if err != nil {
		s.renderDashboardResponse(w, r, r.Context(), params, flashMessage{Kind: "error", Message: err.Error()}, fragmentsAll)
		return
	}
	s.renderDashboardResponse(w, r, r.Context(), params, flashMessage{Kind: "ok", Message: message}, fragmentsAll)
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

	lastStatusKind, lastStatusMessage := statusFromBackendStatus(s.currentBackendStatus(nil))
	if err := s.writeLiveUpdate(w, flusher, streamCtx, params, &lastStatusKind, &lastStatusMessage); err != nil {
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
			if err := s.writeLiveUpdate(w, flusher, streamCtx, params, &lastStatusKind, &lastStatusMessage); err != nil {
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

func (s *Server) writeLiveUpdate(w io.Writer, flusher http.Flusher, ctx context.Context, params viewParams, lastStatusKind, lastStatusMessage *string) error {
	view, err := s.buildDashboardView(ctx, params)
	if err != nil {
		statusKind, statusMessage := statusFromBackendStatus(s.currentBackendStatus(err))
		dashboardURL, filterURLs, sortURLs := controlURLs(params)
		speedLimits := s.currentSpeedLimits(ctx)
		visibleColumns := parseVisibleColumns(params.Cols)
		fallback := dashboardView{
			Params:           params,
			Columns:          fileListColumns,
			VisibleColumns:   visibleColumns,
			VisibleColCount:  visibleColumnsCount(visibleColumns),
			DownloadLimitKiB: speedLimits.DownloadKiB,
			UploadLimitKiB:   speedLimits.UploadKiB,
			StatusKind:       statusKind,
			StatusMessage:    statusMessage,
			VisibleCount:     0,
			TotalDownRate:    "0 B/s",
			TotalUpRate:      "0 B/s",
			StreamURL:        streamURLForParams(params),
			DashboardURL:     dashboardURL,
			FilterURLs:       filterURLs,
			SortURLs:         sortURLs,
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
		if writeErr := s.writeStatusUpdate(w, flusher, fallback, lastStatusKind, lastStatusMessage); writeErr != nil {
			return writeErr
		}
		if writeErr := writeSSEHTML(w, flusher, "table", tableHTML); writeErr != nil {
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
	if err := s.writeStatusUpdate(w, flusher, view, lastStatusKind, lastStatusMessage); err != nil {
		return err
	}
	if err := writeSSEHTML(w, flusher, "table", tableHTML); err != nil {
		return err
	}
	return nil
}

func (s *Server) writeStatusUpdate(w io.Writer, flusher http.Flusher, view dashboardView, lastStatusKind, lastStatusMessage *string) error {
	if lastStatusKind != nil && lastStatusMessage != nil {
		if *lastStatusKind == view.StatusKind && *lastStatusMessage == view.StatusMessage {
			return nil
		}
	}

	statusHTML, err := s.renderTemplateToString("status", view)
	if err != nil {
		return err
	}
	if err := writeSSEHTML(w, flusher, "status", statusHTML); err != nil {
		return err
	}

	if lastStatusKind != nil && lastStatusMessage != nil {
		*lastStatusKind = view.StatusKind
		*lastStatusMessage = view.StatusMessage
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
		s.renderIndex(w, r)
		return
	}
	s.staticRoot.ServeHTTP(w, r)
}

func (s *Server) renderIndex(w http.ResponseWriter, r *http.Request) {
	params := parseInitialViewParams(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if err := s.indexTemplate.Execute(w, indexView{
		UIJSVersion:  s.uiJSVersion,
		DashboardURL: dashboardURLForParams(params),
	}); err != nil {
		slog.Error("render index failed", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

const (
	cookieFilter = "gtorrent.view.filter"
	cookieSort   = "gtorrent.view.sort"
	cookieDir    = "gtorrent.view.dir"
)

func parseInitialViewParams(r *http.Request) viewParams {
	params := defaultViewParams()
	if r == nil {
		return params
	}

	cookieFilterValue := ""
	cookieSortValue := ""
	cookieDirValue := ""
	queryFilterValue := strings.TrimSpace(r.URL.Query().Get("filter"))
	querySortValue := strings.TrimSpace(r.URL.Query().Get("sort"))
	queryDirValue := strings.TrimSpace(r.URL.Query().Get("dir"))
	queryColsValue := strings.TrimSpace(r.URL.Query().Get("cols"))

	if cookie, err := r.Cookie(cookieFilter); err == nil {
		cookieFilterValue = strings.TrimSpace(cookie.Value)
		if isValidFilter(cookieFilterValue) {
			params.Filter = cookieFilterValue
		}
	}
	if cookie, err := r.Cookie(cookieSort); err == nil {
		cookieSortValue = strings.TrimSpace(cookie.Value)
		if isValidSort(cookieSortValue) {
			params.Sort = cookieSortValue
		}
	}
	if cookie, err := r.Cookie(cookieDir); err == nil {
		cookieDirValue = strings.TrimSpace(cookie.Value)
		if cookieDirValue == "asc" || cookieDirValue == "desc" {
			params.Dir = cookieDirValue
		}
	}

	if isValidFilter(queryFilterValue) {
		params.Filter = queryFilterValue
	}
	if isValidSort(querySortValue) {
		params.Sort = querySortValue
	}
	if queryDirValue == "asc" || queryDirValue == "desc" {
		params.Dir = queryDirValue
	}
	if queryColsValue != "" {
		params.Cols = encodeVisibleColumns(parseVisibleColumns(queryColsValue))
	}

	slog.Debug("resolve initial ui view params",
		"path", r.URL.Path,
		"cookieFilter", cookieFilterValue,
		"cookieSort", cookieSortValue,
		"cookieDir", cookieDirValue,
		"queryFilter", queryFilterValue,
		"querySort", querySortValue,
		"queryDir", queryDirValue,
		"queryCols", queryColsValue,
		"resolvedFilter", params.Filter,
		"resolvedSort", params.Sort,
		"resolvedDir", params.Dir,
		"resolvedCols", params.Cols,
	)

	return params
}

func (s *Server) renderDashboardResponse(w http.ResponseWriter, r *http.Request, ctx context.Context, params viewParams, flash flashMessage, fragments dashboardFragments) {
	if isHTMXFragmentRequest(r) {
		s.renderDashboardFragments(w, ctx, params, flash, fragments)
		return
	}
	s.renderDashboard(w, ctx, params, flash)
}

func isHTMXFragmentRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("HX-Request")), "true") {
		return false
	}
	return strings.TrimSpace(r.Header.Get("HX-Target")) != "dashboard"
}

func (s *Server) renderDashboard(w http.ResponseWriter, ctx context.Context, params viewParams, flash flashMessage) {
	view := s.dashboardViewWithFlash(ctx, params, flash)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "dashboard", view); err != nil {
		slog.Error("render dashboard failed", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

func (s *Server) renderDashboardFragments(w http.ResponseWriter, ctx context.Context, params viewParams, flash flashMessage, fragments dashboardFragments) {
	view := s.dashboardViewWithFlash(ctx, params, flash)
	view.SwapOOB = true

	var body bytes.Buffer
	if fragments.ViewState {
		if err := s.templates.ExecuteTemplate(&body, "view-state", view); err != nil {
			slog.Error("render dashboard fragments failed", "fragment", "view-state", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}
	if fragments.Stats {
		if err := s.templates.ExecuteTemplate(&body, "stats", view); err != nil {
			slog.Error("render dashboard fragments failed", "fragment", "stats", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}
	if fragments.Controls {
		if err := s.templates.ExecuteTemplate(&body, "controls", view); err != nil {
			slog.Error("render dashboard fragments failed", "fragment", "controls", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}
	if fragments.AddDialog {
		if err := s.templates.ExecuteTemplate(&body, "add-dialog", view); err != nil {
			slog.Error("render dashboard fragments failed", "fragment", "add-dialog", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}
	if fragments.Status {
		if err := s.templates.ExecuteTemplate(&body, "status", view); err != nil {
			slog.Error("render dashboard fragments failed", "fragment", "status", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}
	if fragments.FileList {
		if err := s.templates.ExecuteTemplate(&body, "file-list", view); err != nil {
			slog.Error("render dashboard fragments failed", "fragment", "file-list", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := io.Copy(w, &body); err != nil {
		slog.Error("write dashboard fragments failed", "error", err)
	}
}

func (s *Server) dashboardViewWithFlash(ctx context.Context, params viewParams, flash flashMessage) dashboardView {
	view, err := s.buildDashboardView(ctx, params)
	if err != nil {
		statusKind, statusMessage := statusFromBackendStatus(s.currentBackendStatus(err))
		dashboardURL, filterURLs, sortURLs := controlURLs(params)
		speedLimits := s.currentSpeedLimits(ctx)
		visibleColumns := parseVisibleColumns(params.Cols)
		view = dashboardView{
			Params:           params,
			Columns:          fileListColumns,
			VisibleColumns:   visibleColumns,
			VisibleColCount:  visibleColumnsCount(visibleColumns),
			DownloadLimitKiB: speedLimits.DownloadKiB,
			UploadLimitKiB:   speedLimits.UploadKiB,
			StatusKind:       statusKind,
			StatusMessage:    statusMessage,
			VisibleCount:     0,
			TotalDownRate:    "0 B/s",
			TotalUpRate:      "0 B/s",
			StreamURL:        streamURLForParams(params),
			DashboardURL:     dashboardURL,
			FilterURLs:       filterURLs,
			SortURLs:         sortURLs,
		}
	}
	if strings.TrimSpace(flash.Message) != "" {
		view.StatusMessage = strings.TrimSpace(flash.Message)
		switch flash.Kind {
		case "error", "ok":
			view.StatusKind = flash.Kind
		default:
			view.StatusKind = "info"
		}
	}
	if flash.OpenAddDialog || strings.TrimSpace(flash.AddFormError) != "" {
		view.OpenAddDialog = true
	}
	if flash.OpenColumnsDialog {
		view.OpenColumnsDialog = true
	}
	if flash.OpenRemoveDialog && view.HasSelected {
		view.OpenRemoveDialog = true
	}
	if strings.TrimSpace(flash.AddFormError) != "" {
		view.AddFormError = strings.TrimSpace(flash.AddFormError)
		view.AddFormMagnet = strings.TrimSpace(flash.AddFormMagnet)
	}
	return view
}

func (s *Server) buildDashboardView(ctx context.Context, params viewParams) (dashboardView, error) {
	visibleColumns := parseVisibleColumns(params.Cols)
	params.Cols = encodeVisibleColumns(visibleColumns)

	items, err := s.svc.List(ctx)
	if err != nil {
		return dashboardView{}, err
	}

	filtered := filterTorrents(items, params)
	sortTorrents(filtered, params)

	rows := make([]torrentRow, 0, len(filtered))
	var downTotal int64
	var upTotal int64
	selectedHash := strings.TrimSpace(params.Selected)
	selectedFound := false
	selectedRunning := false
	selectedName := ""

	for _, item := range filtered {
		state := normalizeState(item.State)
		progress := clampProgress(item.Progress)
		active := selectedHash != "" && item.Hash == selectedHash
		rowParams := params
		rowParams.Selected = item.Hash
		rows = append(rows, torrentRow{
			Hash:          item.Hash,
			Name:          torrentDisplayName(item),
			State:         state,
			StateClass:    state,
			Running:       state == "downloading" || state == "seeding",
			Active:        active,
			SelectURL:     dashboardURLForParams(rowParams),
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
		if active {
			selectedFound = true
			selectedRunning = state == "downloading" || state == "seeding"
			selectedName = torrentDisplayName(item)
		}
		downTotal += item.DownRate
		upTotal += item.UpRate
	}

	if !selectedFound {
		params.Selected = ""
		selectedHash = ""
		selectedRunning = false
		selectedName = ""
	}

	statusKind, statusMessage := statusFromBackendStatus(s.currentBackendStatus(nil))
	speedLimits := s.currentSpeedLimits(ctx)
	dashboardURL, filterURLs, sortURLs := controlURLs(params)
	return dashboardView{
		Params:           params,
		Torrents:         rows,
		Columns:          fileListColumns,
		VisibleColumns:   visibleColumns,
		VisibleColCount:  visibleColumnsCount(visibleColumns),
		HasSelected:      selectedHash != "",
		SelectedHash:     selectedHash,
		SelectedName:     selectedName,
		SelectedRunning:  selectedRunning,
		DownloadLimitKiB: speedLimits.DownloadKiB,
		UploadLimitKiB:   speedLimits.UploadKiB,
		StatusKind:       statusKind,
		StatusMessage:    statusMessage,
		VisibleCount:     len(rows),
		TotalDownRate:    formatRate(downTotal),
		TotalUpRate:      formatRate(upTotal),
		StreamURL:        streamURLForParams(params),
		DashboardURL:     dashboardURL,
		FilterURLs:       filterURLs,
		SortURLs:         sortURLs,
	}, nil
}

func (s *Server) currentBackendStatus(fallbackErr error) domain.BackendStatus {
	if fallbackErr != nil {
		return domain.BackendStatus{
			Kind:    "error",
			Message: fallbackErr.Error(),
		}
	}

	type backendStatusProvider interface {
		BackendStatus() domain.BackendStatus
	}

	if provider, ok := s.svc.(backendStatusProvider); ok {
		status := provider.BackendStatus()
		switch status.Kind {
		case "ok", "error":
		default:
			status.Kind = ""
		}
		status.Message = strings.TrimSpace(status.Message)
		if status.Kind != "" && status.Message != "" {
			return status
		}
	}

	return domain.BackendStatus{}
}

func (s *Server) currentSpeedLimits(ctx context.Context) domain.SpeedLimits {
	limits, err := s.svc.GetSpeedLimits(ctx)
	if err != nil {
		slog.Warn("get speed limits failed", "error", err)
		return domain.SpeedLimits{}
	}
	if limits.DownloadKiB < 0 {
		limits.DownloadKiB = 0
	}
	if limits.UploadKiB < 0 {
		limits.UploadKiB = 0
	}
	return limits
}

func statusFromBackendStatus(status domain.BackendStatus) (string, string) {
	kind := strings.TrimSpace(status.Kind)
	message := strings.TrimSpace(status.Message)

	if kind == "error" && message != "" {
		return "error", message
	}

	return "", ""
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
			if !strings.Contains(name, query) {
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
	params.Cols = encodeVisibleColumns(parseVisibleColumnsValues(values))
	return params
}

func parseVisibleColumnsValues(values url.Values) map[string]bool {
	if values == nil {
		return defaultVisibleColumns()
	}
	selected := values["visibleCol"]
	if len(selected) > 0 {
		return parseVisibleColumnsList(selected)
	}
	if strings.TrimSpace(values.Get("applyCols")) != "" {
		return defaultVisibleColumns()
	}
	return parseVisibleColumns(values.Get("cols"))
}

func parseVisibleColumns(raw string) map[string]bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultVisibleColumns()
	}
	return parseVisibleColumnsList(strings.Split(trimmed, ","))
}

func parseVisibleColumnsList(items []string) map[string]bool {
	visible := make(map[string]bool, len(fileListColumns))
	for _, item := range items {
		key := strings.TrimSpace(item)
		if _, ok := fileListColumnSet[key]; !ok {
			continue
		}
		visible[key] = true
	}
	if len(visible) == 0 {
		return defaultVisibleColumns()
	}
	return visible
}

func encodeVisibleColumns(visible map[string]bool) string {
	if len(visible) == 0 {
		return ""
	}
	keys := make([]string, 0, len(fileListColumns))
	allVisible := true
	for _, column := range fileListColumns {
		if visible[column.Key] {
			keys = append(keys, column.Key)
			continue
		}
		allVisible = false
	}
	if allVisible {
		return ""
	}
	return strings.Join(keys, ",")
}

func defaultVisibleColumns() map[string]bool {
	visible := make(map[string]bool, len(fileListColumns))
	for _, column := range fileListColumns {
		visible[column.Key] = true
	}
	return visible
}

func visibleColumnsCount(visible map[string]bool) int {
	count := 0
	for _, column := range fileListColumns {
		if visible[column.Key] {
			count++
		}
	}
	if count == 0 {
		return len(fileListColumns)
	}
	return count
}

func buildColumnSet(columns []fileListColumn) map[string]struct{} {
	keys := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		keys[column.Key] = struct{}{}
	}
	return keys
}

func buildSortableColumnKeys(columns []fileListColumn) []string {
	keys := make([]string, 0, len(columns))
	for _, column := range columns {
		if column.Sortable {
			keys = append(keys, column.Key)
		}
	}
	return keys
}

func buildColumnSetFromKeys(keys []string) map[string]struct{} {
	result := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		result[key] = struct{}{}
	}
	return result
}

func parseSpeedLimits(values url.Values) (domain.SpeedLimits, error) {
	downloadKiB, err := parseNonNegativeInt64(values.Get("downloadLimitKiB"))
	if err != nil {
		return domain.SpeedLimits{}, errors.New("download limit must be a non-negative integer")
	}
	uploadKiB, err := parseNonNegativeInt64(values.Get("uploadLimitKiB"))
	if err != nil {
		return domain.SpeedLimits{}, errors.New("upload limit must be a non-negative integer")
	}
	return domain.SpeedLimits{
		DownloadKiB: downloadKiB,
		UploadKiB:   uploadKiB,
	}, nil
}

func parseNonNegativeInt64(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, err
	}
	if parsed < 0 {
		return 0, errors.New("negative value")
	}
	return parsed, nil
}

func parseBoolFormValue(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

func defaultViewParams() viewParams {
	return viewParams{Filter: "all", Sort: "addedAt", Dir: "desc"}
}

func defaultSortDir(sortKey string) string {
	if sortKey == "name" || sortKey == "state" {
		return "asc"
	}
	return "desc"
}

func controlURLs(params viewParams) (string, map[string]string, map[string]string) {
	dashboardURL := dashboardURLForParams(params)

	filterURLs := make(map[string]string, 5)
	for _, filter := range []string{"all", "downloading", "seeding", "complete", "stopped"} {
		next := params
		next.Filter = filter
		filterURLs[filter] = dashboardURLForParams(next)
	}

	sortURLs := make(map[string]string, len(fileListSortableColumnKeys))
	for _, sortKey := range fileListSortableColumnKeys {
		next := params
		next.Sort = sortKey
		if params.Sort == sortKey {
			if params.Dir == "asc" {
				next.Dir = "desc"
			} else {
				next.Dir = "asc"
			}
		} else {
			next.Dir = defaultSortDir(sortKey)
		}
		sortURLs[sortKey] = dashboardURLForParams(next)
	}

	return dashboardURL, filterURLs, sortURLs
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
	_, ok := fileListSortableColumnSet[v]
	return ok
}

func streamURLForParams(params viewParams) string {
	return urlForParams("/ui/stream", params)
}

func dashboardURLForParams(params viewParams) string {
	return urlForParams("/ui/dashboard", params)
}

func urlForParams(base string, params viewParams) string {
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
	if params.Cols != "" {
		values.Set("cols", params.Cols)
	}
	encoded := values.Encode()
	if encoded == "" {
		return base
	}
	return base + "?" + encoded
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

func staticHash(path string) (string, error) {
	data, err := fs.ReadFile(staticFS, path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8]), nil
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
