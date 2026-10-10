ALTER TABLE sessions ADD COLUMN mfa_verified boolean NOT NULL DEFAULT false;
ALTER TABLE owners ADD COLUMN mfa_secret bytea, ADD COLUMN mfa_last_step bigint NOT NULL DEFAULT -1;
CREATE TABLE mfa_pending(owner_id text PRIMARY KEY REFERENCES owners(id),secret bytea NOT NULL,expires_at timestamptz NOT NULL);
CREATE TABLE mfa_recovery(owner_id text NOT NULL REFERENCES owners(id),code_hash text NOT NULL,PRIMARY KEY(owner_id,code_hash));
CREATE TABLE mfa_challenges(challenge_hash text PRIMARY KEY,owner_id text NOT NULL REFERENCES owners(id),expires_at timestamptz NOT NULL,attempts integer NOT NULL DEFAULT 0);
CREATE INDEX mfa_challenge_expiry ON mfa_challenges(expires_at);
CREATE TABLE agent_rotations(agent_id text PRIMARY KEY REFERENCES agents(id),old_hash text NOT NULL,new_hash text NOT NULL,secret bytea NOT NULL,expires_at timestamptz NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),acked_at timestamptz);
CREATE TABLE control_migrations(id text PRIMARY KEY,origin text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),expires_at timestamptz NOT NULL);
CREATE TABLE agent_control_acks(agent_id text PRIMARY KEY REFERENCES agents(id),migration_id text NOT NULL REFERENCES control_migrations(id),acked_at timestamptz NOT NULL DEFAULT now());
