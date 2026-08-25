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
                 carries a non-string value, carries an English-equal value
                 that is not allowlisted, or omits a plural category its
                 language requires (see PLURAL_CATEGORIES). It also scans the
                 TypeScript sources for literal t("...") keys that en.json does
                 not define (see find_unresolved_source_keys).
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


# ── Plural categories ────────────────────────────────────────────────────────

# i18next selects a plural form with Intl.PluralRules and then looks up
# "<base>_<category>". English only ever has "one" and "other", so a locale
# that ships only those two silently breaks for every other category its
# language defines: Russian count=22 asks for "_few", Arabic count=2 asks for
# "_two". i18next then falls back to the bare base key (right locale, wrong
# grammatical form) or, when there is none, to the English string. Key parity
# cannot see this, because every locale carries the same _one/_other pair.
#
# Generated from Intl.PluralRules itself (the resolver i18next uses), for every
# locale either web app ships plus the "nb" alias:
#
#   node -e 'for (const l of [...]) console.log(l,
#     new Intl.PluralRules(l).resolvedOptions().pluralCategories)'
#
# Two entries surprise people and are deliberate:
#   * he is one/two/other. CLDR dropped Hebrew's "many" (it only ever covered
#     round tens in a register no UI string uses), so "_many" is NOT required.
#   * ca/es/fr/it/pt carry "many". It is not a general plural: it selects only
#     for exact multiples of a million, where these languages use the same
#     wording as "other" ("1000000 modelos"). The form is therefore usually a
#     copy of "_other", and that is correct rather than lazy. It exists so a
#     count of exactly 1000000 renders Spanish instead of English.
PLURAL_CATEGORIES = {
	"af": ("one", "other"),
	"ar": ("zero", "one", "two", "few", "many", "other"),
	"ca": ("one", "many", "other"),
	"cs": ("one", "few", "many", "other"),
	"da": ("one", "other"),
	"de": ("one", "other"),
	"el": ("one", "other"),
	"en": ("one", "other"),
	"es": ("one", "many", "other"),
	"fi": ("one", "other"),
	"fr": ("one", "many", "other"),
	"he": ("one", "two", "other"),
	"hu": ("one", "other"),
	"it": ("one", "many", "other"),
	"ja": ("other",),
	"ko": ("other",),
	"nb": ("one", "other"),
	"nl": ("one", "other"),
	"no": ("one", "other"),
	"pl": ("one", "few", "many", "other"),
	"pt": ("one", "many", "other"),
	"ro": ("one", "few", "other"),
	"ru": ("one", "few", "many", "other"),
	"sk": ("one", "few", "many", "other"),
	"sr": ("one", "few", "other"),
	"sv": ("one", "other"),
	"tr": ("one", "other"),
	"uk": ("one", "few", "many", "other"),
	"vi": ("other",),
	"zh": ("other",),
}

# Every CLDR cardinal category, so a suffix can be recognised as one. Ordered
# longest-lived first only for readability; membership is what matters.
PLURAL_SUFFIXES = ("zero", "one", "two", "few", "many", "other")

# A locale file with no entry above is a new language nobody has classified.
# Requiring one/other keeps the gate honest without guessing extra forms; add
# the language to PLURAL_CATEGORIES when you add its catalog.
DEFAULT_PLURAL_CATEGORIES = ("one", "other")


# "_other" and "_one" are the two forms en.json always carries, so either one
# marks a key as pluralised. Deliberately NOT the rest: "_zero", "_two", "_few"
# and "_many" are ordinary English words that turn up in key names
# ("failover.toast_entry_min_two" is a sentence about a minimum of two members,
# not the dual of "failover.toast_entry_min"), and treating those as families
# would demand five forms of a key that has none.
PLURAL_MARKERS = ("_other", "_one")


