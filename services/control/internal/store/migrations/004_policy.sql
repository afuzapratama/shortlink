CREATE TABLE rule_sets (
 id text PRIMARY KEY,
 scope text NOT NULL CHECK(scope IN ('global','domain','link')),
 scope_id text,
 domain_id text REFERENCES domains(id),
 link_id text REFERENCES links(id),
 revision bigint NOT NULL DEFAULT 0,
 published_revision bigint,
 draft jsonb NOT NULL DEFAULT '[]',
 published jsonb NOT NULL DEFAULT '[]',
 CHECK((scope='global' AND scope_id IS NULL AND domain_id IS NULL AND link_id IS NULL) OR
 (scope='domain' AND scope_id=domain_id AND domain_id IS NOT NULL AND link_id IS NULL) OR
 (scope='link' AND scope_id=link_id AND link_id IS NOT NULL AND domain_id IS NOT NULL))
);
CREATE UNIQUE INDEX rule_sets_binding ON rule_sets(scope, coalesce(scope_id,''));
CREATE TABLE rule_history (
 rule_set_id text NOT NULL REFERENCES rule_sets(id), revision bigint NOT NULL,
 rules jsonb NOT NULL, published_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(rule_set_id,revision)
);
CREATE TABLE link_history (
 link_id text NOT NULL REFERENCES links(id), revision bigint NOT NULL,
 destination text NOT NULL, published_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(link_id,revision)
);
-- Global rules have no domain binding; preserve a real NULL in their audit entry.
ALTER TABLE audit_logs ALTER COLUMN domain_id DROP NOT NULL;
