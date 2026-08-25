import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { CopyRow } from "../CopyRow";

// A resolving clipboard by default; the failure test installs its own.
function stubClipboard(writeText: (text: string) => Promise<void>) {
	Object.defineProperty(navigator, "clipboard", {
		value: { writeText },
		configurable: true,
	});
	return writeText;
}

describe("CopyRow", () => {
	beforeEach(() => {
		stubClipboard(vi.fn().mockResolvedValue(undefined));
	});

	it("renders the labelled shape with a named copy button", () => {
		render(
			<CopyRow value="ntfy.example.com" label="Server" testId="row-copy" />,
		);

		expect(screen.getByText("Server")).toBeInTheDocument();
		expect(screen.getByText("ntfy.example.com")).toBeInTheDocument();
		expect(screen.getByTestId("row-copy")).toHaveAccessibleName("Copy: Server");
	});

	it("renders the unlabelled shape without a test id or extra name", () => {
		render(<CopyRow value="http://mh.example.com:8080/v1" />);

		const button = screen.getByRole("button");
		expect(button).toHaveAccessibleName("Copy");
		expect(button).not.toHaveAttribute("data-testid");
		expect(screen.getByText("http://mh.example.com:8080/v1")).toHaveClass(
			"ui-input",
		);
	});

	it("renders nothing for an empty value", () => {
		const { container } = render(<CopyRow value="" label="Topic" />);
		expect(container).toBeEmptyDOMElement();
	});

	it("writes the value and confirms on the button", async () => {
		const writeText = stubClipboard(vi.fn().mockResolvedValue(undefined));
		render(<CopyRow value="secret-topic" label="Topic" testId="row-copy" />);

		await userEvent.click(screen.getByTestId("row-copy"));

		expect(writeText).toHaveBeenCalledWith("secret-topic");
		await waitFor(() =>
			expect(screen.getByTestId("row-copy")).toHaveTextContent("Copied"),
		);
	});

	it("stays silent when the clipboard refuses", async () => {
		stubClipboard(vi.fn().mockRejectedValue(new Error("denied")));
		render(<CopyRow value="secret-topic" label="Topic" testId="row-copy" />);

		await userEvent.click(screen.getByTestId("row-copy"));

		// The value is still on screen to select by hand and the button never
		// claims a copy that did not happen.
		expect(screen.getByText("secret-topic")).toBeInTheDocument();
		expect(screen.getByTestId("row-copy")).toHaveTextContent("Copy");
	});
});
