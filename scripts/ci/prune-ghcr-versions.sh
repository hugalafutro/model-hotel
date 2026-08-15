#!/usr/bin/env bash
#
# Prune old versions of a GHCR container package with the same retention as
# the Docker Hub prune in .github/workflows/dockerhub-prune.yml: keep the
# newest KEEP release versions (X.Y.Z), every protected moving tag (latest,
# dev, X.Y), and delete the rest.
#
# GHCR has no "delete a tag" call: a package *version* is one manifest digest
# carrying every tag that points at it, and deleting the version drops all of
# them. Each release pushes X.Y.Z and sha-<commit> at the same digest, so on
# GHCR they are one version and are dropped together, which is exactly what
# the Hub prune achieves by comparing digests. A version is retained when any
# of its tags is retained; a version whose tags are all release/sha- tags of a
# dropped release is deleted; a version with an unrecognised tag is left alone.
#
# Untagged versions are deleted too, with one exception that matters: older
# releases were multi-arch (or carried a provenance attestation), so their
# X.Y.Z tag is an image index whose per-platform manifests show up as
# *untagged* versions. Deleting those would break the retained index they
# belong to. Every retained version's manifest is therefore fetched from the
# registry, and any child digest it references is protected. If a retained
# manifest cannot be read the run aborts before deleting anything, because
# "probably fine" is not a retention policy.
#
# Usage: prune-ghcr-versions.sh <owner> <package> <keep> <dry_run>
#   owner    GHCR namespace (the GitHub user that owns the package)
#   package  container package name (e.g. model-hotel)
#   keep     number of newest release versions to retain (integer >= 1)
#   dry_run  "true" lists what would be deleted without deleting
# Env:
#   GH_TOKEN  token for the GitHub REST API and the GHCR registry. In Actions,
#             GITHUB_TOKEN with `packages: write` works for packages this
#             repository publishes (the publishing repo holds admin on them).
set -euo pipefail

OWNER="${1:?owner required}"
PKG="${2:?package required}"
KEEP="${3:?keep required}"
DRY_RUN="${4:?dry_run required}"
: "${GH_TOKEN:?GH_TOKEN required}"

# Guard against keep=0 (or non-numeric), which would leave every release
# version unprotected and eligible for deletion.
if ! [ "$KEEP" -ge 1 ] 2>/dev/null; then
  echo "::error::keep must be an integer >= 1 (got '${KEEP}')"
  exit 1
fi

echo "Package: ghcr.io/${OWNER}/${PKG}"
echo "Keep newest ${KEEP} release versions; dry_run=${DRY_RUN}"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# --- collect every version (id, digest, tags, created_at) --------------------
versions_api="/users/${OWNER}/packages/container/${PKG}/versions"
gh api --paginate "${versions_api}?per_page=100" \
  --jq '.[] | {id, digest: .name, tags: (.metadata.container.tags // []), created_at}' \
  >"$work/versions.jsonl"
total=$(wc -l <"$work/versions.jsonl")
echo "Found ${total} versions total."
if [ "$total" -eq 0 ]; then
  echo "Nothing to prune in ghcr.io/${OWNER}/${PKG}." | tee -a "${GITHUB_STEP_SUMMARY:-/dev/null}"
  exit 0
fi

# --- decide what to retain --------------------------------------------------
# Retained: versions carrying a protected tag, the newest KEEP versions that
# carry a release tag, and versions carrying any tag we do not recognise.
# Everything tagged that is not retained is a dropped release (or its sha-).
jq -cs --argjson keep "$KEEP" '
  # Release tags carry no v prefix (docker/metadata-action type=semver strips
  # it; only the git tag is vX.Y.Z), but the earliest pushes did, so accept both.
  def isVersion: test("^v?[0-9]+\\.[0-9]+\\.[0-9]+$");
  def isProtected: (. == "latest") or (. == "dev") or test("^[0-9]+\\.[0-9]+$");
  def isSha: startswith("sha-");
  def isKnown: isVersion or isProtected or isSha;

  ( [ .[] | select(any(.tags[]; isVersion)) ]
    | sort_by(.created_at) | reverse | .[0:$keep] | map(.id) ) as $keepIds
  | map(
      . as $v
      | $v + { retained:
                 ( any($v.tags[]; isProtected)
                   or (($keepIds | index($v.id)) != null)
                   or any($v.tags[]; isKnown | not) ) }
    )
