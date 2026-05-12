import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { CopyButton } from "../CopyButton";

describe("CopyButton", () => {
	it("renders button with default icon", () => {
		renderWithProviders(<CopyButton text="test content" />);
		const button = screen.getByRole("button");
		expect(button).toBeInTheDocument();
		expect(button).toHaveAttribute("title", "Copy");
	});

	it("renders with custom title", () => {
		renderWithProviders(<CopyButton text="test content" title="Copy URL" />);
		const button = screen.getByRole("button");
		expect(button).toHaveAttribute("title", "Copy URL");
	});

	it("renders with custom size", () => {
		renderWithProviders(<CopyButton text="test content" size={16} />);
		const button = screen.getByRole("button");
		expect(button).toBeInTheDocument();
	});

	it("renders with custom className", () => {
		renderWithProviders(
			<CopyButton text="test content" className="custom-class" />,
		);
		const button = screen.getByRole("button");
		expect(button).toHaveClass("custom-class");
	});

	it("shows success toast on copy", async () => {
		const user = userEvent.setup();
		renderWithProviders(<CopyButton text="test content" />);

		await user.click(screen.getByRole("button"));

		await waitFor(() => {
			expect(screen.getByText("Copied to clipboard")).toBeInTheDocument();
		});
	});
});
