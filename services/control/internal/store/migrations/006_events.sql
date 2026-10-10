ALTER TABLE agents ADD COLUMN buffered_events bigint NOT NULL DEFAULT 0 CHECK(buffered_events>=0), ADD COLUMN queue_write_errors bigint NOT NULL DEFAULT 0 CHECK(queue_write_errors>=0);
CREATE TABLE event_keys (
 agent_id text NOT NULL REFERENCES agents(id), event_id text NOT NULL,
 payload_hash text NOT NULL, received_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(agent_id,event_id)
);
CREATE INDEX event_keys_received ON event_keys(received_at);
CREATE TABLE traffic_events (
 id bigserial NOT NULL, occurred_at timestamptz NOT NULL, received_at timestamptz NOT NULL DEFAULT now(),
 agent_id text NOT NULL, domain_id text NOT NULL, link_id text NOT NULL,
 event_id text NOT NULL, payload jsonb NOT NULL, processed boolean NOT NULL DEFAULT false,
 PRIMARY KEY(occurred_at,id)
) PARTITION BY RANGE(occurred_at);
CREATE INDEX traffic_event_id ON traffic_events(id);
CREATE INDEX traffic_pending ON traffic_events(id) WHERE NOT processed;
CREATE TABLE traffic_aggregates (
 grain text NOT NULL CHECK(grain IN ('minute','hour')), bucket timestamptz NOT NULL,
 domain_id text NOT NULL, link_id text NOT NULL, country text NOT NULL,
 device text NOT NULL, os text NOT NULL, browser text NOT NULL,
 action text NOT NULL, reason text NOT NULL, events bigint NOT NULL,
 PRIMARY KEY(grain,bucket,domain_id,link_id,country,device,os,browser,action,reason)
);
CREATE TABLE event_worker_state (id boolean PRIMARY KEY DEFAULT true CHECK(id), watermark timestamptz);
INSERT INTO event_worker_state(id) VALUES(true);
