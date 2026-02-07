-- +goose Up

-- Global mirror of all ~4,516 OpenAlex topics
CREATE TABLE openalex_topics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    openalex_id TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    keywords TEXT NOT NULL DEFAULT '[]',     -- JSON array of strings
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

-- Free-text research interests on researcher
ALTER TABLE researchers ADD COLUMN research_interests TEXT NOT NULL DEFAULT '';

-- Which fields/subfields a researcher has selected for topic mapping
CREATE TABLE researcher_field_selections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    researcher_id INTEGER NOT NULL REFERENCES researchers(id) ON DELETE CASCADE,
    level TEXT NOT NULL CHECK(level IN ('field', 'subfield')),
    openalex_id TEXT NOT NULL,
    display_name TEXT NOT NULL,
    UNIQUE(researcher_id, level, openalex_id)
);

-- +goose Down
DROP TABLE IF EXISTS researcher_field_selections;
ALTER TABLE researchers DROP COLUMN research_interests;
DROP TABLE IF EXISTS openalex_topics;
