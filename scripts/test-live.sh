#!/usr/bin/env bash
set -euo pipefail
project_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
for tool in initdb pg_ctl psql go node; do
  command -v "$tool" >/dev/null || { echo "Missing test tool: $tool" >&2; exit 1; }
done
[[ $(id -u) != 0 ]] || { echo 'Run integration tests as a non-root user.' >&2; exit 1; }
control_test_dir=$(mktemp -d /tmp/shortlink-live-test.XXXXXXXX)
control_test_pid=''
cleanup() {
  if [[ -n "$control_test_pid" ]]; then
    kill -TERM "$control_test_pid" 2>/dev/null || true
    wait "$control_test_pid" 2>/dev/null || true
  fi
  pg_ctl -D "$control_test_dir/db" -m immediate -w stop >/dev/null 2>&1 || true
  rm -rf -- "$control_test_dir"
}
trap cleanup EXIT
initdb -D "$control_test_dir/db" -U shortlink_test -A trust --no-locale >/dev/null
pg_ctl -D "$control_test_dir/db" -l "$control_test_dir/postgres.log" -o "-h '' -k '$control_test_dir' -p 55440" -w start >/dev/null
export DATABASE_URL="postgres://shortlink_test@localhost/postgres?host=$control_test_dir&port=55440&sslmode=disable"
export CONTROL_MODE=development CONTROL_LISTEN=127.0.0.1:8085 CONTROL_ORIGIN=http://127.0.0.1:5175
cd "$project_root/services/control"
go build -o "$control_test_dir/control" ./cmd/control
"$control_test_dir/control" migrate
OWNER_EMAIL=browser-owner@example.com OWNER_PASSWORD='browser test password 123' "$control_test_dir/control" bootstrap-owner
if [[ -n "${AGENT_TEST_BUILD_INPUT:-}" ]]; then
  export AGENT_ASSET_ROOT="$control_test_dir/agent-assets" AGENT_RELEASE_INPUT="$AGENT_TEST_BUILD_INPUT"
  "$control_test_dir/control" publish-agent
fi
export PHP_ASSET_ROOT="$control_test_dir/php-assets" PHP_RELEASE_INPUT="$control_test_dir/php-build"
python3 "$project_root/scripts/build-php-release.py" --output "$PHP_RELEASE_INPUT"
"$control_test_dir/control" publish-php
"$control_test_dir/control" serve >"$control_test_dir/control.log" 2>&1 &
control_test_pid=$!
export CONTROL_API_TARGET=http://127.0.0.1:8085
cd "$project_root"
node --input-type=module <<'JS'
let ready=false;for(let i=0;i<100;i++){try{const r=await fetch('http://127.0.0.1:8085/health/ready');if(r.ok){ready=true;break}}catch{}await new Promise(r=>setTimeout(r,100))}if(!ready)throw Error('Test API failed to become ready');
JS
kill -0 "$control_test_pid" || { echo 'Test API exited before browser tests.' >&2; exit 1; }
# Browser suites share one private owner. Reset only disposable test auth counters
# between groups; keep the production login limiter unchanged.
for browser_project in desktop mobile; do
  npx playwright test --config playwright.live.config.ts --project "$browser_project" --grep-invert 'traffic uses real empty data|api protection persists centrally|php packages download|owner 2FA|proxycheck settings|domain opening'
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -c 'TRUNCATE auth_buckets' >/dev/null
  npx playwright test --config playwright.live.config.ts --project "$browser_project" --grep 'traffic uses real empty data|api protection persists centrally|php packages download|owner 2FA|proxycheck settings|domain opening'
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -c 'TRUNCATE auth_buckets' >/dev/null
done
