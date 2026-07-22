import unittest
import diff_coverage


class TestCompute(unittest.TestCase):
    def test_counts_only_changed_coverable_lines(self):
        changed = {
            "internal/proxy/p.go": {10, 11, 14, 99},  # 99 not coverable
            "web/src/x.tsx": {1, 2},
            "web/src/x.test.tsx": {1},                 # excluded file
            "docs/readme.md": {1},                     # no coverage data
        }
        cov = {
            "internal/proxy/p.go": {10: True, 11: True, 14: False},
            "web/src/x.tsx": {1: True, 2: False},
        }
        covered, total, rows = diff_coverage.compute(changed, cov, 90.0)
        # coverable changed: p.go 10,11,14 (3) + x.tsx 1,2 (2) = 5 total, 3 covered
        self.assertEqual((covered, total), (3, 5))

    def test_no_coverable_changes_is_total_zero(self):
        changed = {"docs/x.md": {1, 2}, "web/src/x.test.tsx": {5}}
        covered, total, rows = diff_coverage.compute(changed, {}, 90.0)
        self.assertEqual((covered, total), (0, 0))


class TestMainExit(unittest.TestCase):
    def test_passes_when_no_coverable_changes(self):
        # empty diff base handled by monkeypatching git call
        rc = diff_coverage.evaluate({}, {}, 90.0)
        self.assertEqual(rc, 0)

    def test_fails_below_threshold(self):
        cov = {"a.go": {1: True, 2: False}}
        changed = {"a.go": {1, 2}}
        rc = diff_coverage.evaluate(changed, cov, 90.0)  # 50%
        self.assertEqual(rc, 1)

    def test_passes_at_threshold(self):
        cov = {"a.ts": {i: True for i in range(1, 10)}}
        cov["a.ts"][10] = False
        changed = {"a.ts": set(range(1, 11))}
        rc = diff_coverage.evaluate(changed, cov, 90.0)  # 90%
        self.assertEqual(rc, 0)


if __name__ == "__main__":
    unittest.main()
