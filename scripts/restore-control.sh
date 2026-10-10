#!/usr/bin/env bash
set -euo pipefail
umask 077
[[ $# == 1 && -n "${RESTORE_DATABASE_URL:-}" ]] || { echo 'Usage: RESTORE_DATABASE_URL=... bash scripts/restore-control.sh /private/backup-directory' >&2; exit 2; }
backup_dir=$1
python3 - "$backup_dir" <<'PY'
import hashlib,json,pathlib,sys,stat
root=pathlib.Path(sys.argv[1]);manifest=json.loads((root/'manifest.json').read_text());p=root/'control.dump'
if root.stat().st_mode&0o077 or p.stat().st_mode&0o077:raise SystemExit('Backup must have private permissions.')
if manifest.get('format')!='shortlink-database-backup-v1':raise SystemExit('Unknown backup format.')
h=hashlib.sha256()
with p.open('rb') as f:
 for block in iter(lambda:f.read(1048576),b''):h.update(block)
if h.hexdigest()!=manifest['sha256']:raise SystemExit('Backup checksum mismatch.')
PY
# Fail before mutation unless target is a fresh empty database. Never drop existing data.
objects=$(psql "$RESTORE_DATABASE_URL" -At -v ON_ERROR_STOP=1 -c "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema' AND c.relkind IN ('r','p','v','m','S','f')")
[[ "$objects" == 0 ]] || { echo 'Restore target must be an empty database; existing data was not changed.' >&2; exit 1; }
pg_restore --dbname="$RESTORE_DATABASE_URL" --no-owner --no-privileges --exit-on-error --single-transaction "$backup_dir/control.dump"
echo 'Database restored atomically. Reconfigure private env and valid GeoLite/assets before serving traffic.'
