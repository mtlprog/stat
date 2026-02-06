CREATE TABLE fund_entities (
    id SERIAL PRIMARY KEY,
    slug VARCHAR(255) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE fund_snapshots (
    id SERIAL PRIMARY KEY,
    entity_id INTEGER NOT NULL REFERENCES fund_entities(id) ON DELETE CASCADE,
    snapshot_date DATE NOT NULL,
    data JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    UNIQUE (entity_id, snapshot_date)
);

CREATE INDEX idx_fund_snapshots_entity_date
    ON fund_snapshots(entity_id, snapshot_date DESC);

CREATE INDEX idx_fund_snapshots_data
    ON fund_snapshots USING GIN (data);

CREATE TABLE external_quotes (
    symbol VARCHAR(10) PRIMARY KEY,
    price_in_eur NUMERIC NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
