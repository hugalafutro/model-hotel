import js from "@eslint/js";
import { defineConfig, globalIgnores } from "eslint/config";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import globals from "globals";
import tseslint from "typescript-eslint";

export default defineConfig([
	globalIgnores(["dist", "coverage"]),
	{
		files: ["**/*.{ts,tsx}"],
		extends: [
			js.configs.recommended,
			tseslint.configs.recommended,
			reactHooks.configs.flat.recommended,
			reactRefresh.configs.vite,
		],
		languageOptions: {
			ecmaVersion: 2020,
			globals: globals.browser,
		},
		// Kept at "error" (plugin default is warn): warn-level findings pass
		// `pnpm lint` silently, so genuine missing deps would never block CI.
		rules: {
			"react-hooks/exhaustive-deps": "error",
			// Function-length ratchet, the per-function half of the file-size
			// gate in scripts/ci/size-gate.sh. Blank lines and comments do not
			// count, so a documented function is never penalised for its
			// documentation; IIFEs are measured like any other function.
			"max-lines-per-function": [
				"error",
				{ max: 500, skipBlankLines: true, skipComments: true, IIFEs: true },
			],
		},
	},
	{
		// A vitest file is one describe() call, so the whole suite reads as a
		// single arrow function and the rule measures the file rather than any
		// unit of logic. Test length is capped by the file-size gate instead.
		files: [
			"**/*.test.{ts,tsx}",
			"**/__tests__/**/*.{ts,tsx}",
			"src/test/**/*.{ts,tsx}",
		],
		rules: {
			"max-lines-per-function": "off",
		},
	},
]);
