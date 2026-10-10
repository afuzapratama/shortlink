ALTER TABLE domains ADD COLUMN verified_at timestamptz;
CREATE TABLE verification_challenges(domain_id text PRIMARY KEY REFERENCES domains(id), value text NOT NULL, expires_at timestamptz NOT NULL);
CREATE TABLE control_keys(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton), seed bytea NOT NULL CHECK(octet_length(seed)=32));
CREATE TABLE install_tokens(token_hash text PRIMARY KEY, domain_id text NOT NULL REFERENCES domains(id), installation_id text NOT NULL UNIQUE, method text NOT NULL, expires_at timestamptz NOT NULL, used_at timestamptz, proof_hash text, agent_id text, revoked boolean NOT NULL DEFAULT false);
CREATE TABLE agents(id text PRIMARY KEY, domain_id text NOT NULL REFERENCES domains(id), installation_id text NOT NULL UNIQUE, credential_hash text NOT NULL UNIQUE, method text NOT NULL, version text NOT NULL, queue_depth bigint NOT NULL DEFAULT 0, queue_bytes bigint NOT NULL DEFAULT 0, dropped_events bigint NOT NULL DEFAULT 0, geo_release text, feed_version text, revoked_at timestamptz, created_at timestamptz NOT NULL DEFAULT now());
CREATE UNIQUE INDEX agents_active_domain ON agents(domain_id) WHERE revoked_at IS NULL;
CREATE TABLE snapshots(domain_id text PRIMARY KEY REFERENCES domains(id), revision bigint NOT NULL, lease_expires_at timestamptz NOT NULL, payload bytea NOT NULL, signature text NOT NULL, etag text NOT NULL);
CREATE TABLE audit_logs(id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, actor_id text NOT NULL, action text NOT NULL, domain_id text NOT NULL, occurred_at timestamptz NOT NULL DEFAULT now());