def plural_bases(keys) -> set[str]:
	"""Every pluralised key, minus its category suffix.

	Keying only off "_other" would miss the one broken shape it cannot see: a
	plural key shipped with "_one" alone. Such a key resolves for count=1 and
	renders nothing else, and no other check would notice.
	"""
	return {
		k[: -len(marker)]
		for k in keys
		for marker in PLURAL_MARKERS
		if k.endswith(marker)
	}


def missing_plural_forms(code: str, keys) -> list[str]:
	"""Plural forms `code`'s language requires but this key set does not have."""
	categories = PLURAL_CATEGORIES.get(code, DEFAULT_PLURAL_CATEGORIES)
	return sorted(
		f"{base}_{category}"
		for base in plural_bases(keys)
		for category in categories
		if f"{base}_{category}" not in keys
	)


def plural_reference(key: str, en_bases: set[str]) -> str | None:
	"""en.json's "_other" form for a category English itself cannot have.

	Russian's "<base>_few" has no English counterpart, so it would otherwise
	read as an extra key and escape the placeholder/untranslated checks. Its
	reference is the "_other" form, the only category English is guaranteed to
	carry. Returns None for keys that are not plural forms of a known base.
	"""
	for category in PLURAL_SUFFIXES:
		suffix = f"_{category}"
		if key.endswith(suffix) and key[: -len(suffix)] in en_bases:
			return f"{key[: -len(suffix)]}_other"
	return None


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
	"✏️Custom",
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


# ── Source-key scan ──────────────────────────────────────────────────────────

# Locale parity only ever compares catalogs against en.json, so a t("some.key")
# whose key was never added to en.json is invisible to it: every catalog agrees,
# and i18next renders the raw key string on screen. That is how ~40 such keys
# shipped green in PR #583. This scan closes the other half of the loop by
# reading the sources and checking each literal key resolves in en.json.
#
# Only the two web apps participate. Bellhop refers to its copy as R.string.foo,
# which aapt2 and the Kotlin compiler resolve at build time (there is not one
# getIdentifier() call in android/), so a missing Android string is already a
# build failure rather than a silent raw key.
SOURCE_TARGETS = {
	"web": os.path.normpath(os.path.join(SCRIPT_DIR, "..", "..", "web", "src")),
	"fd": os.path.normpath(
		os.path.join(SCRIPT_DIR, "..", "..", "frontdesk", "web", "src")
	),
}

SOURCE_EXTENSIONS = (".ts", ".tsx")

# Tests are excluded on purpose: they never render to a user, and they routinely
# name synthetic keys ("a.b.c") to exercise fallback and error paths, which the
# catalog must not be forced to carry.
SOURCE_SKIP_DIRS = {"__tests__", "node_modules", "dist"}
SOURCE_SKIP_SUFFIXES = (".test.ts", ".test.tsx", ".d.ts")

_IDENT_CHAR = re.compile(r"[A-Za-z0-9_$]")

# `t(` / `i18next.t(` / `i18n.t(`, tolerating the line breaks Biome inserts. The
# lookbehind is what keeps `parseInt(`, `expect(` and `at(` out; a preceding "."
# is deliberately allowed, because i18next.t is the same lookup.
_T_CALL = re.compile(r"t\s*\(\s*")


def _read_js_string(src: str, i: int) -> tuple[str | None, int]:
	"""Read the string literal starting at src[i] (a quote character).

	Returns (value, index after the closing quote), or (None, i + 1) when this
	is not in fact a complete single-line string. That second case is the
	resync: a quote inside a regex literal (/["']/) would otherwise swallow the
	rest of the file. Ordinary JS strings cannot span a newline, so hitting one
	before the closing quote proves the opening quote was not a string start.

	Template literals always return None: their value is only decidable when
	they carry no ${...} substitution, and the caller skips them wholesale
	rather than resolving half of a family (see the module docstring).
	"""
	quote = src[i]
	if quote == "`":
		# Still consume it, so a backtick string's contents are not scanned as
		# code, but never offer its value as a key.
		j = i + 1
		while j < len(src):
			if src[j] == "\\":
				j += 2
				continue
			if src[j] == "`":
				return None, j + 1
			j += 1
		return None, i + 1
	out = []
	j = i + 1
	while j < len(src):
		c = src[j]
		if c == "\\":
			out.append(src[j + 1 : j + 2])
			j += 2
			continue
		if c == quote:
			return "".join(out), j + 1
		if c == "\n":
			return None, i + 1
		out.append(c)
		j += 1
	return None, i + 1


