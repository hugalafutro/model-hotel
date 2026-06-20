#!/usr/bin/env python3
"""
i18n locale maintenance (stdlib only, no dependencies, no network).

New user-facing strings are added to en.json and translated into every other
locale by hand (the assistant/contributor does this directly). The quickest
correct way to bulk-apply a batch of translations is a one-off script that
reuses the load_locale/set_path/save_locale helpers below, which preserve each
file's nesting and tab/ensure_ascii formatting.

Subcommands:
    check        CI gate: fail when any locale is missing keys, has extra
                 keys, breaks {{placeholder}} parity, carries a non-string
                 value, or carries an English-equal value that is not
                 allowlisted.
    grandfather  Snapshot all current English-equal values into the
                 allowlist so `check` only flags future additions.

Intentionally-English values (brand names, loanwords like "Failover") live
in allow-english.json next to this script: {"dot.key": ["af", "da"]} or
{"dot.key": ["*"]}. Remove entries to force retranslation later.
"""

import argparse
import json
import os
import re
import sys

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
LOCALES_DIR = os.path.normpath(
    os.path.join(SCRIPT_DIR, "..", "..", "web", "src", "i18n", "locales")
)
ALLOWLIST_PATH = os.path.join(SCRIPT_DIR, "allow-english.json")


# ── Interpolation parity ─────────────────────────────────────────────────────

# {{interpolations}} plus <Trans> markup tags (e.g. <code>...</code>); both
# must survive translation verbatim, so the check compares each locale's set
# against en.json.
PROTECTED_RE = r"\{\{[^}]+\}\}|</?[a-zA-Z][a-zA-Z0-9]*>"


def interpolations(text: str) -> set[str]:
    """The set of {{placeholders}} and markup tags a string uses."""
    return set(re.findall(PROTECTED_RE, text))


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
        "\nFix: translate the listed keys into the listed locales by hand and commit"
        "\n(reuse this script's load_locale/set_path/save_locale from a one-off script)."
        "\nIntentionally-English values go into tools/i18n-translate/allow-english.json."
    )
    if problems["malformed"]:
        print(
            "Note: `malformed` entries (value is not a string) must be edited by hand."
        )
    return 1


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


# ── CLI ──────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="cmd", required=True)

    sub.add_parser("check", help="verify all locales are in sync with en.json (CI gate)")
    sub.add_parser("grandfather", help="allowlist all current English-equal values")

    args = parser.parse_args()

    if args.cmd == "check":
        sys.exit(cmd_check())
    if args.cmd == "grandfather":
        sys.exit(cmd_grandfather())


if __name__ == "__main__":
    main()
