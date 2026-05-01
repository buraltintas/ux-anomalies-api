-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_anomalies_description_trgm
ON anomalies USING GIN (description gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_users_nickname_trgm
ON users USING GIN (nickname gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_anomalies_category_created
ON anomalies (category, created_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_anomalies_category_created;
DROP INDEX IF EXISTS idx_users_nickname_trgm;
DROP INDEX IF EXISTS idx_anomalies_description_trgm;
