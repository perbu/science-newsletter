-- name: UpsertCoAuthor :exec
INSERT INTO co_authors (researcher_id, openalex_id, name, affiliation, collaboration_count, last_collaborated)
VALUES (?, ?, ?, ?, 1, ?)
ON CONFLICT(researcher_id, openalex_id) DO UPDATE SET
    name = excluded.name,
    affiliation = excluded.affiliation,
    collaboration_count = co_authors.collaboration_count + 1,
    last_collaborated = excluded.last_collaborated;

-- name: ListCoAuthorsByResearcher :many
SELECT * FROM co_authors
WHERE researcher_id = ?
ORDER BY collaboration_count DESC;

-- name: GetCoAuthorByOpenAlexID :one
SELECT * FROM co_authors
WHERE researcher_id = ? AND openalex_id = ?
LIMIT 1;

-- name: ListTopCoAuthorsByResearcher :many
SELECT * FROM co_authors
WHERE researcher_id = ?
ORDER BY collaboration_count DESC
LIMIT ?;

-- name: IsCoAuthor :one
SELECT COUNT(*) > 0 AS is_coauthor FROM co_authors
WHERE researcher_id = ? AND openalex_id = ?;
