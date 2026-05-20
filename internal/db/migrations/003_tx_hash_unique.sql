-- Prevent the same on-chain payment from settling two orders.
-- A NULL tx_hash means "not paid yet"; Postgres UNIQUE allows multiple NULLs.
CREATE UNIQUE INDEX IF NOT EXISTS orders_tx_hash_unique
    ON orders (tx_hash)
    WHERE tx_hash IS NOT NULL;
