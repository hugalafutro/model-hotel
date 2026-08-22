#!/usr/bin/env bash
#
# Report what is actually open in the GitHub Security tab.
#
# The weekly scan uploads an unfiltered SARIF "so findings are tracked over
# time", and then nothing ever read that track. 54 fixable PostgreSQL CVEs sat
# on the published image behind a green run, because Alpine's security DB
# assigns them no severity, Trivy reports UNKNOWN, GitHub maps that to `note`,
# and `note` is below every threshold this repo has. A record nobody reads is
# not a record. This script is what reads it.
#
# It reports rather than gates: the CRITICAL/HIGH gate stays the only thing that
# decides red or green. What lands here goes into the tracking issue, so a
# backlog has to be dismissed on purpose instead of by never being looked at.
#
# Usage: summarise-code-scanning.sh <output-markdown-file>
#
# Environment:
#   GH_TOKEN        gh credentials with security-events:read (required)
#   REPO            owner/name (required)
#   REF             git ref to report alerts for (default refs/heads/master)
#   SARIF_IDS       space-separated sarif-id values to wait for before reading
#   WAIT_SECONDS    how long to wait for those uploads to finish processing
#   SELF_CATEGORIES SARIF categories for images this repo builds, and can
#                   therefore fix by rebuilding. Only these drive the tracking
#                   issue; everything else is reported but never nags, because
#                   an issue that cannot be closed by any action here would
#                   train exactly the ignore reflex this is meant to break.
set -euo pipefail

out=${1:-}
if [ -z "$out" ]; then
  echo "::error::usage: $0 <output-markdown-file>"
  exit 2
fi

REPO=${REPO:-}
REF=${REF:-refs/heads/master}
SARIF_IDS=${SARIF_IDS:-}
WAIT_SECONDS=${WAIT_SECONDS:-180}
SELF_CATEGORIES=${SELF_CATEGORIES:-"published-image published-frontdesk-image"}

if [ -z "$REPO" ]; then
  echo "::error::REPO is required"
  exit 2
fi

# GitHub processes an uploaded SARIF asynchronously. Reading the alert list too
# early reports the PREVIOUS run's picture, which for a backlog that appeared
# this week would mean reporting "all clear" on the one run that mattered — the
# exact failure this script exists to stop. Wait for each upload to finish, and
# say so plainly if one never does rather than passing off a stale count as
# current.
stale_note=""
wait_for_sarif() {
  local id=$1 deadline=$((SECONDS + WAIT_SECONDS)) status
  while [ "$SECONDS" -lt "$deadline" ]; do
    status=$(gh api "/repos/$REPO/code-scanning/sarifs/$id" --jq '.processing_status' 2>/dev/null || echo "")
    case "$status" in
    complete) return 0 ;;
    failed)
      echo "SARIF $id failed to process"
      return 1
      ;;
    *) sleep 10 ;;
    esac
  done
  echo "SARIF $id still processing after ${WAIT_SECONDS}s"
  return 1
}

for id in $SARIF_IDS; do
  if ! wait_for_sarif "$id"; then
    stale_note="One or more SARIF uploads from this run had not finished processing, so the counts below may lag this run by one scan."
  fi
done

alerts=$(mktemp)
trap 'rm -f "$alerts"' EXIT

# A missing alert list must not be reported as an empty one.
if ! gh api --paginate -X GET \
  -f state=open -f ref="$REF" -f per_page=100 \
  "/repos/$REPO/code-scanning/alerts" >"$alerts" 2>/dev/null; then
  {
    echo "### What is open in the Security tab"
    echo
    echo "Could not read the code-scanning alert list for \`$REF\`. Treat the counts as unknown, not as zero."
  } >"$out"
  echo "fixable_backlog=unknown"
  exit 0
fi

