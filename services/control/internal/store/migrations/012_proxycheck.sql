CREATE TABLE proxycheck_settings (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 revision bigint NOT NULL DEFAULT 0,
 enabled boolean NOT NULL DEFAULT false,
 api_key bytea,
 daily_budget integer NOT NULL DEFAULT 900 CHECK(daily_budget BETWEEN 1 AND 1000000),
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK(NOT enabled OR api_key IS NOT NULL)
);
INSERT INTO proxycheck_settings(singleton) VALUES(true);
CREATE TABLE proxycheck_usage(day date PRIMARY KEY, queries integer NOT NULL CHECK(queries > 0));

ALTER TABLE agents DROP CONSTRAINT agents_routing_version_check;
ALTER TABLE agents ADD CONSTRAINT agents_routing_version_check CHECK(routing_version BETWEEN 0 AND 2);
