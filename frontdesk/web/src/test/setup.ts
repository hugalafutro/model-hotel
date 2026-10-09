import "@testing-library/jest-dom/vitest";
import { cleanup, configure } from "@testing-library/react";
import { afterAll, afterEach, beforeAll } from "vitest";
import "../i18n"; // initialize i18next (English bundled) for every test
import { server } from "./server";

// testing-library's default 1s asyncUtilTimeout is too tight for a page that
// mounts several panels, each with its own MSW reads, on a CI runner under
// coverage: the Settings page's save-error test missed its alert by ~150ms on
// two runs in a row. The per-test budget (vitest testTimeout) stays the real
// ceiling and no assertion changes, so this tolerates scheduling jitter and
// hides no failure. The main dashboard's setup carries the same setting.
configure({ asyncUtilTimeout: 5000 });

// MSW lifecycle, shared by every test file. Unhandled requests error so a test
// that hits an unmocked endpoint fails loudly instead of silently.
beforeAll(() => server.listen({ onUnhandledFrame: "error" }));
afterEach(() => {
	cleanup();
	server.resetHandlers();
	localStorage.clear();
});
afterAll(() => server.close());
