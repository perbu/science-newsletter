-- name: UpsertSyncedCitedAuthor :exec
INSERT INTO cited_authors (researcher_id, openalex_id, name, affiliation, citation_count, source, active)
VALUES (?, ?, ?, ?, ?, 'openalex_sync', 1)
ON CONFLICT (researcher_id, openalex_id) DO UPDATE SET
    name = excluded.name,
    affiliation = excluded.affiliation,
    citation_count = excluded.citation_count
WHERE cited_authors.source = 'openalex_sync';

-- name: ListSyncedCitedAuthorIDs :many
SELECT openalex_id FROM cited_authors
WHERE researcher_id = ? AND source = 'openalex_sync';

-- name: DeleteSyncedCitedAuthor :exec
DELETE FROM cited_authors
WHERE researcher_id = ? AND openalex_id = ? AND source = 'openalex_sync';

-- name: ListCitedAuthorsByResearcher :many
SELECT * FROM cited_authors
WHERE researcher_id = ?
ORDER BY citation_count DESC
LIMIT 60;

-- name: ListTopActiveCitedAuthorsByResearcher :many
SELECT * FROM cited_authors
WHERE researcher_id = ? AND active = 1
ORDER BY citation_count DESC
LIMIT ?;

-- name: IsActiveCitedAuthor :one
SELECT COUNT(*) > 0 AS is_cited_author FROM cited_authors
WHERE researcher_id = ? AND openalex_id = ? AND active = 1;

-- name: ToggleCitedAuthorActive :exec
UPDATE cited_authors SET active = NOT active
WHERE id = ? AND researcher_id = ?;

-- name: DeleteCitedAuthor :exec
DELETE FROM cited_authors
WHERE id = ? AND researcher_id = ?;

-- name: InsertCitedAuthor :exec
INSERT INTO cited_authors (researcher_id, openalex_id, name, affiliation, citation_count, source)
VALUES (?, ?, ?, ?, ?, ?);

-- name: DeleteCitedAuthorsByResearcher :exec
DELETE FROM cited_authors WHERE researcher_id = ?;
