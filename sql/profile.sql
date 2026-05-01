-- name: GetMe :one
SELECT id, email, nickname, avatar_url, created_at
FROM users
WHERE id = $1;

-- name: UpdateMe :exec
UPDATE users
SET nickname = $1,
    nickname_normalized = $2,
    avatar_url = $3,
    updated_at = now()
WHERE id = $4;

-- name: UpdateMyAvatar :exec
UPDATE users
SET avatar_url = $1,
    updated_at = now()
WHERE id = $2;
