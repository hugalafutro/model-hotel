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
		// Downgrade from "error" to "warn": streaming/orchestration hooks
		// intentionally access mutable refs (abortMapRef, roundsRef, etc.)
		// inside useCallback bodies without listing them in dep arrays.
		// Ref identity is stable so they're never needed as deps, but the
		// linter can't distinguish refs from other values. "warn" catches
		// genuine missing deps without blocking CI on ref false positives.
		rules: {
			"react-hooks/exhaustive-deps": "warn",
		},
	},
]);
