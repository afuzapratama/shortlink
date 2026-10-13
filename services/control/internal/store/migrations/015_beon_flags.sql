-- Remove the retired provider, preserving an existing BEON configuration.
UPDATE proxycheck_settings SET enabled=false,api_key=NULL,base_url='',provider='beon' WHERE provider<>'beon';
ALTER TABLE proxycheck_settings ALTER COLUMN provider SET DEFAULT 'beon';
ALTER TABLE proxycheck_settings DROP CONSTRAINT proxycheck_settings_provider_check;
ALTER TABLE proxycheck_settings ADD CONSTRAINT proxycheck_settings_provider_check CHECK(provider='beon');
ALTER TABLE agents DROP CONSTRAINT agents_routing_version_check;
ALTER TABLE agents ADD CONSTRAINT agents_routing_version_check CHECK(routing_version BETWEEN 0 AND 4);
