import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import {
	QuotaBar,
	QuotaDetailGrid,
	QuotaDetailItem,
	QuotaModalShell,
	quotaRightText,
	resetSublabel,
	resetSublabelFromEpoch,
} from "../shared";

// Returns the last dotted segment of the key, ignoring interpolation options,
// same convention as the quotaRightText tests below: locale-independent,
// asserts on the translation KEY rather than a translated value.
const stubT = (key: string) => key.split(".").pop() as string;

describe("QuotaBar", () => {
	it("fills to the used share in used mode", () => {
		render(
			<QuotaBar
				label="Weekly"
				rightText="75%"
				percentage={75}
				barMode="used"
				testId="bar"
				fillTestId="fill"
			/>,
		);
		expect(screen.getByTestId("bar")).toBeInTheDocument();
		expect(screen.getByTestId("fill")).toHaveStyle({ width: "75%" });
	});

	it("fills to the remaining share in remaining mode", () => {
		render(
			<QuotaBar
				label="Weekly"
				rightText="25%"
				percentage={75}
				barMode="remaining"
				testId="bar"
				fillTestId="fill"
			/>,
		);
		expect(screen.getByTestId("fill")).toHaveStyle({ width: "25%" });
	});

	it("clamps an over-100 percentage", () => {
		render(
			<QuotaBar
				label="Weekly"
				rightText="x"
				percentage={140}
				barMode="used"
				fillTestId="fill"
			/>,
		);
		expect(screen.getByTestId("fill")).toHaveStyle({ width: "100%" });
	});

	it("clamps the lower bound when a remaining-mode share goes negative", () => {
		// percentage is percent USED; 140 used in remaining mode computes a raw
		// shown value of 100 - 140 = -40, which must clamp to 0, not render
		// negative width.
		render(
			<QuotaBar
				label="Weekly"
				rightText="x"
				percentage={140}
				barMode="remaining"
				fillTestId="fill"
			/>,
		);
		expect(screen.getByTestId("fill")).toHaveStyle({ width: "0%" });
	});

	it("tones the fill by severity", () => {
		render(
			<QuotaBar
				label="Weekly"
				rightText="x"
				percentage={95}
				barMode="used"
				fillTestId="fill"
			/>,
		);
		expect(screen.getByTestId("fill").className).toContain(
			"fd-quota-fill-danger",
		);
	});

	it("tones a remaining-mode bar off the used percentage, not the inverted width", () => {
		// percentage is always percent USED. 75 used in remaining mode means 25
		// remaining, which is barTone's "warn" band (< 60 remaining). This fails
		// under two plausible bugs: passing the already-inverted/clamped width
		// into barTone instead of percentage, or hardcoding barMode to "used".
		render(
			<QuotaBar
				label="Weekly"
				rightText="x"
				percentage={75}
				barMode="remaining"
				fillTestId="fill"
			/>,
		);
		expect(screen.getByTestId("fill").className).toContain(
			"fd-quota-fill-warn",
		);
	});

	it("renders a sublabel and a footer when given", () => {
		render(
			<QuotaBar
				label="Weekly"
				rightText="x"
				percentage={10}
				barMode="used"
				footer={<span data-testid="foot">F</span>}
			>
				<span data-testid="sub">S</span>
			</QuotaBar>,
		);
		expect(screen.getByTestId("sub")).toBeInTheDocument();
		expect(screen.getByTestId("foot")).toBeInTheDocument();
	});
});

describe("QuotaModalShell", () => {
	function renderShell(
		over: Partial<Parameters<typeof QuotaModalShell>[0]> = {},
	) {
		const props = {
			title: "NanoGPT",
			barMode: "remaining" as const,
			onToggleBarMode: vi.fn(),
			onRefresh: vi.fn(),
			isRefreshing: false,
			fetchedAt: "2026-07-26T10:00:00Z",
			onClose: vi.fn(),
			children: <p data-testid="body">body</p>,
			...over,
		};
		render(<QuotaModalShell {...props} />);
		return props;
	}

	it("renders the title, body and snapshot stamp", () => {
		renderShell();
		expect(screen.getByRole("dialog")).toHaveAttribute("aria-label", "NanoGPT");
		expect(screen.getByTestId("body")).toBeInTheDocument();
		expect(screen.getByTestId("quota-modal-fetched-at")).toBeInTheDocument();
		// The default fixture renders with isRefreshing: false, so the refresh
		// icon must not carry the spin class here. getAttribute returns null
		// when isRefreshing is false (className is undefined, not "undefined"),
		// so fall back to "" before asserting.
		const icon = screen.getByTestId("quota-modal-refresh").querySelector("svg");
		expect(icon?.getAttribute("class") ?? "").not.toContain("fd-spin");
	});

	it("toggles bar mode when the toggle is pressed", async () => {
		const props = renderShell();
		await userEvent.click(screen.getByTestId("quota-modal-toggle"));
		expect(props.onToggleBarMode).toHaveBeenCalledTimes(1);
	});

	it("refreshes when the refresh button is pressed", async () => {
		const props = renderShell();
		await userEvent.click(screen.getByTestId("quota-modal-refresh"));
		expect(props.onRefresh).toHaveBeenCalledTimes(1);
	});

	it("disables the refresh button while a refresh is in flight", () => {
		renderShell({ isRefreshing: true });
		expect(screen.getByTestId("quota-modal-refresh")).toBeDisabled();
	});

	it("renders a subtitle when given", () => {
		renderShell({ subtitle: <span data-testid="sub">pro</span> });
		expect(screen.getByTestId("sub")).toBeInTheDocument();
	});

	it("spins the refresh icon while a refresh is in flight", () => {
		renderShell({ isRefreshing: true });
		const icon = screen.getByTestId("quota-modal-refresh").querySelector("svg");
		expect(icon?.getAttribute("class")).toContain("fd-spin");
	});
});

