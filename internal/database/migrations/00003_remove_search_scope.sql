-- +goose Up
DROP TABLE IF EXISTS researcher_field_selections;
DROP TABLE IF EXISTS openalex_topics;

-- +goose Down
-- These tables would need to be recreated from 00001_initial_schema.sql if needed.
-- This migration is intentionally not reversible.
