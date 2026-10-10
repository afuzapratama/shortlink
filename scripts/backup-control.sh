#!/usr/bin/env bash
# Database-only backup: includes control_keys/MFA, excludes licensed MMDB/assets.
set -euo pipefail
umask 077
[[ $# == 1 && -n "${DATABASE_URL:-}" ]] || { echo 'Usage: DATABASE_URL=... bash scripts/backup-control.sh /private/new-backup-directory' >&2; exit 2; }
backup_dir=$1
[[ ! -e "$backup_dir" ]] || { echo 'Backup destination must not exist.' >&2; exit 2; }
mkdir -m 700 -- "$backup_dir"
[[ $(stat -c %a -- "$backup_dir") == 700 ]] || { echo 'Filesystem does not enforce private permissions.' >&2; exit 1; }
trap 'echo "Backup incomplete; do not use destination." >&2' ERR
pg_dump --dbname="$DATABASE_URL" --format=custom --file="$backup_dir/control.dump"
chmod 600 "$backup_dir/control.dump"
[[ $(stat -c %a -- "$backup_dir/control.dump") == 600 ]] || exit 1
pg_restore --list "$backup_dir/control.dump" >/dev/null
python3 - "$backup_dir" <<'PY'
import hashlib,json,pathlib,sys,datetime
root=pathlib.Path(sys.argv[1]);h=hashlib.sha256()
with (root/'control.dump').open('rb') as f:
 for block in iter(lambda:f.read(1048576),b''):h.update(block)
(root/'manifest.json').write_text(json.dumps({'format':'shortlink-database-backup-v1','createdAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'sha256':h.hexdigest(),'mmdbIncluded':False},indent=2)+'\n')
PY
chmod 600 "$backup_dir/manifest.json"
echo 'Private database backup validated; MMDB and runtime config are excluded.'
