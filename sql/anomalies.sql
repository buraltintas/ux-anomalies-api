-- name: ListFeed :many
SELECT a.id, a.author_id, a.category, a.description, a.source_url, a.like_count, a.comment_count, a.created_at, a.updated_at
FROM anomalies a
WHERE (a.created_at, a.id) < ($1, $2)
ORDER BY a.created_at DESC, a.id DESC
LIMIT $3;

-- name: GetAnomalyByID :one
SELECT id, author_id, category, description, source_url, like_count, comment_count, created_at, updated_at
FROM anomalies
WHERE id = $1;
