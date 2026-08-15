#!/usr/bin/env bash
#
# Pull a container image, retrying on transient registry failures.
#
# Used by .github/workflows/image-scan.yml, which scans already-published
# images. Trivy's own registry fetch has no retry and a 5m default timeout, so
# one registry blip (`dial tcp ...: i/o timeout`) fails the whole scheduled
# run: no SARIF is uploaded, and the Security tab shows Trivy red until the
# next weekly run. Pulling here instead means a blip costs a retry, not a week
# of scan coverage. Trivy resolves an image from the local docker daemon before
# reaching for the registry, so the scan steps reuse what this pulls.
set -euo pipefail

readonly ATTEMPTS=3

image=${1:-}
if [ -z "$image" ]; then
  echo "::error::usage: $0 <image-ref>"
  exit 2
fi

for attempt in $(seq 1 "$ATTEMPTS"); do
  if docker pull "$image"; then
    exit 0
  fi
  echo "::warning::docker pull $image failed (attempt $attempt/$ATTEMPTS)"
  # No sleep after the final attempt: nothing follows it but the failure.
  if [ "$attempt" -lt "$ATTEMPTS" ]; then
    sleep $((attempt * 30))
  fi
done

echo "::error::could not pull $image after $ATTEMPTS attempts"
exit 1
