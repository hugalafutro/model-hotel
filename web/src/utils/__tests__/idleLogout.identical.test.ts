import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

// The idle-logout helper is duplicated verbatim in the two Vite roots (web/ and
// frontdesk/web/) because they cannot import across roots. Nothing else enforces
// that the copies stay in sync, so this test fails the moment they drift: edit
// one, copy it to the other. Paths resolve from the web/ package root (vitest's
// cwd); the FD copy is its sibling.
describe("idleLogout.ts copies", () => {
	it("are byte-identical across the two frontends", () => {
		const webCopy = resolve(process.cwd(), "src/utils/idleLogout.ts");
		const fdCopy = resolve(
			process.cwd(),
			"../frontdesk/web/src/utils/idleLogout.ts",
		);
		expect(readFileSync(fdCopy, "utf8")).toBe(readFileSync(webCopy, "utf8"));
	});
});
