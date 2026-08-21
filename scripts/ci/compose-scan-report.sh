#!/usr/bin/env bash
#
# Turn the scan + rebuild results into the tracking issue's body, then file it
# via report-image-scan-issue.sh.
#
# Detection runs daily; the rebuild that clears below-gate findings runs weekly.
# AUTOFIX says which kind of run this is, so a report from a detect-only day can
# say the rebuild is waiting rather than implying one failed.
#
# The point of the wording here is to answer "what do you want from me" without
# a log dig. A rebuild can only clear OS-level CVEs, because it rebuilds the
# released tag; anything left after a successful rebuild comes from go.mod and
# needs a patch release cut from master. Saying which of the two you are looking
# at is the whole value of this report.
#
# The report covers two tiers. The CRITICAL/HIGH gate decides whether the run is
# red. Below it sits everything fixable at any severity, which used to be
# reported nowhere: Alpine ships its advisories with no severity, so Trivy calls
# them UNKNOWN and every threshold here ignored them, and 27 fixable PostgreSQL
# CVEs rode the published image behind a green run. Both tiers reach this issue
# now; only the first one turns anything red.
set -euo pipefail

APP_GATE=${APP_GATE:-}
FRONTDESK_GATE=${FRONTDESK_GATE:-}
APP_ADVISORY=${APP_ADVISORY:-}
FRONTDESK_ADVISORY=${FRONTDESK_ADVISORY:-}
BACKLOG=${BACKLOG:-}
DIGEST_FILE=${DIGEST_FILE:-}
POSTGRES_LINE=${POSTGRES_LINE:-}
RUN_URL=${RUN_URL:-}
STATUS_DIR=${STATUS_DIR:-refresh-status}
# Defaults true so a caller that does not set it keeps the old wording: every
# other entry point to this script rebuilds when it finds something fixable.
AUTOFIX=${AUTOFIX:-true}

body=$(mktemp)
trap 'rm -f "$body"' EXIT

# read_status <image> <field> - pull one field out of a refresh status file.
read_status() {
  local file="$STATUS_DIR/$1.txt"
  [ -f "$file" ] || return 1
  sed -n "s/^$2=//p" "$file" | head -1
}

vulnerable=false
backlog=false
needs_release=false
republished=false
partial=false
unverified=false

# The digest counts alerts already open in the Security tab, which includes any
# that predate this run. The per-image advisory outcomes only speak for this
# run. Either is enough to say there is something fixable outstanding.
if [ -n "$BACKLOG" ] && [ "$BACKLOG" != "0" ] && [ "$BACKLOG" != "unknown" ]; then
  backlog=true
fi

{
  echo "| image | fixable HIGH/CRITICAL | other fixable | rebuild clears it | republished |"
  echo "|---|---|---|---|---|"
} >"$body"

