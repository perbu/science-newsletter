# Science Newsletter

## What This Is

A weekly academic newsletter agent. Given a researcher, it syncs their profile from OpenAlex (publications, co-authors,
research topics), scans for recent papers relevant to their work, uses Google Gemini to generate contextual summaries,
and produces an HTML newsletter. There's an HTMX frontend for managing researchers and triggering scans.

The target user is a non-technical person (my wife, a researcher) who wants a weekly digest of papers relevant to her
field without manually trawling OpenAlex or Google Scholar.

## Tech Stack

- **Go** with stdlib `net/http` (Go 1.24+ routing patterns)
- **SQLite** via `modernc.org/sqlite` (pure Go, no CGO)
- **sqlc** for type-safe SQL queries (`go tool sqlc generate`)
- **goose** for migrations (embedded via `//go:embed`)
- **Google Gemini** via `google.golang.org/genai` for paper enrichment
- **Resend** via `github.com/resend/resend-go/v2` for email delivery
- **OpenAlex REST API** for academic data (no Go library exists, we have a thin client)
- **HTMX + Tailwind CSS** (both via CDN) for the frontend

## Project Layout

```
cmd/server/main.go          — entry point, wiring
internal/
  config/                    — YAML config + .env loading
  database/
    migrations/              — goose SQL migrations
    queries/                 — sqlc query definitions (.sql)
    db/                      — sqlc generated code (DO NOT EDIT)
    migrate.go               — embedded migration runner
  openalex/                  — OpenAlex REST client + response types
  sync/                      — sync researcher profile from OpenAlex -> SQLite
  scanner/                   — find recent papers, score relevancy
  agent/                     — Gemini enrichment (generate paper summaries)
  email/                     — Resend email client (send newsletters)
  newsletter/                — render HTML newsletter from items
  web/                       — HTTP handlers, routes, HTMX templates
```

## Key Commands

```bash
go run ./cmd/server/              # start the server
go tool sqlc generate             # regenerate db code after editing queries/*.sql
go build ./...                    # build check
go vet ./...                      # lint
```

## Configuration

`config.yaml` has all settings. Secrets go in `.env` (not committed):

```
GEMINI_API_KEY=...
OPENALEX_API_KEY=...
OPENALEX_EMAIL=...
RESEND_API_KEY=...
LOG_LEVEL=debug
```

Environment variables override config.yaml values. The `.env` file is loaded before env var overrides are applied.

## Database

SQLite with WAL mode and foreign keys enabled. Schema managed by goose migrations in `internal/database/migrations/`.
Tables: `researchers`, `publications`, `co_authors`, `topics`, `scanned_works`, `newsletter_runs`, `newsletter_items`.

After editing `internal/database/queries/*.sql`, run `go tool sqlc generate`. Never edit files in
`internal/database/db/` — they are generated.

## How the Scanner Works

The scanner is split into two independent phases so you can re-analyze cached data without re-fetching from OpenAlex.

### Fetch phase (`POST /researchers/{id}/fetch`)
1. Load the researcher's top N topics (by share score) from the DB
2. Query OpenAlex for works published in the last `lookback_days` matching those topic IDs
3. Deduplicate, reconstruct abstracts from inverted index, marshal authorships/topics to JSON
4. Upsert all works into `scanned_works` table, keyed by `(researcher_id, openalex_id, scan_week)`

### Analyze phase (`POST /researchers/{id}/analyze`)
1. Load cached works from `scanned_works` for the current ISO week
2. Score each paper: `sum(matching topic shares) / sum(search topic shares)` — range [0,1]
3. Papers above the researcher's `relevancy_threshold` are included; co-author papers always included
4. Gemini generates a one-sentence summary for each included paper
5. Results stored in `newsletter_runs` + `newsletter_items`, HTML rendered from template

All scans align to ISO weeks (Monday–Sunday). The `scan_week` column stores the Monday date (e.g. `"2026-02-02"`).
Re-fetching the same week refreshes the cached data. Re-analyzing re-scores and re-enriches without hitting OpenAlex.

## Logging

Uses `log/slog` throughout. Set `LOG_LEVEL=debug` in `.env` for verbose output including per-paper scoring, OpenAlex
request timing, and Gemini token usage. Default level is `info`.

## Architecture Decisions

- **genai directly, not ADK**: The Google ADK framework is designed for conversational agent servers with
  sessions/events. We just need batch LLM calls, so we use `google.golang.org/genai` directly.
- **Template rendering**: Go's `html/template` with the layout+page pattern. Each page template is parsed together with
  `layout.html.tmpl` individually to avoid `{{define "content"}}` collisions.
- **No JSON API**: The frontend is pure HTMX — server returns HTML fragments. No separate API layer.
- **Scoring uses search topics only**: The relevancy denominator is the sum of the top N search topics, not all
  researcher topics. This prevents scores from being diluted when a researcher has many niche topics.
- **Fetch/analyze split**: OpenAlex fetching and scoring/enrichment are separate phases. Fetched works are cached in
  `scanned_works` with JSON columns for authorships and topics. This allows threshold tweaks and prompt changes without
  re-fetching hundreds of papers from OpenAlex.
