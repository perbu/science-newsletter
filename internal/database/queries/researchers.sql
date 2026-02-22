-- name: GetResearcher :one
SELECT * FROM researchers WHERE id = ? LIMIT 1;

-- name: GetResearcherByOpenAlexID :one
SELECT * FROM researchers WHERE openalex_id = ? LIMIT 1;

-- name: ListResearchers :many
SELECT * FROM researchers ORDER BY name;

-- name: CreateResearcher :one
INSERT INTO researchers (id, openalex_id, name, affiliation, h_index, works_count, cited_by_count, relevancy_threshold)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateResearcherStats :exec
UPDATE researchers
SET name = ?, affiliation = ?, h_index = ?, works_count = ?, cited_by_count = ?, last_synced_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: UpdateResearcherThreshold :exec
UPDATE researchers SET relevancy_threshold = ? WHERE id = ?;

-- name: UpdateResearcherInterests :exec
UPDATE researchers SET research_interests = ? WHERE id = ?;

-- name: GetResearcherByEmail :one
SELECT * FROM researchers WHERE email = ? LIMIT 1;

-- name: LinkResearcherEmail :exec
UPDATE researchers SET email = ? WHERE id = ?;

-- name: ListResearchersWithEmail :many
SELECT * FROM researchers WHERE email IS NOT NULL AND email != '' ORDER BY name;

-- name: ListResearchersAdmin :many
SELECT r.*,
    (SELECT COUNT(*) FROM topics WHERE researcher_id = r.id) as topics_count,
    (SELECT COUNT(*) FROM cited_authors WHERE researcher_id = r.id AND active = 1) as cited_authors_count,
    (SELECT COUNT(*) FROM newsletter_runs WHERE researcher_id = r.id) as newsletter_runs_count,
    (SELECT COUNT(*) FROM newsletter_runs WHERE researcher_id = r.id AND status = 'completed') as completed_runs_count
FROM researchers r ORDER BY r.name;

-- name: DeleteResearcher :exec
DELETE FROM researchers WHERE id = ?;
