#!/usr/bin/env bash
#
# Turn the weekly scan + rebuild results into the tracking issue's body, then
# file it via report-image-scan-issue.sh.
#
# The point of the wording here is to answer "what do you want from me" without
# a log dig. A rebuild can only clear OS-level CVEs, because it rebuilds the
# released tag; anything left after a successful rebuild comes from go.mod and
# needs a patch release cut from master. Saying which of the two you are looking
# at is the whole value of this report.
set -euo pipefail

APP_GATE=${APP_GATE:-}
FRONTDESK_GATE=${FRONTDESK_GATE:-}
RUN_URL=${RUN_URL:-}
STATUS_DIR=${STATUS_DIR:-refresh-status}

body=$(mktemp)
trap 'rm -f "$body"' EXIT

# read_status <image> <field> - pull one field out of a refresh status file.
read_status() {
  local file="$STATUS_DIR/$1.txt"
  [ -f "$file" ] || return 1
  sed -n "s/^$2=//p" "$file" | head -1
}

vulnerable=false
needs_release=false
republished=false

{
  echo "The weekly scan of the published images found fixable HIGH/CRITICAL vulnerabilities."
  echo
  echo "| image | scan | rebuild clears it | republished |"
  echo "|---|---|---|---|"
} >"$body"

for entry in "model-hotel:$APP_GATE" "model-hotel-frontdesk:$FRONTDESK_GATE"; do
  image=${entry%%:*}
  gate=${entry#*:}

  case "$gate" in
  failure) vulnerable=true; scan_cell="fixable HIGH/CRITICAL" ;;
  success) scan_cell="clean" ;;
  *) scan_cell="not scanned ($gate)" ;;
  esac

  rebuild=$(read_status "$image" rebuild_clean || echo "not attempted")
  published=$(read_status "$image" published || echo "not attempted")

  case "$rebuild" in
  success) rebuild_cell="yes" ;;
  failure)
    rebuild_cell="no, still vulnerable"
    if [ "$gate" = failure ]; then
      needs_release=true
    fi
    ;;
  *) rebuild_cell="$rebuild" ;;
  esac

  case "$published" in
  success)
    published_cell="yes, :latest refreshed"
    republished=true
    ;;
  failure) published_cell="push failed" ;;
  skipped) published_cell="no" ;;
  *) published_cell="$published" ;;
  esac

  echo "| \`$image\` | $scan_cell | $rebuild_cell | $published_cell |" >>"$body"
done

{
  echo
  if [ "$needs_release" = true ]; then
    echo "### This one needs you"
    echo
    echo "Rebuilding the released tag did **not** clear the findings, which means they come from"
    echo "a Go dependency rather than an Alpine package. A rebuild cannot fix that: the fix has to"
    echo "be in \`go.mod\`, and only a patch release cut from \`master\` will put it in the published"
    echo "image. Check whether \`master\` already carries the bump before doing anything else."
  elif [ "$republished" = true ]; then
    echo "### Handled automatically"
    echo
    echo "A rebuild of the released tag cleared the findings and \`:latest\` has been refreshed."
    echo "No release is needed. This issue is here as a record and will close on the next clean scan."
  else
    # Never claim a rebuild happened just because one was not reported as
    # failed: the refresh job can be skipped or die before publishing, and an
    # issue that says ":latest was refreshed" when it was not is worse than no
    # issue at all.
    echo "### Needs a look"
    echo
    echo "The scan found fixable vulnerabilities, but the rebuild did not get as far as"
    echo "republishing \`:latest\`, so nothing has been fixed automatically. Check the run below"
    echo "to see whether the rebuild failed or never started."
  fi
  echo
  if [ -n "$RUN_URL" ]; then
    echo "[Full scan run]($RUN_URL)"
  fi
} >>"$body"

if [ "$vulnerable" != true ]; then
  echo "No fixable findings; closing any open tracking issue."
  scripts/ci/report-image-scan-issue.sh clean "Published images: fixable vulnerabilities found"
  exit 0
fi

title="Published images: fixable vulnerabilities found"
if [ "$needs_release" = true ]; then
  title="Published images need a patch release"
fi

cat "$body"
scripts/ci/report-image-scan-issue.sh vulnerable "$title" "$body"
