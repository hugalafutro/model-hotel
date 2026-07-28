#!/usr/bin/env bash
#
# Prove a rebuilt image actually boots and serves before it replaces :latest.
#
# The normal release path never smoke-tests the image: it relies on the full
# test suite passing before the build, which is sound because the build is the
# last step. An automated rebuild runs none of that suite, and it changes the
# base OS packages underneath the same application code, so this is the only
# thing between a bad Alpine update and everyone who pulls :latest. It is
# deliberately shallow: boot, serve /health, exit. Anything deeper belongs in
# the test suite, not in a Monday-morning rebuild.
#
# Expects a Postgres reachable on localhost:5432 (the workflow's service
# container) and uses host networking so the container can see it.
#
# Usage: smoke-test-image.sh <image-ref>
set -euo pipefail

readonly HEALTH_URL="http://localhost:8080/health"
readonly BOOT_TIMEOUT=90

image=${1:-}
if [ -z "$image" ]; then
  echo "::error::usage: $0 <image-ref>"
  exit 2
fi

container=""
# shellcheck disable=SC2329 # invoked indirectly via trap
cleanup() {
  if [ -n "$container" ]; then
    echo "--- container logs ---"
    docker logs "$container" 2>&1 | tail -40 || true
    docker rm -f "$container" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

echo "Starting $image"
container=$(docker run -d --network host \
  -e MASTER_KEY=smoke-test-master-key-not-a-real-secret-000 \
  -e POSTGRES_USER=modelhotel \
  -e POSTGRES_PASSWORD=changeme \
  -e POSTGRES_HOST=localhost \
  -e POSTGRES_DB=modelhotel \
  -e DATA_DIR=/data \
  "$image")

deadline=$((SECONDS + BOOT_TIMEOUT))
while [ "$SECONDS" -lt "$deadline" ]; do
  # A dead container will never become healthy, so stop waiting on it rather
  # than burning the full timeout.
  if [ "$(docker inspect -f '{{.State.Running}}' "$container" 2>/dev/null)" != "true" ]; then
    echo "::error::container exited before serving $HEALTH_URL"
    exit 1
  fi
  if curl -fsS --max-time 5 "$HEALTH_URL" >/dev/null 2>&1; then
    echo "Healthy: $image answered $HEALTH_URL"
    exit 0
  fi
  sleep 3
done

echo "::error::$image did not answer $HEALTH_URL within ${BOOT_TIMEOUT}s"
exit 1
