import { render, screen } from "@testing-library/react";
import {
	QuotaBar,
	QuotaDetailGrid,
	QuotaDetailItem,
} from "@web-shared/quota/modals";

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
			"ui-quota-fill-danger",
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
			"ui-quota-fill-warn",
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

	it("draws a remaining bar and its tone from a remaining share given apart from the used one", () => {
		// 30 used but only 10 remaining (the provider's figures do not add up):
		// the bar shows 10% left and grades it as 10% left, i.e. danger.
		render(
			<QuotaBar
				label="kWh"
				rightText="x"
				percentage={30}
				remainingPercentage={10}
				barMode="remaining"
				fillTestId="fill"
			/>,
		);
		expect(screen.getByTestId("fill")).toHaveStyle({ width: "10%" });
		expect(screen.getByTestId("fill")).toHaveClass("ui-quota-fill-danger");
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
			"ui-quota-detail-grid-2",
		);
		// No span prop was passed, so the span class must be absent.
		expect(screen.getByTestId("plan").className).not.toContain(
			"ui-quota-detail-span",
		);
	});

	it("applies the 3-column grid class when given columns={3}", () => {
		render(
			<QuotaDetailGrid columns={3}>
				<QuotaDetailItem label="Plan" value="pro" testId="plan" />
			</QuotaDetailGrid>,
		);
		expect(screen.getByTestId("plan").parentElement?.className).toContain(
			"ui-quota-detail-grid-3",
		);
	});

	it("spans a detail item across the full grid width when span is set", () => {
		render(
			<QuotaDetailGrid columns={2}>
				<QuotaDetailItem label="Notes" value="long text" span testId="notes" />
			</QuotaDetailGrid>,
		);
		expect(screen.getByTestId("notes").className).toContain(
			"ui-quota-detail-span",
		);
	});
});
