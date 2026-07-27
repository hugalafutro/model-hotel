"""Self-tests for the plural-category rule in translate.py's `check`.

Run the same way as the coverage scripts' tests:

    cd tools/i18n-translate && python3 -m unittest discover -p 'test_*.py'

The rest of `check` (missing/extra/placeholders/untranslated over ordinary
keys) predates this file; these tests cover the plural rule and the four
places it had to change the existing checks so a locale-only category is not
mistaken for an extra key.
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

	def test_other_is_the_marker_for_a_pluralised_key(self):
		self.assertEqual(
			translate.plural_bases({"a_one", "a_other", "b_other", "c"}),
			{"a", "b"},
		)


if __name__ == "__main__":
	unittest.main()
