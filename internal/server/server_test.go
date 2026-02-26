package server

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/thiagokokada/gtorrent/internal/domain"
	"golang.org/x/net/html"
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

func parseHTML(t *testing.T, body string) *html.Node {
	t.Helper()

	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("html parse error = %v", err)
	}
	return doc
}

func findElement(root *html.Node, pred func(*html.Node) bool) *html.Node {
	var walk func(*html.Node) *html.Node
	walk = func(n *html.Node) *html.Node {
		if n == nil {
			return nil
		}
		if pred(n) {
			return n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if found := walk(c); found != nil {
				return found
			}
		}
		return nil
	}
	return walk(root)
}

func getAttr(n *html.Node, key string) (string, bool) {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val, true
		}
	}
	return "", false
}

func hasAttr(n *html.Node, key string) bool {
	_, ok := getAttr(n, key)
	return ok
}

func hasAttrs(n *html.Node, attrs map[string]string) bool {
	for key, val := range attrs {
		got, ok := getAttr(n, key)
		if !ok || got != val {
			return false
		}
	}
	return true
}

func findByID(root *html.Node, id string) *html.Node {
	return findElement(root, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		val, ok := getAttr(n, "id")
		return ok && val == id
	})
}

func hasClass(n *html.Node, class string) bool {
	val, ok := getAttr(n, "class")
	if !ok {
		return false
	}
	for _, candidate := range strings.Fields(val) {
		if candidate == class {
			return true
		}
	}
	return false
}

func hasClasses(n *html.Node, classes ...string) bool {
	for _, class := range classes {
		if !hasClass(n, class) {
			return false
		}
	}
	return true
}

func findByClass(root *html.Node, class string) *html.Node {
	return findElement(root, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		return hasClass(n, class)
	})
}

func textContains(root *html.Node, substring string) bool {
	var walk func(*html.Node) bool
	walk = func(n *html.Node) bool {
		if n.Type == html.TextNode && strings.Contains(n.Data, substring) {
			return true
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if walk(c) {
				return true
			}
		}
		return false
	}
	return walk(root)
}

func isLowerHex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
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
	doc := parseHTML(t, body)
	if !textContains(doc, "Ubuntu ISO") {
		t.Fatalf("expected torrent row, body=%s", body)
	}
	if findByID(doc, "toggle-selected") == nil {
		t.Fatalf("expected top-bar action buttons, body=%s", body)
	}
	addBtn := findByID(doc, "add-btn")
	if addBtn == nil || !hasClass(addBtn, "primary") || !textContains(addBtn, "Add") {
		t.Fatalf("expected add button enabled by default in add dialog, body=%s", body)
	}
	statusPreserve := findByID(doc, "status-preserve")
	if statusPreserve == nil || !hasAttr(statusPreserve, "hx-preserve") {
		t.Fatalf("expected preserved status wrapper, body=%s", body)
	}
	searchForm := findByID(doc, "search-form")
	if searchForm == nil || !hasClass(searchForm, "search-form") {
		t.Fatalf("expected search form in controls, body=%s", body)
	}
	searchQuery := findByID(doc, "search-query")
	if searchQuery == nil || !hasAttrs(searchQuery, map[string]string{"name": "q", "value": ""}) {
		t.Fatalf("expected search input bound to q param, body=%s", body)
	}
	row := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.Data != "tr" {
			return false
		}
		if !hasAttrs(n, map[string]string{
			"data-hash": "abc",
			"hx-get":    "/ui/dashboard?dir=desc&filter=all&selected=abc&sort=addedAt",
			"hx-swap":   "none",
		}) {
			return false
		}
		classVal, ok := getAttr(n, "class")
		return ok && classVal == ""
	})
	if row == nil {
		t.Fatalf("expected row selection to use fragment mode, body=%s", body)
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
	doc := parseHTML(t, body)
	filterButton := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		return hasAttrs(n, map[string]string{
			"data-filter": "downloading",
			"hx-get":      "/ui/dashboard?dir=desc&filter=downloading&selected=abc&sort=addedAt",
			"hx-target":   "#dashboard",
			"hx-swap":     "outerHTML",
		})
	})
	if filterButton == nil {
		t.Fatalf("expected filter button to refresh full dashboard, body=%s", body)
	}
	sortButton := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		return hasAttrs(n, map[string]string{
			"data-sort": "addedAt",
			"hx-get":    "/ui/dashboard?dir=asc&filter=all&selected=abc&sort=addedAt",
			"hx-target": "#dashboard",
			"hx-swap":   "outerHTML",
		})
	})
	if sortButton == nil {
		t.Fatalf("expected active sort button to refresh full dashboard, body=%s", body)
	}
	nameSortButton := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		return hasAttrs(n, map[string]string{
			"data-sort": "name",
			"hx-get":    "/ui/dashboard?dir=asc&filter=all&selected=abc&sort=name",
		})
	})
	if nameSortButton == nil {
		t.Fatalf("expected sort URL default direction for name, body=%s", body)
	}
	refreshButton := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		return hasAttrs(n, map[string]string{
			"id":        "refresh",
			"hx-get":    "/ui/dashboard?dir=desc&filter=all&selected=abc&sort=addedAt",
			"hx-target": "#dashboard",
			"hx-swap":   "outerHTML",
		})
	})
	if refreshButton == nil {
		t.Fatalf("expected refresh button to refresh full dashboard, body=%s", body)
	}
}

