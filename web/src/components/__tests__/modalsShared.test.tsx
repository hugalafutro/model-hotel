import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
	LastRefreshedRow,
	QuotaBar,
	resetAtLabel,
	usedLeftText,
} from "../modals/shared";

const t = (key: string) => key;

describe("usedLeftText", () => {
	it("reports the used share in used mode and the rest in remaining mode", () => {
		expect(usedLeftText(37.4, "used", t)).toBe(
			"37% components.providerModals.used",
		);
		expect(usedLeftText(37.4, "remaining", t)).toBe(
			"63% components.providerModals.left",
		);
	});
});

describe("resetAtLabel", () => {
	it("falls back to the translated N/A for a missing or unparsable time", () => {
		expect(resetAtLabel(undefined, t)).toBe("common.n_a");
		expect(resetAtLabel("not-a-date", t)).toBe("common.n_a");
	});

	it("names the reset moment and the countdown on its own line", () => {
		const label = resetAtLabel(
			new Date("2999-01-01T00:00:00Z").toISOString(),
			t,
		);
		expect(label.startsWith("components.providerModals.resets ")).toBe(true);
		expect(label.split("\n")).toHaveLength(2);
	});
});

describe("LastRefreshedRow", () => {
	it("renders nothing until a quota has been fetched", () => {
		const { container } = render(<LastRefreshedRow />);
		expect(container).toBeEmptyDOMElement();
	});

	it("names when the shown quota was fetched", () => {
		render(<LastRefreshedRow at={Date.now()} />);
		expect(screen.getByText("Last refreshed")).toBeInTheDocument();
	});
});

// A window consumed past its cap draws as a full bar in used mode and an
// empty one in remaining mode, never as a negative width CSS drops (which
// left the fill at its natural full width).
describe("QuotaBar", () => {
	it("bounds the fill width both ways", () => {
		const { rerender } = render(
			<QuotaBar
				label="w"
				rightText=""
				percentage={105}
				barMode="remaining"
				fillTestId="fill"
			/>,
		);
		expect(screen.getByTestId("fill").style.width).toBe("0%");
		rerender(
			<QuotaBar
				label="w"
				rightText=""
				percentage={105}
				barMode="used"
				fillTestId="fill"
			/>,
		);
		expect(screen.getByTestId("fill").style.width).toBe("100%");
		rerender(
			<QuotaBar
				label="w"
				rightText=""
				percentage={40}
				barMode="remaining"
				fillTestId="fill"
			/>,
		);
		expect(screen.getByTestId("fill").style.width).toBe("60%");
	});
});
