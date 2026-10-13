ALTER TABLE proxycheck_settings
 ADD COLUMN provider text NOT NULL DEFAULT 'proxycheck' CHECK(provider IN ('proxycheck','beon')),
 ADD COLUMN base_url text NOT NULL DEFAULT '';
