import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { DetailSectionHeader } from "../DetailSectionHeader";

const StubIcon = ({
	size,
	className,
}: {
	size?: number;
	className?: string;
}) => <svg data-testid="section-icon" data-size={size} className={className} />;

describe("DetailSectionHeader", () => {
	it("renders the label text", () => {
		renderWithProviders(
			<DetailSectionHeader icon={StubIcon}>Performance</DetailSectionHeader>,
		);
		expect(screen.getByText("Performance")).toBeInTheDocument();
	});

	it("renders the accent-colored icon at the small group-header size", () => {
		renderWithProviders(
			<DetailSectionHeader icon={StubIcon}>Tokens</DetailSectionHeader>,
		);
		const icon = screen.getByTestId("section-icon");
		expect(icon).toBeInTheDocument();
		expect(icon).toHaveClass("text-(--accent)");
		expect(icon).toHaveAttribute("data-size", "13");
	});
});
