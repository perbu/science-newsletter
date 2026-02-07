-- name: UpsertTopic :exec
INSERT INTO topics (researcher_id, openalex_id, name, subfield, field, domain, score, source)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(researcher_id, openalex_id) DO UPDATE SET
    name = excluded.name,
    subfield = excluded.subfield,
    field = excluded.field,
    domain = excluded.domain,
    score = excluded.score,
    source = excluded.source;

-- name: ListTopicsByResearcher :many
SELECT * FROM topics
WHERE researcher_id = ?
ORDER BY score DESC;

-- name: ListTopTopicsByResearcher :many
SELECT * FROM topics
WHERE researcher_id = ?
ORDER BY score DESC
LIMIT ?;

-- name: DeleteTopicsByResearcher :exec
DELETE FROM topics WHERE researcher_id = ?;

-- name: DeleteOpenAlexTopicsByResearcher :exec
DELETE FROM topics WHERE researcher_id = ? AND source = 'openalex';

-- name: DeleteTopic :exec
DELETE FROM topics WHERE id = ? AND researcher_id = ?;

-- name: UpdateTopicScore :exec
UPDATE topics SET score = ?, source = 'manual' WHERE id = ? AND researcher_id = ?;

-- name: GetTopic :one
SELECT * FROM topics WHERE id = ?;

-- name: DeleteLLMTopicsByResearcher :exec
DELETE FROM topics WHERE researcher_id = ? AND source = 'llm';
