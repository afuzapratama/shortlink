#!/usr/bin/env bash
set -euo pipefail
project_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
if [[ -n "${CONTROL_TEST_DATABASE_URL:-}" ]]; then
  cd "$project_root/services/control"
  go test -race -count=1 ./...
  exit
fi
for tool in initdb pg_ctl; do
  if ! command -v "$tool" >/dev/null; then
    echo "Install PostgreSQL binaries or set CONTROL_TEST_DATABASE_URL to a dedicated test database." >&2
    exit 1
  fi
done
if [[ $(id -u) == 0 ]]; then
  echo "Run this test script as a non-root user, or supply CONTROL_TEST_DATABASE_URL." >&2
  exit 1
fi
control_test_dir=$(mktemp -d /tmp/shortlink-control-test.XXXXXXXX)
cleanup() {
  pg_ctl -D "$control_test_dir/db" -m immediate -w stop >/dev/null 2>&1 || true
  rm -rf -- "$control_test_dir"
}
trap cleanup EXIT
initdb -D "$control_test_dir/db" -U shortlink_test -A trust --no-locale >/dev/null
# Unix socket only, in a mode-0700 directory; no TCP listener or existing database is used.
pg_ctl -D "$control_test_dir/db" -l "$control_test_dir/postgres.log" -o "-h '' -k '$control_test_dir' -p 55439" -w start >/dev/null
export CONTROL_TEST_DATABASE_URL="postgres://shortlink_test@localhost/postgres?host=$control_test_dir&port=55439&sslmode=disable"
cd "$project_root/services/control"
go test -race -count=1 ./...
