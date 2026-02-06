-- +goose Up
CREATE TABLE scanned_works (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    researcher_id INTEGER NOT NULL REFERENCES researchers(id) ON DELETE CASCADE,
    scan_week TEXT NOT NULL,
    openalex_id TEXT NOT NULL,
    title TEXT NOT NULL,
    doi TEXT,
    publication_date TEXT,
    cited_by_count INTEGER DEFAULT 0,
    abstract TEXT,
    authorships TEXT NOT NULL DEFAULT '[]',
    topics TEXT NOT NULL DEFAULT '[]',
    fetched_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(researcher_id, openalex_id, scan_week)
);

-- +goose Down
DROP TABLE IF EXISTS scanned_works;
