import os
import shutil
import tempfile
import unittest

import select_related


class TestSelectable(unittest.TestCase):
    """Pins the seed-list filter for the pre-push `vitest related` run.

    The only thing dropped is a file that compiles to no runtime code - the
    same exemption assert_instrumented.py grants, so a dropped file is never
    one whose absence from the report would trip the full-suite fallback.
    Everything ambiguous stays selected: an over-full seed list only costs
    test time, an over-trimmed one skips tests silently.
    """

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp)

    def _write(self, name, src):
        path = os.path.join(self.tmp, name)
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(src)
        return path

    def test_drops_types_only_hub_keeps_runtime_source(self):
        types_only = self._write(
            "types.ts", "export interface Claim { model_id: string }\n"
        )
        runtime = self._write("client.ts", "export const api = fetch;\n")
        self.assertEqual(
            select_related.selectable([types_only, runtime]), [runtime]
        )

    def test_keeps_changed_test_files(self):
        # A changed test selects itself under `vitest related`; its top-level
        # describe() call reads as runtime code, so it must survive the filter.
        test = self._write("foo.test.tsx", "describe('foo', () => {});\n")
        self.assertEqual(select_related.selectable([test]), [test])

    def test_keeps_unreadable_paths(self):
        # A path that cannot be read answers "emits" (covlib returns False),
        # staying in the seed list - the direction that runs more tests.
        gone = os.path.join(self.tmp, "absent.ts")
        self.assertEqual(select_related.selectable([gone]), [gone])


if __name__ == "__main__":
    unittest.main()
