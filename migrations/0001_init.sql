-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL,
    email_normalized TEXT NOT NULL UNIQUE,
    nickname TEXT NOT NULL,
    nickname_normalized TEXT NOT NULL UNIQUE,
    avatar_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE oauth_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (provider IN ('google','github')),
    provider_user_id TEXT NOT NULL,
    provider_email TEXT,
    provider_email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_user_id)
);

CREATE TABLE anomalies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    author_id UUID NOT NULL REFERENCES users(id),
    category TEXT NOT NULL CHECK (category IN ('web','mobile_app','automotive_display','device_screen','tv','game','other')),
    description TEXT NOT NULL,
    source_url TEXT,
    like_count INTEGER NOT NULL DEFAULT 0,
    comment_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE anomaly_images (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    anomaly_id UUID NOT NULL REFERENCES anomalies(id) ON DELETE CASCADE,
    bucket TEXT NOT NULL,
    object_name TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    byte_size INTEGER NOT NULL,
    position SMALLINT NOT NULL CHECK (position BETWEEN 1 AND 3),
    UNIQUE (anomaly_id, position),
    UNIQUE (bucket, object_name)
);

CREATE TABLE anomaly_likes (
    anomaly_id UUID NOT NULL REFERENCES anomalies(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (anomaly_id, user_id)
);

CREATE TABLE anomaly_comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    anomaly_id UUID NOT NULL REFERENCES anomalies(id) ON DELETE CASCADE,
    author_id UUID NOT NULL REFERENCES users(id),
    text TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_anomalies_feed ON anomalies (created_at DESC, id DESC);
CREATE INDEX idx_anomalies_author ON anomalies (author_id, created_at DESC, id DESC);
CREATE INDEX idx_comments_anomaly ON anomaly_comments (anomaly_id, created_at ASC, id ASC);

-- +goose Down
DROP TABLE IF EXISTS anomaly_comments;
DROP TABLE IF EXISTS anomaly_likes;
DROP TABLE IF EXISTS anomaly_images;
DROP TABLE IF EXISTS anomalies;
DROP TABLE IF EXISTS oauth_accounts;
DROP TABLE IF EXISTS users;
