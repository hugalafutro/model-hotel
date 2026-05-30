import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithProviders } from "../../../test/utils";
import { LogsErrorState } from "../../logs/LogsErrorState";

describe("LogsErrorState", () => {
	it("renders the error message", () => {
		renderWithProviders(<LogsErrorState message="Failed to load logs" />);
		expect(screen.getByText("Failed to load logs")).toBeInTheDocument();
	});

	it("renders with card styling", () => {
		const { container } = renderWithProviders(
			<LogsErrorState message="Error message" />,
		);
		const div = container.firstChild as HTMLElement;
		expect(div).toHaveClass("ui-card");
		expect(div).toHaveClass("p-8");
		expect(div).toHaveClass("text-center");
	});

	it("renders message with red styling", () => {
		renderWithProviders(<LogsErrorState message="Something went wrong" />);
		const paragraph = screen.getByText("Something went wrong");
		expect(paragraph).toHaveClass("text-red-400");
		expect(paragraph).toHaveClass("text-sm");
	});

	it("renders different error messages correctly", () => {
		const { rerender } = renderWithProviders(
			<LogsErrorState message="First error" />,
		);
		expect(screen.getByText("First error")).toBeInTheDocument();

		rerender(<LogsErrorState message="Second error" />);
		expect(screen.getByText("Second error")).toBeInTheDocument();
		expect(screen.queryByText("First error")).not.toBeInTheDocument();
	});

	it("handles long error message", () => {
		const longMessage =
			"Failed to fetch logs: Connection timeout after 30 seconds. Please check your network connection and try again.";
		renderWithProviders(<LogsErrorState message={longMessage} />);
		expect(screen.getByText(longMessage)).toBeInTheDocument();
	});
});
