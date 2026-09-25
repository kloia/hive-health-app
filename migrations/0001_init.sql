-- Baseline schema. IF NOT EXISTS keeps it safe to apply on databases that
-- were created by the pre-migration version of HIVE.

CREATE TABLE IF NOT EXISTS products (
    id          SERIAL PRIMARY KEY,
    name        TEXT,
    warehouse   TEXT,
    quantity    INTEGER,
    updated_at  TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS movements (
    id          SERIAL PRIMARY KEY,
    product_id  INTEGER REFERENCES products(id),
    delta       INTEGER,
    note        TEXT,
    created_at  TIMESTAMP DEFAULT NOW()
);

-- Filled by the external daily report job (report.sh).
CREATE TABLE IF NOT EXISTS daily_reports (
    id              SERIAL PRIMARY KEY,
    generated_at    TIMESTAMP DEFAULT NOW(),
    total_products  INTEGER,
    total_quantity  INTEGER
);
