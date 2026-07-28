#!/usr/bin/env bash
#
# Open, update or close the tracking issue for the weekly published-image scan.
#
# The weekly scan's only previous output was a failure email, which is how a
# real finding sat unnoticed: the one run that mattered had failed on a network
# error, and an email saying "network error" reads exactly like noise you learn
# to ignore. An issue has a lifecycle, sits where work is tracked, and says what
# it wants from you. It is deliberately ONE issue, reused: a new issue every
# Monday would train the same reflex the email did.
#
# Usage:
#   report-image-scan-issue.sh vulnerable <title> <body-file>
#   report-image-scan-issue.sh clean      <title>
#
# Requires gh authenticated with issues:write (GH_TOKEN in CI).
set -euo pipefail

readonly LABEL="published-image-scan"

state=${1:-}
title=${2:-}
body_file=${3:-}

if [ -z "$state" ] || [ -z "$title" ]; then
  echo "::error::usage: $0 <vulnerable|clean> <title> [body-file]"
  exit 2
fi

# Reuse the existing open issue if there is one. --limit 1 with the label is
# enough: this script is the only thing that creates them.
existing=$(gh issue list --label "$LABEL" --state open --limit 1 --json number --jq '.[0].number // empty')

case "$state" in
vulnerable)
  if [ -z "$body_file" ] || [ ! -f "$body_file" ]; then
    echo "::error::vulnerable requires a readable body file"
    exit 2
  fi
  if [ -n "$existing" ]; then
    echo "Updating issue #$existing"
    # Rewrite the body so the issue always shows the CURRENT state rather than
    # forcing a reader to scroll to the newest comment, and comment so the issue
    # still surfaces in notifications.
    gh issue edit "$existing" --title "$title" --body-file "$body_file"
    gh issue comment "$existing" --body-file "$body_file"
  else
    echo "Opening a new issue"
    gh issue create --label "$LABEL" --title "$title" --body-file "$body_file"
  fi
  ;;
clean)
  if [ -z "$existing" ]; then
    echo "Nothing to close: no open $LABEL issue."
    exit 0
  fi
  echo "Closing issue #$existing"
  gh issue comment "$existing" --body "The published images are clean of fixable HIGH/CRITICAL vulnerabilities as of this run. Closing automatically; the weekly scan will reopen a fresh issue if that changes."
  gh issue close "$existing"
  ;;
*)
  echo "::error::unknown state '$state' (expected vulnerable or clean)"
  exit 2
  ;;
esac