def literal_t_keys(src: str) -> list[tuple[str, int]]:
	"""Every literal translation key the source asks i18next to resolve.

	Returns [(key, line number)]. A call is reported only when its key is a
	plain string literal AND the call either ends there or continues with an
	options object; both of those render the catalog value. Deliberately NOT
	reported, because none of them can be missing keys:

	  * t(expr) / t(`a.${b}`) / t("a." + b)  - undecidable statically.
	  * t("key", "Fallback text")            - i18next renders the fallback, so
	                                           the key is legitimately absent.

	Distinguishing those last two is the whole reason this walks the file
	instead of running a regex over it: the character after the comma decides
	(a quote means a default string, "{" means options), and getting there
	requires knowing where the key's own literal ended.
	"""
	keys: list[tuple[str, int]] = []
	i, n = 0, len(src)
	while i < n:
		c = src[i]
		pair = src[i : i + 2]
		if pair == "//":
			nl = src.find("\n", i)
			i = n if nl < 0 else nl
		elif pair == "/*":
			end = src.find("*/", i + 2)
			i = n if end < 0 else end + 2
		elif c in "\"'`":
			_, i = _read_js_string(src, i)
		elif c == "t" and (i == 0 or not _IDENT_CHAR.match(src[i - 1])):
			m = _T_CALL.match(src, i)
			if m is None:
				i += 1
				continue
			start, j = i, m.end()
			if j >= n or src[j] not in "\"'":
				i = j
				continue
			key, after = _read_js_string(src, j)
			if key is None:
				i = j
				continue
			k = after
			while k < n and src[k] in " \t\r\n":
				k += 1
			nxt = src[k : k + 1]
			if nxt == ",":
				k += 1
				while k < n and src[k] in " \t\r\n":
					k += 1
				nxt = src[k : k + 1]
				# A default string renders instead of the catalog value.
				if nxt not in "\"'`":
					keys.append((key, src.count("\n", 0, start) + 1))
			elif nxt == ")":
				keys.append((key, src.count("\n", 0, start) + 1))
			# Anything else ("+", "?", ":") means the literal was one fragment
			# of a larger expression, which is undecidable.
			i = after
		else:
			i += 1
	return keys


def source_files(root: str):
	for dirpath, dirnames, filenames in os.walk(root):
		dirnames[:] = sorted(d for d in dirnames if d not in SOURCE_SKIP_DIRS)
		for name in sorted(filenames):
			if name.endswith(SOURCE_EXTENSIONS) and not name.endswith(SOURCE_SKIP_SUFFIXES):
				yield os.path.join(dirpath, name)


def key_resolves(key: str, en: dict) -> bool:
	"""Whether i18next would find a value for `key` in en.json.

	A counted key is asked for as t("x", {count}) but stored as "x_one"/"x_other",
	so the bare base must count as resolved when any category of it exists.
	"""
	return key in en or any(f"{key}_{c}" in en for c in PLURAL_SUFFIXES)


def find_unresolved_source_keys(src_root: str, locales_dir: str) -> list[tuple[str, int, str]]:
	"""[(file, line, key)] for literal t() keys en.json does not define."""
	en = flatten(load_locale("en", locales_dir))
	repo = os.path.normpath(os.path.join(SCRIPT_DIR, "..", ".."))
	found = []
	for path in source_files(src_root):
		with open(path, encoding="utf-8") as f:
			source = f.read()
		for key, line in literal_t_keys(source):
			if not key_resolves(key, en):
				found.append((os.path.relpath(path, repo), line, key))
	return found