describe("QuotaDetailGrid", () => {
	it("renders label and value pairs", () => {
		render(
			<QuotaDetailGrid columns={2}>
				<QuotaDetailItem label="Plan" value="pro" testId="plan" />
			</QuotaDetailGrid>,
		);
		expect(screen.getByTestId("plan")).toHaveTextContent("pro");
		expect(screen.getByTestId("plan").parentElement?.className).toContain(
			"fd-quota-detail-grid-2",
		);
		// No span prop was passed, so the span class must be absent.
		expect(screen.getByTestId("plan").className).not.toContain(
			"fd-quota-detail-span",
		);
	});

	it("applies the 3-column grid class when given columns={3}", () => {
		render(
			<QuotaDetailGrid columns={3}>
				<QuotaDetailItem label="Plan" value="pro" testId="plan" />
			</QuotaDetailGrid>,
		);
		expect(screen.getByTestId("plan").parentElement?.className).toContain(
			"fd-quota-detail-grid-3",
		);
	});

	it("spans a detail item across the full grid width when span is set", () => {
		render(
			<QuotaDetailGrid columns={2}>
				<QuotaDetailItem label="Notes" value="long text" span testId="notes" />
			</QuotaDetailGrid>,
		);
		expect(screen.getByTestId("notes").className).toContain(
			"fd-quota-detail-span",
		);
	});
});

describe("quotaRightText", () => {
	it("reports the used share in used mode", () => {
		expect(
			quotaRightText(40, "used", (k) => k.split(".").pop() as string),
		).toBe("40% used");
	});

	it("reports the remaining share in remaining mode", () => {
		expect(
			quotaRightText(40, "remaining", (k) => k.split(".").pop() as string),
		).toBe("60% left");
	});
});

describe("resetSublabel", () => {
	it("returns the dash sentinel for undefined", () => {
		expect(resetSublabel(undefined, stubT)).toBe("-");
	});

	it("returns the dash sentinel for null", () => {
		expect(resetSublabel(null, stubT)).toBe("-");
	});

	it("returns the dash sentinel for an empty string", () => {
		expect(resetSublabel("", stubT)).toBe("-");
	});

	it("returns the dash sentinel for an unparseable ISO string", () => {
		expect(resetSublabel("not-a-date", stubT)).toBe("-");
	});

	it("composes the resets key and a non-empty relative line for a valid ISO string", () => {
		const iso = new Date(Date.now() + 3_600_000).toISOString();
		const out = resetSublabel(iso, stubT);
		const lines = out.split("\n");
		expect(lines).toHaveLength(2);
		expect(lines[0]).toBe("resets");
		expect(lines[1].length).toBeGreaterThan(0);
	});
});

describe("resetSublabelFromEpoch", () => {
	it("returns the dash sentinel for null", () => {
		expect(resetSublabelFromEpoch(null, stubT)).toBe("-");
	});

	it("returns the dash sentinel for undefined", () => {
		expect(resetSublabelFromEpoch(undefined, stubT)).toBe("-");
	});

	it("returns the dash sentinel for NaN", () => {
		expect(resetSublabelFromEpoch(Number.NaN, stubT)).toBe("-");
	});

	it("returns the dash sentinel for zero", () => {
		expect(resetSublabelFromEpoch(0, stubT)).toBe("-");
	});

	it("returns the dash sentinel for a negative instant", () => {
		expect(resetSublabelFromEpoch(-1000, stubT)).toBe("-");
	});

	it("delegates to resetSublabel for a valid future instant", () => {
		const out = resetSublabelFromEpoch(Date.now() + 3_600_000, stubT);
		const lines = out.split("\n");
		expect(lines).toHaveLength(2);
		expect(lines[0]).toBe("resets");
		expect(lines[1].length).toBeGreaterThan(0);
	});
});
