#!/usr/bin/env bash
# go-race-shard.sh - run one shard of `go test -race` over the module.
#
# `go test -race ./...` runs packages in parallel, so the job takes about as
# long as its slowest package: internal/proxy alone was ~9.5 min under the race
# detector, with internal/api and internal/frontdesk behind it. This splits the
# work across CI runners instead:
#
#   - a HEAVY package (more than HEAVY_MIN top-level tests) is run on every
#     shard, its tests dealt longest-first to the least-loaded shard: each
#     test weighs 1 unless scripts/ci/go-race-weights.txt names it (a test
#     that alone outweighs a third of its package's others);
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
# directory; a name can only be declared once per package anyway).
tests_in() {
	grep -hoE '^func (Test|Example|Fuzz)[A-Za-z0-9_]*\(' "$1"/*_test.go |
		sed -E 's/^func //; s/\($//' | grep -vx 'TestMain' | sort -u || true
}

# deal DIR TOTAL prints "SHARD NAME" for every test of a heavy package: weights
# from WEIGHTS (default 1), heaviest first and then by name, each to the shard
# with the least weight so far (the lowest-numbered on a tie). Every shard
# computes the same deal, so the shards partition the package.
WEIGHTS="scripts/ci/go-race-weights.txt"
deal() {
	tests_in "$1" | awk -v d="$1" -v t="$2" -v wf="$WEIGHTS" '
		BEGIN {
			while ((getline line < wf) > 0) {
				if (line ~ /^#/ || line ~ /^[[:space:]]*$/) continue
				split(line, f, /[[:space:]]+/)
				if (f[1] == d) w[f[2]] = f[3]
			}
		}
		{ names[NR] = $0; wt[$0] = ($0 in w) ? w[$0] : 1 }
		END {
			n = NR
			# Heaviest first, then by name: an insertion sort, fine at a few
			# thousand tests.
			for (i = 2; i <= n; i++) {
				x = names[i]; j = i - 1
				while (j >= 1 && (wt[names[j]] < wt[x] || (wt[names[j]] == wt[x] && names[j] > x))) {
					names[j + 1] = names[j]; j--
				}
				names[j + 1] = x
			}
			for (s = 1; s <= t; s++) load[s] = 0
			for (i = 1; i <= n; i++) {
				best = 1
				for (s = 2; s <= t; s++) if (load[s] < load[best]) best = s
				load[best] += wt[names[i]]
				print best, names[i]
			}
		}'
}

# plan SHARD TOTAL prints "heavy DIR NAME" and "light DIR" lines for the shard.
plan() {
	local shard=$1 total=$2 i=0 dir n
	while read -r dir; do
		n=$(tests_in "$dir" | wc -l)
		if [ "$n" -gt "$HEAVY_MIN" ]; then
			deal "$dir" "$total" | awk -v s="$shard" -v d="$dir" '$1 == s { print "heavy", d, $2 }'
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
	local pids=()
	for dir in $heavy_dirs; do
		regex=$(awk -v d="$dir" '$1 == "heavy" && $2 == d { print $3 }' <<<"$p" | paste -sd '|')
		echo "shard $shard/$total: ./$dir, $(awk -v d="$dir" '$1 == "heavy" && $2 == d' <<<"$p" | wc -l) tests"
		go test -race -count=1 -timeout "$TIMEOUT" -run "^($regex)\$" "./$dir" &
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
	return "$status"
}

case "${1:-}" in
--plan) plan "$2" "$3" ;;
--check) check "$2" ;;
*) run "$1" "$2" ;;
esac
