-- +goose Up
ALTER TABLE researchers ADD COLUMN email TEXT UNIQUE;

-- +goose Down
ALTER TABLE researchers DROP COLUMN email;
