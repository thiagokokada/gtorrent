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
- Keep edits ASCII unless file already requires Unicode.

## Validation

- Run tests before finishing changes:
  - `CGO_ENABLED=0 GOCACHE=/tmp/go-cache go test ./...`

## Commits

- Use clear, scoped commit messages.
- Avoid mixing unrelated changes in the same commit.
