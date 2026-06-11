#!/usr/bin/env python3
"""
i18n locale maintenance via DeepL (stdlib only, no dependencies).

Subcommands:
    check        CI gate: fail when any locale is missing keys, has extra
                 keys, breaks {{placeholder}} parity, carries a non-string
                 value, or carries an English-equal value that is not
                 allowlisted.
    fill         Fix what `check` flags, in place: translate missing and
                 non-allowlisted English keys, delete stale keys that left
                 en.json. Review the printed report before committing -
                 DeepL gets word order and temporal/causal "since" wrong
                 sometimes.
    bootstrap    Translate the full en.json into one new language
                 (the original behavior of this script).
    grandfather  Snapshot all current English-equal values into the
                 allowlist so `check` only flags future additions.
    usage        Show DeepL quota.

Intentionally-English values (brand names, loanwords like "Failover") live
in allow-english.json next to this script: {"dot.key": ["af", "da"]} or
{"dot.key": ["*"]}. Remove entries to force retranslation on next `fill`.

Requires DEEPL_API_KEY for fill/bootstrap/usage; `check` needs no network.
"""

import argparse
import copy
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

API_KEY = os.environ.get("DEEPL_API_KEY", "")
if API_KEY.endswith(":fx"):
    API_URL = "https://api-free.deepl.com/v2"
else:
    API_URL = "https://api.deepl.com/v2"

BATCH_SIZE = 40
RETRY_MAX = 3
RETRY_BASE_WAIT = 2

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
LOCALES_DIR = os.path.normpath(
    os.path.join(SCRIPT_DIR, "..", "..", "web", "src", "i18n", "locales")
)
ALLOWLIST_PATH = os.path.join(SCRIPT_DIR, "allow-english.json")

# Repo locale code -> DeepL target code. Regional variants are collapsed to
# one per language across the app; these are the only non-identity mappings.
DEEPL_LANG = {"no": "NB", "pt": "PT-BR", "zh": "ZH-HANS"}

# Domain context passed to DeepL; measurably improves term choice.
CONTEXT = (
    "UI strings in an admin dashboard for an AI/LLM gateway. "
    "'Provider' means an LLM API provider (like OpenAI). 'Model' means an "
    "AI model. 'Failover group' is a routing group of interchangeable "
    "models; keep the word 'failover' as commonly used in that language's "
    "technical UI. 'Listed' refers to a model appearing in the provider's "
    "published model listing. Keep placeholders exactly as they are."
)


# ── API ──────────────────────────────────────────────────────────────────────

def deepl_translate(texts: list[str], target_lang: str, source_lang: str = "EN") -> list[str]:
    """Translate a batch of texts via DeepL API (header-based auth)."""
    if not texts:
        return []
    if not API_KEY:
        print("DEEPL_API_KEY not set", file=sys.stderr)
        sys.exit(1)

    data = urllib.parse.urlencode({
        "text": texts,
        "source_lang": source_lang,
        "target_lang": target_lang,
        "context": CONTEXT,
    }, doseq=True).encode("utf-8")

    req = urllib.request.Request(
        f"{API_URL}/translate",
        data=data,
        headers={
            "Content-Type": "application/x-www-form-urlencoded",
            "Authorization": f"DeepL-Auth-Key {API_KEY}",
        },
        method="POST",
    )

    for attempt in range(RETRY_MAX):
        last_attempt = attempt == RETRY_MAX - 1
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                result = json.loads(resp.read().decode("utf-8"))
                return [t["text"] for t in result["translations"]]
        except urllib.error.HTTPError as e:
            if e.code not in (429, *range(500, 600)) or last_attempt:
                body = e.read().decode("utf-8", errors="replace")
                print(f"DeepL API error {e.code}: {body}", file=sys.stderr)
                sys.exit(1)
            retry_after = e.headers.get("Retry-After", "")
            wait = int(retry_after) if retry_after.isdigit() else RETRY_BASE_WAIT ** (attempt + 1)
            print(f"  HTTP {e.code}, retry {attempt+1}/{RETRY_MAX} in {wait}s...", file=sys.stderr)
            time.sleep(wait)
        except urllib.error.URLError as e:
            if last_attempt:
                raise
            wait = RETRY_BASE_WAIT ** (attempt + 1)
            print(f"  Network error: {e}, retry in {wait}s...", file=sys.stderr)
            time.sleep(wait)

    raise AssertionError("unreachable: the last attempt returns, exits, or raises")