func TestDashboardSearchPreservesQueryInControlURLs(t *testing.T) {
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return []domain.Torrent{{Hash: "abc", Name: "Ubuntu ISO", State: "downloading"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ui/dashboard?q=ubuntu&filter=all&sort=addedAt&dir=desc&selected=abc", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	doc := parseHTML(t, body)
	searchQuery := findByID(doc, "search-query")
	if searchQuery == nil || !hasAttrs(searchQuery, map[string]string{"name": "q", "value": "ubuntu"}) {
		t.Fatalf("expected search input to keep current query, body=%s", body)
	}
	refreshButton := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		return hasAttrs(n, map[string]string{
			"id":     "refresh",
			"hx-get": "/ui/dashboard?dir=desc&filter=all&q=ubuntu&selected=abc&sort=addedAt",
		})
	})
	if refreshButton == nil {
		t.Fatalf("expected refresh URL to preserve query, body=%s", body)
	}
	filterButton := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		return hasAttrs(n, map[string]string{
			"data-filter": "downloading",
			"hx-get":      "/ui/dashboard?dir=desc&filter=downloading&q=ubuntu&selected=abc&sort=addedAt",
		})
	})
	if filterButton == nil {
		t.Fatalf("expected filter URL to preserve query, body=%s", body)
	}
	sortButton := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		return hasAttrs(n, map[string]string{
			"data-sort": "addedAt",
			"hx-get":    "/ui/dashboard?dir=asc&filter=all&q=ubuntu&selected=abc&sort=addedAt",
		})
	})
	if sortButton == nil {
		t.Fatalf("expected sort URL to preserve query, body=%s", body)
	}
}

