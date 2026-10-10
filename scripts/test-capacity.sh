#!/usr/bin/env bash
set -euo pipefail
project_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
mode=${1:-smoke}
[[ "$mode" == smoke || "$mode" == full ]] || { echo 'Usage: test-capacity.sh [smoke|full] [report.json]' >&2; exit 2; }
load_test_dir=$(mktemp -d /tmp/shortlink-capacity-test.XXXXXXXX)
cleanup(){ pg_ctl -D "$load_test_dir/db" -m immediate -w stop >/dev/null 2>&1 || true; rm -rf -- "$load_test_dir"; }
trap cleanup EXIT
initdb -D "$load_test_dir/db" -U shortlink_test -A trust --no-locale >/dev/null
pg_ctl -D "$load_test_dir/db" -l "$load_test_dir/postgres.log" -o "-h '' -k '$load_test_dir' -p 55442" -w start >/dev/null
export CONTROL_TEST_DATABASE_URL="postgres://shortlink_test@localhost/postgres?host=$load_test_dir&port=55442"
if [[ "${SHORTLINK_LOAD_RESTORE:-}" == 1 ]]; then
 psql "$CONTROL_TEST_DATABASE_URL" -v ON_ERROR_STOP=1 -c 'CREATE DATABASE capacity_restore' >/dev/null
 export SHORTLINK_RESTORE_DATABASE_URL="postgres://shortlink_test@localhost/capacity_restore?host=$load_test_dir&port=55442"
fi
export SHORTLINK_LOAD_MODE="$mode" SHORTLINK_LOAD_REPORT="${2:-$load_test_dir/report.json}"
cd "$project_root/services/control"
# No race instrumentation: timings are measured against the normal binary.
profile_args=()
if [[ -n "${SHORTLINK_CPU_PROFILE:-}" ]]; then profile_args=(-cpuprofile "$SHORTLINK_CPU_PROFILE" -o "$load_test_dir/control-load.test"); fi
test_name='^TestCapacityHTTP$'
if [[ "${SHORTLINK_LOAD_PATH:-}" == events ]]; then test_name='^TestBatchCapacityHTTP$'; fi
go test "${profile_args[@]}" -count=1 -timeout 65m -run "$test_name" -v ./internal/httpapi
