#!/usr/bin/env python3
"""Aggregate coverage across Go + JS surfaces; emit shields endpoint JSON."""
import argparse
import json
import sys

import covlib


def summary_line_counts(path: str):
    d = json.load(open(path))["total"]["lines"]
    return int(d["covered"]), int(d["total"])


def aggregate(go_texts, summary_paths):
    covered = total = 0
    for text in go_texts:
        c, t = covlib.go_statement_counts(text)
        covered += c
        total += t
    for p in summary_paths:
        c, t = summary_line_counts(p)
        covered += c
        total += t
    pct = round(100.0 * covered / total, 1) if total else 0.0
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
    go_texts = [open(p).read() for p in args.go]
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
