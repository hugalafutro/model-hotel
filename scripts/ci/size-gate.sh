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
# Usage:
#   size-gate.sh            check; quiet on success, one line per violation
#   size-gate.sh --list     print every current offender as path<TAB>lines
#   size-gate.sh --update   lower the recorded counts, drop stale entries
#
# Exit codes: 0 clean, 1 violations found, 2 bad usage or unreadable allowlist.

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
ALLOWLIST="$ROOT/scripts/ci/size-allowlist.txt"

PROD_CEILING=800
TEST_CEILING=2000

# Trees the gate covers. These are the source roots only, so build output,
# vendored dependencies, locale catalogs and the generated SQL migrations are
# out of scope by construction rather than by exclusion pattern.
GO_ROOTS=(internal cmd)
TS_ROOTS=(web/src frontdesk/web/src)

usage() {
	cat <<-'USAGE'
		size-gate.sh            check every source file against its ceiling
		size-gate.sh --list     print every current offender as path<TAB>lines
		size-gate.sh --update   lower the recorded counts, drop stale entries

		Ceilings: 800 lines of production code, 2000 lines of tests.
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
# not by anyone who could split them. Only files already past their ceiling are
# sniffed, so the common case reads no file contents at all.
is_generated() {
	case "$1" in
	*.pb.go | *_gen.go | *.gen.go | *.gen.ts | *.gen.tsx) return 0 ;;
	esac
	head -n 3 "$ROOT/$1" 2>/dev/null | grep -qE 'Code generated .* DO NOT EDIT|@generated'
}

# Every in-scope path, repo-relative, one per line.
list_scope() {
	(
		cd "$ROOT"
		find "${GO_ROOTS[@]}" -type f -name '*.go' -print
		find "${TS_ROOTS[@]}" -type f \( -name '*.ts' -o -name '*.tsx' \) -print
	)
}

# path<TAB>lines for every file currently above its ceiling, sorted by path.
# Line counts are newline counts, exactly what `wc -l` reports.
list_offenders() {
	local path lines ceiling
	while IFS= read -r path; do
		lines=$(wc -l <"$ROOT/$path")
		ceiling=$(ceiling_for "$path")
		if [ "$lines" -le "$ceiling" ]; then
			continue
		fi
		if is_generated "$path"; then
			continue
		fi
		printf '%s\t%s\n' "$path" "$lines"
	done < <(list_scope | LC_ALL=C sort)
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

# Recorded counts keyed by path. Malformed and duplicate entries are rejected so
# a botched edit fails the gate instead of silently exempting a file.
declare -A RECORDED=()

load_allowlist() {
	local line path lines lineno=0
	if [ ! -f "$ALLOWLIST" ]; then
		echo "size-gate: missing allowlist $ALLOWLIST" >&2
		exit 2
	fi
	while IFS= read -r line || [ -n "$line" ]; do
		lineno=$((lineno + 1))
		case "$line" in '#'* | '') continue ;; esac
		path=${line%%$'\t'*}
		lines=${line#*$'\t'}
		if [ "$path" = "$line" ] || [ -z "$path" ] || ! [[ $lines =~ ^[0-9]+$ ]]; then
			echo "size-gate: $ALLOWLIST:$lineno: expected path<TAB>lines, got: $line" >&2
			exit 2
		fi
		if [ -n "${RECORDED[$path]+set}" ]; then
			echo "size-gate: $ALLOWLIST:$lineno: duplicate entry for $path" >&2
			exit 2
		fi
		RECORDED[$path]=$lines
	done <"$ALLOWLIST"
}

# Compares the tree against the allowlist. Violations go to stderr, shrink hints
# to stdout, and a clean run prints nothing at all.
check() {
	local violations=0 path lines ceiling recorded
	local -A seen=()

	while IFS=$'\t' read -r path lines; do
		seen[$path]=1
		ceiling=$(ceiling_for "$path")
		recorded=${RECORDED[$path]-}
		if [ -z "$recorded" ]; then
			echo "$path: $lines lines, ceiling $ceiling: split it (the allowlist takes no new entries)" >&2
			violations=$((violations + 1))
		elif [ "$lines" -gt "$recorded" ]; then
			echo "$path: $lines lines, recorded $recorded (ceiling $ceiling): allowlisted files may shrink, never grow" >&2
			violations=$((violations + 1))
		elif [ "$lines" -lt "$recorded" ]; then
			echo "$path shrank to $lines lines (recorded $recorded): run scripts/ci/size-gate.sh --update to tighten the ratchet"
		fi
	done < <(list_offenders)

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
		violations=$((violations + 1))
	done

	if [ "$violations" -ne 0 ]; then
		echo "size-gate: $violations violation(s)" >&2
		return 1
	fi
	return 0
}

# Lowers recorded counts to match reality and drops stale entries. It never
# raises a count and never adds a path, so running it cannot loosen the ratchet.
update() {
	local path lines
	local -A current=()
	while IFS=$'\t' read -r path lines; do
		current[$path]=$lines
	done < <(list_offenders)

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

case "${1-}" in
"")
	load_allowlist
	check
	;;
--list)
	list_offenders
	;;
--update)
	load_allowlist
	update
	;;
-h | --help)
	usage
	;;
*)
	echo "size-gate: unknown argument: $1" >&2
	usage >&2
	exit 2
	;;
esac
