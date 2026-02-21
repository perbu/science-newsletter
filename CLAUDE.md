# Science Newsletter

## What This Is

A monthly academic newsletter agent. Given a researcher, it syncs their profile from OpenAlex (publications, frequently
cited authors from referenced works), scans for recent papers relevant to their work, uses Google Gemini to generate
contextual summaries, and produces an HTML newsletter. There's an HTMX frontend for managing researchers and triggering
scans.

The target user is a non-technical person (my wife, a researcher) who wants a monthly digest of papers relevant to her
field without manually trawling OpenAlex or Google Scholar. I have a few more friends which will be interested as
well. This is mostly a community service.

## Tech Stack

- **Go** with stdlib `net/http` (Go 1.24+ routing patterns)
- **SQLite** via `modernc.org/sqlite` (pure Go, no CGO)
- **sqlc** for type-safe SQL queries (`go tool sqlc generate`)
- **goose** for migrations (embedded via `//go:embed`)
- **Google Gemini** via `google.golang.org/genai` for paper enrichment
- **Resend** via `github.com/resend/resend-go/v2` for email delivery
- **OpenAlex REST API** for academic data (we have our own thin client)
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
Tables: `researchers`, `publications`, `cited_authors`, `topics`, `scanned_works`, `newsletter_runs`, `newsletter_items`.

After editing `internal/database/queries/*.sql`, run `go tool sqlc generate`. Never edit files in
`internal/database/db/` — they are generated.

## How the Scanner Works

The scanner is split into two independent phases so you can re-analyze cached data without re-fetching from OpenAlex.

### Fetch phase (`POST /researchers/{id}/fetch`)

1. Check the researcher's **field/subfield selections** (`researcher_field_selections` table)
2. If selections exist: resolve to subfield IDs (expanding field-level selections) and query OpenAlex with
   `topics.subfield.id:SF1|SF2,from_publication_date:YYYY-MM-DD`
3. If no selections exist: fall back to topic-based search using researcher's `topics` table
4. Also search for recent works by top cited authors (from `cited_authors` table)
5. Deduplicate, reconstruct abstracts from inverted index, marshal authorships/topics to JSON
6. Upsert all works into `scanned_works` table, keyed by `(researcher_id, openalex_id, scan_month)`

### Analyze phase (`POST /researchers/{id}/analyze`)

1. Load cached works from `scanned_works` for the previous calendar month
2. Score each paper: `sum(matching topic shares) / sum(search topic shares)` — range [0,1]
3. Papers above the researcher's `relevancy_threshold` are included; cited author papers always included
4. Gemini generates a one-sentence summary for each included paper
5. Results stored in `newsletter_runs` + `newsletter_items`, HTML rendered from template

Both phases always target the **previous** calendar month (e.g. running in February covers January). This means you
can run on the 1st of the month and get a complete period. The `scan_month` column stores the year-month
(e.g. `"2026-01"`). Re-fetching the same month refreshes the cached data. Re-analyzing re-scores and re-enriches
without hitting OpenAlex.

### Open questions

The fetch phase now casts a wider net via subfields, but the analyze phase still scores against the researcher's
`topics` table (populated by OpenAlex profile sync). This means filtering precision depends on having good topics.
How to best fetch and filter/score papers is an active area of exploration.

## Logging

Uses `log/slog` throughout. Set `LOG_LEVEL=debug` in `.env` for verbose output including per-paper scoring, OpenAlex
request timing, and Gemini token usage. Default level is `info`.

## Search Scope UI

The researcher detail page has a "Search Scope" section where the user selects OpenAlex subfields to search. This
drives the fetch phase directly — selected subfields become the `topics.subfield.id` filter in OpenAlex queries.

- **Subfield search box**: type-ahead search over `openalex_topics` subfield names (LIKE query, 300ms debounce)
- **Browse hierarchy**: collapsible Domain > Field > Subfield tree (lazy-loaded on open)
- Selections stored in `researcher_field_selections` table with `level` ("field" or "subfield") and `openalex_id`
- Topic management UI was removed — topics come from OpenAlex profile sync only and are used for scoring in the
  analyze phase

## Primary User's Research Interests

The primary user researches **teaching quality** and **instructional quality**, which don't map neatly to existing
OpenAlex topics. Key sub-areas:

- **Classroom management**
- **Cognitive activation**
- **Supportive climate / supportive teaching**

Her interests span multiple OpenAlex subfields (primarily Education within Social Sciences) and don't have direct
one-to-one representations in the taxonomy.

## Deployment

Runs as a systemd user service (`~/.config/systemd/user/science-newsletter.service`). Linger is enabled so it starts
at boot without a login session.

```bash
go build -o science-newsletter ./cmd/server/   # rebuild binary
systemctl --user restart science-newsletter     # restart after rebuild
systemctl --user status science-newsletter      # check status
journalctl --user -u science-newsletter -f      # tail logs
```

## Architecture Decisions

- **genai directly, not ADK**: The Google ADK framework is designed for conversational agent servers with
  sessions/events. We just need batch LLM calls, so we use `google.golang.org/genai` directly.
- **Template rendering**: Go's `html/template` with the layout+page pattern. Each page template is parsed together with
  `layout.html.tmpl` individually to avoid `{{define "content"}}` collisions.
- **No JSON API**: The frontend is pure HTMX — server returns HTML fragments. No separate API layer.
- **Subfield-based search**: Fetch phase queries OpenAlex by subfield IDs from the researcher's field selections,
  not individual topics. This gives broader coverage at the cost of precision — filtering is deferred to the analyze
  phase.
- **Fetch/analyze split**: OpenAlex fetching and scoring/enrichment are separate phases. Fetched works are cached in
  `scanned_works` with JSON columns for authorships and topics. This allows threshold tweaks and prompt changes without
  re-fetching hundreds of papers from OpenAlex.
- **Cited authors over co-authors**: During researcher sync, all referenced works from the researcher's publications
  are batch-fetched to extract author citation frequencies. The most frequently cited authors represent deliberate
  intellectual engagement and are better signals for interesting content than co-authors. These cited authors are
  used for paper discovery (fetch phase) and threshold bypass (analyze phase).
