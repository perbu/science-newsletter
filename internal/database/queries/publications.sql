-- name: UpsertPublication :exec
INSERT INTO publications (openalex_id, researcher_id, title, publication_date, doi, cited_by_count)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(openalex_id) DO UPDATE SET
    title = excluded.title,
    cited_by_count = excluded.cited_by_count;

-- name: ListPublicationsByResearcher :many
SELECT * FROM publications
WHERE researcher_id = ?
ORDER BY publication_date DESC
LIMIT ?;

-- name: CountPublicationsByResearcher :one
SELECT COUNT(*) FROM publications WHERE researcher_id = ?;
