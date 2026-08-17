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
#
# A name ending in `?` is advisory: it is printed and reported, but it cannot
# fail the run. Two kinds of step need that. The fixable-at-any-severity checks
# "fail" whenever a backlog exists, which is a finding for the report to act on
# rather than a broken run. And the third-party images we scan for information
# are not ours to fix, so a registry blip on one must not turn red a run that
# says nothing about our own images. Advisory steps still get an empty-outcome
# check, so a renamed step id cannot retire one silently.
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "::error::usage: $0 name=outcome [name=outcome ...]"
  exit 2
fi

failed=()
skipped=()
unknown=()
advisory=()

echo "Weekly image scan outcomes:"
for pair in "$@"; do
  name=${pair%%=*}
  outcome=${pair#*=}

  soft=false
  if [ "${name%\?}" != "$name" ]; then
    soft=true
    name=${name%\?}
  fi

  printf '  %-20s %s%s\n' "$name" "${outcome:-<empty>}" "$([ "$soft" = true ] && echo ' (advisory)')"

  case "$outcome" in
  success) ;;
  skipped) skipped+=("$name") ;;
  # An empty outcome means the workflow referenced a step id that does not
  # exist, so a gate we believe is running is in fact absent. Treating it as
  # "skipped" would let a typo silently retire a gate and still report green.
  '') unknown+=("$name") ;;
  *)
    if [ "$soft" = true ]; then
      advisory+=("$name")
    else
      failed+=("$name")
    fi
    ;;
  esac
done

if [ "${#advisory[@]}" -gt 0 ]; then
  echo "::notice::advisory (reported, does not fail the run): ${advisory[*]}"
fi

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