def check_usage() -> dict:
    """Return DeepL usage stats."""
    if not API_KEY:
        return {"error": "DEEPL_API_KEY not set"}
    try:
        req = urllib.request.Request(
            f"{API_URL}/usage",
            headers={"Authorization": f"DeepL-Auth-Key {API_KEY}"},
        )
        with urllib.request.urlopen(req, timeout=10) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except Exception as e:
        return {"error": str(e)}


# ── Interpolation protection ────────────────────────────────────────────────

def protect(text: str) -> tuple[str, dict[str, str]]:
    """Replace {{vars}} and <markup> tags with stable placeholders."""
    placeholders = {}
    counter = [0]

    def repl(m):
        counter[0] += 1
        ph = f"XPHL{counter[0]}X"
        placeholders[ph] = m.group(0)
        return ph

    protected = re.sub(PROTECTED_RE, repl, text)
    return protected, placeholders


def restore(text: str, placeholders: dict[str, str]) -> str:
    """Restore {{vars}} from placeholders."""
    for ph, orig in placeholders.items():
        text = text.replace(ph, orig)
    return text


# {{interpolations}} plus <Trans> markup tags (e.g. <code>...</code>); both
# must survive translation verbatim, so both are protected from DeepL and
# compared against en.json by the check.
PROTECTED_RE = r"\{\{[^}]+\}\}|</?[a-zA-Z][a-zA-Z0-9]*>"


def interpolations(text: str) -> set[str]:
    """The set of {{placeholders}} and markup tags a string uses."""
    return set(re.findall(PROTECTED_RE, text))


def translate_batch(values: list[str], target_lang: str) -> list[str]:
    """Protect {{vars}} in a batch of source strings, translate, restore."""
    protected, all_ph = [], []
    for value in values:
        pv, ph = protect(value)
        protected.append(pv)
        all_ph.append(ph)
    translated = deepl_translate(protected, target_lang)
    return [restore(text, ph) for text, ph in zip(translated, all_ph)]


# ── Locale file helpers ─────────────────────────────────────────────────────

def locale_codes() -> list[str]:
    codes = sorted(
        f[:-5] for f in os.listdir(LOCALES_DIR)
        if f.endswith(".json") and f != "en.json"
    )
    if not codes:
        print(f"no locale files found in {LOCALES_DIR}", file=sys.stderr)
        sys.exit(1)
    return codes


def load_locale(code: str) -> dict:
    with open(os.path.join(LOCALES_DIR, f"{code}.json"), encoding="utf-8") as f:
        return json.load(f)


