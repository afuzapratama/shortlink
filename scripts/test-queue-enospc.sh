#!/usr/bin/env bash
set -euo pipefail
project_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
queue_test_dir=$(mktemp -d /tmp/shortlink-queue-enospc.XXXXXXXX)
trap 'rm -rf -- "$queue_test_dir"' EXIT
cd "$project_root/services/agent"
queue_arch=$(docker version --format '{{.Server.Arch}}')
[[ "$queue_arch" == amd64 || "$queue_arch" == arm64 ]] || { echo 'Queue test requires a Linux amd64/arm64 Docker engine.' >&2; exit 2; }
queue_image=${SHORTLINK_QUEUE_TEST_IMAGE:-shortlink-agent:v0.13.0-b13-$queue_arch}
queue_platform=$(docker image inspect "$queue_image" --format '{{.Os}}/{{.Architecture}}')
[[ "$queue_platform" == "linux/$queue_arch" ]] || { echo 'Cached queue test image must match the Docker engine architecture.' >&2; exit 2; }
CGO_ENABLED=0 GOOS=linux GOARCH="$queue_arch" go test -c -o "$queue_test_dir/eventqueue.test" ./internal/eventqueue
chmod 0755 "$queue_test_dir/eventqueue.test"
# Uses an already cached matching release image only as an isolated Linux runtime.
# No network, credentials, project data volume, privileged mount or production disk fill.
docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges \
 --user 0 --tmpfs /tmp:rw,nosuid,nodev,size=8m --tmpfs /capacity:rw,nosuid,nodev,size=1m \
 --mount "type=bind,src=$queue_test_dir/eventqueue.test,dst=/eventqueue.test,readonly" \
 -e SHORTLINK_ENOSPC_DIR=/capacity --entrypoint /eventqueue.test \
 "$queue_image" -test.run '^TestActualENOSPCRecovery$' -test.v -test.timeout 20s
