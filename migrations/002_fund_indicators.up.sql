CREATE TABLE IF NOT EXISTS fund_indicators (
    entity_id     INTEGER NOT NULL REFERENCES fund_entities(id) ON DELETE CASCADE,
    snapshot_date DATE    NOT NULL,
    indicator_id  INTEGER NOT NULL,
    value         NUMERIC NOT NULL,
    computed_at   TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (entity_id, snapshot_date, indicator_id)
);

CREATE INDEX IF NOT EXISTS idx_fund_indicators_entity_date
    ON fund_indicators(entity_id, snapshot_date DESC);

CREATE INDEX IF NOT EXISTS idx_fund_indicators_entity_indicator_date
    ON fund_indicators(entity_id, indicator_id, snapshot_date DESC);
