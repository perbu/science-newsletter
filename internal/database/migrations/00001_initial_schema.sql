-- +goose Up
CREATE TABLE researchers (
    id TEXT PRIMARY KEY,
    openalex_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    affiliation TEXT NOT NULL DEFAULT '',
    h_index INTEGER NOT NULL DEFAULT 0,
    works_count INTEGER NOT NULL DEFAULT 0,
    cited_by_count INTEGER NOT NULL DEFAULT 0,
    relevancy_threshold REAL NOT NULL DEFAULT 0.5,
    last_synced_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    research_interests TEXT NOT NULL DEFAULT ''
);

CREATE TABLE publications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    openalex_id TEXT NOT NULL UNIQUE,
    researcher_id TEXT NOT NULL REFERENCES researchers(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    publication_date TEXT NOT NULL DEFAULT '',
    doi TEXT NOT NULL DEFAULT '',
    cited_by_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE topics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    researcher_id TEXT NOT NULL REFERENCES researchers(id) ON DELETE CASCADE,
    openalex_id TEXT NOT NULL,
    name TEXT NOT NULL,
    subfield TEXT NOT NULL DEFAULT '',
    field TEXT NOT NULL DEFAULT '',
    domain TEXT NOT NULL DEFAULT '',
    score REAL NOT NULL DEFAULT 0.0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    source TEXT NOT NULL DEFAULT 'openalex',
    UNIQUE(researcher_id, openalex_id)
);

CREATE TABLE cited_authors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    researcher_id TEXT NOT NULL REFERENCES researchers(id) ON DELETE CASCADE,
    openalex_id TEXT NOT NULL,
    name TEXT NOT NULL,
    affiliation TEXT NOT NULL DEFAULT '',
    citation_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    source TEXT NOT NULL DEFAULT 'openalex_sync',
    active INTEGER NOT NULL DEFAULT 1,
    UNIQUE(researcher_id, openalex_id)
);

CREATE TABLE scanned_works (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    researcher_id TEXT NOT NULL REFERENCES researchers(id) ON DELETE CASCADE,
    scan_month TEXT NOT NULL,
    openalex_id TEXT NOT NULL,
    title TEXT NOT NULL,
    doi TEXT,
    publication_date TEXT,
    cited_by_count INTEGER DEFAULT 0,
    abstract TEXT,
    authorships TEXT NOT NULL DEFAULT '[]',
    topics TEXT NOT NULL DEFAULT '[]',
    fetched_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    source_name TEXT NOT NULL DEFAULT '',
    source_citedness REAL NOT NULL DEFAULT 0,
    UNIQUE(researcher_id, openalex_id, scan_month)
);

CREATE TABLE newsletter_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    researcher_id TEXT NOT NULL REFERENCES researchers(id) ON DELETE CASCADE,
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
    is_cited_author_paper INTEGER NOT NULL DEFAULT 0,
    cited_author_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE openalex_topics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    openalex_id TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    keywords TEXT NOT NULL DEFAULT '[]',
    subfield_id TEXT NOT NULL DEFAULT '',
    subfield_name TEXT NOT NULL DEFAULT '',
    field_id TEXT NOT NULL DEFAULT '',
    field_name TEXT NOT NULL DEFAULT '',
    domain_id TEXT NOT NULL DEFAULT '',
    domain_name TEXT NOT NULL DEFAULT '',
    works_count INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_openalex_topics_field ON openalex_topics(field_id);
CREATE INDEX idx_openalex_topics_subfield ON openalex_topics(subfield_id);

CREATE TABLE researcher_field_selections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    researcher_id TEXT NOT NULL REFERENCES researchers(id) ON DELETE CASCADE,
    level TEXT NOT NULL CHECK(level IN ('field', 'subfield')),
    openalex_id TEXT NOT NULL,
    display_name TEXT NOT NULL,
    UNIQUE(researcher_id, level, openalex_id)
);

-- +goose Down
DROP TABLE researcher_field_selections;
DROP TABLE openalex_topics;
DROP TABLE newsletter_items;
DROP TABLE newsletter_runs;
DROP TABLE scanned_works;
DROP TABLE cited_authors;
DROP TABLE topics;
DROP TABLE publications;
DROP TABLE researchers;
