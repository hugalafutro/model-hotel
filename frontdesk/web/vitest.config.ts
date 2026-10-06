import path from "node:path";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
	plugins: [react()],
	resolve: {
		alias: {
			"@": path.resolve(import.meta.dirname, "./src"),
			// Same prefix alias as vite.config.ts: a vitest config replaces the vite
			// config wholesale, so the two have to be kept in step.
			"@web-shared": path.resolve(import.meta.dirname, "../../web-shared"),
		},
	},
	test: {
		globals: true,
		environment: "jsdom",
		// Leave setImmediate real. msw 3 intercepts at the socket layer, so a
		// mocked response travels through Node stream plumbing that hops over
		// setImmediate; with it faked, nothing short of an advanceTimers tick
		// drains that queue and fetches issued under vi.useFakeTimers() never
		// resolve (every fake-timer test stalled on "Loading…").
		fakeTimers: {
			toFake: [
				"setTimeout",
				"clearTimeout",
				"setInterval",
				"clearInterval",
				"Date",
			],
		},
		setupFiles: ["./src/test/setup.ts"],
		css: false,
		coverage: {
			provider: "v8",
			include: ["src/**/*.{ts,tsx}"],
			exclude: ["src/**/*.test.{ts,tsx}", "src/test/**", "src/main.tsx"],
		},
	},
});
