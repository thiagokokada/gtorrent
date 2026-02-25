# AGENTS.md

This file provides guidance for coding agents and contributors working in this repository.

## Scope

- Applies to the whole repository.
- Prefer minimal, targeted changes.

## Project Overview

- `gtorrent` is a Go application with a web UI to control rTorrent.
- Backend serves API + static frontend assets.
- Frontend is plain HTML/CSS/JavaScript (no Node build step).

## Structure

- `cmd/gtorrent`: application entrypoint
- `internal/config`: configuration loading/validation
- `internal/domain`: shared domain models
- `internal/rtorrent`: rTorrent service + XML-RPC mapping
- `internal/rtorrent/transport`: transport abstractions and implementations
- `internal/server`: HTTP handlers and embedded static files
- `internal/server/static`: frontend assets

## Working Rules

- Keep package boundaries clean; prefer interfaces at integration boundaries.
- Do not introduce frontend build tooling.
- Keep UI changes compatible with modern Chrome/Firefox.
- Prefer server-side/htmx solutions for UI behavior first.
- Add client-side JavaScript only when there is no viable server-side/htmx approach.
- Example of valid client-side-only logic: connection/disconnection handling, since a disconnected client cannot rely on a server-rendered update.
- Keep form validation server-side; on validation errors, preserve submitted values when possible and render errors close to the related form controls.
- Keep edits ASCII unless file already requires Unicode.

## UI Fragment Contract

- Keep UI fragment ownership explicit:
  - `controls`: actions, filters/sort controls, speed-limit form, add dialog and add-form inline errors.
  - `status`: global notification area only.
  - `file-list`: torrent table/list only.
  - `stats`: global rate/count summary only.
  - `view-state`: hidden navigation/filter/sort/selection state inputs only.
- For endpoint changes, preserve action-to-fragment boundaries:
  - Add form validation errors should update `controls` only.
  - Speed limit updates should update `controls` + `status`.
  - Torrent start/stop/recheck/remove should update `file-list` + `controls` + `status` (+ `stats` when totals may change).
  - Filter/sort/select/refresh should update `file-list` + `controls` (+ `stats` when totals may change).

## Validation

- Run tests before finishing changes:
  - `CGO_ENABLED=0 GOCACHE=/tmp/go-cache go test ./...`
- Template/handler behavior changes should include or update assertions in `internal/server/server_test.go`.

## Commits

- Use clear, scoped commit messages.
- Commit messages must use sentence case (capitalize only the first word, except proper nouns/acronyms).
- Avoid mixing unrelated changes in the same commit.
