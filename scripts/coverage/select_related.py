#!/usr/bin/env python3
"""Print the changed files worth seeding `vitest related` test selection with.

The pre-push hook's scoped vitest run picks its tests from the changed files.
Seeding that selection with a file that compiles to no runtime JavaScript is
worse than useless: such a file can never be measured by coverage, and the one
this repo actually has - web/src/api/types.ts, the typed boundary to the
backend - is imported by nearly every module, so leaving it in the seed list
degenerated the scoped run into 183 of 190 test files (6m29s, the entire
pre-push wait).

Dropping those files here cannot weaken the gate: assert_instrumented.py
exempts exactly the same files from its "every changed file was measured"
check (same covlib.emits_no_runtime_code call), so a file dropped from
selection is never one whose absence from the report the guard would flag.
Everything else - including changed test files, which select themselves - is
kept, and any under-selection still trips the guard's full-suite rerun.
"""
import argparse
import sys

import covlib


def selectable(paths: list) -> list:
    """The subset of paths that could ever appear in a coverage report."""
    return [p for p in paths if not covlib.emits_no_runtime_code(p)]


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("files", nargs="*", help="changed files, repo-relative")
    args = ap.parse_args()

    for path in selectable(args.files):
        print(path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
