import path from "node:path";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
	plugins: [react()],
	resolve: {
		alias: {
			"@": path.resolve(__dirname, "./src"),
			"@quota-shared": path.resolve(__dirname, "../web-shared/quota/index.ts"),
		},
	},
	test: {
		environment: "jsdom",
		env: { TZ: "UTC" },
		globals: true,
		setupFiles: ["./src/test/setup.ts"],
		include: ["src/**/*.test.{ts,tsx}"],
		retry: 2,
		testTimeout: 15000,
		coverage: {
			provider: "v8",
			reporter: ["text", "html", "lcov", "json-summary"],
			// web-shared/ holds the quota parsing both SPAs import through
			// @quota-shared, and web/ is its owning app for coverage. It sits
			// outside this project's root, so it needs allowExternal plus a
			// pattern that matches an ABSOLUTE path: vitest tests coverage.include
			// against the resolved filename (picomatch, `contains: true`), where a
			// `../`-relative glob can never match.
			allowExternal: true,
			include: ["src/**/*.{ts,tsx}", "**/web-shared/**/*.ts"],
			exclude: [
				"src/**/*.test.{ts,tsx}",
				"src/**/__tests__/**",
				"src/test/**",
				"src/main.tsx",
				"src/**/*.d.ts",
				"src/**/types.ts",
				"src/components/logs/index.ts",
				"src/components/ProviderModals.tsx",
			],
		},
	},
});
