#!/usr/bin/env bash
# Size gate: caps how long a source file may get, as a ratchet that only tightens.
#
# Every file in scope has a ceiling (800 lines for production code, 2000 for
# tests). Files already above it are recorded in size-allowlist.txt with the
# exact line count they had when the gate landed, and that count becomes their
# personal ceiling: they may shrink, they may not grow. The gate fails when a
# file crosses its ceiling with no entry, when an allowlisted file exceeds its
# recorded count, and when an entry has gone stale (its file is back under the
# ceiling, or gone). So the list only ever shrinks and the godfiles stop growing.
#
# A line is a newline, so the gate also rejects the two shapes whose size it cannot
# measure rather than merely dislikes: a symlink, whose target the walk never
# reads, and any file holding a CR byte, since lone CR separators hide every line
# from a newline count. Neither is a size the allowlist can record, so neither has
# an exemption.
#
# Renaming or moving an allowlisted file is the one hand-edit the list takes: move
# the entry to the new path with the same or a lower count, never a new entry.
#
# The allowlist lives in the branch, so a branch could edit it to exempt whatever
# it just grew. --base makes shrink-only a check rather than a comment: it reads
# the allowlist as it exists at a trusted ref and fails on any entry whose count
# is higher than at that ref, and on any entry the base does not have. A rename is
# the one exception: a new entry is accepted only for a path git detects as an
# EXACT rename of a removed entry (--find-renames=100%, byte-identical content),
# at the same or a lower count. So an exemption follows a file only through a pure
# move: edit the file in the same commit that moves it, copy it, or swap an
# unrelated oversized file in for a dropped one, and the new path is an addition
# and fails. Move the file first and edit it after, keep it where it is, or split
# it under the ceiling. Lowered counts and removed entries always pass.
#
# A base with no allowlist at all is only legitimate while the gate is being
# introduced, so it is an error unless the base has no size-gate.sh either. In
# that bootstrap case the base is empty and snapshot mode applies instead: an entry
# has to name a file the base branch already carried over the ceiling, record that
# file's exact size now, and record no more than it measured at the base. So the
# first list can only inherit debt that predates the gate, in the diff that
# introduces it, and the branch cannot exempt a file it adds or grows itself.
#
# Usage:
#   size-gate.sh                check; quiet on success, one line per violation
#   size-gate.sh --base <ref>   also compare the allowlist against that ref's copy
#   size-gate.sh --list         print every current offender as path<TAB>lines
#   size-gate.sh --update       lower the recorded counts, drop stale entries
#
# The base ref also comes from SIZE_GATE_BASE, which is how CI passes the pull
# request's base commit. With neither set the comparison runs against
# origin/master. A base that does not resolve to a commit is exit 2; a base that
# resolves but does not carry the allowlist yet is the one case that says so and
# skips, and the working-tree checks still run.
#
# Exit codes: 0 clean, 1 violations found, 2 bad usage or unreadable allowlist.

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
ALLOWLIST_REL="scripts/ci/size-allowlist.txt"
ALLOWLIST="$ROOT/$ALLOWLIST_REL"
GATE_REL="scripts/ci/size-gate.sh"

# Set when the base predates the gate itself, which turns the comparison into an
# exact snapshot of the tree rather than a shrink-only diff.
BOOTSTRAP=0

# The ref whose allowlist the working-tree one is compared against. A base that
# does not resolve to a commit is a hard error either way: skipping there would
# turn a mistyped ref, or a fetch that did not bring the base into the clone, into
# a silent pass on the one check that stops the allowlist from growing. The flag
# only picks the wording, so the fallback can point at the override.
BASE_REF=${SIZE_GATE_BASE-}
BASE_EXPLICIT=0
if [ -n "$BASE_REF" ]; then
	BASE_EXPLICIT=1
fi

# Running total across the base comparison and the working-tree check, so one run
# reports every problem and prints a single summary.
VIOLATIONS=0

PROD_CEILING=800
TEST_CEILING=2000

# Trees the gate covers. These are the source roots only, so build output,
# vendored dependencies, locale catalogs and the generated SQL migrations are
# out of scope by construction rather than by exclusion pattern. web-shared/ is
# in scope because both SPAs compile it through their @quota-shared alias, which
# makes it shipped frontend code like anything under either src/.
#
# Every .go, .ts and .tsx file under these roots is measured. Nothing a file says
# about itself exempts it, because a marker a file carries is a marker any file
# can add. Should a generator ever write into one of these roots, its output is
# exempted by an explicit path list here, reviewed like any other script change.
GO_ROOTS=(internal cmd)
TS_ROOTS=(web/src frontdesk/web/src web-shared)