# Trivy encodes the actionable detail as text in the alert message; there is no
# structured field for it. "Fixed Version" present and non-empty is the same
# definition of actionable the gates use via `ignore-unfixed`.
read -r -d '' jq_program <<'JQ' || true
def field($k):
  (.most_recent_instance.message.text // "")
  | split("\n")
  | map(select(startswith($k + ": ")))
  | (.[0] // "")
  | ltrimstr($k + ": ")
  | sub("^\\s+|\\s+$"; "");

# The SARIF category is an upload label, not a name anyone would recognise in
# the report: the table says "scanned image", so the two images this repo
# builds are shown under the names the rest of the issue already uses.
def image_name:
  if . == "published-image" then "model-hotel"
  elif . == "published-frontdesk-image" then "model-hotel-frontdesk"
  else . end;

($self | split(" ") | map(select(length > 0))) as $mine
| map({
    tool:      (.tool.name // "unknown"),
    severity:  (.rule.security_severity_level // .rule.severity // "unknown" | ascii_upcase),
    category:  (.most_recent_instance.category // "uncategorised"),
    package:   field("Package"),
    installed: field("Installed Version"),
    fixed:     field("Fixed Version")
  })
| map(. + {ours: (.category as $c | $mine | index($c) != null)})
| . as $r
| ($r | map(select(.package != "" and .fixed != ""))) as $fix
| {
    total:         ($r | length),
    fixable:       ($fix | map(select(.ours)) | length),
    fixable_other: ($fix | map(select(.ours | not)) | length),
    by_tool:       ($r | group_by(.tool) | map({k: .[0].tool, n: length})),
    by_sev:        ($r | group_by(.severity) | map({k: .[0].severity, n: length})),
    packages: (
      $fix
      | group_by(.category + " " + .package)
      | map({
          name:      .[0].package,
          installed: .[0].installed,
          fixed:     .[0].fixed,
          category:  (.[0].category | image_name),
          ours:      .[0].ours,
          n:         length
        })
      | sort_by([(if .ours then 0 else 1 end), -.n])
    )
  }
JQ

summary=$(jq --arg self "$SELF_CATEGORIES" "$jq_program" "$alerts")

total=$(jq -r '.total' <<<"$summary")
fixable=$(jq -r '.fixable' <<<"$summary")
fixable_other=$(jq -r '.fixable_other' <<<"$summary")

{
  echo "### What is open in the Security tab"
  echo
  if [ "$total" -eq 0 ]; then
    echo "No open code-scanning alerts on \`$REF\`."
  else
    echo -n "**$total open alert(s)** on \`$REF\`"
    jq -r '" (" + (.by_tool | map(.k + " " + (.n|tostring)) | join(", ")) + ")."' <<<"$summary"
    echo
    echo "By severity: $(jq -r '.by_sev | map(.k + " " + (.n|tostring)) | join(", ")' <<<"$summary")."

    if [ $((fixable + fixable_other)) -gt 0 ]; then
      echo
      echo "#### Fixable OS packages"
      echo
      echo "The backlog expressed as the thing you would actually change. A rebuild picks"
      echo "these up from the current Alpine index:"
      echo
      echo "| scanned image | package | installed | fixed in | alerts |"
      echo "|---|---|---|---|---|"
      jq -r '.packages[] | "| \(.category)\(if .ours then "" else " (dependency)" end) | `\(.name)` | \(.installed) | \(.fixed) | \(.n) |"' <<<"$summary"

      if [ "$fixable_other" -gt 0 ]; then
        echo
        echo "Rows marked _(dependency)_ are third-party images this repo does not build, so"
        echo "rebuilding ours will not clear them. They are listed because they run in the"
        echo "stack; fixing one means repinning it, or pulling the tag if it floats."
      fi
    fi
  fi

  if [ -n "$stale_note" ]; then
    echo
    echo "> $stale_note"
  fi
} >"$out"

cat "$out"

if [ -n "${GITHUB_OUTPUT:-}" ]; then
  {
    echo "total=$total"
    echo "fixable_backlog=$fixable"
    echo "fixable_other=$fixable_other"
  } >>"$GITHUB_OUTPUT"
fi
