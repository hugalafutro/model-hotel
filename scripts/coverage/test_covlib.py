import os
import shutil
import tempfile
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

    def test_line_counts_exclude_cmd(self):
        # p.go: lines 10-12 covered, line 14 uncovered -> 3/4. cmd/ block excluded.
        covered, total = covlib.go_line_counts(self.PROFILE)
        self.assertEqual((covered, total), (3, 4))


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


class TestEmitsNoRuntimeCode(unittest.TestCase):
    """The guard that stops a types-only module forcing a full-suite rerun.

    Wrong in the "emits" direction only costs the old behaviour (rerun the full
    suite). Wrong in the "erased" direction would let an untested change through
    the gate, so every ambiguous shape below is asserted to answer False.
    """

    def _write(self, name, src):
        path = os.path.join(self.tmp, name)
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(src)
        return path

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp)

    def test_types_only_module_is_erased(self):
        p = self._write("types.ts", """
import type { Foo } from "./foo";

/** A model claim. */
export interface Claim {
	model_id: string;
	state: "gone" | "retired";
}

export type ClaimState = Claim["state"];
""")
        self.assertTrue(covlib.emits_no_runtime_code(p))

    def test_interface_method_signature_does_not_count_as_a_call(self):
        # Indented `refresh(): void;` inside an interface must not read as a
        # top-level call, or every interface-bearing file would look emitting.
        p = self._write("iface.ts", """
export interface Api {
	refresh(): void;
	load<T>(id: string): Promise<T>;
}
""")
        self.assertTrue(covlib.emits_no_runtime_code(p))

    def test_value_declarations_emit(self):
        for src in (
            "export const x = 1;\n",
            "function f() {}\n",
            "export default class A {}\n",
            "export enum E { A }\n",
            "export * from './x';\n",
            "export { thing } from './x';\n",
            "import './side-effect.css';\n",
        ):
            with self.subTest(src=src):
                p = self._write("v.ts", src)
                self.assertFalse(covlib.emits_no_runtime_code(p))

    def test_bare_top_level_call_emits(self):
        p = self._write("setup.ts", "configure({ adapter: 1 });\n")
        self.assertFalse(covlib.emits_no_runtime_code(p))

    def test_type_only_reexport_is_erased(self):
        p = self._write("reexport.ts", "export type { Foo } from './foo';\n")
        self.assertTrue(covlib.emits_no_runtime_code(p))

    def test_non_typescript_and_missing_paths_answer_false(self):
        self.assertFalse(covlib.emits_no_runtime_code("nope.go"))
        self.assertFalse(
            covlib.emits_no_runtime_code(os.path.join(self.tmp, "absent.ts"))
        )


if __name__ == "__main__":
    unittest.main()
