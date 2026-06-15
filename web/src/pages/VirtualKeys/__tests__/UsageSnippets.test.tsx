import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithProviders } from "../../../test/utils";
import { UsageSnippets } from "../UsageSnippets";

describe("UsageSnippets", () => {
	it("renders the snippet windows with the YOUR_API_KEY placeholder by default", () => {
		renderWithProviders(<UsageSnippets />);

		// One TerminalPreview per language, each with its own copy button.
		expect(
			screen.getByRole("button", { name: /Copy cURL snippet/i }),
		).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: /Copy Python snippet/i }),
		).toBeInTheDocument();

		// The unresolved key sentinel is shown for the user to replace.
		expect(
			screen.getAllByText((content) => content.includes("YOUR_API_KEY")).length,
		).toBeGreaterThan(0);
	});

	it("substitutes a supplied key into every snippet and drops the placeholder", () => {
		const apiKey = "sk_real_key_abcdef123456";
		renderWithProviders(<UsageSnippets apiKey={apiKey} />);

		// Key is woven into multiple snippets (curl, python, JS, claude-code, ...).
		expect(
			screen.getAllByText((content) => content.includes(apiKey)).length,
		).toBeGreaterThan(1);

		// No leftover placeholder once a real key is provided.
		expect(
			screen.queryByText((content) => content.includes("YOUR_API_KEY")),
		).not.toBeInTheDocument();
	});
});
