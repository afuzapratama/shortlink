CREATE INDEX traffic_events_ingest_page ON traffic_events (received_at DESC,id DESC);
CREATE INDEX traffic_aggregate_time ON traffic_aggregates (grain,bucket);
ALTER TABLE traffic_aggregates ADD COLUMN bot_events bigint NOT NULL DEFAULT 0;
