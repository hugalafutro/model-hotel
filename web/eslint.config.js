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
		// All react-hooks rules run at "error": the codebase is clean, and
		// known false positives (stable-ref access in streaming hooks,
		// Date.now() in render, TanStack Virtual's mutable functions) carry
		// per-line eslint-disable comments with justifications. Keep it that
		// way — a warn-level downgrade would let new violations pass CI
		// silently.
		rules: {
			"react-hooks/exhaustive-deps": "error",
			"react-hooks/preserve-manual-memoization": "error",
			"react-hooks/purity": "error",
			"react-hooks/refs": "error",
			"react-hooks/set-state-in-effect": "error",
			// Underscore prefix marks intentionally-unused params/vars
			// (mock interfaces, destructure-to-omit) instead of per-line
			// disables.
			"@typescript-eslint/no-unused-vars": [
				"error",
				{
					argsIgnorePattern: "^_",
					varsIgnorePattern: "^_",
					caughtErrorsIgnorePattern: "^_",
				},
			],
		},
	},
]);
