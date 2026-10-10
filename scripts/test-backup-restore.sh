#!/usr/bin/env bash
set -euo pipefail
umask 077
project_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
backup_test_dir=$(mktemp -d /tmp/shortlink-backup-test.XXXXXXXX)
cleanup(){ pg_ctl -D "$backup_test_dir/db" -m immediate -w stop >/dev/null 2>&1 || true; rm -rf -- "$backup_test_dir"; }
trap cleanup EXIT
initdb -D "$backup_test_dir/db" -U shortlink_test -A trust --no-locale >/dev/null
pg_ctl -D "$backup_test_dir/db" -l "$backup_test_dir/postgres.log" -o "-h '' -k '$backup_test_dir' -p 55441" -w start >/dev/null
psql "postgres://shortlink_test@localhost/postgres?host=$backup_test_dir&port=55441" -v ON_ERROR_STOP=1 -c 'CREATE DATABASE restore_test' >/dev/null
export DATABASE_URL="postgres://shortlink_test@localhost/postgres?host=$backup_test_dir&port=55441"
export RESTORE_DATABASE_URL="postgres://shortlink_test@localhost/restore_test?host=$backup_test_dir&port=55441"
export CONTROL_MODE=development CONTROL_LISTEN=127.0.0.1:8089 CONTROL_ORIGIN=http://127.0.0.1:5179
cd "$project_root/services/control"
go build -o "$backup_test_dir/control" ./cmd/control
"$backup_test_dir/control" migrate
OWNER_EMAIL=backup-fixture@example.com OWNER_PASSWORD='disposable backup test password 123' "$backup_test_dir/control" bootstrap-owner
cd "$project_root"
# A fixture session and encrypted MFA marker exercise preservation of private state.
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -c "INSERT INTO sessions(token_hash,owner_id,expires_at,mfa_verified) SELECT 'fixture-session',id,now()+interval '1 hour',true FROM owners; UPDATE owners SET mfa_secret=decode('010203','hex'); INSERT INTO audit_logs(actor_id,action,domain_id) SELECT id,'restore_fixture',id FROM owners" >/dev/null
bash scripts/backup-control.sh "$backup_test_dir/backup"
bash scripts/restore-control.sh "$backup_test_dir/backup"
fingerprint_sql="SELECT md5(jsonb_build_object('owners',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM owners o),'keys',(SELECT jsonb_agg(to_jsonb(k)) FROM control_keys k),'sessions',(SELECT jsonb_agg(to_jsonb(s)) FROM sessions s),'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM audit_logs a),'migrations',(SELECT jsonb_agg(to_jsonb(m) ORDER BY version) FROM schema_migrations m))::text)"
[[ $(psql "$DATABASE_URL" -At -c "$fingerprint_sql") == $(psql "$RESTORE_DATABASE_URL" -At -c "$fingerprint_sql") ]] || { echo 'Restored state differs.' >&2; exit 1; }
if bash scripts/restore-control.sh "$backup_test_dir/backup" >"$backup_test_dir/rejected.log" 2>&1; then echo 'Nonempty restore target was accepted.' >&2; exit 1; fi
printf 'tampered' >> "$backup_test_dir/backup/control.dump"
if bash scripts/restore-control.sh "$backup_test_dir/backup" >"$backup_test_dir/rejected.log" 2>&1; then echo 'Corrupt backup was accepted.' >&2; exit 1; fi
DATABASE_URL="$RESTORE_DATABASE_URL" "$backup_test_dir/control" migrate
printf 'Backup/restore fixture passed: root key, owner/password/MFA/session/audit/schema preserved; nonempty target and checksum tamper rejected.\n'
