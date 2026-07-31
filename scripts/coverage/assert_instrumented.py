#!/usr/bin/env python3
"""Fail when a changed source file is missing from an lcov report.

Guards the pre-push hook's scoped vitest run. Running only the tests vitest
believes are affected by a change is roughly 4x faster (281s -> 68s on this
repo), but vitest's affected-test detection is not reliable: changing
web/src/components/CapBadge.tsx did not run ModelTable.test.tsx, even though
ModelTable.test.tsx imports ModelTable, which imports CapBadge.

On its own that would only make the gate stricter. The danger is the
interaction with diff_coverage.py, which skips any changed file it has no
coverage data for (`path not in cov: continue`). A changed file whose tests
vitest missed is never loaded, so it never appears in the report, so it is
skipped, so the gate reports PASS on a completely untested change.

This script closes that hole: if any changed, non-excluded source file is
absent from the report, the caller must rerun the full suite rather than trust
the scoped one. Exclusions come from covlib so they cannot drift from what
diff_coverage.py itself ignores.
"""
import argparse
import sys

import covlib


def missing_files(lcov_text: str, root: str, changed: list) -> list:
    """Changed files that diff_coverage would silently skip."""
    cov = covlib.parse_lcov(lcov_text, root)
    # is_excluded mirrors diff_coverage's own filter: a file it would ignore
    # anyway is not evidence that the scoped run was incomplete.
    #
    # emits_no_runtime_code covers the other way a file can be legitimately
    # absent. A types-only module compiles to nothing, so it appears in no lcov
    # report ever produced and its absence says nothing about which tests ran.
    # Without this the guard failed permanently on web/src/api/types.ts, and
    # since that is the typed boundary to the backend, most API-surface branches
    # paid the full suite on every push for a file that could never pass.
    return [
        f
        for f in changed
        if not covlib.is_excluded(f)
        and not covlib.emits_no_runtime_code(f)
        and f not in cov
    ]


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--lcov", required=True, help="path to lcov.info")
    ap.add_argument("--root", required=True, help="repo-relative prefix, e.g. web/")
    ap.add_argument("files", nargs="*", help="changed files, repo-relative")
    args = ap.parse_args()

    if not args.files:
        return 0

    try:
        with open(args.lcov, encoding="utf-8") as fh:
            text = fh.read()
    except OSError as exc:
        # No report at all means nothing can be verified. Treat as incomplete
        # so the caller falls back rather than trusting an absent file.
        print(f"assert_instrumented: cannot read {args.lcov}: {exc}", file=sys.stderr)
        return 1

    missing = missing_files(text, args.root, args.files)
    if missing:
        print("assert_instrumented: no coverage recorded for:", file=sys.stderr)
        for path in missing:
            print(f"  {path}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
