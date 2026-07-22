import json
import os
import tempfile
import unittest

import aggregate_coverage as agg


class TestAggregate(unittest.TestCase):
    def test_summary_line_counts(self):
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "s.json")
            json.dump({"total": {"lines": {"covered": 90, "total": 100}}}, open(p, "w"))
            self.assertEqual(agg.summary_line_counts(p), (90, 100))

    def test_aggregate_sums_go_and_summaries(self):
        go = ("mode: atomic\n"
              "github.com/hugalafutro/model-hotel/internal/x.go:1.1,2.1 8 1\n"
              "github.com/hugalafutro/model-hotel/internal/x.go:3.1,3.5 2 0\n")
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "s.json")
            json.dump({"total": {"lines": {"covered": 2, "total": 10}}}, open(p, "w"))
            covered, total, pct = agg.aggregate([go], [p])
            # go: 8/10 ; summary: 2/10 -> 10/20 = 50.0
            self.assertEqual((covered, total), (10, 20))
            self.assertEqual(pct, 50.0)

    def test_badge_obj(self):
        b = agg.badge_obj("coverage", 93.9)
        self.assertEqual(b, {"schemaVersion": 1, "label": "coverage",
                             "message": "93.9%", "color": "brightgreen"})


class TestMain(unittest.TestCase):
    def test_writes_badge_and_passes(self):
        go = "mode: atomic\ngithub.com/hugalafutro/model-hotel/internal/x.go:1.1,2.1 10 1\n"
        with tempfile.TemporaryDirectory() as d:
            gp = os.path.join(d, "c.out"); open(gp, "w").write(go)
            out = os.path.join(d, "coverage.json")
            rc = agg.main(["--go", gp, "--threshold", "90", "--out", out, "--label", "coverage"])
            self.assertEqual(rc, 0)
            b = json.load(open(out))
            self.assertEqual(b["message"], "100.0%")

    def test_fails_below_threshold(self):
        go = ("mode: atomic\n"
              "github.com/hugalafutro/model-hotel/internal/x.go:1.1,2.1 5 1\n"
              "github.com/hugalafutro/model-hotel/internal/x.go:3.1,3.5 5 0\n")
        with tempfile.TemporaryDirectory() as d:
            gp = os.path.join(d, "c.out"); open(gp, "w").write(go)
            out = os.path.join(d, "coverage.json")
            rc = agg.main(["--go", gp, "--threshold", "90", "--out", out, "--label", "coverage"])
            self.assertEqual(rc, 1)  # 50%


if __name__ == "__main__":
    unittest.main()
