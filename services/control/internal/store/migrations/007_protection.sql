ALTER TABLE domains ADD COLUMN protection jsonb NOT NULL DEFAULT '{"mode":"observe","ratePerSecond":20,"burst":40}';
ALTER TABLE agents ADD COLUMN protection_version integer NOT NULL DEFAULT 0 CHECK(protection_version BETWEEN 0 AND 1);
CREATE TABLE risk_observations (
 bucket timestamptz NOT NULL, domain_id text NOT NULL, link_id text NOT NULL,
 mode text NOT NULL, signal text NOT NULL, candidate_action text NOT NULL,
 candidate_reason text NOT NULL, candidate_rule_id text NOT NULL,
 events bigint NOT NULL,
 PRIMARY KEY(bucket,domain_id,link_id,mode,signal,candidate_action,candidate_reason,candidate_rule_id)
);
