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
# Renaming or moving an allowlisted file is the one hand-edit the list takes: move
# the entry to the new path with the same or a lower count, never a new entry.
#
# The allowlist lives in the branch, so a branch could edit it to exempt whatever
# it just grew. --base makes shrink-only a check rather than a comment: it reads
# the allowlist as it exists at a trusted ref and fails on any entry whose count
# is higher than at that ref, and on any entry the base does not have. A rename is
# the exception the pairing recognises: an added path is allowed when an entry the
# base has is gone from the new list with a count at least as large. Added and
# removed entries pair off largest-count first, so the list can never gain an
# entry. Lowered counts and removed entries always pass.
#
# Usage:
#   size-gate.sh                check; quiet on success, one line per violation
#   size-gate.sh --base <ref>   also compare the allowlist against that ref's copy
#   size-gate.sh --list         print every current offender as path<TAB>lines
#   size-gate.sh --update       lower the recorded counts, drop stale entries
#
# The base ref also comes from SIZE_GATE_BASE, which is how CI passes the pull
# request's base commit. With neither set the comparison runs against
# origin/master, and says so and skips when that ref is not in the clone.
#
# Exit codes: 0 clean, 1 violations found, 2 bad usage or unreadable allowlist.

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
ALLOWLIST_REL="scripts/ci/size-allowlist.txt"
ALLOWLIST="$ROOT/$ALLOWLIST_REL"

# The ref whose allowlist the working-tree one is compared against. An explicitly
# named ref that cannot be resolved is a hard error, because whoever named it
# asked for the comparison; the origin/master fallback merely skips, since a fresh
# clone or a shallow CI checkout legitimately lacks it.
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

# A file counts as a test when its name or its directory says so. Tests get the
# higher ceiling because table-driven Go tests and per-concern vitest files are
# legitimately long, but they stay capped so splitting one stays cheap.
is_test_file() {
	case "$1" in
	*_test.go | *.test.ts | *.test.tsx | *.spec.ts | *.spec.tsx) return 0 ;;
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

# Machine-written files are exempt: their length is decided by their generator,
# not by anyone who could split them. The content sniff reads the first three
# lines, where both conventions put the marker, and runs only on files already
# past their ceiling, so the common case reads no file contents at all.
is_generated() {
	case "$1" in
	*.pb.go | *_gen.go | *.gen.go | *.gen.ts | *.gen.tsx) return 0 ;;
	esac
	head -n 3 "$ROOT/$1" 2>/dev/null | grep -qE 'Code generated .* DO NOT EDIT|@generated'
}

# Scratch files for the scope walk and the offender list. They are read back in
# the main shell rather than through a process substitution, so a failure inside
# either step can exit the whole script instead of just a subshell.
SCOPE_FILE=$(mktemp)
OFFENDERS_FILE=$(mktemp)
PARSED_FILE=$(mktemp)
BASE_FILE=$(mktemp)
trap 'rm -f "$SCOPE_FILE" "$SCOPE_FILE.raw" "$OFFENDERS_FILE" "$PARSED_FILE" "$BASE_FILE"' EXIT

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

# Writes every in-scope path, repo-relative and sorted, to $SCOPE_FILE. Both find
# runs report through one status, so an unreadable subdirectory in the FIRST of
# them cannot be masked by the second one succeeding.
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
		if is_generated "$path"; then
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
		echo "size-gate: missing allowlist $ALLOWLIST" >&2
		exit 2
	fi
	parse_allowlist "$ALLOWLIST" "$ALLOWLIST"
	while IFS=$'\t' read -r path lines; do
		RECORDED[$path]=$lines
	done <"$PARSED_FILE"
}