def save_locale(code: str, data: dict):
    with open(os.path.join(LOCALES_DIR, f"{code}.json"), "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent="\t")
        f.write("\n")


def flatten(obj, prefix="") -> dict[str, str]:
    out = {}
    for k, v in obj.items():
        path = f"{prefix}.{k}" if prefix else k
        if isinstance(v, dict):
            out.update(flatten(v, path))
        else:
            out[path] = v
    return out


def true_path(obj: dict, path: str) -> list[str] | None:
    """The actual key chain in `obj` whose flatten() path equals `path`.
    Locale keys may contain literal dots (e.g. "restoreRequirements.masterKey"
    nested under settings.backup), so splitting on "." is ambiguous."""
    if path in obj and not isinstance(obj[path], dict):
        return [path]
    for k, v in obj.items():
        if isinstance(v, dict) and path.startswith(k + "."):
            rest = true_path(v, path[len(k) + 1:])
            if rest is not None:
                return [k, *rest]
    return None


def set_path(data: dict, en: dict, path: str, value: str):
    """Set `path` in `data`, updating an existing entry in place or else
    mirroring en.json's actual nesting for new keys."""
    chain = true_path(data, path) or true_path(en, path) or path.split(".")
    cur = data
    for p in chain[:-1]:
        cur = cur.setdefault(p, {})
    cur[chain[-1]] = value


def delete_path(data: dict, path: str) -> bool:
    """Remove `path` from `data`, pruning parents it leaves empty."""
    chain = true_path(data, path)
    if chain is None:
        return False
    parents = [data]
    for p in chain[:-1]:
        parents.append(parents[-1][p])
    del parents[-1][chain[-1]]
    for i in range(len(parents) - 1, 0, -1):
        if parents[i]:
            break
        del parents[i - 1][chain[i - 1]]
    return True


def load_allowlist() -> dict[str, list[str]]:
    if not os.path.exists(ALLOWLIST_PATH):
        return {}
    with open(ALLOWLIST_PATH, encoding="utf-8") as f:
        return json.load(f)


def save_allowlist(allow: dict[str, list[str]]):
    with open(ALLOWLIST_PATH, "w", encoding="utf-8") as f:
        json.dump(dict(sorted(allow.items())), f, ensure_ascii=False, indent="\t")
        f.write("\n")


def allowed(allow: dict[str, list[str]], key: str, code: str) -> bool:
    langs = allow.get(key)
    return langs is not None and ("*" in langs or code in langs)


# ── should_skip: values translation never touches ───────────────────────────

SKIP_VALUES = {
    "hotel/model", "TBD", "VS", "ON", "OFF", "auto", "N/A", "OK",
    "⬡ Pages", "⇊ Scroll", "✏️Custom",
}


def should_skip(value: str) -> bool:
    if not value or not value.strip():
        return True
    s = value.strip()
    if s in SKIP_VALUES:
        return True
    if len(s) <= 3 and s.isupper():
        return True
    if re.match(r"^[0-9\s\-:./]+$", s):
        return True
    return False


# ── check ───────────────────────────────────────────────────────────────────

def find_problems(allow: dict[str, list[str]]) -> dict[str, list[tuple[str, str]]]:
    """Map of problem type -> [(locale, key)]."""
    en = flatten(load_locale("en"))
    problems = {"missing": [], "extra": [], "malformed": [], "placeholders": [], "untranslated": []}
    for code in locale_codes():
        loc = flatten(load_locale(code))
        for key in en.keys() - loc.keys():
            problems["missing"].append((code, key))
        for key in loc.keys() - en.keys():
            problems["extra"].append((code, key))
        for key, value in loc.items():
            if key not in en:
                continue
            if not isinstance(value, str) or not isinstance(en[key], str):
                problems["malformed"].append((code, key))
            elif interpolations(value) != interpolations(en[key]):
                problems["placeholders"].append((code, key))
            elif value == en[key] and not should_skip(value) and not allowed(allow, key, code):
                problems["untranslated"].append((code, key))
    return problems


def cmd_check() -> int:
    problems = find_problems(load_allowlist())
    total = sum(len(v) for v in problems.values())
    if total == 0:
        print(f"i18n check OK: {len(locale_codes())} locales in sync with en.json")
        return 0
    for kind, entries in problems.items():
        if not entries:
            continue
        print(f"\n{kind} ({len(entries)}):")
        for code, key in sorted(entries):
            print(f"  {code}: {key}")
    print(
        f"\ni18n check FAILED ({total} problems)."
        "\nFix: run `make i18n-fill` (needs DEEPL_API_KEY), review the diff for"
        "\nDeepL mistakes (word order, temporal vs causal 'since'), and commit."
        "\nIntentionally-English values go into tools/i18n-translate/allow-english.json."
    )
    if problems["malformed"]:
        print(
            "Note: `malformed` entries (value is not a string) are NOT fixed by"
            "\ni18n-fill - edit those locale files by hand."
        )
    return 1


# ── fill ────────────────────────────────────────────────────────────────────

def cmd_fill(only_langs: list[str] | None) -> int:
    allow = load_allowlist()
    en = load_locale("en")
    en_flat = flatten(en)
    problems = find_problems(allow)
    work: dict[str, list[str]] = {}
    for kind in ("missing", "untranslated", "placeholders"):
        for code, key in problems[kind]:
            if only_langs and code not in only_langs:
                continue
            work.setdefault(code, []).append(key)
    removals: dict[str, list[str]] = {}
    for code, key in problems["extra"]:
        if only_langs and code not in only_langs:
            continue
        removals.setdefault(code, []).append(key)

    if not work and not removals:
        print("nothing to fill - all locales in sync")
        return 0

    for code in sorted(set(work) | set(removals)):
        data = load_locale(code)
        for key in sorted(set(removals.get(code, []))):
            if delete_path(data, key):
                print(f"{code}: removed stale key {key}")
        keys = sorted(set(work.get(code, [])))
        if keys:
            target = DEEPL_LANG.get(code, code.upper())
            print(f"{code} ({target}): translating {len(keys)} strings")
            for i in range(0, len(keys), BATCH_SIZE):
                chunk = keys[i:i + BATCH_SIZE]
                for key, value in zip(chunk, translate_batch([en_flat[k] for k in chunk], target)):
                    set_path(data, en, key, value)
                    print(f"  {key} = {value}")
                    if interpolations(value) != interpolations(en_flat[key]):
                        print(f"  WARNING: DeepL broke placeholder parity in {key}; fix by hand", file=sys.stderr)
                if i + BATCH_SIZE < len(keys):
                    time.sleep(0.3)
        save_locale(code, data)

    print(
        "\nDone. REVIEW THE DIFF before committing - check word order around"
        "\nplaceholders and that temporal 'since' did not become causal."
    )
    return 0


# ── grandfather ─────────────────────────────────────────────────────────────

def cmd_grandfather() -> int:
    allow = load_allowlist()
    problems = find_problems(allow)
    added = 0
    for code, key in problems["untranslated"]:
        langs = allow.setdefault(key, [])
        if code not in langs and "*" not in langs:
            langs.append(code)
            added += 1
    for langs in allow.values():
        langs.sort()
    save_allowlist(allow)
    print(f"allowlisted {added} locale/key pairs into {ALLOWLIST_PATH}")
    return 0


# ── bootstrap (full-file translation for a new language) ────────────────────

def cmd_bootstrap(target_lang: str, output: str | None) -> int:
    source = load_locale("en")
    target = copy.deepcopy(source)

    items = []

    def collect(obj):
        for k in obj:
            if isinstance(obj[k], str):
                items.append((obj, k, obj[k]))
            elif isinstance(obj[k], dict):
                collect(obj[k])

    collect(target)
    to_translate = [(p, k, v) for p, k, v in items if not should_skip(v)]
    print(f"Total strings: {len(items)}, translating: {len(to_translate)}, skipping: {len(items) - len(to_translate)}")

    deepl_code = DEEPL_LANG.get(target_lang.lower(), target_lang.upper())
    total = len(to_translate)
    for i in range(0, total, BATCH_SIZE):
        chunk = to_translate[i:i + BATCH_SIZE]
        for (parent, key, _), value in zip(chunk, translate_batch([v for _, _, v in chunk], deepl_code)):
            parent[key] = value
        print(f"  Batch {i // BATCH_SIZE + 1}/{(total + BATCH_SIZE - 1) // BATCH_SIZE} done")
        if i + BATCH_SIZE < total:
            time.sleep(0.3)

    out = output or os.path.join(LOCALES_DIR, f"{target_lang.lower()}.json")
    with open(out, "w", encoding="utf-8") as f:
        json.dump(target, f, ensure_ascii=False, indent="\t")
        f.write("\n")
    print(f"Written: {out}")
    return 0


# ── CLI ──────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="cmd", required=True)

    sub.add_parser("check", help="verify all locales are in sync with en.json (CI gate)")

    p_fill = sub.add_parser("fill", help="DeepL-translate missing/untranslated keys in place")
    p_fill.add_argument("--langs", help="comma-separated locale codes (default: all)")

    p_boot = sub.add_parser("bootstrap", help="translate full en.json into a new language")
    p_boot.add_argument("lang", help="locale code, e.g. cs")
    p_boot.add_argument("--output", default=None, help="output file (default: locales/<lang>.json)")

    sub.add_parser("grandfather", help="allowlist all current English-equal values")
    sub.add_parser("usage", help="show DeepL quota")

    args = parser.parse_args()

    if args.cmd == "check":
        sys.exit(cmd_check())
    if args.cmd == "fill":
        langs = args.langs.split(",") if args.langs else None
        sys.exit(cmd_fill(langs))
    if args.cmd == "bootstrap":
        sys.exit(cmd_bootstrap(args.lang, args.output))
    if args.cmd == "grandfather":
        sys.exit(cmd_grandfather())
    if args.cmd == "usage":
        usage = check_usage()
        if "error" in usage:
            print(f"Error: {usage['error']}")
        else:
            count = usage.get("character_count", 0)
            limit = usage.get("character_limit", 0)
            pct = (count / limit * 100) if limit else 0
            print(f"DeepL quota: {count:,} / {limit:,} chars ({pct:.1f}%)")
        sys.exit(0)


if __name__ == "__main__":
    main()