# ── check ───────────────────────────────────────────────────────────────────

def find_problems(allow: dict[str, list[str]], locales_dir: str = WEB_LOCALES_DIR) -> dict[str, list[tuple[str, str]]]:
	"""Map of problem type -> [(locale, key)]."""
	en = flatten(load_locale("en", locales_dir))
	problems = {"missing": [], "extra": [], "malformed": [], "placeholders": [], "untranslated": [], "plural": []}
	en_bases = plural_bases(en)
	# en is the fallback for every other locale, so a category missing here is
	# missing everywhere; it is checked even though it is nobody's translation.
	for key in missing_plural_forms("en", en):
		problems["plural"].append(("en", key))
	for code in locale_codes(locales_dir):
		loc = flatten(load_locale(code, locales_dir))
		for key in en.keys() - loc.keys():
			problems["missing"].append((code, key))
		for key in loc.keys() - en.keys():
			# A category English has no equivalent of (ru's "few") is required,
			# not extra.
			if plural_reference(key, en_bases) is None:
				problems["extra"].append((code, key))
		for key in missing_plural_forms(code, loc):
			problems["plural"].append((code, key))
		for key, value in loc.items():
			# Plural forms English cannot have are measured against the "_other"
			# form, so they still have to keep their placeholders and still have
			# to be translated.
			reference_key = key if key in en else plural_reference(key, en_bases)
			if reference_key is None:
				continue
			reference = en[reference_key]
			if not isinstance(value, str) or not isinstance(reference, str):
				problems["malformed"].append((code, key))
			elif interpolations(value) != interpolations(reference):
				problems["placeholders"].append((code, key))
			elif (
				value == reference
				and not should_skip(value)
				and not allowed(allow, key, code)
				# An "_other" allowlisted as intentionally English covers the
				# extra categories built from it, which say the same thing.
				and not allowed(allow, reference_key, code)
			):
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


def _report_source(label: str, unresolved: list[tuple[str, int, str]]) -> int:
	"""Print one target's source scan; return its problem count."""
	if not unresolved:
		print(f"[{label}] source keys OK: every literal t() key resolves in en.json")
		return 0
	print(f"\n[{label}] unresolved source keys ({len(unresolved)}):")
	for path, line, key in sorted(unresolved):
		print(f"  {path}:{line}: {key}")
	return len(unresolved)


def cmd_check() -> int:
	total = 0
	source_failed = False
	total += _report("android", find_android_problems(load_allowlist(ANDROID_ALLOWLIST_PATH)),
	                 len(android_locale_codes()), "values/strings.xml")
	web_problems = []
	for label, locales_dir in LOCALE_TARGETS:
		if not os.path.isdir(locales_dir):
			continue
		problems = find_problems(load_allowlist(ALLOWLIST_PATHS[label]), locales_dir)
		web_problems.append(problems)
		total += _report(label, problems, len(locale_codes(locales_dir)), "en.json")
		src_root = SOURCE_TARGETS[label]
		if os.path.isdir(src_root):
			unresolved = find_unresolved_source_keys(src_root, locales_dir)
			count = _report_source(label, unresolved)
			source_failed = source_failed or count > 0
			total += count
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
	if any(p["plural"] for p in web_problems):
		print(
			"Note: `plural` entries are plural forms the language requires and the"
			"\ncatalog omits. Each needs a real translation with the right numeric"
			"\nagreement, not a copy of _other; en.json cannot supply them, so"
			"\ni18next would fall back to English or to the wrong form."
		)
	if any(p["malformed"] for p in web_problems):
		print(
			"Note: `malformed` entries (value is not a string) must be edited by hand."
		)
	if source_failed:
		print(
			"Note: `unresolved source keys` are t(\"...\") calls whose key is in no"
			"\ncatalog at all, so i18next renders the raw key string on screen. Add the"
			"\nkey to en.json (and translate it), point the call at the key that already"
			"\nexists, or give the call an inline default: t(\"key\", \"Fallback text\")."
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
