#!/usr/bin/env python3
"""
i18n locale maintenance (stdlib only, no dependencies, no network).

New user-facing strings are added to en.json and translated into every other
locale by hand (the assistant/contributor does this directly). The quickest
correct way to bulk-apply a batch of translations is a one-off script that
reuses the load_locale/set_path/save_locale helpers below, which preserve each
file's nesting and tab/ensure_ascii formatting.

Subcommands:
    check        CI gate: fail when any locale (in web/ OR frontdesk/web/) is
                 missing keys, has extra keys, breaks {{placeholder}} parity,
                 carries a non-string value, or carries an English-equal value
                 that is not allowlisted.
    grandfather  Snapshot all current English-equal values into the
                 allowlist(s) so `check` only flags future additions.

Intentionally-English values (brand names, loanwords like "Failover") live
in allow-english.json (main dashboard), allow-english-fd.json (Front Desk) and
allow-english-android.json (Bellhop) next to this script: {"dot.key": ["af", "da"]} or {"dot.key": ["*"]}. Remove
entries to force retranslation later.
"""

import argparse
import json
import os
import re
import sys
from xml.etree import ElementTree

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))

# Each app has its own locale directory and its own allowlist. Keys are
# dot-paths and the two apps share key names (e.g. "common.cancel") with
# different intended treatment, so the allowlists must not collide.
LOCALE_TARGETS = [
	("web", os.path.normpath(
		os.path.join(SCRIPT_DIR, "..", "..", "web", "src", "i18n", "locales")
	)),
	("fd", os.path.normpath(
		os.path.join(SCRIPT_DIR, "..", "..", "frontdesk", "web", "src", "i18n", "locales")
	)),
]

WEB_LOCALES_DIR = LOCALE_TARGETS[0][1]

ALLOWLIST_PATHS = {
	"web": os.path.join(SCRIPT_DIR, "allow-english.json"),
	"fd": os.path.join(SCRIPT_DIR, "allow-english-fd.json"),
}


# ── Interpolation parity ─────────────────────────────────────────────────────

# {{interpolations}} plus <Trans> markup tags (e.g. <code>...</code>); both
# must survive translation verbatim, so the check compares each locale's set
# against en.json.
PROTECTED_RE = r"\{\{[^}]+\}\}|</?[a-zA-Z][a-zA-Z0-9]*>"


def interpolations(text: str) -> set[str]:
	"""The set of {{placeholders}} and markup tags a string uses."""
	return set(re.findall(PROTECTED_RE, text))


# ── Locale file helpers ─────────────────────────────────────────────────────

def locale_codes(locales_dir: str = WEB_LOCALES_DIR) -> list[str]:
	codes = sorted(
		f[:-5] for f in os.listdir(locales_dir)
		if f.endswith(".json") and f != "en.json"
	)
	if not codes:
		print(f"no locale files found in {locales_dir}", file=sys.stderr)
		sys.exit(1)
	return codes


def load_locale(code: str, locales_dir: str = WEB_LOCALES_DIR) -> dict:
	with open(os.path.join(locales_dir, f"{code}.json"), encoding="utf-8") as f:
		return json.load(f)


def save_locale(code: str, data: dict, locales_dir: str = WEB_LOCALES_DIR):
	with open(os.path.join(locales_dir, f"{code}.json"), "w", encoding="utf-8") as f:
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


def load_allowlist(path: str) -> dict[str, list[str]]:
	if not os.path.exists(path):
		return {}
	with open(path, encoding="utf-8") as f:
		return json.load(f)


def save_allowlist(path: str, allow: dict[str, list[str]]):
	with open(path, "w", encoding="utf-8") as f:
		json.dump(dict(sorted(allow.items())), f, ensure_ascii=False, indent="\t")
		f.write("\n")


def allowed(allow: dict[str, list[str]], key: str, code: str) -> bool:
	langs = allow.get(key)
	return langs is not None and ("*" in langs or code in langs)


# ── should_skip: values translation never touches ───────────────────────────

