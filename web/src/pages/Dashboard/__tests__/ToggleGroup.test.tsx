import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { MetricToggle, RangeToggle } from "../ToggleGroup";

describe("RangeToggle", () => {
	it("renders all range options", () => {
		render(<RangeToggle value="24h" onChange={vi.fn()} />);

		expect(screen.getByText("1H")).toBeInTheDocument();
		expect(screen.getByText("1D")).toBeInTheDocument();
		expect(screen.getByText("1W")).toBeInTheDocument();
	});

	it("calls onChange with selected value when clicked", async () => {
		const onChange = vi.fn();
		const user = userEvent.setup();

		render(<RangeToggle value="24h" onChange={onChange} />);

		const oneHButton = screen.getByText("1H");
		await user.click(oneHButton);

		expect(onChange).toHaveBeenCalledWith("1h");
		expect(onChange).toHaveBeenCalledTimes(1);
	});

	it("calls onChange with 24h value when 1D clicked", async () => {
		const onChange = vi.fn();
		const user = userEvent.setup();

		render(<RangeToggle value="1h" onChange={onChange} />);

		const oneDButton = screen.getByText("1D");
		await user.click(oneDButton);

		expect(onChange).toHaveBeenCalledWith("24h");
	});

	it("calls onChange with 1w value when 1W clicked", async () => {
		const onChange = vi.fn();
		const user = userEvent.setup();

		render(<RangeToggle value="1h" onChange={onChange} />);

		const oneWButton = screen.getByText("1W");
		await user.click(oneWButton);

		expect(onChange).toHaveBeenCalledWith("1w");
	});

	it("applies active style to selected value", () => {
		render(<RangeToggle value="24h" onChange={vi.fn()} />);

		// Note: getByText finds the inner <span class="badge-text">, so we check
		// the parent element which is the button with styling
		const activeButton = screen.getByText("1D").closest("button");
		expect(activeButton).toHaveClass("ui-tab-active");
	});

	it("applies inactive style to non-selected values", () => {
		render(<RangeToggle value="24h" onChange={vi.fn()} />);

		const inactiveButton1h = screen.getByText("1H").closest("button");
		const inactiveButton1w = screen.getByText("1W").closest("button");

		expect(inactiveButton1h).not.toHaveClass("ui-tab-active");
		expect(inactiveButton1h).toHaveClass("text-(--text-muted)");

		expect(inactiveButton1w).not.toHaveClass("ui-tab-active");
		expect(inactiveButton1w).toHaveClass("text-(--text-muted)");
	});

	it("has hover style on inactive buttons", () => {
		render(<RangeToggle value="24h" onChange={vi.fn()} />);

		const inactiveButton = screen.getByText("1H").closest("button");
		expect(inactiveButton).toHaveClass("hover:text-(--text-secondary)");
	});
});

describe("MetricToggle", () => {
	it("renders all metric options", () => {
		render(<MetricToggle value="tokens" onChange={vi.fn()} />);

		expect(screen.getByText("Tok")).toBeInTheDocument();
		expect(screen.getByText("Req")).toBeInTheDocument();
	});

	it("calls onChange with 'tokens' when Tok clicked", async () => {
		const onChange = vi.fn();
		const user = userEvent.setup();

		render(<MetricToggle value="requests" onChange={onChange} />);

		const tokButton = screen.getByText("Tok");
		await user.click(tokButton);

		expect(onChange).toHaveBeenCalledWith("tokens");
		expect(onChange).toHaveBeenCalledTimes(1);
	});

	it("calls onChange with 'requests' when Req clicked", async () => {
		const onChange = vi.fn();
		const user = userEvent.setup();

		render(<MetricToggle value="tokens" onChange={onChange} />);

		const reqButton = screen.getByText("Req");
		await user.click(reqButton);

		expect(onChange).toHaveBeenCalledWith("requests");
		expect(onChange).toHaveBeenCalledTimes(1);
	});

	it("applies active style to selected value", () => {
		render(<MetricToggle value="requests" onChange={vi.fn()} />);

		const activeButton = screen.getByText("Req").closest("button");
		expect(activeButton).toHaveClass("ui-tab-active");
	});

	it("applies inactive style to non-selected values", () => {
		render(<MetricToggle value="requests" onChange={vi.fn()} />);

		const inactiveButton = screen.getByText("Tok").closest("button");
		expect(inactiveButton).not.toHaveClass("ui-tab-active");
		expect(inactiveButton).toHaveClass("text-(--text-muted)");
	});

	it("has hover style on inactive buttons", () => {
		render(<MetricToggle value="requests" onChange={vi.fn()} />);

		const inactiveButton = screen.getByText("Tok").closest("button");
		expect(inactiveButton).toHaveClass("hover:text-(--text-secondary)");
	});
});
