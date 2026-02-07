-- name: UpsertOpenAlexTopic :exec
INSERT INTO openalex_topics (openalex_id, display_name, description, keywords, subfield_id, subfield_name, field_id, field_name, domain_id, domain_name, works_count, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(openalex_id) DO UPDATE SET
    display_name = excluded.display_name,
    description = excluded.description,
    keywords = excluded.keywords,
    subfield_id = excluded.subfield_id,
    subfield_name = excluded.subfield_name,
    field_id = excluded.field_id,
    field_name = excluded.field_name,
    domain_id = excluded.domain_id,
    domain_name = excluded.domain_name,
    works_count = excluded.works_count,
    updated_at = CURRENT_TIMESTAMP;

-- name: CountOpenAlexTopics :one
SELECT COUNT(*) FROM openalex_topics;

-- name: ListDistinctFields :many
SELECT DISTINCT field_id, field_name, domain_id, domain_name
FROM openalex_topics
WHERE field_id != ''
ORDER BY domain_name, field_name;

-- name: ListDistinctSubfieldsByField :many
SELECT DISTINCT subfield_id, subfield_name
FROM openalex_topics
WHERE field_id = ?
ORDER BY subfield_name;

-- name: ListTopicsBySubfield :many
SELECT openalex_id, display_name, description, keywords
FROM openalex_topics
WHERE subfield_id = ?
ORDER BY works_count DESC;

-- name: SearchSubfieldsByName :many
SELECT DISTINCT subfield_id, subfield_name, field_name, domain_name
FROM openalex_topics
WHERE subfield_name LIKE ?
ORDER BY subfield_name
LIMIT 20;

-- name: GetOpenAlexTopicByOpenAlexID :one
SELECT * FROM openalex_topics WHERE openalex_id = ? LIMIT 1;
