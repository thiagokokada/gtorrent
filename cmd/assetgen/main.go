package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type asset struct {
	URL      string
	RelPath  string
	SHA256   string
	MaxBytes int64
}

var assets = []asset{
	{
		URL:      "https://unpkg.com/htmx.org@2.0.8/dist/htmx.min.js",
		RelPath:  "internal/server/static/vendor/htmx-2.0.8.min.js",
		SHA256:   "22283ef68cb7545914f0a88a1bdedc7256a703d1d580c1d255217d0a50d31313",
		MaxBytes: 1 << 20,
	},
	{
		URL:      "https://unpkg.com/htmx-ext-sse@2.2.4/dist/sse.min.js",
		RelPath:  "internal/server/static/vendor/htmx-sse-2.2.4.min.js",
		SHA256:   "98a46496de0c3605fbffdce9167ba427bdd9553184f83f149c261891a92c0136",
		MaxBytes: 1 << 20,
	},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "assetgen: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := findRepoRoot()
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 45 * time.Second}
	for _, item := range assets {
		if err := syncAsset(client, root, item); err != nil {
			return err
		}
	}
	return nil
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("could not find repository root (missing go.mod)")
		}
		dir = parent
	}
}

func syncAsset(client *http.Client, root string, item asset) error {
	data, err := downloadAsset(client, item)
	if err != nil {
		return err
	}

	destPath := filepath.Join(root, filepath.FromSlash(item.RelPath))
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(item.RelPath), err)
	}

	if current, err := os.ReadFile(destPath); err == nil && bytes.Equal(current, data) {
		fmt.Printf("asset up-to-date: %s\n", item.RelPath)
		return nil
	}

	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", item.RelPath, err)
	}
	fmt.Printf("asset updated: %s\n", item.RelPath)
	return nil
}

func downloadAsset(client *http.Client, item asset) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, item.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", item.URL, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", item.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: unexpected status %s", item.URL, resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, item.MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", item.URL, err)
	}
	if int64(len(data)) > item.MaxBytes {
		return nil, fmt.Errorf("asset %s exceeds size limit (%d bytes)", item.URL, item.MaxBytes)
	}

	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	expected := strings.ToLower(strings.TrimSpace(item.SHA256))
	if actual != expected {
		return nil, fmt.Errorf("checksum mismatch for %s: expected %s, got %s", item.URL, expected, actual)
	}

	return data, nil
}
