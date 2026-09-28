import { describe, expect, it } from "vitest";
import type { LogEntry } from "../../../api/types";
import { keepFinishedRow } from "../useRequestLogLiveUpdates";

const row = (state: LogEntry["state"]) => ({ state }) as LogEntry;

describe("keepFinishedRow", () => {
	it("refuses an in-flight snapshot over a finished row", () => {
		expect(keepFinishedRow(row("completed"), row("streaming"))).toBe(true);
		expect(keepFinishedRow(row("completed"), row("pending"))).toBe(true);
	});

	it("takes every other update", () => {
		expect(keepFinishedRow(row("streaming"), row("completed"))).toBe(false);
		expect(keepFinishedRow(row("pending"), row("streaming"))).toBe(false);
		expect(keepFinishedRow(row("completed"), row("completed"))).toBe(false);
	});
});
