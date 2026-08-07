"""Shared coverage parsing/exclusion helpers (stdlib only)."""
import posixpath
import re

MODULE_PREFIX = "github.com/hugalafutro/model-hotel/"

_EXCLUDE_SUFFIXES = (".d.ts",)
_EXCLUDE_SUBSTRINGS = ("/__tests__/",)
_EXCLUDE_PREFIXES = (
    "cmd/", "tools/",
    "web/src/test/", "frontdesk/web/src/test/",
)
_EXCLUDE_EXACT = ("web/src/main.tsx", "frontdesk/web/src/main.tsx")
_TEST_FILE_RE = re.compile(r"(_test\.go|\.(test|spec)\.(ts|tsx))$")


def is_excluded(path: str) -> bool:
    path = path.lstrip("./")
    if path in _EXCLUDE_EXACT:
        return True
    if path.endswith(_EXCLUDE_SUFFIXES):
        return True
    if any(s in path for s in _EXCLUDE_SUBSTRINGS):
        return True
    if any(path.startswith(p) for p in _EXCLUDE_PREFIXES):
        return True
    if _TEST_FILE_RE.search(path):
        return True
    return False


# Anything that makes TypeScript emit runtime JavaScript. A file with none of
# these is erased completely at compile time, so it can never appear in a
# coverage report no matter which tests run.
#
# `declare` is counted as emitting even though it does not, and so is every
# `export {` that is not spelled `export type {`. Both are deliberate: guessing
# WRONG in this direction only means the caller keeps today's behaviour and
# reruns the full suite, whereas guessing wrong the other way would let an
# untested change through the gate.
#
# The modifiers are listed rather than matched loosely because a missing one is
# silent: `abstract class` has no other tell, so without it here a file whose
# only runtime construct were an abstract class would read as erased and skip
# the guard entirely. `using` and `export =` are here for the same reason. None
# of the three appears in either frontend today; they are cheap to cover and
# expensive to notice missing.
_EMITS_RE = re.compile(
    r"""^\s*(?:
          (?:export\s+)?(?:declare\s+)?(?:default\s+)?(?:abstract\s+)?(?:async\s+)?
              (?:const|let|var|using|function|class|enum|namespace|module)\b
        | export\s+default\b
        | export\s*\*
        | export\s*=
        | export\s*\{(?!\s*type\b)
        | import\s+['"]
    )""",
    re.M | re.X,
)

# A bare top-level call statement — `configure(...)`, `describe(...)` — emits
# code without using any declaration keyword, so _EMITS_RE alone would read such
# a file as erased. Anchored at column 0 on purpose: a method signature inside an
# interface (`  refresh(): void;`) is indented, and matching that would classify
# every interface-bearing file as emitting and undo the whole optimisation.
_TOP_LEVEL_CALL_RE = re.compile(r"^[A-Za-z_$][\w$.]*\s*[(<]", re.M)


def emits_no_runtime_code(path: str) -> bool:
    """True when a TypeScript file compiles to nothing at all.

    web/src/api/types.ts is the case this exists for: 109 interface and type
    declarations, zero runtime ones. TypeScript erases it, so it is absent from
    every lcov report ever produced, so assert_instrumented read it as "the
    scoped run missed this file" and fell back to the full suite. That fired on
    every push touching the typed boundary to the backend, which is most
    API-surface work, and cost roughly 4x the run time for a file that could
    never have satisfied the check.

    It is kept out of is_excluded because that function is path-only and is
    called for paths that need not exist on disk; this one reads the file.
    Unreadable or non-TypeScript paths answer False, which changes nothing.
    """
    if not path.endswith((".ts", ".tsx")):
        return False
    try:
        with open(path, encoding="utf-8") as fh:
            src = fh.read()
    except OSError:
        return False
    return not _EMITS_RE.search(src) and not _TOP_LEVEL_CALL_RE.search(src)


def _strip_module(path: str) -> str:
    return path[len(MODULE_PREFIX):] if path.startswith(MODULE_PREFIX) else path


def parse_go_profile(text: str) -> dict:
    """file(repo-relative) -> {line: covered_bool}. Excludes are kept here;
    callers filter with is_excluded when they need to."""
    out: dict = {}
    for line in text.splitlines():
        if not line or line.startswith("mode:"):
            continue
        left, _, count = line.rsplit(" ", 2)
        path, rng = left.rsplit(":", 1)
        path = _strip_module(path)
        start = int(rng.split(".", 1)[0])
        end = int(rng.split(",", 1)[1].split(".", 1)[0])
        covered = int(count) > 0
        fmap = out.setdefault(path, {})
        for ln in range(start, end + 1):
            fmap[ln] = fmap.get(ln, False) or covered
    return out


def go_line_counts(text: str) -> tuple:
    """(covered, total) source lines over non-excluded Go files. A line counts
    as covered when any statement block spanning it has a nonzero hit count.
    This is line coverage (not Go's native statement coverage) so the aggregate
    badge matches how the former Codecov config measured Go (codecov.yml used
    line coverage with the same ignore list)."""
    per_file = parse_go_profile(text)
    covered = total = 0
    for path, lines in per_file.items():
        if is_excluded(path):
            continue
        for is_covered in lines.values():
            total += 1
            if is_covered:
                covered += 1
    return covered, total


def parse_lcov(text: str, root: str) -> dict:
    """file(root-prefixed, repo-relative) -> {line: covered_bool}. Excluded
    files are dropped."""
    out: dict = {}
    cur = None
    for line in text.splitlines():
        if line.startswith("SF:"):
            # An app's report can name a file outside its own tree: web/'s suite
            # covers web-shared/, which vitest records as `../web-shared/...`.
            # Normalising folds that back onto the repo-relative path git reports,
            # so those lines are matched rather than silently skipped.
            rel = posixpath.normpath(root + line[3:].strip())
            cur = None if is_excluded(rel) else rel
        elif line.startswith("DA:") and cur is not None:
            ln, hits = line[3:].split(",", 1)
            fmap = out.setdefault(cur, {})
            fmap[int(ln)] = fmap.get(int(ln), False) or int(hits) > 0
        elif line.startswith("end_of_record"):
            cur = None
    return out


_HUNK_RE = re.compile(r"^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@")


def parse_diff(text: str) -> dict:
    """file -> set of added/modified new-file line numbers."""
    out: dict = {}
    cur = None
    new_ln = 0
    for line in text.splitlines():
        if line.startswith("+++ "):
            p = line[4:].strip()
            cur = None if p == "/dev/null" else p[2:] if p.startswith("b/") else p
            continue
        m = _HUNK_RE.match(line)
        if m:
            new_ln = int(m.group(1))
            continue
        if cur is None:
            continue
        if line.startswith("+") and not line.startswith("+++"):
            out.setdefault(cur, set()).add(new_ln)
            new_ln += 1
        elif line.startswith("-") and not line.startswith("---"):
            pass
        elif not line.startswith("\\"):
            new_ln += 1
    return out


def color_for(pct: float) -> str:
    if pct >= 90:
        return "brightgreen"
    if pct >= 80:
        return "green"
    if pct >= 70:
        return "yellowgreen"
    if pct >= 60:
        return "yellow"
    if pct >= 50:
        return "orange"
    return "red"
