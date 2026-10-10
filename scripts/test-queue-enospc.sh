#!/usr/bin/env bash
set -euo pipefail
project_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
queue_test_dir=$(mktemp -d /tmp/shortlink-queue-enospc.XXXXXXXX)
trap 'rm -rf -- "$queue_test_dir"' EXIT
cd "$project_root/services/agent"
CGO_ENABLED=0 go test -c -o "$queue_test_dir/eventqueue.test" ./internal/eventqueue
# Uses an existing amd64 release image only as an isolated Linux filesystem/runtime.
# No network, credentials, project data volume, privileged mount or production disk fill.
docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges \
 --user 0 --tmpfs /tmp:rw,nosuid,nodev,size=8m --tmpfs /capacity:rw,nosuid,nodev,size=1m \
 --mount "type=bind,src=$queue_test_dir/eventqueue.test,dst=/eventqueue.test,readonly" \
 -e SHORTLINK_ENOSPC_DIR=/capacity --entrypoint /eventqueue.test \
 shortlink-agent:v0.13.0-b13-amd64 -test.run '^TestActualENOSPCRecovery$' -test.v -test.timeout 20s
