-- name: CreateNewsletterRun :one
INSERT INTO newsletter_runs (researcher_id, status)
VALUES (?, 'pending')
RETURNING *;

-- name: UpdateNewsletterRun :exec
UPDATE newsletter_runs
SET status = ?, papers_found = ?, papers_included = ?, html_content = ?, completed_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: GetNewsletterRun :one
SELECT * FROM newsletter_runs WHERE id = ? LIMIT 1;

-- name: ListNewsletterRunsByResearcher :many
SELECT * FROM newsletter_runs
WHERE researcher_id = ?
ORDER BY created_at DESC
LIMIT ?;

-- name: CreateNewsletterItem :exec
INSERT INTO newsletter_items (newsletter_run_id, openalex_id, title, authors, publication_date, doi, relevancy_score, summary, is_cited_author_paper, cited_author_name)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: HasCompletedRunSince :one
SELECT count(*) > 0 FROM newsletter_runs WHERE researcher_id = ? AND status = 'completed' AND created_at >= ?;

-- name: ListAllNewsletterRuns :many
SELECT nr.id, nr.researcher_id, nr.status, nr.papers_found, nr.papers_included,
       nr.created_at, nr.completed_at, r.name as researcher_name
FROM newsletter_runs nr
JOIN researchers r ON r.id = nr.researcher_id
ORDER BY nr.created_at DESC LIMIT ?;

-- name: ListNewsletterItems :many
SELECT * FROM newsletter_items
WHERE newsletter_run_id = ?
ORDER BY is_cited_author_paper DESC, relevancy_score DESC;
