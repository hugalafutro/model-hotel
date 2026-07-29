"""Self-tests for translate.py's `check`: the plural-category rule and the
source-key scan.

Run the same way as the coverage scripts' tests:

    cd tools/i18n-translate && python3 -m unittest discover -p 'test_*.py'

The rest of `check` (missing/extra/placeholders/untranslated over ordinary
keys) predates this file; these tests cover the plural rule and the four
places it had to change the existing checks so a locale-only category is not
mistaken for an extra key, plus the literal-t()-key scan, whose whole value is
in what it refuses to report as much as in what it reports.
"""

import json
import os
import tempfile
import unittest

import translate


def write_catalogs(directory: str, catalogs: dict[str, dict]):
	for code, data in catalogs.items():
		with open(os.path.join(directory, f"{code}.json"), "w", encoding="utf-8") as f:
			json.dump(data, f, ensure_ascii=False)


class PluralRuleTest(unittest.TestCase):
	"""Each case builds a throwaway locale directory and runs find_problems."""

	def problems(self, catalogs: dict[str, dict], allow: dict | None = None):
		with tempfile.TemporaryDirectory() as directory:
			write_catalogs(directory, catalogs)
			return translate.find_problems(allow or {}, directory)

	def test_reports_every_category_the_language_defines_and_the_catalog_omits(self):
		problems = self.problems({
			"en": {"k_one": "{{count}} model", "k_other": "{{count}} models"},
			"ru": {"k_one": "{{count}} модель", "k_other": "{{count}} моделей"},
		})
		self.assertEqual(
			problems["plural"], [("ru", "k_few"), ("ru", "k_many")]
		)

	def test_reports_nothing_once_every_required_category_is_present(self):
		problems = self.problems({
			"en": {"k_one": "{{count}} model", "k_other": "{{count}} models"},
			"ru": {
				"k_one": "{{count}} модель",
				"k_few": "{{count}} модели",
				"k_many": "{{count}} моделей",
				"k_other": "{{count}} модели",
			},
		})
		self.assertEqual(problems["plural"], [])
		self.assertEqual(problems["extra"], [])

	def test_does_not_require_categories_the_language_does_not_define(self):
		# de is one/other and ja is other-only: neither may be asked for "_few",
		# and ja's spare "_one" is legitimate rather than missing.
		problems = self.problems({
			"en": {"k_one": "{{count}} model", "k_other": "{{count}} models"},
			"de": {"k_one": "{{count}} Modell", "k_other": "{{count}} Modelle"},
			"ja": {"k_one": "{{count}} 個のモデル", "k_other": "{{count}} 個のモデル"},
		})
		self.assertEqual(problems["plural"], [])

	def test_unclassified_language_is_held_to_one_and_other_only(self):
		self.assertNotIn("zz", translate.PLURAL_CATEGORIES)
		problems = self.problems({
			"en": {"k_one": "{{count}} model", "k_other": "{{count}} models"},
			"zz": {"k_other": "{{count}} mdl"},
		})
		self.assertEqual(problems["plural"], [("zz", "k_one")])

	def test_checks_en_itself_because_it_is_everyone_s_fallback(self):
		# The legacy i18next v3 shape: a bare base key instead of "_one". It
		# resolves, but only by accident, and it leaves every locale without a
		# singular to mirror.
		problems = self.problems({
			"en": {"k": "{{count}} model", "k_other": "{{count}} models"},
			"de": {
				"k": "{{count}} Modell",
				"k_one": "{{count}} Modell",
				"k_other": "{{count}} Modelle",
			},
		})
		self.assertIn(("en", "k_one"), problems["plural"])

	def test_category_english_cannot_have_is_not_an_extra_key(self):
		problems = self.problems({
			"en": {"k_one": "{{count}} model", "k_other": "{{count}} models"},
			"ru": {
				"k_one": "{{count}} модель",
				"k_few": "{{count}} модели",
				"k_many": "{{count}} моделей",
				"k_other": "{{count}} модели",
			},
		})
		self.assertEqual(problems["extra"], [])

	def test_a_key_that_is_not_a_plural_form_is_still_an_extra_key(self):
		# "_few" only earns its exemption when the base is pluralised in en.
		problems = self.problems({
			"en": {"k_one": "{{count}} model", "k_other": "{{count}} models"},
			"de": {
				"k_one": "{{count}} Modell",
				"k_other": "{{count}} Modelle",
				"unrelated_few": "verirrt",
			},
		})
		self.assertEqual(problems["extra"], [("de", "unrelated_few")])

	def test_locale_only_category_must_keep_the_placeholders_of_other(self):
		problems = self.problems({
			"en": {"k_one": "{{count}} model", "k_other": "{{count}} models"},
			"ru": {
				"k_one": "{{count}} модель",
				"k_few": "модели",
				"k_many": "{{count}} моделей",
				"k_other": "{{count}} модели",
			},
		})
		self.assertEqual(problems["placeholders"], [("ru", "k_few")])

	def test_locale_only_category_left_in_english_is_untranslated(self):
		problems = self.problems({
			"en": {"k_one": "{{count}} model", "k_other": "{{count}} models"},
			"ru": {
				"k_one": "{{count}} модель",
				"k_few": "{{count}} models",
				"k_many": "{{count}} моделей",
				"k_other": "{{count}} модели",
			},
		})
		self.assertEqual(problems["untranslated"], [("ru", "k_few")])

	def test_allowlisting_other_as_english_covers_the_categories_built_from_it(self):
		# "procs" and friends are deliberately English in some locales; the
		# extra categories say the same word and must not have to be listed
		# again one by one.
		problems = self.problems(
			{
				"en": {"k_one": "proc", "k_other": "procs"},
				"ru": {
					"k_one": "proc",
					"k_few": "procs",
					"k_many": "procs",
					"k_other": "procs",
				},
			},
			allow={"k_one": ["ru"], "k_other": ["ru"]},
		)
		self.assertEqual(problems["untranslated"], [])


