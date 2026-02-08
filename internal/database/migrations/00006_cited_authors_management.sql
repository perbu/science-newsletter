-- +goose Up
ALTER TABLE cited_authors ADD COLUMN source TEXT NOT NULL DEFAULT 'openalex_sync';
ALTER TABLE cited_authors ADD COLUMN active INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE cited_authors DROP COLUMN active;
ALTER TABLE cited_authors DROP COLUMN source;
