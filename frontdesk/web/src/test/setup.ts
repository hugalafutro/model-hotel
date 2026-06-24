import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterAll, afterEach, beforeAll } from "vitest";
import "../i18n"; // initialize i18next (English bundled) for every test
import { server } from "./server";

// MSW lifecycle, shared by every test file. Unhandled requests error so a test
// that hits an unmocked endpoint fails loudly instead of silently.
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
	cleanup();
	server.resetHandlers();
	localStorage.clear();
});
afterAll(() => server.close());
