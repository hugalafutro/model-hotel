import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ShikiCode } from "../ShikiCode";

const BASH_CODE = `curl -X POST http://my-hotel:8080/v1/chat/completions \\
  -H "Authorization: Bearer YOUR_API_KEY"`;

describe("ShikiCode", () => {
	it("renders the plain code as fallback, then highlighted tokens", async () => {
		const { container } = render(
			<ShikiCode
				code={BASH_CODE}
				lang="bash"
				highlights={["http://my-hotel:8080", "YOUR_API_KEY"]}
			/>,
		);

		// Content is available immediately (plain fallback or tokens).
		expect(container.textContent).toBe(BASH_CODE);

		// After the lazy highlighter resolves, tokens are colored spans.
		await waitFor(() => {
			expect(container.querySelectorAll("span").length).toBeGreaterThan(1);
		});
		// Tokenization must not alter the text content.
		expect(container.textContent).toBe(BASH_CODE);
	});

	it("wraps the highlight targets in terminal-highlight spans", async () => {
		render(
			<ShikiCode
				code={BASH_CODE}
				lang="bash"
				highlights={["http://my-hotel:8080", "YOUR_API_KEY"]}
			/>,
		);

		await waitFor(() => {
			const key = screen.getByText("YOUR_API_KEY");
			expect(key.className).toContain("terminal-highlight");
		});
		const url = screen.getByText("http://my-hotel:8080");
		expect(url.className).toContain("terminal-highlight");
	});

	it("highlights instance origin and model id in JSON snippets", async () => {
		const json = `{
  "api_url": "http://my-hotel:8080/v1",
  "name": "model_name"
}`;
		const { container } = render(
			<ShikiCode
				code={json}
				lang="json"
				highlights={["http://my-hotel:8080", "model_name"]}
			/>,
		);

		await waitFor(() => {
			expect(container.querySelectorAll(".terminal-highlight").length).toBe(2);
		});
		expect(container.textContent).toBe(json);
	});
});
