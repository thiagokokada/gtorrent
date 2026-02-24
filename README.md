# gTorrent

`gTorrent` is a Go web app for controlling rTorrent using XML-RPC over:

- Unix socket (SCGI)
- HTTP endpoint

No Node.js or frontend build tooling is required. The UI is static HTML/CSS/JS served by the Go binary.

## Features

- List torrents with progress, state, download/upload rates
- Add torrents from magnet links
- Add torrents from `.torrent` files
- Remove torrents
- Live table/stat updates over SSE
- Works in modern Chrome and Firefox

## Project Layout

- `cmd/assetgen`: `go generate` helper that downloads and checksum-verifies vendored frontend assets
- `cmd/gtorrent`: application entrypoint
- `internal/config`: env/flag configuration
- `internal/domain`: shared domain models
- `internal/rtorrent`: rTorrent service implementation
- `internal/rtorrent/xmlrpc`: XML-RPC codec and client interface
- `internal/rtorrent/transport/http`: HTTP XML-RPC transport
- `internal/rtorrent/transport/scgi`: Unix SCGI transport
- `internal/server`: HTTP handlers + static/htmx UI rendering
- `internal/server/templates`: HTML templates for dynamic UI fragments

## Quick Start

### 1. Build

```bash
go build -o gtorrent ./cmd/gtorrent
```

### 2. Run with Unix socket

```bash
GTORRENT_MODE=unix \
GTORRENT_UNIX_SOCKET=/path/to/rtorrent.sock \
GTORRENT_LISTEN=:8080 \
./gtorrent
```

### 3. Run with HTTP endpoint

```bash
GTORRENT_MODE=http \
GTORRENT_HTTP_URL=http://127.0.0.1/RPC2 \
GTORRENT_HTTP_USER=user \
GTORRENT_HTTP_PASS=pass \
GTORRENT_LISTEN=:8080 \
./gtorrent
```

The app opens `http://localhost:8080` in your default browser on startup unless disabled.

## Configuration

Flags and env vars are both supported:

- `--listen` / `GTORRENT_LISTEN` (default `:8080`)
- `--open-browser` / `GTORRENT_OPEN_BROWSER` (default `true`)
- `--no-open-browser` (always disables browser auto-open)
- `--verbose` / `GTORRENT_VERBOSE` (default `false`, enables debug logs)
- `NO_COLOR=1` disables tint colors in logs
- `--rtorrent-mode` / `GTORRENT_MODE` (`unix` or `http`)
- `--rtorrent-socket` / `GTORRENT_UNIX_SOCKET`
- `--rtorrent-http-url` / `GTORRENT_HTTP_URL`
- `--rtorrent-http-user` / `GTORRENT_HTTP_USER`
- `--rtorrent-http-pass` / `GTORRENT_HTTP_PASS`

## UI Endpoints

- `GET /` (htmx shell)
- `GET /ui` (same shell as `/`)
- `GET /ui/dashboard`
- `POST /ui/torrents`
- `POST /ui/torrents/{hash}/{action}` where action is `start`, `stop`, `recheck`, or `remove`
- `GET /ui/stream` (SSE updates for table/stats fragments)

## Testing

```bash
CGO_ENABLED=0 GOCACHE=/tmp/go-cache go test ./...
```

## Frontend Assets

Frontend dependencies are vendored under `internal/server/static/vendor` so the app works without internet access.

Refresh/check these files with:

```bash
CGO_ENABLED=0 GOCACHE=/tmp/go-cache go generate ./internal/server
```

## Notes

- `deleteData=true` is best-effort; the current implementation runs stop/close before erase.
- rTorrent XML-RPC methods can vary by setup. If your deployment uses different method names, adapt `internal/rtorrent/client.go`.
