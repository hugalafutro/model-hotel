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
	{
		// The components and hooks still above the ratchet. The list only
		// shrinks: a file leaves it when it is split, and nothing new goes on
		// it. Keeping the exemptions here rather than in per-line disables
		// makes the remaining debt one readable list.
		files: [
			"src/components/Layout.tsx",
			"src/components/ModelDiscrepancyModal.tsx",
			"src/components/VirtualModelTable.tsx",
			"src/pages/Arena.tsx",
			"src/pages/Arena/useArenaRunner.ts",
			"src/pages/Chat.tsx",
			"src/pages/Chat/useChat.ts",
			"src/pages/Dashboard.tsx",
			"src/pages/Dashboard/useDashboard.ts",
			"src/pages/FailoverGroups.tsx",
			"src/pages/Logs.tsx",
			"src/pages/Models/ModelDetailModal.tsx",
			"src/pages/Security/index.tsx",
			"src/pages/Settings/AlertsSettings.tsx",
			"src/pages/Settings/DataStorageSettings.tsx",
			"src/pages/Settings/DatabaseBackupSettings.tsx",
			"src/pages/Users/UserModal.tsx",
			"src/pages/VirtualKeys/KeyDetailModal.tsx",
		],
		rules: {
			"max-lines-per-function": "off",
		},
	},
]);
