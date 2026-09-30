#!/usr/bin/env bash
# go-race-shard.sh - run one shard of `go test -race` over the module.
#
# `go test -race ./...` runs packages in parallel, so the job takes about as
# long as its slowest package: internal/proxy alone was ~9.5 min under the race
# detector, with internal/api and internal/frontdesk behind it. This splits the
# work across CI runners instead:
#
#   - a HEAVY package (more than HEAVY_MIN top-level tests) is run on every
#     shard, each shard taking every TOTAL-th of its tests by sorted name;
#   - every other package runs whole on one shard, dealt round-robin.
#
# Test names are read from the _test.go sources, not from `go test -list`: the
# latter runs each package's TestMain, which for a database-backed package is
# a DROP, CREATE and every migration, and on a CI runner that setup cost more
# than the tests it listed (the reason an earlier CI sharding attempt was
# reverted). A top-level test, example or fuzz target is a line starting
# `func Test`, `func Example` or `func Fuzz`; every one in this module is
# declared that way, and --check proves the split covers each exactly once.
#
# Usage:
#   scripts/ci/go-race-shard.sh SHARD TOTAL        run shard SHARD of TOTAL
#   scripts/ci/go-race-shard.sh --plan SHARD TOTAL print what the shard runs
#   scripts/ci/go-race-shard.sh --check TOTAL      prove the split is a partition
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

HEAVY_MIN="${HEAVY_MIN:-400}"
TIMEOUT="${TIMEOUT:-20m}"

# packages prints every package directory that has test files, sorted.
packages() {
	find internal cmd -name '*_test.go' -printf '%h\n' | sort -u
}

# tests_in DIR prints the package's top-level test, example and fuzz names,
# sorted and unique (an internal and an external test package can share a
# directory; a name can only be declared once per package anyway). The files
# come from `go list`, which applies build constraints the way the race job's
# `go test` will (a //go:build live file is left out, a //go:build linux one
# kept) and reads sources only, never running a TestMain.
tests_in() {
	local files
	files=$(go list -f '{{range .TestGoFiles}}{{$.Dir}}/{{.}} {{end}}{{range .XTestGoFiles}}{{$.Dir}}/{{.}} {{end}}' "./$1")
	[ -n "$files" ] || return 0
	# shellcheck disable=SC2086 # one argument per file; paths hold no spaces
	grep -hoE '^func (Test|Example|Fuzz)[A-Za-z0-9_]*\(' $files |
		sed -E 's/^func //; s/\($//' | grep -vx 'TestMain' | sort -u || true
}

# plan SHARD TOTAL prints "heavy DIR NAME" and "light DIR" lines for the shard.
plan() {
	local shard=$1 total=$2 i=0 dir n
	while read -r dir; do
		n=$(tests_in "$dir" | wc -l)
		if [ "$n" -gt "$HEAVY_MIN" ]; then
			tests_in "$dir" | awk -v s="$shard" -v t="$total" -v d="$dir" \
				'(NR - 1) % t == s - 1 { print "heavy", d, $0 }'
		else
			if [ $((i % total)) -eq $((shard - 1)) ]; then
				echo "light $dir"
			fi
			i=$((i + 1))
		fi
	done < <(packages)
}

check() {
	local total=$1 shard dir want got
	local tmp
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' RETURN
	for shard in $(seq 1 "$total"); do plan "$shard" "$total"; done >"$tmp/all"
	local failed=0
	while read -r dir; do
		want=$(tests_in "$dir" | wc -l)
		if grep -qx "light $dir" "$tmp/all"; then
			got=$(grep -cx "light $dir" "$tmp/all")
			if [ "$got" -ne 1 ]; then
				echo "FAIL: $dir runs whole on $got shards" >&2
				failed=1
			fi
			if grep -q "^heavy $dir " "$tmp/all"; then
				echo "FAIL: $dir is both whole and split" >&2
				failed=1
			fi
		else
			got=$(grep "^heavy $dir " "$tmp/all" | awk '{print $3}' | sort | uniq | wc -l)
			local dup
			dup=$(grep "^heavy $dir " "$tmp/all" | awk '{print $3}' | sort | uniq -d | wc -l)
			if [ "$got" -ne "$want" ] || [ "$dup" -ne 0 ]; then
				echo "FAIL: $dir split covers $got of $want tests, $dup twice" >&2
				failed=1
			fi
		fi
	done < <(packages)
	[ "$failed" -eq 0 ] && echo "ok: $(packages | wc -l) packages, split $total ways, every test exactly once"
	return "$failed"
}

run() {
	local shard=$1 total=$2 status=0
	local heavy_dirs light_dirs regex dir
	local p
	p=$(plan "$shard" "$total")
	light_dirs=$(awk '$1 == "light" { print "./" $2 }' <<<"$p")
	heavy_dirs=$(awk '$1 == "heavy" { print $2 }' <<<"$p" | sort -u)

	# One `go test` per heavy package: -run applies to every package of an
	# invocation, and a name one heavy package gives this shard may belong to
	# another shard in a second package.
	#
	# A -run pattern that matches nothing still exits 0, printing "[no tests to
	# run]", which would leave the shard green with nothing raced. Each heavy
	# package's output is kept (and still streamed) so that line fails the shard.
	# Light packages run whole without -run, where the same line only means a
	# package whose test files hold helpers and no tests.
	local tmp
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' RETURN
	local pids=()
	for dir in $heavy_dirs; do
		regex=$(awk -v d="$dir" '$1 == "heavy" && $2 == d { print $3 }' <<<"$p" | paste -sd '|')
		echo "shard $shard/$total: ./$dir, $(awk -v d="$dir" '$1 == "heavy" && $2 == d' <<<"$p" | wc -l) tests"
		mkdir -p "$tmp/$dir"
		go test -race -count=1 -timeout "$TIMEOUT" -run "^($regex)\$" "./$dir" 2>&1 |
			tee "$tmp/$dir/out" &
		pids+=($!)
	done
	if [ -n "$light_dirs" ]; then
		echo "shard $shard/$total: $(wc -l <<<"$light_dirs") whole packages"
		# shellcheck disable=SC2086 # one argument per package
		go test -race -count=1 -timeout "$TIMEOUT" $light_dirs &
		pids+=($!)
	fi
	for pid in "${pids[@]}"; do
		wait "$pid" || status=1
	done
	for dir in $heavy_dirs; do
		# go's own summary line, so a test that prints the phrase cannot match.
		if grep -qE '^ok[[:space:]].*\[no tests to run\]$' "$tmp/$dir/out"; then
			echo "FAIL: ./$dir ran no tests on shard $shard/$total: its -run pattern matched nothing" >&2
			status=1
		fi
	done
	return "$status"
}

case "${1:-}" in
--plan) plan "$2" "$3" ;;
--check) check "$2" ;;
*) run "$1" "$2" ;;
esac
