-- +goose Up
ALTER TABLE scanned_works ADD COLUMN source_name TEXT NOT NULL DEFAULT '';
ALTER TABLE scanned_works ADD COLUMN source_citedness REAL NOT NULL DEFAULT 0;
ALTER TABLE topics ADD COLUMN source TEXT NOT NULL DEFAULT 'openalex';

-- +goose Down
ALTER TABLE scanned_works DROP COLUMN source_name;
ALTER TABLE scanned_works DROP COLUMN source_citedness;
ALTER TABLE topics DROP COLUMN source;
