import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PageHeader } from "../PageHeader";

const StubIcon = ({
	className,
	size,
	strokeWidth,
}: {
	className?: string;
	size?: number;
	strokeWidth?: number;
}) => (
	<svg
		className={className}
		data-testid="stub-icon"
		width={size}
		height={size}
		data-strokeWidth={strokeWidth}
	/>
);

describe("PageHeader", () => {
	it("renders title", () => {
		render(<PageHeader icon={StubIcon} title="Dashboard" />);
		expect(screen.getByText("Dashboard")).toBeInTheDocument();
		expect(screen.getByTestId("stub-icon")).toBeInTheDocument();
	});

	it("renders description", () => {
		render(
			<PageHeader
				icon={StubIcon}
				title="Dashboard"
				description="Manage your settings"
			/>,
		);
		expect(screen.getByText("Manage your settings")).toBeInTheDocument();
		expect(screen.getByText("Manage your settings")).toHaveClass(
			"text-gray-400",
		);
	});

	it("renders badge", () => {
		render(
			<PageHeader
				icon={StubIcon}
				title="Dashboard"
				badge={<span data-testid="badge">Beta</span>}
			/>,
		);
		expect(screen.getByTestId("badge")).toBeInTheDocument();
		expect(screen.getByText("Beta")).toBeInTheDocument();
	});

	it("renders actions", () => {
		render(
			<PageHeader
				icon={StubIcon}
				title="Dashboard"
				actions={
					<button type="button" data-testid="action-btn">
						Action
					</button>
				}
			/>,
		);
		expect(screen.getByTestId("action-btn")).toBeInTheDocument();
		expect(screen.getByText("Action")).toBeInTheDocument();
	});

	it("renders all elements together", () => {
		render(
			<PageHeader
				icon={StubIcon}
				title="Providers"
				description="Manage your AI providers"
				badge={<span data-testid="badge">New</span>}
				actions={
					<button type="button" data-testid="action-btn">
						Add Provider
					</button>
				}
			/>,
		);

		expect(screen.getByText("Providers")).toBeInTheDocument();
		expect(screen.getByText("Manage your AI providers")).toBeInTheDocument();
		expect(screen.getByTestId("badge")).toBeInTheDocument();
		expect(screen.getByText("New")).toBeInTheDocument();
		expect(screen.getByTestId("action-btn")).toBeInTheDocument();
		expect(screen.getByText("Add Provider")).toBeInTheDocument();
		expect(screen.getByTestId("stub-icon")).toBeInTheDocument();
	});

	it("renders icon with correct props", () => {
		render(<PageHeader icon={StubIcon} title="Dashboard" />);
		const icon = screen.getByTestId("stub-icon");
		expect(icon).toHaveAttribute("width", "28");
		expect(icon).toHaveAttribute("data-strokeWidth", "2");
		expect(icon).toHaveClass("text-(--accent)");
	});
});
