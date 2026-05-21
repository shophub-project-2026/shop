-- Enable the pg_trgm extension so the GIN operator class gin_trgm_ops is available.
-- The ILIKE search in articles.List scans the entire table without this index;
-- with it PostgreSQL can use a bitmap index scan even for leading-wildcard patterns.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS articles_name_trgm_idx
    ON articles USING GIN (name gin_trgm_ops);