class CategoryTableTest(unittest.TestCase):
	def test_every_shipped_catalog_is_classified_explicitly(self):
		# DEFAULT_PLURAL_CATEGORIES exists so an unknown language still gets a
		# gate, but guessing one/other for a language that has four is exactly
		# the failure this rule was added for. A new catalog must be listed.
		for _, locales_dir in translate.LOCALE_TARGETS:
			if not os.path.isdir(locales_dir):
				continue
			for code in ["en", *translate.locale_codes(locales_dir)]:
				self.assertIn(
					code, translate.PLURAL_CATEGORIES,
					f"{code}.json ships without a PLURAL_CATEGORIES entry",
				)

	def test_either_one_or_other_marks_a_pluralised_key(self):
		# "d_one" alone is the shape an _other-only rule cannot see: it renders
		# at count=1 and nowhere else.
		self.assertEqual(
			translate.plural_bases({"a_one", "a_other", "b_other", "c", "d_one"}),
			{"a", "b", "d"},
		)

	def test_a_category_word_in_a_key_name_does_not_invent_a_family(self):
		# failover.toast_entry_min_two really is a sentence about a minimum of
		# two members. Treating "_two" as a marker would demand a singular and
		# a plural of a key that has neither.
		self.assertEqual(
			translate.plural_bases({"toast_entry_min_two", "cache_zero", "n_few"}),
			set(),
		)


