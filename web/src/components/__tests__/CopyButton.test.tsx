import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import i18n from "../../i18n";
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

	it("shows failure toast when the clipboard refuses", async () => {
		const user = userEvent.setup();
		renderWithProviders(<CopyButton text="test content" />);
		// Spied after render: userEvent.setup() installs its own clipboard stub,
		// so the spy has to go on whatever object is in place by then.
		vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(
			new Error("denied"),
		);

		await user.click(screen.getByRole("button"));

		await waitFor(() => {
			expect(screen.getByTestId("toast-message")).toHaveTextContent(
				i18n.t("common.failedToCopy"),
			);
		});
	});

	it("swaps its label instead of toasting in the label variant", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<CopyButton
				variant="label"
				text="test content"
				testId="label-copy"
				ariaLabel="Copy: row"
			/>,
		);
		const button = screen.getByTestId("label-copy");
		expect(button).toHaveClass("ui-btn-secondary");
		expect(button).toHaveAttribute("aria-label", "Copy: row");
		expect(button).toHaveTextContent(i18n.t("common.copy"));

		await user.click(button);

		await waitFor(() => {
			expect(button).toHaveTextContent(i18n.t("common.copied"));
		});
		expect(screen.queryByTestId("toast-message")).not.toBeInTheDocument();
	});
});