usage() {
	cat <<-'USAGE'
		size-gate.sh                check every source file against its ceiling
		size-gate.sh --base <ref>   also check the allowlist only shrank since <ref>
		size-gate.sh --list         print every current offender as path<TAB>lines
		size-gate.sh --update       lower the recorded counts, drop stale entries

		Ceilings: 800 lines of production code, 2000 lines of tests.
		The base ref also comes from SIZE_GATE_BASE; it defaults to origin/master.
		Exit codes: 0 clean, 1 violations found, 2 bad usage or unreadable allowlist.
	USAGE
}

# Tests get the higher ceiling because table-driven Go tests and per-concern vitest
# files are legitimately long, but they stay capped so splitting one stays cheap.
#
# What counts as a test is decided by DIRECTORY on the TypeScript side, never by
# the filename: a .test.ts suffix is something a production file can be given, and
# that would hand it the 2000-line ceiling. A suite therefore lives in a
# __tests__/ directory or in an app's test/ directory, which is where this repo
# keeps them anyway; a .test.ts or .spec.ts anywhere else is production code and
# gets the production ceiling. Go keeps its suffix rule because the toolchain
# enforces it: _test.go files are compiled only into the test binary.
is_test_file() {
	case "$1" in
	*_test.go) return 0 ;;
	*/__tests__/*) return 0 ;;
	web/src/test/* | frontdesk/web/src/test/*) return 0 ;;
	esac
	return 1
}

ceiling_for() {
	if is_test_file "$1"; then
		echo "$TEST_CEILING"
	else
		echo "$PROD_CEILING"
	fi
}

# Scratch files for the scope walk and the offender list. They are read back in
# the main shell rather than through a process substitution, so a failure inside
# either step can exit the whole script instead of just a subshell.
SCOPE_FILE=$(mktemp)
OFFENDERS_FILE=$(mktemp)
PARSED_FILE=$(mktemp)
BASE_FILE=$(mktemp)
RENAME_FILE=$(mktemp)
LINK_FILE=$(mktemp)
CR_FILE=$(mktemp)
trap 'rm -f "$SCOPE_FILE" "$SCOPE_FILE.raw" "$OFFENDERS_FILE" "$PARSED_FILE" "$BASE_FILE" "$RENAME_FILE" "$LINK_FILE" "$CR_FILE"' EXIT

# A gate that scans less than it claims and still reports clean is worse than no
# gate, so a scope root that is not a directory is a hard error, not a warning.
require_scope_roots() {
	local dir
	for dir in "${GO_ROOTS[@]}" "${TS_ROOTS[@]}"; do
		if [ ! -d "$ROOT/$dir" ]; then
			echo "size-gate: scope root missing: $dir" >&2
			exit 2
		fi
	done
}

# Writes every in-scope path, repo-relative and sorted, to $SCOPE_FILE, and every
# in-scope symlink to $LINK_FILE. Both find runs report through one status, so an
# unreadable subdirectory in the FIRST of them cannot be masked by the second one
# succeeding.
#
# -type f skips symlinks, which would otherwise let a source path stand in for a
# file outside the scan, so they are collected here and rejected in check().
walk_scope() {
	local status=0
	(
		cd "$ROOT" || exit 3
		rc=0
		find "${GO_ROOTS[@]}" -type f -name '*.go' -print || rc=$?
		find "${TS_ROOTS[@]}" -type f \( -name '*.ts' -o -name '*.tsx' \) -print || rc=$?
		exit "$rc"
	) >"$SCOPE_FILE.raw" || status=$?
	if [ "$status" -ne 0 ]; then
		echo "size-gate: could not walk the scope roots (find exited $status)" >&2
		exit 2
	fi
	LC_ALL=C sort "$SCOPE_FILE.raw" >"$SCOPE_FILE"
	rm -f "$SCOPE_FILE.raw"

	status=0
	(
		cd "$ROOT" || exit 3
		find "${GO_ROOTS[@]}" "${TS_ROOTS[@]}" -type l -print
	) >"$LINK_FILE" || status=$?
	if [ "$status" -ne 0 ]; then
		echo "size-gate: could not scan the scope roots for symlinks (find exited $status)" >&2
		exit 2
	fi
}

# Writes every in-scope file holding a CR byte to $CR_FILE. Line counts are
# newline counts, so a file separated by lone CRs measures zero lines however long
# it is, which is the last way a file could dictate its own measurement. Rejecting
# CR outright is the structural answer, and the repo wants LF everywhere regardless
# (.gitattributes pins it, biome enforces it for TypeScript). One grep over the
# whole list, so this costs a single process rather than one per file.
scan_line_endings() {
	local status=0
	local -a files=()
	mapfile -t files <"$SCOPE_FILE"
	if [ ${#files[@]} -eq 0 ]; then
		: >"$CR_FILE"
		return
	fi
	(cd "$ROOT" && LC_ALL=C grep -lU -e "$(printf '\r')" -- "${files[@]}") >"$CR_FILE" 2>/dev/null || status=$?
	# grep exits 1 when nothing matches, which is the clean case; anything above
	# that is a real failure and must not read as "no CR anywhere".
	if [ "$status" -gt 1 ]; then
		echo "size-gate: could not scan the scope for CR line endings (grep exited $status)" >&2
		exit 2
	fi
}

# Writes path<TAB>lines to $OFFENDERS_FILE for every file above its ceiling,
# sorted by path. Line counts are newline counts, exactly what `wc -l` reports.
collect_offenders() {
	local path lines ceiling
	require_scope_roots
	walk_scope
	: >"$OFFENDERS_FILE"
	while IFS= read -r path; do
		lines=$(wc -l <"$ROOT/$path")
		ceiling=$(ceiling_for "$path")
		if [ "$lines" -le "$ceiling" ]; then
			continue
		fi
		printf '%s\t%s\n' "$path" "$lines" >>"$OFFENDERS_FILE"
	done <"$SCOPE_FILE"
}

# Writes an allowlist file from the path<TAB>lines pairs on stdin, header included.
write_allowlist() {
	{
		cat <<-'HEADER'
			# Files above the size gate's ceiling (800 lines of production code, 2000 of
			# tests), each with the line count that is its personal ceiling.
			#
			# This list only shrinks. A recorded count may be lowered when its file gets
			# smaller (scripts/ci/size-gate.sh --update does that), and an entry is dropped
			# once its file is back under the ceiling. Nothing may be added: a new file
			# over the ceiling gets split instead.
			#
			# Format: path<TAB>lines
		HEADER
		cat
	} >"$ALLOWLIST.tmp"
	mv "$ALLOWLIST.tmp" "$ALLOWLIST"
}

# Recorded counts keyed by path: RECORDED from the working tree, BASE_RECORDED
# from the base ref. Malformed and duplicate entries are rejected so a botched
# edit fails the gate instead of silently exempting a file.
declare -A RECORDED=()
declare -A BASE_RECORDED=()

# New path -> old path for every rename git detects between the base and the
# working tree, filled on demand by load_renames.
declare -A RENAMED_FROM=()

# Validates the allowlist in file $1 (named $2 in errors) and writes its entries
# to $PARSED_FILE as path<TAB>lines. The redirection keeps this in the main shell,
# so a malformed entry exits the script rather than a subshell.
parse_allowlist() {
	local file=$1 label=$2 line path lines lineno=0
	local -A seen_paths=()
	: >"$PARSED_FILE"
	while IFS= read -r line || [ -n "$line" ]; do
		lineno=$((lineno + 1))
		case "$line" in '#'* | '') continue ;; esac
		path=${line%%$'\t'*}
		lines=${line#*$'\t'}
		if [ "$path" = "$line" ] || [ -z "$path" ] || ! [[ $lines =~ ^[0-9]+$ ]]; then
			echo "size-gate: $label:$lineno: expected path<TAB>lines, got: $line" >&2
			exit 2
		fi
		if [ -n "${seen_paths[$path]+set}" ]; then
			echo "size-gate: $label:$lineno: duplicate entry for $path" >&2
			exit 2
		fi
		seen_paths[$path]=1
		printf '%s\t%s\n' "$path" "$lines" >>"$PARSED_FILE"
	done <"$file"
}

load_allowlist() {
	local path lines
	if [ ! -f "$ALLOWLIST" ]; then
		echo "size-gate: missing allowlist $ALLOWLIST; restore it from the base branch" >&2
		exit 2
	fi
	parse_allowlist "$ALLOWLIST" "$ALLOWLIST"
	while IFS=$'\t' read -r path lines; do
		RECORDED[$path]=$lines
	done <"$PARSED_FILE"
}

# Fills BASE_RECORDED from the allowlist at $BASE_REF, or sets BOOTSTRAP when the
# base predates the gate. Anything else it cannot read is a hard error: a base
# whose allowlist the gate cannot see is a comparison that did not happen, and
# skipping one is how a branch would exempt whatever it just grew.
load_base_allowlist() {
	local path lines
	# ^{commit} is load-bearing: rev-parse --verify accepts any full 40-hex string
	# as a well-formed object name and returns 0 without the object being present,
	# which is exactly the shape CI passes. Peeling to a commit is what actually
	# reads the object store.
	if ! git -C "$ROOT" rev-parse --verify --quiet "${BASE_REF}^{commit}" >/dev/null 2>&1; then
		if [ "$BASE_EXPLICIT" -eq 1 ]; then
			echo "size-gate: base ref $BASE_REF is not in this clone; fetch it before comparing" >&2
		else
			echo "size-gate: base ref $BASE_REF is not in this clone; fetch it, or name another with --base" >&2
		fi
		exit 2
	fi
	if git -C "$ROOT" show "$BASE_REF:$ALLOWLIST_REL" >"$BASE_FILE" 2>/dev/null; then
		parse_allowlist "$BASE_FILE" "$BASE_REF:$ALLOWLIST_REL"
		while IFS=$'\t' read -r path lines; do
			BASE_RECORDED[$path]=$lines
		done <"$PARSED_FILE"
		return 0
	fi
	# The base has no allowlist. That is only the truth while the gate is being
	# introduced, which the base's own copy of the script settles: if the script
	# is there, the allowlist belongs beside it and its absence means the branch
	# deleted it or the base is the wrong ref.
	if git -C "$ROOT" cat-file -e "$BASE_REF:$GATE_REL" 2>/dev/null; then
		echo "size-gate: $BASE_REF carries $GATE_REL but no $ALLOWLIST_REL; the allowlist comes from the base branch, so restore it or name the right base" >&2
		exit 2
	fi
	BOOTSTRAP=1
	echo "size-gate: $BASE_REF predates the gate; bootstrap snapshot mode, where an entry names only a file already over its ceiling at $BASE_REF, at its exact current size and no more than it measured there"
	return 0
}

# Renames git detects between the base and the working tree, keyed by new path.
# --find-renames=100% means byte-identical content, so only a pure move carries an
# entry across: edit as much as one line while moving and the new path is an
# addition. --diff-filter=R keeps renames only and copy detection stays off, so a
# file copied to a second path is an addition too. The diff has no second ref on
# purpose: it compares the tree, which is the PR head in CI and picks up a staged
# `git mv` locally.
load_renames() {
	local status old new
	if ! git -C "$ROOT" -c core.quotePath=false diff --name-status \
		--find-renames=100% --diff-filter=R "$BASE_REF" -- \
		"${GO_ROOTS[@]}" "${TS_ROOTS[@]}" >"$RENAME_FILE" 2>/dev/null; then
		echo "size-gate: could not diff the scope roots against $BASE_REF" >&2
		exit 2
	fi
	while IFS=$'\t' read -r status old new; do
		case "$status" in R*) ;; *) continue ;; esac
		RENAMED_FROM[$new]=$old
	done <"$RENAME_FILE"
}

# Enforces shrink-only against the base: no count may rise, and a path the base
# does not list is an entry the list is gaining unless git calls it a rename of an
# entry the list is dropping. Git decides what a rename is, so an unrelated
# oversized file cannot take a dropped entry's place by matching its line count.
compare_with_base() {
	local path count base_lines apath acount origin
	local -a added=()
	local -A actual=()

	# Bootstrap: the base is empty, so every entry is new and the rename rule would
	# reject the whole list. Snapshot mode takes its place, and it photographs the
	# BASE tree, not this one. An entry has to name a file the base branch already
	# carried over the ceiling, record that file's exact size now, and record no
	# more than it measured at the base. So the first list can only ever inherit
	# debt that predates the gate: a file the branch itself adds or grows past the
	# ceiling has no entry available to it, and no entry grants headroom. Entries
	# for files that are not over their ceiling now are caught by the stale-entry
	# rule in check(), and a file over its ceiling with no entry by rule one.
	if [ "$BOOTSTRAP" -eq 1 ]; then
		while IFS=$'\t' read -r path count; do
			actual[$path]=$count
		done <"$OFFENDERS_FILE"
		for path in "${!RECORDED[@]}"; do
			count=${actual[$path]-}
			if [ -z "$count" ]; then
				continue
			fi
			if ! git -C "$ROOT" cat-file -e "$BASE_REF:$path" 2>/dev/null; then
				echo "$path: not in $BASE_REF: a bootstrap snapshot exempts only files the base branch already carried over the ceiling" >&2
				VIOLATIONS=$((VIOLATIONS + 1))
				continue
			fi
			base_lines=$(git -C "$ROOT" cat-file -p "$BASE_REF:$path" | wc -l)
			if [ "$base_lines" -le "$(ceiling_for "$path")" ]; then
				echo "$path: $base_lines lines at $BASE_REF, under its ceiling there: a bootstrap snapshot exempts only files the base branch already carried over the ceiling" >&2
				VIOLATIONS=$((VIOLATIONS + 1))
				continue
			fi
			if [ "${RECORDED[$path]}" -ne "$count" ]; then
				echo "$path: recorded ${RECORDED[$path]}, $count lines on disk: a bootstrap snapshot records each file's exact current size" >&2
				VIOLATIONS=$((VIOLATIONS + 1))
			fi
			if [ "${RECORDED[$path]}" -gt "$base_lines" ]; then
				echo "$path: recorded ${RECORDED[$path]}, $base_lines lines at $BASE_REF: a bootstrap snapshot may not record more than the base branch carried" >&2
				VIOLATIONS=$((VIOLATIONS + 1))
			fi
		done
		return
	fi

	for path in "${!RECORDED[@]}"; do
		count=${RECORDED[$path]}
		if [ -z "${BASE_RECORDED[$path]+set}" ]; then
			added+=("$path")
		elif [ "$count" -gt "${BASE_RECORDED[$path]}" ]; then
			echo "$path: recorded $count, ${BASE_RECORDED[$path]} at $BASE_REF: a recorded count may only be lowered" >&2
			VIOLATIONS=$((VIOLATIONS + 1))
		fi
	done

	if [ ${#added[@]} -eq 0 ]; then
		return
	fi
	load_renames

	for apath in "${added[@]}"; do
		acount=${RECORDED[$apath]}
		origin=${RENAMED_FROM[$apath]-}
		if [ -z "$origin" ]; then
			echo "$apath: new entry at $acount lines: the allowlist takes an entry for a new path only when git detects an EXACT rename of an allowlisted file into it, so split it, or move it in one commit and edit it in the next" >&2
			VIOLATIONS=$((VIOLATIONS + 1))
		elif [ -z "${BASE_RECORDED[$origin]+set}" ]; then
			echo "$apath: renamed from $origin, which $BASE_REF does not allowlist: an entry moves with a file that already had one" >&2
			VIOLATIONS=$((VIOLATIONS + 1))
		elif [ -n "${RECORDED[$origin]+set}" ]; then
			echo "$apath: renamed from $origin, whose entry is still listed: a rename moves its entry, it does not add a second one" >&2
			VIOLATIONS=$((VIOLATIONS + 1))
		elif [ "$acount" -gt "${BASE_RECORDED[$origin]}" ]; then
			echo "$apath: $acount lines, ${BASE_RECORDED[$origin]} at $BASE_REF as $origin: a rename may not raise the count" >&2
			VIOLATIONS=$((VIOLATIONS + 1))
		fi
	done
}

# Rejects the two shapes that would make a file's size unmeasurable rather than
# merely large: a symlink, whose target the walk never reads, and a CR byte, which
# hides lines from a newline count. Neither is a size the allowlist can record, so
# neither has an exemption; the fix is to check in a real LF-separated file.
check_scope_hygiene() {
	local path
	while IFS= read -r path; do
		[ -n "$path" ] || continue
		echo "$path: symlink in the scope roots: the gate measures files, so check in the file itself" >&2
		VIOLATIONS=$((VIOLATIONS + 1))
	done <"$LINK_FILE"

	scan_line_endings
	while IFS= read -r path; do
		[ -n "$path" ] || continue
		echo "$path: CR line endings are not accepted in source: normalize the file to LF" >&2
		VIOLATIONS=$((VIOLATIONS + 1))
	done <"$CR_FILE"
}

# Compares the tree against the allowlist. Violations go to stderr, shrink hints
# to stdout, and a clean run prints nothing at all.
check() {
	local path lines ceiling recorded
	local -A seen=()

	while IFS=$'\t' read -r path lines; do
		seen[$path]=1
		ceiling=$(ceiling_for "$path")
		recorded=${RECORDED[$path]-}
		if [ -z "$recorded" ]; then
			echo "$path: $lines lines, ceiling $ceiling: split it (the allowlist takes no new entries)" >&2
			VIOLATIONS=$((VIOLATIONS + 1))
		elif [ "$lines" -gt "$recorded" ]; then
			echo "$path: $lines lines, recorded $recorded (ceiling $ceiling): allowlisted files may shrink, never grow" >&2
			VIOLATIONS=$((VIOLATIONS + 1))
		elif [ "$lines" -lt "$recorded" ]; then
			echo "$path shrank to $lines lines (recorded $recorded): run scripts/ci/size-gate.sh --update to tighten the ratchet"
		fi
	done <"$OFFENDERS_FILE"

	# Leftover entries: the file is back under its ceiling, or gone. Either way the
	# entry buys nothing, so it has to go and the list cannot quietly accumulate
	# exemptions nobody needs.
	for path in "${!RECORDED[@]}"; do
		if [ -n "${seen[$path]+set}" ]; then
			continue
		fi
		if [ -e "$ROOT/$path" ]; then
			echo "$path: no longer over its ceiling: remove its allowlist entry" >&2
		else
			echo "$path: allowlisted but missing: remove its allowlist entry" >&2
		fi
		VIOLATIONS=$((VIOLATIONS + 1))
	done
}

# Lowers recorded counts to match reality and drops stale entries. It never
# raises a count and never adds a path, so running it cannot loosen the ratchet.
update() {
	local path lines
	local -A current=()
	collect_offenders
	while IFS=$'\t' read -r path lines; do
		current[$path]=$lines
	done <"$OFFENDERS_FILE"

	for path in "${!RECORDED[@]}"; do
		lines=${current[$path]-}
		if [ -z "$lines" ]; then
			unset "RECORDED[$path]"
		elif [ "$lines" -lt "${RECORDED[$path]}" ]; then
			RECORDED[$path]=$lines
		fi
	done

	for path in "${!RECORDED[@]}"; do
		printf '%s\t%s\n' "$path" "${RECORDED[$path]}"
	done | LC_ALL=C sort | write_allowlist
}

MODE=check
while [ $# -gt 0 ]; do
	case "$1" in
	--list) MODE=list ;;
	--update) MODE=update ;;
	-h | --help) MODE=help ;;
	--base)
		shift
		if [ $# -eq 0 ]; then
			echo "size-gate: --base needs a git ref" >&2
			exit 2
		fi
		BASE_REF=$1
		BASE_EXPLICIT=1
		;;
	--base=*)
		BASE_REF=${1#--base=}
		BASE_EXPLICIT=1
		if [ -z "$BASE_REF" ]; then
			echo "size-gate: --base needs a git ref" >&2
			exit 2
		fi
		;;
	*)
		echo "size-gate: unknown argument: $1" >&2
		usage >&2
		exit 2
		;;
	esac
	shift
done

case "$MODE" in
help)
	usage
	;;
list)
	collect_offenders
	cat "$OFFENDERS_FILE"
	;;
update)
	load_allowlist
	update
	;;
check)
	load_allowlist
	# The base comparison guards the allowlist itself; the tree check guards the
	# files. Both read the same single walk of the tree, and both run before
	# anything is decided, so one run names every problem.
	if [ -z "$BASE_REF" ]; then
		BASE_REF=origin/master
	fi
	collect_offenders
	check_scope_hygiene
	load_base_allowlist
	compare_with_base
	check
	if [ "$VIOLATIONS" -ne 0 ]; then
		echo "size-gate: $VIOLATIONS violation(s)" >&2
		exit 1
	fi
	;;
esac
