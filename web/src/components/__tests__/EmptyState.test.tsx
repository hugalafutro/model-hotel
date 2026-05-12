import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { EmptyState } from "../EmptyState";

const StubIcon = ({ className }: { className?: string }) => (
	<svg className={className} data-testid="stub-icon" />
);

describe("EmptyState", () => {
	it("renders message", () => {
		render(<EmptyState message="No items found" />);
		expect(screen.getByText("No items found")).toBeInTheDocument();
	});

	it("renders with icon", () => {
		render(<EmptyState message="No items found" icon={StubIcon} />);
		expect(screen.getByTestId("stub-icon")).toBeInTheDocument();
		expect(screen.getByText("No items found")).toBeInTheDocument();
	});

	it("renders with action button", async () => {
		const onClick = vi.fn();
		render(
			<EmptyState
				message="No items found"
				action={{ label: "Add Item", onClick }}
			/>,
		);

		const button = screen.getByRole("button", { name: "Add Item" });
		expect(button).toBeInTheDocument();
		expect(button).toHaveClass("ui-btn-primary");

		const user = userEvent.setup();
		await user.click(button);

		expect(onClick).toHaveBeenCalledTimes(1);
	});

	it("renders with icon and action button", async () => {
		const onClick = vi.fn();
		render(
			<EmptyState
				message="No items found"
				icon={StubIcon}
				action={{ label: "Add Item", onClick }}
			/>,
		);

		expect(screen.getByTestId("stub-icon")).toBeInTheDocument();
		expect(screen.getByText("No items found")).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: "Add Item" }),
		).toBeInTheDocument();
	});

	it("applies custom className", () => {
		render(
			<EmptyState message="No items found" className="custom-container" />,
		);
		const container = screen.getByText("No items found").parentElement;
		expect(container).toHaveClass("custom-container");
	});

	it("uses default ui-card class when no className provided", () => {
		render(<EmptyState message="No items found" />);
		const container = screen.getByText("No items found").parentElement;
		expect(container).toHaveClass("ui-card");
	});
});
