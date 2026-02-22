-- +goose Up
ALTER TABLE researchers ADD COLUMN email TEXT;
CREATE UNIQUE INDEX idx_researchers_email ON researchers(email) WHERE email IS NOT NULL;

-- +goose Down
DROP INDEX idx_researchers_email;
ALTER TABLE researchers DROP COLUMN email;