func TestDashboardSearchMatchesTorrentNameOnly(t *testing.T) {
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return []domain.Torrent{
				{Hash: "deadbeef", Name: "Ubuntu ISO", State: "downloading"},
				{Hash: "cafebabe", Name: "Arch Linux", State: "seeding"},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ui/dashboard?q=ubuntu", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	doc := parseHTML(t, body)
	if !textContains(doc, "Ubuntu ISO") {
		t.Fatalf("expected torrent matched by name, body=%s", body)
	}
	if textContains(doc, "Arch Linux") {
		t.Fatalf("did not expect unmatched torrent in filtered results, body=%s", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/ui/dashboard?q=deadbeef", nil)
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	body = rr.Body.String()
	doc = parseHTML(t, body)
	if textContains(doc, "Ubuntu ISO") {
		t.Fatalf("did not expect hash-only match to pass name search, body=%s", body)
	}
	if !textContains(doc, "No torrents in this view") {
		t.Fatalf("expected empty-state placeholder for non-matching name search, body=%s", body)
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
	doc := parseHTML(t, body)
	downloadInput := findByID(doc, "download-limit-kib")
	if downloadInput == nil || !hasAttrs(downloadInput, map[string]string{"value": "2048"}) {
		t.Fatalf("expected download speed limit input value, body=%s", body)
	}
	uploadInput := findByID(doc, "upload-limit-kib")
	if uploadInput == nil || !hasAttrs(uploadInput, map[string]string{"value": "512"}) {
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
	doc := parseHTML(t, body)
	toggleButton := findByID(doc, "toggle-selected")
	if toggleButton == nil || !hasClass(toggleButton, "secondary") || !hasAttrs(toggleButton, map[string]string{"hx-post": "/ui/torrents/abc/stop"}) {
		t.Fatalf("expected selected running toggle button, body=%s", body)
	}
	recheckButton := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		return hasAttrs(n, map[string]string{"hx-post": "/ui/torrents/abc/recheck"})
	})
	if recheckButton == nil {
		t.Fatalf("expected selected recheck button, body=%s", body)
	}
	removeButton := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		return hasAttrs(n, map[string]string{"hx-post": "/ui/torrents/abc/remove"})
	})
	if removeButton == nil {
		t.Fatalf("expected selected remove button, body=%s", body)
	}
	if !hasAttrs(removeButton, map[string]string{"hx-include": "#view-state,#remove-delete-data"}) {
		t.Fatalf("expected remove button to include delete-data field, body=%s", body)
	}
	deleteDataInput := findByID(doc, "remove-delete-data")
	if deleteDataInput == nil || !hasAttrs(deleteDataInput, map[string]string{"name": "deleteData", "value": "true"}) {
		t.Fatalf("expected delete-data checkbox, body=%s", body)
	}
	if hasAttr(deleteDataInput, "disabled") {
		t.Fatalf("did not expect delete-data checkbox to be disabled when a torrent is selected, body=%s", body)
	}
}

func TestDashboardHTMXReturnsFragmentBundle(t *testing.T) {
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return []domain.Torrent{{Hash: "abc", Name: "Ubuntu ISO", State: "downloading"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ui/dashboard?selected=abc", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "toggle-selected")
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	doc := parseHTML(t, body)
	if findByID(doc, "dashboard") != nil {
		t.Fatalf("did not expect full dashboard for htmx fragment request, body=%s", body)
	}
	controlsPanel := findByID(doc, "controls-panel")
	if controlsPanel == nil || !hasClasses(controlsPanel, "controls", "card") || !hasAttrs(controlsPanel, map[string]string{"hx-swap-oob": "outerHTML"}) {
		t.Fatalf("expected controls fragment oob swap, body=%s", body)
	}
	fileList := findByID(doc, "file-list")
	if fileList == nil || !hasClasses(fileList, "table-panel", "card") || !hasAttrs(fileList, map[string]string{"hx-swap-oob": "outerHTML"}) {
		t.Fatalf("expected file-list fragment oob swap, body=%s", body)
	}
	viewState := findByID(doc, "view-state")
	if viewState == nil || !hasAttr(viewState, "hidden") || !hasAttrs(viewState, map[string]string{"hx-swap-oob": "outerHTML"}) {
		t.Fatalf("expected view-state fragment, body=%s", body)
	}
	globalStats := findByID(doc, "global-stats")
	if globalStats == nil || !hasClass(globalStats, "global-stats") || !hasAttrs(globalStats, map[string]string{"hx-swap-oob": "outerHTML"}) {
		t.Fatalf("expected stats fragment oob swap, body=%s", body)
	}
	if findByID(doc, "add-dialog") != nil {
		t.Fatalf("did not expect add-dialog fragment for view navigation, body=%s", body)
	}
	if findByID(doc, "form-message") != nil {
		t.Fatalf("did not expect status fragment for view navigation, body=%s", body)
	}
}

func TestDashboardCancelAddHTMXReturnsAddDialogOnly(t *testing.T) {
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return []domain.Torrent{{Hash: "abc", Name: "Ubuntu ISO", State: "downloading"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ui/dashboard?selected=abc", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "cancel-add")
	req.Header.Set("HX-Trigger", "cancel-add")
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	doc := parseHTML(t, body)
	if findByID(doc, "dashboard") != nil {
		t.Fatalf("did not expect full dashboard for htmx fragment request, body=%s", body)
	}
	addDialog := findByID(doc, "add-dialog")
	if addDialog == nil || !hasClass(addDialog, "add-dialog") || !hasAttrs(addDialog, map[string]string{"hx-swap-oob": "outerHTML"}) {
		t.Fatalf("expected add-dialog fragment oob swap, body=%s", body)
	}
	if addDialog != nil && hasAttr(addDialog, "open") {
		t.Fatalf("did not expect open add-dialog for cancel-add, body=%s", body)
	}
	if findByID(doc, "file-list") != nil {
		t.Fatalf("did not expect file-list fragment for cancel-add, body=%s", body)
	}
	if findByID(doc, "controls-panel") != nil {
		t.Fatalf("did not expect controls fragment for cancel-add, body=%s", body)
	}
	if findByID(doc, "form-message") != nil {
		t.Fatalf("did not expect status fragment for cancel-add, body=%s", body)
	}
	if findByID(doc, "global-stats") != nil {
		t.Fatalf("did not expect stats fragment for cancel-add, body=%s", body)
	}
	if findByID(doc, "view-state") != nil {
		t.Fatalf("did not expect view-state fragment for cancel-add, body=%s", body)
	}
}

func TestDashboardOpenAddHTMXReturnsOpenAddDialogOnly(t *testing.T) {
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return []domain.Torrent{{Hash: "abc", Name: "Ubuntu ISO", State: "downloading"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ui/dashboard?selected=abc", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "open-add")
	req.Header.Set("HX-Trigger", "open-add")
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	doc := parseHTML(t, body)
	if findByID(doc, "dashboard") != nil {
		t.Fatalf("did not expect full dashboard for htmx fragment request, body=%s", body)
	}
	addDialog := findByID(doc, "add-dialog")
	if addDialog == nil || !hasClass(addDialog, "add-dialog") || !hasAttrs(addDialog, map[string]string{"hx-swap-oob": "outerHTML"}) || !hasAttr(addDialog, "open") {
		t.Fatalf("expected open add-dialog fragment oob swap, body=%s", body)
	}
	if findByID(doc, "file-list") != nil {
		t.Fatalf("did not expect file-list fragment for open-add, body=%s", body)
	}
	if findByID(doc, "controls-panel") != nil {
		t.Fatalf("did not expect controls fragment for open-add, body=%s", body)
	}
	if findByID(doc, "form-message") != nil {
		t.Fatalf("did not expect status fragment for open-add, body=%s", body)
	}
	if findByID(doc, "global-stats") != nil {
		t.Fatalf("did not expect stats fragment for open-add, body=%s", body)
	}
	if findByID(doc, "view-state") != nil {
		t.Fatalf("did not expect view-state fragment for open-add, body=%s", body)
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
	doc := parseHTML(t, body)
	scriptTag := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.Data != "script" {
			return false
		}
		if !hasAttrs(n, map[string]string{"type": "module"}) {
			return false
		}
		if !hasAttr(n, "defer") {
			return false
		}
		src, ok := getAttr(n, "src")
		if !ok {
			return false
		}
		const prefix = "/ui.js?v="
		if !strings.HasPrefix(src, prefix) {
			return false
		}
		cacheKey := strings.TrimPrefix(src, prefix)
		return len(cacheKey) == 16 && isLowerHex(cacheKey)
	})
	if scriptTag == nil {
		t.Fatalf("expected cache-busted ui.js script tag, body=%s", body)
	}
	initialDashboard := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		return hasAttrs(n, map[string]string{"hx-get": "/ui/dashboard?dir=desc&filter=all&sort=addedAt"})
	})
	if initialDashboard == nil {
		t.Fatalf("expected default dashboard load URL, body=%s", body)
	}
}

func TestUIPageUsesPersistedViewCookiesForInitialDashboardURL(t *testing.T) {
	s, err := New(&mockService{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ui", nil)
	req.AddCookie(&http.Cookie{Name: cookieFilter, Value: "seeding"})
	req.AddCookie(&http.Cookie{Name: cookieSort, Value: "name"})
	req.AddCookie(&http.Cookie{Name: cookieDir, Value: "asc"})
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	doc := parseHTML(t, body)
	initialDashboard := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		return hasAttrs(n, map[string]string{"hx-get": "/ui/dashboard?dir=asc&filter=seeding&sort=name"})
	})
	if initialDashboard == nil {
		t.Fatalf("expected dashboard load URL from cookies, body=%s", body)
	}
}

func TestUIPageQueryParamsOverridePersistedViewCookies(t *testing.T) {
	s, err := New(&mockService{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ui?filter=downloading&sort=ratio&dir=desc", nil)
	req.AddCookie(&http.Cookie{Name: cookieFilter, Value: "seeding"})
	req.AddCookie(&http.Cookie{Name: cookieSort, Value: "name"})
	req.AddCookie(&http.Cookie{Name: cookieDir, Value: "asc"})
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	doc := parseHTML(t, body)
	initialDashboard := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		return hasAttrs(n, map[string]string{"hx-get": "/ui/dashboard?dir=desc&filter=downloading&sort=ratio"})
	})
	if initialDashboard == nil {
		t.Fatalf("expected query params to override cookies, body=%s", body)
	}
}

func TestEmptyEndpointRendersHiddenStatusPlaceholder(t *testing.T) {
	s, err := New(&mockService{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_empty", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	doc := parseHTML(t, body)
	if findByID(doc, "form-message") == nil {
		t.Fatalf("expected status placeholder, body=%s", body)
	}
	hiddenMessage := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		return hasClasses(n, "message-info", "is-hidden")
	})
	if hiddenMessage == nil {
		t.Fatalf("expected hidden placeholder message, body=%s", body)
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
	responseBody := rr.Body.String()
	doc := parseHTML(t, responseBody)
	if !textContains(doc, "Torrent added") {
		t.Fatalf("expected success flash, body=%s", responseBody)
	}
}

func TestAddTorrentRequiresMagnetOrFile(t *testing.T) {
	called := false
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return nil, nil
		},
		addMagnetFn: func(context.Context, string) error {
			called = true
			return nil
		},
		addFileFn: func(context.Context, []byte, string) error {
			called = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("filter", "all")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/ui/torrents", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if called {
		t.Fatalf("did not expect AddMagnet or AddTorrent call")
	}
	responseBody := rr.Body.String()
	doc := parseHTML(t, responseBody)
	if !textContains(doc, "provide a magnet link or a .torrent file") {
		t.Fatalf("expected missing input error, body=%s", responseBody)
	}
	if findByClass(doc, "add-form-error") == nil {
		t.Fatalf("expected inline add form error, body=%s", responseBody)
	}
	addDialog := findByID(doc, "add-dialog")
	if addDialog == nil || !hasClass(addDialog, "add-dialog") || !hasAttr(addDialog, "open") {
		t.Fatalf("expected add dialog to be open for validation error, body=%s", responseBody)
	}
	formMessageText := findByID(doc, "form-message-text")
	if formMessageText != nil && textContains(formMessageText, "provide a magnet link or a .torrent file") {
		t.Fatalf("did not expect global status message for add form validation error, body=%s", responseBody)
	}
}

func TestAddTorrentRequiresMagnetOrFileHTMXReturnsFragments(t *testing.T) {
	called := false
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return nil, nil
		},
		addMagnetFn: func(context.Context, string) error {
			called = true
			return nil
		},
		addFileFn: func(context.Context, []byte, string) error {
			called = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("filter", "all")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/ui/torrents", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "add-form")
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if called {
		t.Fatalf("did not expect AddMagnet or AddTorrent call")
	}
	responseBody := rr.Body.String()
	doc := parseHTML(t, responseBody)
	if findByID(doc, "dashboard") != nil {
		t.Fatalf("did not expect full dashboard for htmx fragment request, body=%s", responseBody)
	}
	if findByClass(doc, "add-form-error") == nil {
		t.Fatalf("expected inline add form error, body=%s", responseBody)
	}
	addDialog := findByID(doc, "add-dialog")
	if addDialog == nil || !hasClass(addDialog, "add-dialog") || !hasAttr(addDialog, "open") || !hasAttrs(addDialog, map[string]string{"hx-swap-oob": "outerHTML"}) {
		t.Fatalf("expected add-dialog fragment oob swap with open state, body=%s", responseBody)
	}
	if findByID(doc, "controls-panel") != nil {
		t.Fatalf("did not expect controls fragment for add-form validation response, body=%s", responseBody)
	}
	if findByID(doc, "form-message") != nil {
		t.Fatalf("did not expect status fragment for add-form validation response, body=%s", responseBody)
	}
	if findByID(doc, "view-state") != nil {
		t.Fatalf("did not expect view-state fragment for add-form validation response, body=%s", responseBody)
	}
}

func TestAddTorrentMagnetErrorShowsInlineFormError(t *testing.T) {
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return nil, nil
		},
		addMagnetFn: func(context.Context, string) error {
			return errors.New("invalid magnet URI")
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("magnet", "magnet:?xt=urn:btih:invalid")
	_ = writer.WriteField("filter", "all")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/ui/torrents", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	responseBody := rr.Body.String()
	doc := parseHTML(t, responseBody)
	if !textContains(doc, "invalid magnet URI") {
		t.Fatalf("expected magnet error message, body=%s", responseBody)
	}
	if findByClass(doc, "add-form-error") == nil {
		t.Fatalf("expected inline add form error, body=%s", responseBody)
	}
	addDialog := findByID(doc, "add-dialog")
	if addDialog == nil || !hasClass(addDialog, "add-dialog") || !hasAttr(addDialog, "open") {
		t.Fatalf("expected add dialog to be open for validation error, body=%s", responseBody)
	}
	magnetInput := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		return hasAttrs(n, map[string]string{
			"name":        "magnet",
			"placeholder": "magnet:?xt=urn:btih:...",
			"value":       "magnet:?xt=urn:btih:invalid",
		})
	})
	if magnetInput == nil {
		t.Fatalf("expected magnet field value to be preserved, body=%s", responseBody)
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
	body := rr.Body.String()
	doc := parseHTML(t, body)
	if !textContains(doc, "Speed limits updated") {
		t.Fatalf("expected success flash, body=%s", body)
	}
	autoDismiss := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		return hasClass(n, "message-autodismiss") && hasAttrs(n, map[string]string{"hx-trigger": "load delay:4s"})
	})
	if autoDismiss == nil {
		t.Fatalf("expected auto-dismiss marker for non-error flash, body=%s", body)
	}
}

func TestSetSpeedLimitsHTMXReturnsControlsAndStatusOnly(t *testing.T) {
	called := false
	s, err := New(&mockService{
		listFn: func(context.Context) ([]domain.Torrent, error) {
			return []domain.Torrent{{Hash: "abc", Name: "Ubuntu ISO", State: "downloading"}}, nil
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
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "speed-limit-form")
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatalf("expected SetSpeedLimits call")
	}
	body := rr.Body.String()
	doc := parseHTML(t, body)
	if findByID(doc, "dashboard") != nil {
		t.Fatalf("did not expect full dashboard for htmx fragment request, body=%s", body)
	}
	controlsPanel := findByID(doc, "controls-panel")
	if controlsPanel == nil || !hasClasses(controlsPanel, "controls", "card") || !hasAttrs(controlsPanel, map[string]string{"hx-swap-oob": "outerHTML"}) {
		t.Fatalf("expected controls fragment oob swap, body=%s", body)
	}
	if findByID(doc, "form-message") == nil {
		t.Fatalf("expected status fragment in response, body=%s", body)
	}
	if findByID(doc, "file-list") != nil {
		t.Fatalf("did not expect file-list fragment for speed limit update, body=%s", body)
	}
	if findByID(doc, "global-stats") != nil {
		t.Fatalf("did not expect stats fragment for speed limit update, body=%s", body)
	}
	if findByID(doc, "view-state") != nil {
		t.Fatalf("did not expect view-state fragment for speed limit update, body=%s", body)
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
	body := rr.Body.String()
	doc := parseHTML(t, body)
	if !textContains(doc, "download limit must be a non-negative integer") {
		t.Fatalf("expected validation error, body=%s", body)
	}
	if findByClass(doc, "message-autodismiss") != nil {
		t.Fatalf("did not expect auto-dismiss marker for error flash, body=%s", body)
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
	body := rr.Body.String()
	doc := parseHTML(t, body)
	if !textContains(doc, "Torrent removed") {
		t.Fatalf("expected success flash, body=%s", body)
	}
}

func TestTorrentActionRemoveWithDeleteData(t *testing.T) {
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
			if !deleteData {
				t.Fatalf("expected deleteData=true")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/ui/torrents/abc/remove", strings.NewReader("filter=all&sort=addedAt&dir=desc&deleteData=true"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatalf("expected Remove call")
	}
	body := rr.Body.String()
	doc := parseHTML(t, body)
	if !textContains(doc, "Torrent removed and data deleted") {
		t.Fatalf("expected success flash, body=%s", body)
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
	doc := parseHTML(t, body)
	if findByID(doc, "form-message") == nil {
		t.Fatalf("expected status bar in dashboard, body=%s", body)
	}
	hiddenMessage := findElement(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		return hasClasses(n, "message-info", "is-hidden")
	})
	if hiddenMessage == nil {
		t.Fatalf("expected steady backend ok status to be hidden, body=%s", body)
	}
	if textContains(doc, "Connected successfully to rTorrent: /run/rtorrent/rpc.sock") {
		t.Fatalf("expected no steady backend ok message in status bar, body=%s", body)
	}
}
