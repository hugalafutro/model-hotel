import { barTone } from "@web-shared/quota";
import {
	quotaRightText,
	resetSublabel,
	resetSublabelFromEpoch,
	type TimeFormat,
} from "@web-shared/quota/modals";

// Asserts on sub-keys and on the instant handed to the time format, never on a
// translated or locale-formatted value.
const stubT = (key: string, opts?: Record<string, unknown>) =>
	opts?.when ? `${key} ${opts.when}` : key;
const time: TimeFormat = {
	absolute: (iso) => `at ${iso}`,
	relative: (iso) => `in ${iso}`,
};

describe("barTone", () => {
	it("grades remaining mode from the remaining share", () => {
		expect(barTone(90, "remaining")).toBe("danger"); // 10% left
		expect(barTone(50, "remaining")).toBe("warn"); // 50% left
		expect(barTone(10, "remaining")).toBe("ok"); // 90% left
	});

	it("grades used mode from the used share", () => {
		expect(barTone(20, "used")).toBe("warn");
		expect(barTone(60, "used")).toBe("high");
		expect(barTone(95, "used")).toBe("danger");
	});

	// Each threshold is exclusive (`<`). The tests below sit exactly on the
	// boundary and one step below it, so flipping any `<` to `<=` flips an
	// assertion; the round numbers used above all sit mid-band and survive that.
	it("puts exactly 20% remaining in warn, not danger", () => {
		expect(barTone(80, "remaining")).toBe("warn"); // 20% left
		expect(barTone(81, "remaining")).toBe("danger"); // 19% left
	});

	it("puts exactly 60% remaining in ok, not warn", () => {
		expect(barTone(40, "remaining")).toBe("ok"); // 60% left
		expect(barTone(41, "remaining")).toBe("warn"); // 59% left
	});

	it("puts exactly 50% used in high, not warn", () => {
		expect(barTone(50, "used")).toBe("high");
		expect(barTone(49, "used")).toBe("warn");
	});

	it("puts exactly 80% used in danger, not high", () => {
		expect(barTone(80, "used")).toBe("danger");
		expect(barTone(79, "used")).toBe("high");
	});
});

describe("quotaRightText", () => {
	it("reports the used share in used mode", () => {
		expect(quotaRightText(40, "used", stubT)).toBe("40% used");
	});

	it("reports the remaining share in remaining mode", () => {
		expect(quotaRightText(40, "remaining", stubT)).toBe("60% left");
	});
});

describe("resetSublabel", () => {
	it("returns the dash sentinel for undefined", () => {
		expect(resetSublabel(undefined, stubT, time)).toBe("-");
	});

	it("returns the dash sentinel for null", () => {
		expect(resetSublabel(null, stubT, time)).toBe("-");
	});

	it("returns the dash sentinel for an empty string", () => {
		expect(resetSublabel("", stubT, time)).toBe("-");
	});

	it("returns the dash sentinel for an unparseable ISO string", () => {
		expect(resetSublabel("not-a-date", stubT, time)).toBe("-");
	});

	it("puts the absolute instant in the resets line and the relative one under it", () => {
		const iso = "2026-08-01T00:00:00Z";
		expect(resetSublabel(iso, stubT, time)).toBe(`resets at ${iso}\nin ${iso}`);
	});
});

describe("resetSublabelFromEpoch", () => {
	it("returns the dash sentinel for null", () => {
		expect(resetSublabelFromEpoch(null, stubT, time)).toBe("-");
	});

	it("returns the dash sentinel for undefined", () => {
		expect(resetSublabelFromEpoch(undefined, stubT, time)).toBe("-");
	});

	it("returns the dash sentinel for NaN", () => {
		expect(resetSublabelFromEpoch(Number.NaN, stubT, time)).toBe("-");
	});

	it("returns the dash sentinel for zero", () => {
		expect(resetSublabelFromEpoch(0, stubT, time)).toBe("-");
	});

	it("returns the dash sentinel for a negative instant", () => {
		expect(resetSublabelFromEpoch(-1000, stubT, time)).toBe("-");
	});

	it("returns the dash sentinel for an instant beyond the Date range", () => {
		// Finite and positive, yet past the 8.64e15 ms limit a Date can hold.
		expect(resetSublabelFromEpoch(8.64e15 + 1, stubT, time)).toBe("-");
		expect(resetSublabelFromEpoch(1.79e18, stubT, time)).toBe("-");
	});

	it("delegates to resetSublabel with the instant as ISO", () => {
		const iso = "2026-08-01T00:00:00.000Z";
		expect(resetSublabelFromEpoch(Date.parse(iso), stubT, time)).toBe(
			`resets at ${iso}\nin ${iso}`,
		);
	});
});
