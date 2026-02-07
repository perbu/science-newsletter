-- name: UpsertFieldSelection :exec
INSERT INTO researcher_field_selections (researcher_id, level, openalex_id, display_name)
VALUES (?, ?, ?, ?)
ON CONFLICT(researcher_id, level, openalex_id) DO UPDATE SET
    display_name = excluded.display_name;

-- name: DeleteFieldSelection :exec
DELETE FROM researcher_field_selections
WHERE researcher_id = ? AND level = ? AND openalex_id = ?;

-- name: ListFieldSelectionsByResearcher :many
SELECT * FROM researcher_field_selections
WHERE researcher_id = ?
ORDER BY level, display_name;
