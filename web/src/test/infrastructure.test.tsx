/**
 * Validates the test infrastructure: MSW handlers, renderWithProviders,
 * userEvent, and EventSource mock all work correctly.
 */

import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { mockProvider } from "./mocks/data";
import { server } from "./mocks/server";
import { renderWithProviders } from "./utils";

function SimpleComponent() {
	return (
		<div>
			<h1>Test App</h1>
			<button type="button">Click me</button>
		</div>
	);
}

describe("Test infrastructure", () => {
	it("renderWithProviders wraps components without crashing", () => {
		const { container } = renderWithProviders(<SimpleComponent />);
		expect(container.querySelector("h1")).toHaveTextContent("Test App");
	});

	it("userEvent allows clicking buttons", async () => {
		const { user } = renderWithProviders(<SimpleComponent />);
		const button = screen.getByRole("button", { name: /click me/i });
		await user.click(button);
		// No crash = success
		expect(button).toBeInTheDocument();
	});

	it("MSW intercepts API calls and returns mock data", async () => {
		server.use(
			http.get("/api/providers", () => {
				return HttpResponse.json([mockProvider]);
			}),
		);

		const response = await fetch("/api/providers", {
			headers: { Authorization: "Bearer test-admin-token" },
		});
		const data = await response.json();

		expect(data).toHaveLength(1);
		expect(data[0].name).toBe(mockProvider.name);
	});

	it("MSW can override handlers per test", async () => {
		server.use(
			http.get("/api/providers", () => {
				return HttpResponse.json([]);
			}),
		);

		const response = await fetch("/api/providers", {
			headers: { Authorization: "Bearer test-admin-token" },
		});
		const data = await response.json();
		expect(data).toHaveLength(0);
	});

	it("EventSource mock is available globally", () => {
		const es = new EventSource("/api/events");
		expect(es).toBeDefined();
		expect(es.readyState).toBeDefined();
		es.close();
	});
});