class LiteralKeyScanTest(unittest.TestCase):
	"""What literal_t_keys extracts from a source file, and what it declines to.

	A false positive here fails CI on correct code, so the negative cases carry
	as much weight as the positive one.
	"""

	def keys(self, src: str) -> list[str]:
		return [k for k, _ in translate.literal_t_keys(src)]

	def test_extracts_plain_calls_including_the_i18next_object_form(self):
		src = 'const a = t("a.one");\nconst b = i18next.t(\'b.two\');\n'
		self.assertEqual(self.keys(src), ["a.one", "b.two"])

	def test_reports_the_line_the_call_starts_on(self):
		src = 'x\ny\nconst a = t(\n\t"a.one",\n\t{ count },\n);\n'
		self.assertEqual(translate.literal_t_keys(src), [("a.one", 3)])

	def test_an_options_object_still_needs_the_key(self):
		self.assertEqual(self.keys('t("a.one", { count: 2 })'), ["a.one"])

	def test_an_inline_default_makes_the_key_optional(self):
		# i18next renders the second argument when the key is absent, so such a
		# call is not a missing key and must never be reported.
		self.assertEqual(self.keys('t("a.one", "Fallback text")'), [])

	def test_a_template_literal_key_is_skipped_rather_than_guessed(self):
		self.assertEqual(self.keys("t(`field.${name}`)"), [])
		self.assertEqual(self.keys("t(`field.plain`)"), [])

	def test_a_computed_key_is_skipped(self):
		# The literal is one fragment of a larger expression in each of these,
		# so what i18next actually receives is not knowable here.
		self.assertEqual(self.keys('t(item.labelKey)'), [])
		self.assertEqual(self.keys('t("field." + name)'), [])
		self.assertEqual(self.keys('t(on ? "a.on" : "a.off")'), [])

	def test_a_key_named_only_in_a_comment_is_not_a_call(self):
		src = '// see t("ghost.key")\n/* or t("other.ghost") */\nt("real.key")\n'
		self.assertEqual(self.keys(src), ["real.key"])

	def test_a_key_quoted_inside_a_string_is_not_a_call(self):
		self.assertEqual(self.keys('const s = "t(\\"ghost.key\\")";'), [])

	def test_a_quote_inside_a_regex_literal_does_not_swallow_the_rest(self):
		# /["']/ opens a quote the scanner must not follow to the end of file,
		# or every call after it disappears and the gate silently stops working.
		src = 'const q = /["\']/g;\nt("after.regex")\n'
		self.assertEqual(self.keys(src), ["after.regex"])

	def test_identifiers_ending_in_t_are_not_translation_calls(self):
		self.assertEqual(self.keys('parseInt("10"); expect("x"); arr.at("0")'), [])


class UnresolvedSourceKeyTest(unittest.TestCase):
	"""find_unresolved_source_keys over a throwaway source tree + catalog."""

	def unresolved(self, en: dict, files: dict[str, str]):
		with tempfile.TemporaryDirectory() as locales, tempfile.TemporaryDirectory() as src:
			write_catalogs(locales, {"en": en})
			for name, body in files.items():
				path = os.path.join(src, name)
				os.makedirs(os.path.dirname(path), exist_ok=True)
				with open(path, "w", encoding="utf-8") as f:
					f.write(body)
			return [
				(os.path.basename(p), line, key)
				for p, line, key in translate.find_unresolved_source_keys(src, locales)
			]

	def test_a_key_no_catalog_defines_is_reported(self):
		# The #583 failure mode: parity is perfect (every catalog agrees) and
		# the screen still shows "chat.toast.chatReset".
		found = self.unresolved(
			{"chat": {"toast": {"chatCleared": "Chat cleared"}}},
			{"Chat.tsx": 't("chat.toast.chatReset")\n'},
		)
		self.assertEqual(found, [("Chat.tsx", 1, "chat.toast.chatReset")])

	def test_a_key_en_defines_is_not_reported(self):
		found = self.unresolved(
			{"chat": {"toast": {"chatCleared": "Chat cleared"}}},
			{"Chat.tsx": 't("chat.toast.chatCleared")\n'},
		)
		self.assertEqual(found, [])

	def test_a_counted_key_resolves_through_its_plural_categories(self):
		# t("x", {count}) is never stored as "x"; only "x_one"/"x_other" exist.
		found = self.unresolved(
			{"models": {"badge_one": "{{count}} model", "badge_other": "{{count}} models"}},
			{"Models.tsx": 't("models.badge", { count })\n'},
		)
		self.assertEqual(found, [])

	def test_test_files_are_not_scanned(self):
		# Tests name synthetic keys to exercise fallback paths; the catalog must
		# not be forced to carry them.
		found = self.unresolved(
			{"a": "A"},
			{
				"__tests__/Chat.test.tsx": 't("synthetic.key")\n',
				"Chat.test.tsx": 't("other.synthetic.key")\n',
			},
		)
		self.assertEqual(found, [])

	def test_every_shipped_source_tree_is_scanned(self):
		# A typo in SOURCE_TARGETS would make the gate pass by scanning nothing.
		for label, root in translate.SOURCE_TARGETS.items():
			self.assertTrue(os.path.isdir(root), f"{label}: {root} is not a directory")
			self.assertTrue(
				any(True for _ in translate.source_files(root)),
				f"{label}: no .ts/.tsx sources found under {root}",
			)


if __name__ == "__main__":
	unittest.main()
