import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import {
	QuotaBar,
	QuotaDetailGrid,
	QuotaDetailItem,
	QuotaModalShell,
} from "../shared";

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
});

describe("QuotaDetailGrid", () => {
	it("renders label and value pairs", () => {
		render(
			<QuotaDetailGrid columns={2}>
				<QuotaDetailItem label="Plan" value="pro" testId="plan" />
			</QuotaDetailGrid>,
		);
		expect(screen.getByTestId("plan")).toHaveTextContent("pro");
	});
});