' "$work/versions.jsonl" >"$work/classified.json"

# --- protect the children of retained multi-manifest versions ---------------
# One anonymous-or-authenticated pull token for the registry; the package may
# be private, and GITHUB_TOKEN is accepted as the password on ghcr.io.
registry_token=$(curl -fsSL -u "${OWNER}:${GH_TOKEN}" \
  "https://ghcr.io/token?scope=repository:${OWNER}/${PKG}:pull" | jq -r '.token')
if [ -z "$registry_token" ] || [ "$registry_token" = "null" ]; then
  echo "::error::could not obtain a GHCR pull token for ${OWNER}/${PKG}"
  exit 1
fi

accept='application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json'
: >"$work/children.txt"
while IFS= read -r digest; do
  [ -n "$digest" ] || continue
  if ! manifest=$(curl -fsSL -H "Authorization: Bearer ${registry_token}" -H "Accept: ${accept}" \
      "https://ghcr.io/v2/${OWNER}/${PKG}/manifests/${digest}"); then
    echo "::error::could not read retained manifest ${digest}; aborting so no child of it gets deleted"
    exit 1
  fi
  jq -r '.manifests[]?.digest // empty' <<<"$manifest" >>"$work/children.txt"
done < <(jq -r '.[] | select(.retained) | .digest' "$work/classified.json")
protected_children=$(sort -u "$work/children.txt" | jq -R . | jq -cs .)
echo "Retained versions reference $(jq length <<<"$protected_children") child manifest(s)."

# --- final delete list ------------------------------------------------------
# Delete: not retained, and (untagged and not a protected child) or (tagged).
jq -c --argjson children "$protected_children" '
  .[]
  | select(.retained | not)
  | . as $v
  | select(($children | index($v.digest)) == null)
  | {id, digest, tags}
' "$work/classified.json" >"$work/delete.jsonl"

count=$(wc -l <"$work/delete.jsonl")
if [ "$count" -eq 0 ]; then
  echo "Nothing to prune in ghcr.io/${OWNER}/${PKG}." | tee -a "${GITHUB_STEP_SUMMARY:-/dev/null}"
  exit 0
fi

{
  echo "### GHCR prune (ghcr.io/${OWNER}/${PKG})"
  echo ""
  echo "dry_run=\`${DRY_RUN}\` — ${count} version(s) targeted:"
  echo ""
  jq -r '"- `\(.digest[0:19])` \(if (.tags|length) > 0 then "tags: " + (.tags|join(", ")) else "(untagged)" end)"' \
    "$work/delete.jsonl"
} >>"${GITHUB_STEP_SUMMARY:-/dev/null}"

# --- delete -------------------------------------------------------------------
failed=0
while IFS= read -r line; do
  [ -n "$line" ] || continue
  id=$(jq -r '.id' <<<"$line")
  label=$(jq -r '"\(.digest[0:19]) [\(.tags|join(","))]"' <<<"$line")
  if [ "$DRY_RUN" = "true" ]; then
    echo "[dry-run] would delete: ${label} (id ${id})"
    continue
  fi
  echo "Deleting: ${label} (id ${id})"
  # 404: already gone (a concurrent run or a manual delete); anything else is
  # a real failure, reported per version so one refusal does not hide the rest.
  code=$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE \
    -H "Authorization: Bearer ${GH_TOKEN}" -H "Accept: application/vnd.github+json" \
    "https://api.github.com${versions_api}/${id}")
  if [ "$code" != "204" ] && [ "$code" != "404" ]; then
    echo "::error::Failed to delete version ${id} (${label}): HTTP ${code}"
    failed=$((failed + 1))
  fi
done <"$work/delete.jsonl"

if [ "$failed" -gt 0 ]; then
  echo "::error::${failed} version(s) could not be deleted in ghcr.io/${OWNER}/${PKG}"
  exit 1
fi
echo "Prune complete for ghcr.io/${OWNER}/${PKG}."
