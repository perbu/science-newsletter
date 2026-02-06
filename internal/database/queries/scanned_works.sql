-- name: UpsertScannedWork :exec
INSERT INTO scanned_works (researcher_id, scan_week, openalex_id, title, doi, publication_date, cited_by_count, abstract, authorships, topics, source_name, source_citedness)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(researcher_id, openalex_id, scan_week) DO UPDATE SET
    title = excluded.title,
    doi = excluded.doi,
    publication_date = excluded.publication_date,
    cited_by_count = excluded.cited_by_count,
    abstract = excluded.abstract,
    authorships = excluded.authorships,
    topics = excluded.topics,
    source_name = excluded.source_name,
    source_citedness = excluded.source_citedness,
    fetched_at = CURRENT_TIMESTAMP;

-- name: ListScannedWorksByWeek :many
SELECT * FROM scanned_works
WHERE researcher_id = ? AND scan_week = ?
ORDER BY cited_by_count DESC;

-- name: DeleteScannedWorksByResearcher :exec
DELETE FROM scanned_works WHERE researcher_id = ?;

-- name: DeleteScannedWorksByWeek :exec
DELETE FROM scanned_works WHERE researcher_id = ? AND scan_week = ?;
