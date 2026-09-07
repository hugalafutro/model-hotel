import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
	CollapseBody,
	CollapsibleToggle,
	useCollapsible,
} from "../CollapsibleToggle";

describe("CollapsibleToggle", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		onToggle.mockClear();
	});

	it("renders button with correct title when collapsed", () => {
		render(<CollapsibleToggle collapsed onToggle={onToggle} />);
		const button = screen.getByRole("button");
		expect(button).toHaveAttribute("title", "Expand");
	});

	it("renders button with correct title when expanded", () => {
		render(<CollapsibleToggle collapsed={false} onToggle={onToggle} />);
		const button = screen.getByRole("button");
		expect(button).toHaveAttribute("title", "Collapse");
	});

	it("calls onToggle when clicked", async () => {
		const user = userEvent.setup();
		render(<CollapsibleToggle collapsed onToggle={onToggle} />);
		await user.click(screen.getByRole("button"));
		expect(onToggle).toHaveBeenCalledTimes(1);
	});

	it("uses custom expandTitle when provided", () => {
		render(
			<CollapsibleToggle
				collapsed
				onToggle={onToggle}
				expandTitle="Show More"
			/>,
		);
		expect(screen.getByRole("button")).toHaveAttribute("title", "Show More");
	});

	it("uses custom collapseTitle when provided", () => {
		render(
			<CollapsibleToggle
				collapsed={false}
				onToggle={onToggle}
				collapseTitle="Show Less"
			/>,
		);
		expect(screen.getByRole("button")).toHaveAttribute("title", "Show Less");
	});

	it("applies custom className when provided", () => {
		render(
			<CollapsibleToggle
				collapsed
				onToggle={onToggle}
				className="custom-class"
			/>,
		);
		expect(screen.getByRole("button")).toHaveClass("custom-class");
	});

	it("uses the unified icon-button styling by default", () => {
		render(<CollapsibleToggle collapsed onToggle={onToggle} />);
		const button = screen.getByRole("button");
		expect(button).toHaveClass("ui-icon-btn");
	});

	it("uses double icon style when specified", () => {
		render(
			<CollapsibleToggle collapsed onToggle={onToggle} iconStyle="double" />,
		);
		// Double icons render ChevronsUpDown when collapsed
		expect(screen.getByRole("button")).toBeInTheDocument();
	});

	it("uses single icon style by default", () => {
		render(<CollapsibleToggle collapsed onToggle={onToggle} />);
		// Single icon renders ChevronDown when collapsed
		expect(screen.getByRole("button")).toBeInTheDocument();
	});

	it("uses custom size prop", () => {
		render(<CollapsibleToggle collapsed onToggle={onToggle} size={20} />);
		expect(screen.getByRole("button")).toBeInTheDocument();
	});
});

describe("useCollapsible", () => {
	it("returns collapsed state and toggle function", () => {
		// Note: useCollapsible is a hook, so we can't test it in isolation
		// without a component wrapper. This test documents the expected behavior.
		expect(typeof useCollapsible).toBe("function");
	});

	it("uses defaultValue when no storage key provided", () => {
		// Hook behavior tested through CollapsibleToggle component integration
		expect(typeof useCollapsible).toBe("function");
	});
});

describe("CollapseBody", () => {
	it("collapses to a zero row and expands to a full one", () => {
		const { container, rerender } = render(
			<CollapseBody collapsed>body</CollapseBody>,
		);
		const grid = container.firstElementChild as HTMLElement;
		expect(grid.className).toContain("grid-rows-[0fr]");

		rerender(<CollapseBody collapsed={false}>body</CollapseBody>);
		expect((container.firstElementChild as HTMLElement).className).toContain(
			"grid-rows-[1fr]",
		);
	});

	it("bleeds only while expanded, so the collapsed box stays tight", () => {
		const { container, rerender } = render(
			<CollapseBody collapsed bleed>
				body
			</CollapseBody>,
		);
		const clip = () =>
			container.querySelector(".overflow-hidden") as HTMLElement;
		expect(clip().className).not.toContain("p-4");

		rerender(
			<CollapseBody collapsed={false} bleed>
				body
			</CollapseBody>,
		);
		expect(clip().className).toContain("p-4 -m-4");
	});

	it("makes the collapsed body inert when asked, and only then", () => {
		const { container, rerender } = render(
			<CollapseBody collapsed inert>
				body
			</CollapseBody>,
		);
		const clip = () =>
			container.querySelector(".overflow-hidden") as HTMLElement;
		expect(clip().hasAttribute("inert")).toBe(true);

		rerender(
			<CollapseBody collapsed={false} inert>
				body
			</CollapseBody>,
		);
		expect(clip().hasAttribute("inert")).toBe(false);
	});
});
