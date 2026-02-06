-- +goose Up
CREATE TABLE researchers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    openalex_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    affiliation TEXT NOT NULL DEFAULT '',
    h_index INTEGER NOT NULL DEFAULT 0,
    works_count INTEGER NOT NULL DEFAULT 0,
    cited_by_count INTEGER NOT NULL DEFAULT 0,
    relevancy_threshold REAL NOT NULL DEFAULT 0.5,
    last_synced_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE publications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    openalex_id TEXT NOT NULL UNIQUE,
    researcher_id INTEGER NOT NULL REFERENCES researchers(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    publication_date TEXT NOT NULL DEFAULT '',
    doi TEXT NOT NULL DEFAULT '',
    cited_by_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE co_authors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    researcher_id INTEGER NOT NULL REFERENCES researchers(id) ON DELETE CASCADE,
    openalex_id TEXT NOT NULL,
    name TEXT NOT NULL,
    affiliation TEXT NOT NULL DEFAULT '',
    collaboration_count INTEGER NOT NULL DEFAULT 1,
    last_collaborated TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(researcher_id, openalex_id)
);

CREATE TABLE topics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    researcher_id INTEGER NOT NULL REFERENCES researchers(id) ON DELETE CASCADE,
    openalex_id TEXT NOT NULL,
    name TEXT NOT NULL,
    subfield TEXT NOT NULL DEFAULT '',
    field TEXT NOT NULL DEFAULT '',
    domain TEXT NOT NULL DEFAULT '',
    score REAL NOT NULL DEFAULT 0.0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(researcher_id, openalex_id)
);

CREATE TABLE newsletter_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    researcher_id INTEGER NOT NULL REFERENCES researchers(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending',
    papers_found INTEGER NOT NULL DEFAULT 0,
    papers_included INTEGER NOT NULL DEFAULT 0,
    html_content TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP
);

CREATE TABLE newsletter_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    newsletter_run_id INTEGER NOT NULL REFERENCES newsletter_runs(id) ON DELETE CASCADE,
    openalex_id TEXT NOT NULL,
    title TEXT NOT NULL,
    authors TEXT NOT NULL,
    publication_date TEXT NOT NULL DEFAULT '',
    doi TEXT NOT NULL DEFAULT '',
    relevancy_score REAL NOT NULL DEFAULT 0.0,
    summary TEXT NOT NULL DEFAULT '',
    is_coauthor_paper INTEGER NOT NULL DEFAULT 0,
    coauthor_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE newsletter_items;
DROP TABLE newsletter_runs;
DROP TABLE topics;
DROP TABLE co_authors;
DROP TABLE publications;
DROP TABLE researchers;
