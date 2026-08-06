import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { ActiveSessionsPanel } from "../ActiveSessionsSettings";

function mockRevoke(revoked: number) {
	const calls: number[] = [];
	server.use(
		http.post("/api/auth/sessions/revoke-others", () => {
			calls.push(1);
			return HttpResponse.json({ revoked });
		}),
	);
	return calls;
}

describe("ActiveSessionsPanel", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	// The action ends every other session, so a stray click must not fire it.
	it("arms on the first click and only revokes on the second", async () => {
		const calls = mockRevoke(2);
		const user = userEvent.setup();
		renderWithProviders(<ActiveSessionsPanel />);

		const button = screen.getByTestId("revoke-other-sessions");
		await user.click(button);
		expect(calls).toHaveLength(0);

		await user.click(button);
		await waitFor(() => expect(calls).toHaveLength(1));
	});

	it("reports how many sessions were signed out", async () => {
		mockRevoke(3);
		const user = userEvent.setup();
		renderWithProviders(<ActiveSessionsPanel />);

		const button = screen.getByTestId("revoke-other-sessions");
		await user.click(button);
		await user.click(button);

		// Asserted by count rather than translated copy so the test stays
		// locale-independent.
		expect(await screen.findByText(/3/)).toBeInTheDocument();
	});

	it("surfaces a failure instead of implying the sessions are gone", async () => {
		server.use(
			http.post("/api/auth/sessions/revoke-others", () =>
				HttpResponse.json({ error: "nope" }, { status: 500 }),
			),
		);
		const user = userEvent.setup();
		renderWithProviders(<ActiveSessionsPanel />);

		const button = screen.getByTestId("revoke-other-sessions");
		await user.click(button);
		await user.click(button);

		expect(await screen.findByText(/500/)).toBeInTheDocument();
	});
});
