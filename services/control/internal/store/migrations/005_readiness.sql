ALTER TABLE domains ADD COLUMN probe_checked_at timestamptz;
ALTER TABLE domains ADD COLUMN probe_result jsonb;