SKIP_VALUES = {
	"hotel/model", "TBD", "VS", "ON", "OFF", "auto", "N/A", "n/a", "OK",
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


# ── Android string resources ─────────────────────────────────────────────────

# Bellhop keeps its copy in Android XML, not JSON, so it needs its own reader.
# Everything after parsing (missing/extra/placeholders/untranslated, the
# allowlist, should_skip) is shared with the two web apps.
ANDROID_RES_DIR = os.path.normpath(
	os.path.join(SCRIPT_DIR, "..", "..", "android", "app", "src", "main", "res")
)

ANDROID_ALLOWLIST_PATH = os.path.join(SCRIPT_DIR, "allow-english-android.json")

# Android interpolates with printf conversions (%1$s, %1$d, %s), not {{name}},
# and an argument dropped or renumbered in translation crashes at format time.
ANDROID_PROTECTED_RE = r"%\d+\$[a-zA-Z]|%[a-zA-Z]"

# values-cs, values-zh-rCN. Bare "values" is the English base; anything else
# under res/ (values-night, values-v31, drawable*) is not a locale.
ANDROID_LOCALE_DIR_RE = re.compile(r"^values-([a-z]{2,3}(?:-r[A-Z]{2})?)$")


def android_interpolations(text: str) -> set[str]:
	return set(re.findall(ANDROID_PROTECTED_RE, text))


def android_locale_codes() -> list[str]:
	codes = []
	for name in os.listdir(ANDROID_RES_DIR):
		m = ANDROID_LOCALE_DIR_RE.match(name)
		if m and os.path.isfile(os.path.join(ANDROID_RES_DIR, name, "strings.xml")):
			codes.append(m.group(1))
	return sorted(codes)


def load_android(code: str) -> dict[str, str]:
	"""Flat key -> value for one locale.

	A <plurals> set is keyed by its name and represented by its "other" item:
	quantity sets are language-specific (ru/pl carry one/few/many/other where ja
	carries only other), so requiring matching quantities across locales would
	fail correct translations. Every item's placeholders are still checked, via
	the synthetic "<name>#<quantity>" keys below, against English's "other".

	Strings marked translatable="false" (brand names Android itself must not
	localise) are dropped, so they are not reported missing everywhere.
	"""
	path = os.path.join(
		ANDROID_RES_DIR, "values" if code == "en" else f"values-{code}", "strings.xml"
	)
	root = ElementTree.parse(path).getroot()
	out = {}
	for el in root:
		name = el.get("name")
		if not name or el.get("translatable") == "false":
			continue
		if el.tag == "string":
			out[name] = "".join(el.itertext())
		elif el.tag == "plurals":
			for item in el.findall("item"):
				quantity = item.get("quantity", "other")
				text = "".join(item.itertext())
				if quantity == "other":
					out[name] = text
				out[f"{name}#{quantity}"] = text
	return out


def find_android_problems(allow: dict[str, list[str]]) -> dict[str, list[tuple[str, str]]]:
	"""Same problem taxonomy as the web targets, over Android XML."""
	en = load_android("en")
	# Only the "other" item of each plurals set is required everywhere; the rest
	# are checked for placeholder parity when present.
	required = {k for k in en if "#" not in k}
	problems = {"missing": [], "extra": [], "malformed": [], "placeholders": [], "untranslated": []}
	for code in android_locale_codes():
		loc = load_android(code)
		for key in required - loc.keys():
			problems["missing"].append((code, key))
		for key in loc.keys() - en.keys():
			# A quantity English doesn't have (ru's "few") is correct, not extra.
			if "#" in key and key.split("#")[0] in en:
				continue
			problems["extra"].append((code, key))
		for key, value in loc.items():
			# Every plural item is measured against the set's "other", which is
			# the only quantity English is guaranteed to carry.
			reference = en.get(key.split("#")[0] if "#" in key else key)
			if reference is None:
				continue
			if android_interpolations(value) != android_interpolations(reference):
				problems["placeholders"].append((code, key))
			elif (
				"#" not in key
				and value == reference
				and not should_skip(value)
				and not allowed(allow, key, code)
			):
				problems["untranslated"].append((code, key))
	return problems


# ── check ───────────────────────────────────────────────────────────────────

def find_problems(allow: dict[str, list[str]], locales_dir: str = WEB_LOCALES_DIR) -> dict[str, list[tuple[str, str]]]:
	"""Map of problem type -> [(locale, key)]."""
	en = flatten(load_locale("en", locales_dir))
	problems = {"missing": [], "extra": [], "malformed": [], "placeholders": [], "untranslated": []}
	for code in locale_codes(locales_dir):
		loc = flatten(load_locale(code, locales_dir))
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


def _report(label: str, problems: dict[str, list[tuple[str, str]]], locales: int, source: str) -> int:
	"""Print one target's result; return its problem count."""
	total = sum(len(v) for v in problems.values())
	if total == 0:
		print(f"[{label}] i18n check OK: {locales} locales in sync with {source}")
		return 0
	for kind, entries in problems.items():
		if not entries:
			continue
		print(f"\n[{label}] {kind} ({len(entries)}):")
		for code, key in sorted(entries):
			print(f"  {code}: {key}")
	return total


def cmd_check() -> int:
	total = 0
	total += _report("android", find_android_problems(load_allowlist(ANDROID_ALLOWLIST_PATH)),
	                 len(android_locale_codes()), "values/strings.xml")
	for label, locales_dir in LOCALE_TARGETS:
		if not os.path.isdir(locales_dir):
			continue
		allow = load_allowlist(ALLOWLIST_PATHS[label])
		total += _report(label, find_problems(allow, locales_dir), len(locale_codes(locales_dir)), "en.json")
	if total == 0:
		print("i18n check OK: all targets in sync")
		return 0
	print(
		f"\ni18n check FAILED ({total} problems)."
		"\nFix: translate the listed keys into the listed locales by hand and commit"
		"\n(reuse this script's load_locale/set_path/save_locale from a one-off script)."
		"\nIntentionally-English values go into the matching allow-english*.json."
		"\n(Android copy is res/values-*/strings.xml; translate by hand and commit.)"
	)
	if any(
		find_problems(load_allowlist(ALLOWLIST_PATHS[l]), d)["malformed"]
		for l, d in LOCALE_TARGETS if os.path.isdir(d)
	):
		print(
			"Note: `malformed` entries (value is not a string) must be edited by hand."
		)
	return 1


# ── grandfather ─────────────────────────────────────────────────────────────

def _grandfather_one(label: str, path: str, problems_for) -> int:
	"""Snapshot one target's English-equal values into its allowlist."""
	allow = load_allowlist(path)
	added = 0
	for code, key in problems_for(allow)["untranslated"]:
		langs = allow.setdefault(key, [])
		if code not in langs and "*" not in langs:
			langs.append(code)
			added += 1
	for langs in allow.values():
		langs.sort()
	save_allowlist(path, allow)
	print(f"[{label}] allowlisted {added} locale/key pairs into {path}")
	return added


def cmd_grandfather() -> int:
	added_total = _grandfather_one(
		"android", ANDROID_ALLOWLIST_PATH,
		lambda allow: find_android_problems(allow),
	)
	for label, locales_dir in LOCALE_TARGETS:
		if not os.path.isdir(locales_dir):
			continue
		added_total += _grandfather_one(
			label, ALLOWLIST_PATHS[label],
			lambda allow, d=locales_dir: find_problems(allow, d),
		)
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