# Fills BASE_RECORDED from the allowlist at $BASE_REF. Returns 1 when there is
# nothing to compare against, having said why on stdout.
load_base_allowlist() {
	local path lines
	if ! git -C "$ROOT" rev-parse --verify --quiet "$BASE_REF" >/dev/null 2>&1; then
		if [ "$BASE_EXPLICIT" -eq 1 ]; then
			echo "size-gate: base ref $BASE_REF is not in this clone; fetch it before comparing" >&2
			exit 2
		fi
		echo "size-gate: $BASE_REF is not in this clone, skipping the shrink-only comparison"
		return 1
	fi
	if ! git -C "$ROOT" show "$BASE_REF:$ALLOWLIST_REL" >"$BASE_FILE" 2>/dev/null; then
		echo "size-gate: no allowlist at $BASE_REF, skipping the shrink-only comparison"
		return 1
	fi
	parse_allowlist "$BASE_FILE" "$BASE_REF:$ALLOWLIST_REL"
	while IFS=$'\t' read -r path lines; do
		BASE_RECORDED[$path]=$lines
	done <"$PARSED_FILE"
	return 0
}

# Enforces shrink-only against the base: no count may rise, and an added path is
# allowed only as a rename of a removed one. Added and removed entries pair off
# largest-count first, which finds a valid pairing whenever one exists, so an
# added entry that is left without a partner or outgrows the partner it gets is
# an entry the list is gaining.
compare_with_base() {
	local path entry count acount apath rcount rpath index=0
	local -a added=() removed=() added_sorted=() removed_sorted=()

	for path in "${!RECORDED[@]}"; do
		count=${RECORDED[$path]}
		if [ -z "${BASE_RECORDED[$path]+set}" ]; then
			added+=("$count"$'\t'"$path")
		elif [ "$count" -gt "${BASE_RECORDED[$path]}" ]; then
			echo "$path: recorded $count, ${BASE_RECORDED[$path]} at $BASE_REF: a recorded count may only be lowered" >&2
			VIOLATIONS=$((VIOLATIONS + 1))
		fi
	done

	for path in "${!BASE_RECORDED[@]}"; do
		if [ -z "${RECORDED[$path]+set}" ]; then
			removed+=("${BASE_RECORDED[$path]}"$'\t'"$path")
		fi
	done

	if [ ${#added[@]} -eq 0 ]; then
		return
	fi
	mapfile -t added_sorted < <(printf '%s\n' "${added[@]}" | LC_ALL=C sort -rn)
	if [ ${#removed[@]} -gt 0 ]; then
		mapfile -t removed_sorted < <(printf '%s\n' "${removed[@]}" | LC_ALL=C sort -rn)
	fi

	for entry in "${added_sorted[@]}"; do
		acount=${entry%%$'\t'*}
		apath=${entry#*$'\t'}
		if [ "$index" -ge ${#removed_sorted[@]} ]; then
			echo "$apath: new entry at $acount lines with no entry dropped to pair it with: the allowlist takes no new entries (a rename moves an existing one)" >&2
			VIOLATIONS=$((VIOLATIONS + 1))
		else
			rcount=${removed_sorted[$index]%%$'\t'*}
			rpath=${removed_sorted[$index]#*$'\t'}
			if [ "$acount" -gt "$rcount" ]; then
				echo "$apath: new entry at $acount lines, pairs with the dropped $rpath at $rcount: a rename may not raise the count" >&2
				VIOLATIONS=$((VIOLATIONS + 1))
			fi
		fi
		index=$((index + 1))
	done
}

# Compares the tree against the allowlist. Violations go to stderr, shrink hints
# to stdout, and a clean run prints nothing at all.
check() {
	local path lines ceiling recorded
	local -A seen=()
	collect_offenders

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

	# Leftover entries: the file is back under its ceiling, generated, or gone.
	# All three mean the entry buys nothing, so it has to go and the list cannot
	# quietly accumulate exemptions nobody needs.
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
	# files. Both run before anything is reported, so one run names every problem.
	if [ -z "$BASE_REF" ]; then
		BASE_REF=origin/master
	fi
	if load_base_allowlist; then
		compare_with_base
	fi
	check
	if [ "$VIOLATIONS" -ne 0 ]; then
		echo "size-gate: $VIOLATIONS violation(s)" >&2
		exit 1
	fi
	;;
esac
