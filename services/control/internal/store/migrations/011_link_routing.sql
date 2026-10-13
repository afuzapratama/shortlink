ALTER TABLE links ADD COLUMN routing jsonb;
ALTER TABLE links ADD COLUMN draft_routing jsonb;
ALTER TABLE link_history ADD COLUMN routing jsonb;
ALTER TABLE agents ADD COLUMN routing_version integer NOT NULL DEFAULT 0 CHECK (routing_version BETWEEN 0 AND 1);
