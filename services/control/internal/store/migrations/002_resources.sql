CREATE TABLE domains (
 id text PRIMARY KEY,
 hostname text NOT NULL UNIQUE,
 mode text NOT NULL CHECK (mode IN ('local','api')),
 readiness text NOT NULL DEFAULT 'draft' CHECK (readiness IN ('draft','verification_pending','installation_pending','preparing','ready','attention','revoked')),
 connection text NOT NULL DEFAULT 'unknown' CHECK (connection IN ('online','idle','offline','unknown')),
 desired_revision bigint NOT NULL DEFAULT 0 CHECK (desired_revision >= 0),
 applied_revision bigint CHECK (applied_revision >= 0),
 last_seen_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE links (
 id text PRIMARY KEY,
 domain_id text NOT NULL REFERENCES domains(id),
 slug text NOT NULL CHECK (slug ~ '^[a-zA-Z0-9_-]{1,80}$'),
 destination text NOT NULL,
 draft_destination text,
 state text NOT NULL DEFAULT 'draft' CHECK (state IN ('draft','published')),
 revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0),
 clicks bigint NOT NULL DEFAULT 0 CHECK (clicks >= 0),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(domain_id,slug)
);
CREATE INDEX links_domain_id_id ON links(domain_id,id);
