#!/usr/bin/env python3
"""Aggregate coverage across Go + JS surfaces; emit shields endpoint JSON.

Coverage is line-based on every surface and the percentage is rounded DOWN to
one decimal, mirroring the former Codecov config (codecov.yml: precision 1,
round down, line coverage, the same cmd/ + tools/ + test-file ignore list that
covlib enforces). The badge therefore reports the honest floor, never a
rounded-up or statement-inflated number."""
import argparse
import json
import math
import sys

import covlib


def summary_line_counts(path: str):
    with open(path) as f:
        d = json.load(f)["total"]["lines"]
    return int(d["covered"]), int(d["total"])


def floor1(pct: float) -> float:
    """Round a percentage DOWN to one decimal place (codecov round: down)."""
    return math.floor(pct * 10) / 10.0


def aggregate(go_texts, summary_paths):
    covered = total = 0
    for text in go_texts:
        c, t = covlib.go_line_counts(text)
        covered += c
        total += t
    for p in summary_paths:
        c, t = summary_line_counts(p)
        covered += c
        total += t
    pct = floor1(100.0 * covered / total) if total else 0.0
    return covered, total, pct


def badge_obj(label: str, pct: float) -> dict:
    return {
        "schemaVersion": 1,
        "label": label,
        "message": f"{pct:.1f}%",
        "color": covlib.color_for(pct),
    }


def main(argv=None) -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--go", action="append", default=[])
    ap.add_argument("--summary", action="append", default=[])
    ap.add_argument("--threshold", type=float, default=90.0)
    ap.add_argument("--out", required=True)
    ap.add_argument("--label", default="coverage")
    args = ap.parse_args(argv)
    go_texts = []
    for p in args.go:
        with open(p) as f:
            go_texts.append(f.read())
    covered, total, pct = aggregate(go_texts, args.summary)
    with open(args.out, "w") as f:
        json.dump(badge_obj(args.label, pct), f)
        f.write("\n")
    print(f"Overall coverage: {covered}/{total} = {pct:.1f}% (threshold {args.threshold:.0f}%)")
    if pct < args.threshold:
        print(f"FAIL: overall coverage {pct:.1f}% below {args.threshold:.0f}%")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
