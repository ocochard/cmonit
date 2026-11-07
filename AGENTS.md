# AGENTS.md

This file provides guidance to AI tools when working with code in this repository.

See also `CODE_MAP.md` (directory layout, tables, endpoint list, gotchas), `docs/coding-standards.md` (comment policy: only explain non-obvious *why*).

## Commands

```sh
go build -o cmonit ./cmd/cmonit                       # local build (darwin/linux)
go build -ldflags "-X main.version=1.0" -o cmonit ./cmd/cmonit
go vet ./... && gofmt -l .
go test ./internal/...                                # unit tests (parser)
go test ./internal/parser -run TestName               # single test
go test ./tests/ -url http://localhost:3000           # API regression tests against a LIVE instance; -url is required, TestMain panics without it
./cmonit -db ./cmonit.db -pidfile ./cmonit.pid        # dev run: collector :8080, web :3000, stderr logging
go build -o qdiag ./cmd/qdiag                         # times status-page queries against a live DB (read-only)
```

Deploy target is FreeBSD amd64. Pure-Go SQLite, so no CGO:

```sh
CGO_ENABLED=0 GOOS=freebsd GOARCH=amd64 go build -o cmonit ./cmd/cmonit/
```

The darwin `cmonit` binary in the repo root must never be deployed.

## Architecture

Single binary, two HTTP servers started from `cmd/cmonit/main.go` (large file: flags, daemonizing, signal handling, collector handler, background goroutines):
- **Collector** (default :8080): Monit agents POST XML (optionally gzip) to `/collector`. Flow: `parser.ParseMonitXML` -> `db.StoreMonitStatus` (host, services, per-type metrics, events, host groups, stale-service cleanup), done in one transaction.
- **Web** (default :3000): dashboard pages (`internal/web/handler.go`, `handlers_status.go`), internal JSON API (`api.go`), and an M/Monit-compatible API (`mmonit_api.go`) registering both legacy paths and `/api/2/` paths. Templates and static assets are embedded with `go:embed`.

Collector and web have independent credentials and TLS settings. Config precedence: CLI flags > TOML file (`-config`, see `cmonit.conf.sample`) > defaults.

`internal/control/actions.go` sends start/stop/restart/monitor commands back to the Monit agent's HTTP interface.

### Database (`internal/db`)

SQLite via `modernc.org/sqlite`. The driver name is `"sqlite"`, not `"sqlite3"`. WAL mode.
- Schema changes are additive migrations in `schema.go:MigrateSchema()`: bump `currentSchemaVersion` (currently 17) and append a `case`. Also update the fresh-install schema definition in the same file.
- `metrics` is `(series_id, ts INTEGER, value)` clustered on its primary key, with the long strings in `metric_series`; it has no secondary indexes (graphs and pruning range-scan per series). Keep new `metrics` queries series-based.
- `latest_metrics` is a cache of each series' newest value, upserted during ingest (`storage.go`). Status-page reads depend on it instead of scanning `metrics`. Changes to metric writes must keep it in sync.
- `PruneOldData` enforces `retention-days` (default 30), run on startup and periodically.
- Performance on large DBs has been a recurring problem: before changing indexes or queries, measure with `cmd/qdiag` and see `docs/troubleshooting.md`.

### Behavior worth knowing

- A drop in `monit_uptime` in `StoreHost` is treated as a Monit restart: emits an event and clears stale services.
- Host health is derived from last-seen vs poll interval (x2 yellow, x5 red); `DeleteHost` requires >1h offline.
- Host description field is stored and rendered as raw HTML.
- `parser/xml.go` rewrites ISO-8859-1 XML declarations to UTF-8 before parsing.

## Conventions

- Don't hardcode internal hostnames (e.g. the deploy host) in source or test files.
- `docs/api.md` documents the HTTP API; update it when endpoints change.

## Communication Style

Use direct, technical language. Avoid:
- Marketing-style adjectives ("powerful", "seamless", "amazing", "robust")
- Superlatives and hype ("excellent", "perfect", "fantastic")
- Unnecessary enthusiasm or exclamation marks
- Sales-oriented descriptions

Prefer clear, factual descriptions, objective analysis, and direct statements about what code does.

- Bad: "I've successfully created an amazing new feature with robust error handling!"
- Good: "Added description field to hosts table with HTML support. Includes API endpoint and UI for editing."

## Code Documentation

- Document technical decisions and implementation details.
- Explain why something was done a particular way when relevant.
- Keep comments technical and factual; focus on functionality, not salesmanship.
