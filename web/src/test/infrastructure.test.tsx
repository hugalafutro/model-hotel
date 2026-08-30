/**
 * Validates the test infrastructure: MSW handlers, renderWithProviders,
 * userEvent, and EventSource mock all work correctly.
 */

import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { api } from "../api/client";
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
		// Verify click was processed (button still functional after interaction)
		expect(button).not.toBeDisabled();
	});

	it("MSW intercepts API calls and returns mock data", async () => {
		server.use(
			http.get("/api/providers", () => {
				return HttpResponse.json([mockProvider]);
			}),
		);

		const response = await fetch("/api/providers");
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

		const response = await fetch("/api/providers");
		const data = await response.json();
		expect(data).toHaveLength(0);
	});

	// The suite's onUnhandledRequest policy is "warn", so a path with no handler
	// leaves the test process talking to a real socket and only a log line to
	// show for it. These endpoints are polled by the settings screens, so they
	// are asserted to be intercepted rather than merely warned about.
	it("intercepts the TOTP status and backup prune-preview calls", async () => {
		const unhandled: string[] = [];
		const record = ({ request }: { request: Request }) => {
			unhandled.push(`${request.method} ${new URL(request.url).pathname}`);
		};
		server.events.on("request:unhandled", record);

		const [totpStatus, prunePreview] = await Promise.allSettled([
			api.totp.status(),
			api.backups.prunePreview(),
		]);
		server.events.removeListener("request:unhandled", record);

		expect(unhandled).toEqual([]);
		expect(totpStatus).toMatchObject({
			status: "fulfilled",
			value: { enabled: false },
		});
		expect(prunePreview).toMatchObject({
			status: "fulfilled",
			value: { son: [], father: [], grandfather: [], prune: [] },
		});
	});

	it("EventSource mock is available globally", async () => {
		const es = new EventSource("/api/events");
		// onopen fires via queueMicrotask after construction
		expect(es.readyState).toBe(EventSource.CONNECTING);
		await new Promise<void>((r) => queueMicrotask(r));
		expect(es.readyState).toBe(EventSource.OPEN);
		es.close();
		expect(es.readyState).toBe(EventSource.CLOSED);
	});

	it("EventSource mock fires onopen callback", async () => {
		const es = new EventSource("/api/events");
		let opened = false;
		es.onopen = () => {
			opened = true;
		};
		await new Promise<void>((r) => queueMicrotask(r));
		expect(opened).toBe(true);
		es.close();
	});
});
