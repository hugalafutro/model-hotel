#!/usr/bin/env python3
"""Fail if the PR's added/modified lines are <threshold% covered."""
import argparse
import subprocess
import sys

import covlib


def git_diff(base: str) -> str:
    return subprocess.run(
        ["git", "diff", "--unified=0", f"{base}...HEAD"],
        check=True, capture_output=True, text=True,
    ).stdout


def compute(changed: dict, cov: dict, threshold: float):
    covered = total = 0
    rows = []
    for path, lines in sorted(changed.items()):
        if covlib.is_excluded(path) or path not in cov:
            continue
        fcov = cov[path]
        c = t = 0
        for ln in lines:
            if ln in fcov:  # coverable line
                t += 1
                if fcov[ln]:
                    c += 1
        if t:
            covered += c
            total += t
            rows.append((path, c, t))
    return covered, total, rows


def evaluate(changed: dict, cov: dict, threshold: float) -> int:
    covered, total, rows = compute(changed, cov, threshold)
    if total == 0:
        print("Diff coverage: no coverable changed lines - PASS")
        return 0
    pct = 100.0 * covered / total
    for path, c, t in rows:
        print(f"  {100.0 * c / t:6.1f}%  {c:4d}/{t:<4d}  {path}")
    print(f"Diff coverage: {covered}/{total} = {pct:.1f}% (threshold {threshold:.0f}%)")
    if pct < threshold:
        print(f"FAIL: diff coverage {pct:.1f}% below {threshold:.0f}%")
        return 1
    return 0


def build_cov(go_paths, lcov_specs) -> dict:
    cov: dict = {}
    for p in go_paths:
        with open(p) as f:
            for path, lines in covlib.parse_go_profile(f.read()).items():
                cov.setdefault(path, {}).update(lines)
    for spec in lcov_specs:
        root, path = spec.split("=", 1)
        with open(path) as f:
            for fpath, lines in covlib.parse_lcov(f.read(), root).items():
                cov.setdefault(fpath, {}).update(lines)
    return cov


def main(argv=None) -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--diff-base", default="origin/master")
    ap.add_argument("--go", action="append", default=[])
    ap.add_argument("--lcov", action="append", default=[], help="ROOT=PATH")
    ap.add_argument("--threshold", type=float, default=90.0)
    args = ap.parse_args(argv)
    changed = covlib.parse_diff(git_diff(args.diff_base))
    cov = build_cov(args.go, args.lcov)
    return evaluate(changed, cov, args.threshold)


if __name__ == "__main__":
    sys.exit(main())
