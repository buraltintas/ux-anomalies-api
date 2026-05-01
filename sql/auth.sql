-- name: GetOAuthByProviderIdentity :one
SELECT u.id, u.email, u.nickname, u.avatar_url
FROM oauth_accounts oa
JOIN users u ON u.id = oa.user_id
WHERE oa.provider = $1 AND oa.provider_user_id = $2;

-- name: GetUserByEmailNormalized :one
SELECT id, email, nickname, avatar_url
FROM users
WHERE email_normalized = $1;

-- name: NicknameExistsNormalized :one
SELECT EXISTS (
  SELECT 1 FROM users WHERE nickname_normalized = $1
);

-- name: CreateUser :one
INSERT INTO users (email, email_normalized, nickname, nickname_normalized, avatar_url)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, email, nickname, avatar_url;

-- name: LinkOAuthAccount :exec
INSERT INTO oauth_accounts (user_id, provider, provider_user_id, provider_email, provider_email_verified)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (provider, provider_user_id) DO NOTHING;
