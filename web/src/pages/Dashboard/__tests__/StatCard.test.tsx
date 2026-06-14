import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { TrendingUp } from "@/lib/icons";
import { StatCard } from "../StatCard";

// Mock AnimatedValue to render the value directly (bypass animation)
vi.mock("../AnimatedValue", () => ({
	AnimatedValue: ({
		value,
		decimals,
		suffix,
		formatter,
	}: {
		value: number;
		decimals?: number;
		suffix?: string;
		formatter?: (val: number) => string;
	}) => {
		let text: string;
		if (formatter) {
			text = formatter(value);
		} else {
			text =
				decimals !== undefined
					? value.toFixed(decimals)
					: value.toLocaleString();
		}
		return (
			<span data-testid="animated-value">
				{text}
				{suffix && <span className="text-sm font-normal ml-1">{suffix}</span>}
			</span>
		);
	},
}));

describe("StatCard", () => {
	const defaultProps = {
		label: "Total Requests",
		value: 1234,
		icon: TrendingUp,
		accent: "#3b82f6",
	};

	it("renders with label and value", () => {
		render(<StatCard {...defaultProps} />);

		expect(screen.getByText("Total Requests")).toBeInTheDocument();
		expect(screen.getByTestId("animated-value").textContent).toContain("1,234");
	});

	it("displays zero value", () => {
		render(<StatCard {...defaultProps} value={0} />);
		expect(screen.getByTestId("animated-value").textContent).toContain("0");
	});

	it("displays large numbers with locale formatting", () => {
		render(<StatCard {...defaultProps} value={999999} />);
		expect(screen.getByTestId("animated-value").textContent).toContain(
			"999,999",
		);
	});

	it("displays formatted strings with formatter function", () => {
		const currencyFormatter = (val: number) => `$${val.toFixed(2)}`;
		render(
			<StatCard
				{...defaultProps}
				value={1234.56}
				formatter={currencyFormatter}
			/>,
		);
		expect(screen.getByTestId("animated-value").textContent).toContain(
			"$1234.56",
		);
	});

	it("displays value with decimals when specified", () => {
		render(<StatCard {...defaultProps} value={1234.567} decimals={2} />);
		expect(screen.getByTestId("animated-value").textContent).toContain(
			"1234.57",
		);
	});

	it("displays suffix when provided", () => {
		render(<StatCard {...defaultProps} value={50} suffix="ms" />);
		expect(screen.getByTestId("animated-value").textContent).toContain("50");
		expect(screen.getByText("ms")).toBeInTheDocument();
	});

	it("renders tooltip if provided", () => {
		render(
			<StatCard
				{...defaultProps}
				tooltip="This shows the total number of requests"
			/>,
		);

		const card = screen.getByTitle("This shows the total number of requests");
		expect(card).toBeInTheDocument();
	});

	it("applies interactive styles when onClick is provided", () => {
		const onClick = vi.fn();
		render(<StatCard {...defaultProps} onClick={onClick} />);

		const card = screen.getByRole("button");
		expect(card).toHaveClass("cursor-pointer");
		expect(card).toHaveClass("hover:brightness-110");
		expect(card).toHaveClass("transition-all");
		expect(card).toHaveAttribute("tabIndex", "0");
	});

	it("does not apply interactive styles when onClick is not provided", () => {
		render(<StatCard {...defaultProps} />);

		const label = screen.getByText("Total Requests");
		const card = label.closest("div");
		expect(card).not.toHaveClass("cursor-pointer");
	});

	it("calls onClick when clicked", () => {
		const onClick = vi.fn();
		render(<StatCard {...defaultProps} onClick={onClick} />);

		const card = screen.getByRole("button");
		card.click();
		expect(onClick).toHaveBeenCalledTimes(1);
	});

	it("calls onClick when Enter key is pressed", () => {
		const onClick = vi.fn();
		render(<StatCard {...defaultProps} onClick={onClick} />);

		const card = screen.getByRole("button");
		card.dispatchEvent(
			new KeyboardEvent("keydown", { key: "Enter", bubbles: true }),
		);
		expect(onClick).toHaveBeenCalledTimes(1);
	});

	it("calls onClick when Space key is pressed", () => {
		const onClick = vi.fn();
		render(<StatCard {...defaultProps} onClick={onClick} />);

		const card = screen.getByRole("button");
		card.dispatchEvent(
			new KeyboardEvent("keydown", { key: " ", bubbles: true }),
		);
		expect(onClick).toHaveBeenCalledTimes(1);
	});

	it("displays loading spinner when loading is true", () => {
		render(<StatCard {...defaultProps} loading />);
		expect(screen.getByTestId("spinner")).toBeInTheDocument();
	});

	it("does not display loading spinner when loading is false", () => {
		render(<StatCard {...defaultProps} loading={false} />);
		expect(screen.queryByTestId("spinner")).not.toBeInTheDocument();
	});
});
