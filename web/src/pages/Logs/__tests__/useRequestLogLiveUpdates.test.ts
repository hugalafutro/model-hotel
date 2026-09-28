import { describe, expect, it } from "vitest";
import type { LogEntry } from "../../../api/types";
import { keepFresherRow } from "../useRequestLogLiveUpdates";

const row = (state: LogEntry["state"]) => ({ state }) as LogEntry;

describe("keepFresherRow", () => {
	it("refuses a snapshot from earlier in the row's life", () => {
		expect(keepFresherRow(row("completed"), row("streaming"))).toBe(true);
		expect(keepFresherRow(row("completed"), row("pending"))).toBe(true);
		expect(keepFresherRow(row("streaming"), row("pending"))).toBe(true);
	});

	it("takes a later or same-state copy", () => {
		expect(keepFresherRow(row("pending"), row("streaming"))).toBe(false);
		expect(keepFresherRow(row("streaming"), row("completed"))).toBe(false);
		expect(keepFresherRow(row("completed"), row("completed"))).toBe(false);
		expect(keepFresherRow(row("streaming"), row("failed"))).toBe(false);
	});
});
