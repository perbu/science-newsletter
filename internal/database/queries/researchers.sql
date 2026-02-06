-- name: GetResearcher :one
SELECT * FROM researchers WHERE id = ? LIMIT 1;

-- name: GetResearcherByOpenAlexID :one
SELECT * FROM researchers WHERE openalex_id = ? LIMIT 1;

-- name: ListResearchers :many
SELECT * FROM researchers ORDER BY name;

-- name: CreateResearcher :one
INSERT INTO researchers (openalex_id, name, affiliation, h_index, works_count, cited_by_count, relevancy_threshold)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateResearcherStats :exec
UPDATE researchers
SET name = ?, affiliation = ?, h_index = ?, works_count = ?, cited_by_count = ?, last_synced_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: UpdateResearcherThreshold :exec
UPDATE researchers SET relevancy_threshold = ? WHERE id = ?;

-- name: DeleteResearcher :exec
DELETE FROM researchers WHERE id = ?;
