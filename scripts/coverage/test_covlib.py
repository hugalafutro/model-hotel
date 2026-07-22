import unittest
import covlib


class TestExclusions(unittest.TestCase):
    def test_excludes_cmd_tools_tests_and_dts(self):
        for p in [
            "cmd/server/main.go", "tools/i18n/x.go", "internal/x_test.go",
            "web/src/main.tsx", "web/src/test/setup.ts", "web/src/x.d.ts",
            "frontdesk/web/src/main.tsx", "frontdesk/web/src/foo.test.tsx",
            "web/src/components/__tests__/A.test.tsx", "frontdesk/web/src/test/sse.ts",
        ]:
            self.assertTrue(covlib.is_excluded(p), p)

    def test_keeps_real_source(self):
        for p in [
            "internal/proxy/proxy.go", "web/src/components/Foo.tsx",
            "frontdesk/web/src/utils/oidc.ts",
        ]:
            self.assertFalse(covlib.is_excluded(p), p)


class TestGoProfile(unittest.TestCase):
    PROFILE = (
        "mode: atomic\n"
        "github.com/hugalafutro/model-hotel/internal/proxy/p.go:10.2,12.3 2 1\n"
        "github.com/hugalafutro/model-hotel/internal/proxy/p.go:14.2,14.9 1 0\n"
        "github.com/hugalafutro/model-hotel/cmd/server/main.go:5.2,6.3 1 0\n"
    )

    def test_parse_maps_lines_to_covered(self):
        m = covlib.parse_go_profile(self.PROFILE)
        self.assertEqual(m["internal/proxy/p.go"][10], True)
        self.assertEqual(m["internal/proxy/p.go"][11], True)
        self.assertEqual(m["internal/proxy/p.go"][12], True)
        self.assertEqual(m["internal/proxy/p.go"][14], False)

    def test_statement_counts_exclude_cmd(self):
        covered, total = covlib.go_statement_counts(self.PROFILE)
        self.assertEqual((covered, total), (2, 3))


class TestLcov(unittest.TestCase):
    LCOV = "TN:\nSF:src/a.ts\nDA:1,3\nDA:2,0\nend_of_record\nSF:src/a.test.ts\nDA:1,0\nend_of_record\n"

    def test_parse_prefixes_root_and_marks_hits(self):
        m = covlib.parse_lcov(self.LCOV, "frontdesk/web/")
        self.assertEqual(m["frontdesk/web/src/a.ts"], {1: True, 2: False})
        self.assertNotIn("frontdesk/web/src/a.test.ts", m)  # excluded


class TestDiff(unittest.TestCase):
    DIFF = (
        "diff --git a/internal/proxy/p.go b/internal/proxy/p.go\n"
        "--- a/internal/proxy/p.go\n"
        "+++ b/internal/proxy/p.go\n"
        "@@ -9,0 +10,2 @@\n"
        "+line ten\n"
        "+line eleven\n"
        "@@ -20,1 +22,1 @@\n"
        "-old\n"
        "+new line 22\n"
    )

    def test_parse_diff_collects_added_new_lines(self):
        d = covlib.parse_diff(self.DIFF)
        self.assertEqual(d["internal/proxy/p.go"], {10, 11, 22})


class TestColor(unittest.TestCase):
    def test_thresholds(self):
        self.assertEqual(covlib.color_for(93.9), "brightgreen")
        self.assertEqual(covlib.color_for(85.0), "green")
        self.assertEqual(covlib.color_for(72.0), "yellowgreen")
        self.assertEqual(covlib.color_for(65.0), "yellow")
        self.assertEqual(covlib.color_for(55.0), "orange")
        self.assertEqual(covlib.color_for(40.0), "red")


if __name__ == "__main__":
    unittest.main()
