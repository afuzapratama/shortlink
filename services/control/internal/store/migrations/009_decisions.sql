CREATE TABLE api_decisions (
 agent_id text NOT NULL REFERENCES agents(id),
 event_id text NOT NULL,
 request_hash text NOT NULL,
 decision jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(agent_id,event_id)
);
CREATE INDEX api_decisions_created ON api_decisions(created_at);