for entry in "model-hotel:$APP_GATE:$APP_ADVISORY" "model-hotel-frontdesk:$FRONTDESK_GATE:$FRONTDESK_ADVISORY"; do
  image=${entry%%:*}
  rest=${entry#*:}
  gate=${rest%%:*}
  advisory=${rest#*:}

  case "$gate" in
  failure)
    vulnerable=true
    scan_cell="yes"
    ;;
  success) scan_cell="none" ;;
  # Any other outcome means this image was never actually scanned, most often
  # because its pull failed. That is not evidence of anything, least of all of
  # being clean.
  *)
    unverified=true
    scan_cell="not scanned ($gate)"
    ;;
  esac

  case "$advisory" in
  failure)
    backlog=true
    advisory_cell="yes"
    ;;
  success) advisory_cell="none" ;;
  *) advisory_cell="not scanned ($advisory)" ;;
  esac

  rebuild=$(read_status "$image" rebuild_clean || echo "not attempted")
  rebuild_advisory=$(read_status "$image" rebuild_clean_advisory || echo "not attempted")
  published=$(read_status "$image" published || echo "not attempted")

  case "$rebuild" in
  success)
    # A rebuild that clears the gate but leaves the lower tier behind is a
    # partial fix, and saying "yes" to it would overstate what happened.
    if [ "$rebuild_advisory" = failure ]; then
      rebuild_cell="HIGH/CRITICAL only"
      partial=true
    else
      rebuild_cell="yes"
    fi
    ;;
  failure)
    rebuild_cell="no, still vulnerable"
    if [ "$gate" = failure ]; then
      needs_release=true
    fi
    ;;
  # This image had nothing to fix, so its leg of the rebuild was skipped while
  # the other image's ran. Saying "skipped" would read as a malfunction.
  skipped) rebuild_cell="not needed" ;;
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

  echo "| \`$image\` | $scan_cell | $advisory_cell | $rebuild_cell | $published_cell |" >>"$body"
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
  elif [ "$republished" = true ] && [ "$partial" = true ]; then
    echo "### Partly handled automatically"
    echo
    echo "A rebuild of the released tag cleared the HIGH/CRITICAL findings and \`:latest\` has been"
    echo "refreshed, but some fixable findings survived the rebuild. Those come from a Go dependency"
    echo "rather than an Alpine package, so only a patch release cut from \`master\` will clear them."
    echo "Nothing is urgent: what remains is below the gate."
  elif [ "$republished" = true ]; then
    echo "### Handled automatically"
    echo
    echo "A rebuild of the released tag cleared the findings and \`:latest\` has been refreshed."
    echo "No release is needed. This issue is here as a record and will close on the next clean scan."
  elif [ "$vulnerable" = true ]; then
    # Never claim a rebuild happened just because one was not reported as
    # failed: the refresh job can be skipped or die before publishing, and an
    # issue that says ":latest was refreshed" when it was not is worse than no
    # issue at all.
    echo "### Needs a look"
    echo
    echo "The scan found fixable vulnerabilities, but the rebuild did not get as far as"
    echo "republishing \`:latest\`, so nothing has been fixed automatically. Check the run below"
    echo "to see whether the rebuild failed or never started."
  elif [ "$AUTOFIX" != true ]; then
    echo "### Below the gate, queued for the weekly rebuild"
    echo
    echo "Nothing here is HIGH or CRITICAL, so the run is green and no release is needed. These are"
    echo "fixable though, meaning the fix already exists upstream and a rebuild would apply it."
    echo "This run only looks; the rebuild that applies below-gate fixes runs on Mondays, so there"
    echo "is nothing to chase here. Dispatch this workflow to bring it forward."
  else
    echo "### Below the gate, and still unfixed"
    echo
    echo "Nothing here is HIGH or CRITICAL, so the run is green and no release is needed. These are"
    echo "fixable though, meaning the fix already exists upstream and a rebuild would apply it. The"
    echo "rebuild did not get as far as republishing \`:latest\` this time, so they are still on the"
    echo "published image. Check the run below for why."
  fi

  if [ -n "$DIGEST_FILE" ] && [ -f "$DIGEST_FILE" ]; then
    echo
    cat "$DIGEST_FILE"
  fi

  # The database image is scanned for information only: it is not built here, it
  # is not in the Security tab, and nothing above gates on it. It is reported
  # because our images ship only the psql client, so a server-side PostgreSQL CVE
  # can only ever land here, where nothing else would have looked.
  if [ -n "$POSTGRES_LINE" ]; then
    echo
    echo "### The stack's database image"
    echo
    echo "$POSTGRES_LINE"
    echo
    echo "Not built by this repo, so no rebuild here changes it. The compose tag floats, so a"
    echo "\`docker compose pull\` picks up whatever upstream has published."
  fi

  echo
  if [ -n "$RUN_URL" ]; then
    echo "[Full scan run]($RUN_URL)"
  fi
} >>"$body"

if [ "$vulnerable" != true ] && [ "$backlog" != true ]; then
  # Closing requires a clean result for EVERY image, not merely the absence of a
  # failing one. An image whose pull died reports its gate as "skipped", and
  # treating that as clean would close an issue about confirmed HIGH/CRITICAL
  # vulnerabilities on the strength of never having looked. The run is already
  # red in that case, so leaving the issue open costs nothing and assuming
  # recovery costs everything.
  if [ "$unverified" = true ]; then
    echo "Not every image was scanned; leaving any open tracking issue alone."
    exit 0
  fi
  echo "No fixable findings; closing any open tracking issue."
  scripts/ci/report-image-scan-issue.sh clean "Published images: fixable vulnerabilities found"
  exit 0
fi

if [ "$vulnerable" = true ]; then
  header="The scan of the published images found fixable HIGH/CRITICAL vulnerabilities."
  title="Published images: fixable vulnerabilities found"
  if [ "$needs_release" = true ]; then
    title="Published images need a patch release"
  fi
else
  header="The scan found fixable vulnerabilities on the published images, all below the CRITICAL/HIGH gate."
  title="Published images: fixable vulnerabilities below the gate"
fi

# The header goes on last so the counts above can decide what it should say.
full=$(mktemp)
trap 'rm -f "$body" "$full"' EXIT
{
  echo "$header"
  echo
  cat "$body"
} >"$full"

cat "$full"
scripts/ci/report-image-scan-issue.sh vulnerable "$title" "$full"
