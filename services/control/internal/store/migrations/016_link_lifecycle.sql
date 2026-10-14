ALTER TABLE links ADD COLUMN disabled boolean NOT NULL DEFAULT false,
 ADD COLUMN deleted_at timestamptz,
 ADD COLUMN published_revision bigint;
UPDATE links SET published_revision=coalesce((SELECT max(revision) FROM link_history WHERE link_id=links.id),revision) WHERE state='published';
ALTER TABLE links DROP CONSTRAINT links_domain_id_slug_key;
CREATE UNIQUE INDEX links_active_slug ON links(domain_id,slug) WHERE deleted_at IS NULL;
