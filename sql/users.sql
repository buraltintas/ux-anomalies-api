-- name: GetUserByID :one
SELECT id, email, nickname, avatar_url, created_at
FROM users
WHERE id = $1;

-- name: GetPublicUserByNickname :one
SELECT nickname, avatar_url, created_at
FROM users
WHERE nickname_normalized = $1;
