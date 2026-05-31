import path from "node:path";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
	plugins: [react()],
	resolve: {
		alias: {
			"@": path.resolve(__dirname, "./src"),
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
			include: ["src/**/*.{ts,tsx}"],
			exclude: [
				"src/**/*.test.{ts,tsx}",
				"src/**/__tests__/**",
				"src/test/**",
				"src/main.tsx",
				"src/**/*.d.ts",
				"src/**/types.ts",
				"src/components/logs/index.ts",
			],
		},
	},
});
