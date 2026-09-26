import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import i18n from "../../i18n";
import { renderWithProviders } from "../../test/utils";
import { ReasoningEffortSelect } from "../ReasoningEffortSelect";

describe("ReasoningEffortSelect", () => {
	const defaultProps = {
		value: undefined,
		onChange: vi.fn(),
	};

	it("renders five buttons: Default, None, Low, Medium, High", () => {
		renderWithProviders(<ReasoningEffortSelect {...defaultProps} />);

		expect(
			screen.getByRole("button", { name: /Default/i }),
		).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /None/i })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /Low/i })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /Medium/i })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /High/i })).toBeInTheDocument();
	});

	it("Default is the selected button when value is undefined", () => {
		renderWithProviders(<ReasoningEffortSelect {...defaultProps} />);

		// Undefined is a real, nameable choice now rather than the absence of
		// one, so it has to read as selected instead of leaving the row blank.
		expect(screen.getByRole("button", { name: /Default/i })).toHaveClass(
			"bg-(--accent)",
			"text-white",
		);
		for (const name of [/None/i, /Low/i, /Medium/i, /High/i]) {
			expect(screen.getByRole("button", { name })).toHaveClass(
				"text-(--text-secondary)",
			);
		}
	});

	it("the correct button is highlighted when value is set", () => {
		const { rerender } = renderWithProviders(
			<ReasoningEffortSelect {...defaultProps} value="low" />,
		);

		expect(screen.getByRole("button", { name: /Low/i })).toHaveClass(
			"bg-(--accent)",
			"text-white",
		);
		expect(screen.getByRole("button", { name: /Default/i })).not.toHaveClass(
			"bg-(--accent)",
		);

		rerender(<ReasoningEffortSelect {...defaultProps} value="medium" />);
		expect(screen.getByRole("button", { name: /Medium/i })).toHaveClass(
			"bg-(--accent)",
		);
		expect(screen.getByRole("button", { name: /Low/i })).not.toHaveClass(
			"bg-(--accent)",
		);

		rerender(<ReasoningEffortSelect {...defaultProps} value="high" />);
		expect(screen.getByRole("button", { name: /High/i })).toHaveClass(
			"bg-(--accent)",
		);
		expect(screen.getByRole("button", { name: /Medium/i })).not.toHaveClass(
			"bg-(--accent)",
		);
	});

	// The reason this component changed: "none" is a value that has to reach the
	// provider, because the egress translators read it as an explicit off switch.
	// Sending undefined instead omits reasoning_effort and lets a thinking model
	// keep thinking, which is what the old single "off" control did.
	it('None sends the string "none", not undefined', async () => {
		const onChange = vi.fn();
		renderWithProviders(
			<ReasoningEffortSelect {...defaultProps} onChange={onChange} />,
		);

		await userEvent
			.setup()
			.click(screen.getByRole("button", { name: /None/i }));

		expect(onChange).toHaveBeenCalledWith("none");
		expect(onChange).not.toHaveBeenCalledWith(undefined);
	});

	it("Default sends undefined so the field is omitted", async () => {
		const onChange = vi.fn();
		renderWithProviders(
			<ReasoningEffortSelect
				{...defaultProps}
				value="high"
				onChange={onChange}
			/>,
		);

		await userEvent
			.setup()
			.click(screen.getByRole("button", { name: /Default/i }));

		expect(onChange).toHaveBeenCalledWith(undefined);
	});

	it('None is highlighted when value is "none"', () => {
		renderWithProviders(
			<ReasoningEffortSelect {...defaultProps} value="none" />,
		);

		expect(screen.getByRole("button", { name: /None/i })).toHaveClass(
			"bg-(--accent)",
		);
		expect(screen.getByRole("button", { name: /Default/i })).not.toHaveClass(
			"bg-(--accent)",
		);
	});

	it("clicking a button calls onChange with that value", async () => {
		const onChange = vi.fn();
		renderWithProviders(
			<ReasoningEffortSelect {...defaultProps} onChange={onChange} />,
		);

		const user = userEvent.setup();

		for (const [name, expected] of [
			[/Low/i, "low"],
			[/Medium/i, "medium"],
			[/High/i, "high"],
		] as const) {
			onChange.mockClear();
			await user.click(screen.getByRole("button", { name }));
			expect(onChange).toHaveBeenCalledWith(expected);
		}
	});

	it("marks the selected button pressed for assistive tech", () => {
		// Selection is otherwise carried only by background colour, which a
		// screen reader cannot see.
		renderWithProviders(
			<ReasoningEffortSelect {...defaultProps} value="none" />,
		);

		expect(screen.getByRole("button", { name: /None/i })).toHaveAttribute(
			"aria-pressed",
			"true",
		);
		expect(screen.getByRole("button", { name: /Default/i })).toHaveAttribute(
			"aria-pressed",
			"false",
		);
	});

	// Regression pin: the hints are visible text tied to their buttons, not
	// tooltips a keyboard user never sees.
	it("Default and None are described by visible hints, the levels are not", () => {
		renderWithProviders(<ReasoningEffortSelect {...defaultProps} />);

		const key = "components.reasoningEffortSelect";
		expect(
			screen.getByRole("button", { name: i18n.t(`${key}.default`) }),
		).toHaveAccessibleDescription(
			i18n.t(`${key}.hintLine`, {
				label: i18n.t(`${key}.default`),
				hint: i18n.t(`${key}.defaultHint`),
			}),
		);
		expect(
			screen.getByRole("button", { name: i18n.t(`${key}.none`) }),
		).toHaveAccessibleDescription(
			i18n.t(`${key}.hintLine`, {
				label: i18n.t(`${key}.none`),
				hint: i18n.t(`${key}.noneHint`),
			}),
		);
		expect(
			screen.getByText(i18n.t(`${key}.noneHint`), { exact: false }),
		).toBeVisible();
		expect(
			screen.getByRole("button", { name: i18n.t(`${key}.low`) }),
		).not.toHaveAttribute("aria-describedby");
	});

	it("labels the buttons as one group named Reasoning Effort", () => {
		renderWithProviders(<ReasoningEffortSelect {...defaultProps} />);

		const group = screen.getByRole("group", {
			name: i18n.t("components.reasoningEffortSelect.reasoningEffort"),
		});
		expect(group.querySelectorAll("button")).toHaveLength(5);
	});

	it("clicking the selected button keeps that value instead of clearing it", async () => {
		const onChange = vi.fn();
		renderWithProviders(
			<ReasoningEffortSelect
				{...defaultProps}
				value="low"
				onChange={onChange}
			/>,
		);

		// Deselect-on-reclick would silently mean Default, which is now its own
		// button; re-picking the active level must not change what is sent.
		await userEvent.setup().click(screen.getByRole("button", { name: /Low/i }));

		expect(onChange).toHaveBeenCalledWith("low");
		expect(onChange).not.toHaveBeenCalledWith(undefined);
	});
});
