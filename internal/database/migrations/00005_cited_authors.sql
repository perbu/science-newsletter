-- +goose Up
CREATE TABLE cited_authors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    researcher_id INTEGER NOT NULL REFERENCES researchers(id) ON DELETE CASCADE,
    openalex_id TEXT NOT NULL,
    name TEXT NOT NULL,
    affiliation TEXT NOT NULL DEFAULT '',
    citation_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(researcher_id, openalex_id)
);

DROP TABLE IF EXISTS co_authors;

ALTER TABLE newsletter_items RENAME COLUMN is_coauthor_paper TO is_cited_author_paper;
ALTER TABLE newsletter_items RENAME COLUMN coauthor_name TO cited_author_name;

-- +goose Down
ALTER TABLE newsletter_items RENAME COLUMN is_cited_author_paper TO is_coauthor_paper;
ALTER TABLE newsletter_items RENAME COLUMN cited_author_name TO coauthor_name;

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

DROP TABLE IF EXISTS cited_authors;
