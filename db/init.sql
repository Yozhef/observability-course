CREATE TABLE IF NOT EXISTS orders (
    id         BIGSERIAL PRIMARY KEY,
    status     TEXT NOT NULL DEFAULT 'pending',
    total      NUMERIC(10,2) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS order_items (
    id       BIGSERIAL PRIMARY KEY,
    order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    name     TEXT NOT NULL,
    price    NUMERIC(10,2) NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS entitlements (
    id         BIGSERIAL PRIMARY KEY,
    order_id   BIGINT NOT NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_items_order ON order_items(order_id);
