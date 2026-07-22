"""Shared coverage parsing/exclusion helpers (stdlib only)."""
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


def _strip_module(path: str) -> str:
    return path[len(MODULE_PREFIX):] if path.startswith(MODULE_PREFIX) else path


def parse_go_profile(text: str) -> dict:
    """file(repo-relative) -> {line: covered_bool}. Excludes are kept here;
    callers filter with is_excluded when they need to."""
    out: dict = {}
    for line in text.splitlines():
        if not line or line.startswith("mode:"):
            continue
        left, numstmt, count = line.rsplit(" ", 2)
        path, rng = left.rsplit(":", 1)
        path = _strip_module(path)
        start = int(rng.split(".", 1)[0])
        end = int(rng.split(",", 1)[1].split(".", 1)[0])
        covered = int(count) > 0
        fmap = out.setdefault(path, {})
        for ln in range(start, end + 1):
            fmap[ln] = fmap.get(ln, False) or covered
    return out


def go_statement_counts(text: str) -> tuple:
    """(covered, total) statements over non-excluded Go files."""
    covered = total = 0
    for line in text.splitlines():
        if not line or line.startswith("mode:"):
            continue
        left, numstmt, count = line.rsplit(" ", 2)
        path = _strip_module(left.rsplit(":", 1)[0])
        if is_excluded(path):
            continue
        n = int(numstmt)
        total += n
        if int(count) > 0:
            covered += n
    return covered, total


def parse_lcov(text: str, root: str) -> dict:
    """file(root-prefixed, repo-relative) -> {line: covered_bool}. Excluded
    files are dropped."""
    out: dict = {}
    cur = None
    for line in text.splitlines():
        if line.startswith("SF:"):
            rel = root + line[3:].strip()
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
