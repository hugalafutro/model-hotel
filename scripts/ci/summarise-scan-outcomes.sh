#!/usr/bin/env bash
#
# Decide whether the weekly image scan passed, and say why if it did not.
#
# Every step in .github/workflows/image-scan.yml is continue-on-error, so that
# one unreachable image cannot abort the other image's scan, upload or gate.
# The cost of that is the job status no longer reflects reality on its own, so
# this script is the single place that turns per-step outcomes back into a red
# or green run. It also names the failing step, because the failure mode that
# started all this was a red icon whose cause took a log dig to find.
#
# Usage: summarise-scan-outcomes.sh name=outcome [name=outcome ...]
# where outcome is a GitHub steps.<id>.outcome: success, failure, skipped,
# cancelled, or empty when the step never ran.
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "::error::usage: $0 name=outcome [name=outcome ...]"
  exit 2
fi

failed=()
skipped=()
unknown=()

echo "Weekly image scan outcomes:"
for pair in "$@"; do
  name=${pair%%=*}
  outcome=${pair#*=}
  printf '  %-20s %s\n' "$name" "${outcome:-<empty>}"
  case "$outcome" in
  success) ;;
  skipped) skipped+=("$name") ;;
  # An empty outcome means the workflow referenced a step id that does not
  # exist, so a gate we believe is running is in fact absent. Treating it as
  # "skipped" would let a typo silently retire a gate and still report green.
  '') unknown+=("$name") ;;
  *) failed+=("$name") ;;
  esac
done

if [ "${#unknown[@]}" -gt 0 ]; then
  echo "::error::no outcome reported for: ${unknown[*]} (step id renamed or misspelled in image-scan.yml)"
  exit 1
fi

# A skipped step is never a failure on its own: it means the step it depends on
# already failed, and that dependency is reported below in its own right.
if [ "${#skipped[@]}" -gt 0 ]; then
  echo "::notice::skipped (upstream step failed): ${skipped[*]}"
fi

if [ "${#failed[@]}" -gt 0 ]; then
  echo "::error::weekly image scan failed: ${failed[*]}"
  exit 1
fi

echo "All image scans passed."
