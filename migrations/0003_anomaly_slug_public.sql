-- +goose Up
ALTER TABLE anomalies
  ADD COLUMN IF NOT EXISTS title TEXT,
  ADD COLUMN IF NOT EXISTS slug TEXT,
  ADD COLUMN IF NOT EXISTS is_public BOOLEAN NOT NULL DEFAULT TRUE,
  ADD COLUMN IF NOT EXISTS is_published BOOLEAN NOT NULL DEFAULT TRUE,
  ADD COLUMN IF NOT EXISTS is_approved BOOLEAN NOT NULL DEFAULT TRUE;

UPDATE anomalies
SET title = COALESCE(NULLIF(title, ''), LEFT(description, 120))
WHERE title IS NULL OR title = '';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ux_slugify(input TEXT)
RETURNS TEXT AS $$
DECLARE
  s TEXT;
BEGIN
  s := lower(trim(input));
  s := regexp_replace(s, '[^a-z0-9]+', '-', 'g');
  s := regexp_replace(s, '(^-+|-+$)', '', 'g');
  IF s = '' THEN
    s := 'anomaly';
  END IF;
  RETURN s;
END;
$$ LANGUAGE plpgsql IMMUTABLE;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
DECLARE
  rec RECORD;
  base_slug TEXT;
  candidate TEXT;
  suffix INT;
BEGIN
  FOR rec IN SELECT id, title FROM anomalies WHERE slug IS NULL OR slug = '' LOOP
    base_slug := ux_slugify(rec.title);
    candidate := base_slug;
    suffix := 2;
    WHILE EXISTS (SELECT 1 FROM anomalies a WHERE a.slug = candidate AND a.id <> rec.id) LOOP
      candidate := base_slug || '-' || suffix::TEXT;
      suffix := suffix + 1;
    END LOOP;
    UPDATE anomalies SET slug = candidate WHERE id = rec.id;
  END LOOP;
END $$;
-- +goose StatementEnd

ALTER TABLE anomalies
  ALTER COLUMN title SET NOT NULL,
  ALTER COLUMN slug SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_anomalies_slug_unique ON anomalies (slug);
CREATE INDEX IF NOT EXISTS idx_anomalies_public_updated ON anomalies (updated_at DESC, id DESC)
WHERE is_public = TRUE AND is_published = TRUE AND is_approved = TRUE;

-- +goose Down
DROP INDEX IF EXISTS idx_anomalies_public_updated;
DROP INDEX IF EXISTS idx_anomalies_slug_unique;
DROP FUNCTION IF EXISTS ux_slugify(TEXT);
ALTER TABLE anomalies
  DROP COLUMN IF EXISTS is_approved,
  DROP COLUMN IF EXISTS is_published,
  DROP COLUMN IF EXISTS is_public,
  DROP COLUMN IF EXISTS slug,
  DROP COLUMN IF EXISTS title;
