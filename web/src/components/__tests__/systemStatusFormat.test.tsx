import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
	formatBytesPerSec,
	formatDuration,
	formatMB,
	formatNumber,
	unitClass,
} from "../systemStatusFormat";

/** Rendered text of a figure, units included, as the sidebar shows it. */
const text = (node: React.ReactNode) =>
	render(<>{node}</>).container.textContent;

describe("formatDuration", () => {
	it("shows days and hours past a day", () => {
		expect(text(formatDuration(3 * 86400 + 4 * 3600 + 30 * 60))).toBe("3d 4h");
	});

	it("shows hours and minutes past an hour", () => {
		expect(text(formatDuration(4 * 3600 + 5 * 60 + 30))).toBe("4h 5m");
	});

	it("shows minutes alone under an hour", () => {
		expect(text(formatDuration(5 * 60 + 30))).toBe("5m");
	});
});

describe("formatNumber", () => {
	it("abbreviates millions to one decimal", () => {
		expect(text(formatNumber(2_500_000))).toBe("2.5M");
	});

	it("abbreviates thousands to one decimal", () => {
		expect(text(formatNumber(1_200))).toBe("1.2K");
	});

	it("leaves smaller counts as a plain number", () => {
		expect(text(formatNumber(999))).toBe("999");
	});
});

describe("formatMB", () => {
	it("keeps one decimal below a megabyte", () => {
		expect(text(formatMB(0.5))).toBe("0.5 MB");
	});

	it("rounds megabytes", () => {
		expect(text(formatMB(512.4))).toBe("512 MB");
	});

	it("switches to gigabytes at 1024 MB", () => {
		expect(text(formatMB(2048))).toBe("2.0 GB");
	});
});

describe("formatBytesPerSec", () => {
	it("shows a flat zero when there is no traffic", () => {
		expect(text(formatBytesPerSec(0))).toBe("0 B/s");
		expect(text(formatBytesPerSec(-1))).toBe("0 B/s");
	});

	it("rounds bytes per second", () => {
		expect(text(formatBytesPerSec(512.6))).toBe("513 B/s");
	});

	it("switches to kilobytes at 1024 B/s", () => {
		expect(text(formatBytesPerSec(2048))).toBe("2.0 KB/s");
	});

	it("switches to megabytes at 1024 KB/s", () => {
		expect(text(formatBytesPerSec(3 * 1024 * 1024))).toBe("3.0 MB/s");
	});
});

describe("unit styling", () => {
	it("dims the unit that trails the figure", () => {
		const { container } = render(<>{formatMB(512)}</>);
		const unit = container.querySelector("span");
		expect(unit).toHaveClass(unitClass);
		expect(unit).toHaveTextContent("MB");
	});
});
