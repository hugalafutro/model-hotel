import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { ReasoningEffortSelect } from "../ReasoningEffortSelect";

describe("ReasoningEffortSelect", () => {
	const defaultProps = {
		value: undefined,
		onChange: vi.fn(),
	};

	it("renders three buttons: Low, Medium, High", () => {
		renderWithProviders(<ReasoningEffortSelect {...defaultProps} />);

		expect(screen.getByRole("button", { name: /Low/i })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /Medium/i })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /High/i })).toBeInTheDocument();
	});

	it("no button is selected when value is undefined", () => {
		renderWithProviders(<ReasoningEffortSelect {...defaultProps} />);

		const lowButton = screen.getByRole("button", { name: /Low/i });
		const mediumButton = screen.getByRole("button", { name: /Medium/i });
		const highButton = screen.getByRole("button", { name: /High/i });

		// Unselected buttons have the secondary style
		expect(lowButton).toHaveClass("text-(--text-secondary)");
		expect(mediumButton).toHaveClass("text-(--text-secondary)");
		expect(highButton).toHaveClass("text-(--text-secondary)");
	});

	it("the correct button is highlighted when value is set", () => {
		const { rerender } = renderWithProviders(
			<ReasoningEffortSelect {...defaultProps} value="low" />,
		);

		const lowButton = screen.getByRole("button", { name: /Low/i });
		const mediumButton = screen.getByRole("button", { name: /Medium/i });
		const highButton = screen.getByRole("button", { name: /High/i });

		// Selected button has accent style
		expect(lowButton).toHaveClass("bg-(--accent)", "text-white");
		expect(mediumButton).not.toHaveClass("bg-(--accent)");
		expect(highButton).not.toHaveClass("bg-(--accent)");

		// Rerender with medium value
		rerender(<ReasoningEffortSelect {...defaultProps} value="medium" />);

		expect(screen.getByRole("button", { name: /Low/i })).not.toHaveClass(
			"bg-(--accent)",
		);
		expect(screen.getByRole("button", { name: /Medium/i })).toHaveClass(
			"bg-(--accent)",
		);
		expect(screen.getByRole("button", { name: /High/i })).not.toHaveClass(
			"bg-(--accent)",
		);

		// Rerender with high value
		rerender(<ReasoningEffortSelect {...defaultProps} value="high" />);

		expect(screen.getByRole("button", { name: /Low/i })).not.toHaveClass(
			"bg-(--accent)",
		);
		expect(screen.getByRole("button", { name: /Medium/i })).not.toHaveClass(
			"bg-(--accent)",
		);
		expect(screen.getByRole("button", { name: /High/i })).toHaveClass(
			"bg-(--accent)",
		);
	});

	it("clicking a button calls onChange with that value", async () => {
		const onChange = vi.fn();
		renderWithProviders(
			<ReasoningEffortSelect {...defaultProps} onChange={onChange} />,
		);

		const user = userEvent.setup();

		await user.click(screen.getByRole("button", { name: /Low/i }));
		expect(onChange).toHaveBeenCalledWith("low");

		onChange.mockClear();
		await user.click(screen.getByRole("button", { name: /Medium/i }));
		expect(onChange).toHaveBeenCalledWith("medium");

		onChange.mockClear();
		await user.click(screen.getByRole("button", { name: /High/i }));
		expect(onChange).toHaveBeenCalledWith("high");
	});

	it("clicking the same button again calls onChange with undefined (deselect)", async () => {
		const onChange = vi.fn();
		const { rerender } = renderWithProviders(
			<ReasoningEffortSelect
				{...defaultProps}
				value="low"
				onChange={onChange}
			/>,
		);

		const user = userEvent.setup();

		// Click the already-selected Low button
		await user.click(screen.getByRole("button", { name: /Low/i }));
		expect(onChange).toHaveBeenCalledWith(undefined);

		// Simulate parent state update - rerender with undefined
		onChange.mockClear();
		rerender(
			<ReasoningEffortSelect
				{...defaultProps}
				value={undefined}
				onChange={onChange}
			/>,
		);

		// Click Medium (not selected), should set to medium
		await user.click(screen.getByRole("button", { name: /Medium/i }));
		expect(onChange).toHaveBeenCalledWith("medium");

		// Simulate parent state update - rerender with medium
		onChange.mockClear();
		rerender(
			<ReasoningEffortSelect
				{...defaultProps}
				value="medium"
				onChange={onChange}
			/>,
		);

		// Click Medium again (now selected), should deselect
		await user.click(screen.getByRole("button", { name: /Medium/i }));
		expect(onChange).toHaveBeenCalledWith(undefined);
	});

	it("the 'off' button appears when a value is set", () => {
		const { rerender } = renderWithProviders(
			<ReasoningEffortSelect {...defaultProps} value="low" />,
		);

		expect(screen.getByRole("button", { name: /off/i })).toBeInTheDocument();

		// Rerender with undefined value - off button should disappear
		rerender(<ReasoningEffortSelect {...defaultProps} value={undefined} />);

		expect(
			screen.queryByRole("button", { name: /off/i }),
		).not.toBeInTheDocument();
	});

	it("clicking the 'off' button calls onChange with undefined", async () => {
		const onChange = vi.fn();
		renderWithProviders(
			<ReasoningEffortSelect
				{...defaultProps}
				value="high"
				onChange={onChange}
			/>,
		);

		const user = userEvent.setup();

		await user.click(screen.getByRole("button", { name: /off/i }));
		expect(onChange).toHaveBeenCalledWith(undefined);
	});

	it("the 'off' button does NOT appear when value is undefined", () => {
		renderWithProviders(<ReasoningEffortSelect {...defaultProps} />);

		expect(
			screen.queryByRole("button", { name: /off/i }),
		).not.toBeInTheDocument();
	});
});
