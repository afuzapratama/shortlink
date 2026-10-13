ALTER TABLE domains ADD COLUMN opening_url text NOT NULL DEFAULT '';
ALTER TABLE agents DROP CONSTRAINT agents_routing_version_check;
ALTER TABLE agents ADD CONSTRAINT agents_routing_version_check CHECK(routing_version BETWEEN 0 AND 3);
CREATE TABLE agent_manage_keys (
 key_hash text PRIMARY KEY,
 agent_id text NOT NULL REFERENCES agents(id),
 domain_id text NOT NULL REFERENCES domains(id),
 expires_at timestamptz NOT NULL,
 used_at timestamptz
);
CREATE TABLE agent_manage_sessions (
 session_hash text PRIMARY KEY,
 agent_id text NOT NULL REFERENCES agents(id),
 domain_id text NOT NULL REFERENCES domains(id),
 expires_at timestamptz NOT NULL
);
