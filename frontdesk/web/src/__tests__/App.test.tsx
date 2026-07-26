import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import App from "../App";
import { server } from "../test/server";
import { sseHandler } from "../test/sse";

// QuotaStrip is wrapped in an ErrorBoundary in App.tsx specifically so a throw
// inside it cannot take the whole authenticated shell down. That wiring lives
// on a single line with nothing forcing it to stay; this flag lets one test
// flip QuotaStrip into throwing on demand without disturbing every other test
// in this file, which rely on QuotaStrip rendering normally (i.e. nothing,
// since /api/quota returns an empty list in authHandlers).
const quotaStripThrows = vi.hoisted(() => ({ current: false }));

vi.mock("../components/QuotaStrip", () => ({
	QuotaStrip: () => {
		if (quotaStripThrows.current) {
			throw new Error("malformed quota payload");
		}
		return null;
	},
}));

// Auth-gating handlers: TOTP off, no passkey, members list reflects the token.
// Includes the SSE stream the authenticated shell opens after login.
function authHandlers(validToken: string) {
	return [
		sseHandler(),
		http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
		http.get("/api/webauthn/available", () =>
			HttpResponse.json({ enabled: false }),
		),
		http.get("/api/members", ({ request }) => {
			const auth = request.headers.get("Authorization");
			if (auth !== `Bearer ${validToken}`) {
				return new HttpResponse("Invalid admin token or session token", {
					status: 401,
				});
			}
			return HttpResponse.json([]);
		}),
		// The authed shell renders QuotaStrip, which reads /api/quota. An empty
		// list keeps the strip hidden (see QuotaStrip.test.tsx) so it does not
		// disturb any existing assertion here; onUnhandledRequest is "error", so
		// every test that reaches the shell needs this handler regardless.
		http.get("/api/quota", () => HttpResponse.json({ quota: [] })),
	];
}

describe("App auth gating", () => {
	it("shows the login screen when no token is stored", () => {
		server.use(...authHandlers("good"));
		render(<App />);
		expect(screen.getByLabelText(/Front Desk token/i)).toBeInTheDocument();
	});

	it("signs in with a valid token (TOTP off) and shows the tabs", async () => {
		server.use(...authHandlers("good"));
		render(<App />);
		await userEvent.type(screen.getByLabelText(/Front Desk token/i), "good");
		await userEvent.click(screen.getByRole("button", { name: /sign in/i }));
		await waitFor(() => {
			expect(screen.getByRole("tab", { name: /members/i })).toBeInTheDocument();
		});
	});

	it("rejects a bad token with an error and stays on login", async () => {
		server.use(...authHandlers("good"));
		render(<App />);
		await userEvent.type(screen.getByLabelText(/Front Desk token/i), "wrong");
		await userEvent.click(screen.getByRole("button", { name: /sign in/i }));
		await waitFor(() => {
			expect(screen.getByRole("alert")).toHaveTextContent(/not accepted/i);
		});
		expect(
			screen.queryByRole("tab", { name: /members/i }),
		).not.toBeInTheDocument();
	});

	it("clears a stale SSO error after an unrelated login then logout", async () => {
		// A failed SSO callback lands on the SPA with the code in the URL fragment.
		server.use(...authHandlers("good"));
		window.location.hash = "#oidc_error=not_allowed";
		render(<App />);
		// The failure banner shows on the login screen (and the fragment is scrubbed).
		expect(screen.getByRole("alert")).toHaveTextContent(
			/single sign-on failed/i,
		);

		// Log in by an unrelated path (token), reach the shell, then log out.
		await userEvent.type(screen.getByLabelText(/Front Desk token/i), "good");
		await userEvent.click(screen.getByRole("button", { name: /sign in/i }));
		await waitFor(() =>
			expect(screen.getByRole("tab", { name: /members/i })).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByRole("button", { name: /log out/i }));

		// Back on login, the stale SSO banner must NOT reappear.
		await waitFor(() =>
			expect(screen.getByLabelText(/Front Desk token/i)).toBeInTheDocument(),
		);
		expect(screen.queryByRole("alert")).not.toBeInTheDocument();
	});

	it("returns to the Members tab when the brand logo is clicked", async () => {
		server.use(...authHandlers("good"));
		render(<App />);
		await userEvent.type(screen.getByLabelText(/Front Desk token/i), "good");
		await userEvent.click(screen.getByRole("button", { name: /sign in/i }));
		await waitFor(() =>
			expect(screen.getByRole("tab", { name: /members/i })).toBeInTheDocument(),
		);

		// Move off the default tab, then click the top-left brand logo.
		await userEvent.click(screen.getByRole("tab", { name: /settings/i }));
		expect(screen.getByRole("tab", { name: /settings/i })).toHaveAttribute(
			"aria-selected",
			"true",
		);
		await userEvent.click(screen.getByRole("button", { name: /front desk/i }));

		expect(screen.getByRole("tab", { name: /members/i })).toHaveAttribute(
			"aria-selected",
			"true",
		);
	});

	it("drops back to login when an authed request later 401s", async () => {
		// First /api/members call (login validation) succeeds; the next one (the
		// authed shell's own fetch) 401s, which must bounce back to login.
		let calls = 0;
		server.use(
			sseHandler(),
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: false }),
			),
			http.get("/api/members", () => {
				calls += 1;
				return calls === 1
					? HttpResponse.json([])
					: new HttpResponse("expired", { status: 401 });
			}),
			http.get("/api/quota", () => HttpResponse.json({ quota: [] })),
		);
		render(<App />);
		await userEvent.type(screen.getByLabelText(/Front Desk token/i), "good");
		await userEvent.click(screen.getByRole("button", { name: /sign in/i }));
		// The shell mounts, its members fetch 401s, and we return to the login form.
		await waitFor(() =>
			expect(screen.getByLabelText(/Front Desk token/i)).toBeInTheDocument(),
		);
		expect(localStorage.getItem("fdAuthToken")).toBeNull();
	});

	it("contains a QuotaStrip render failure to the strip and keeps the rest of the shell up", async () => {
		// React logs a caught render error to the console; expected here since the
		// point of this test is to throw on purpose. Scoped to this test only.
		const consoleErrorSpy = vi
			.spyOn(console, "error")
			.mockImplementation(() => {});
		quotaStripThrows.current = true;
		try {
			server.use(...authHandlers("good"));
			render(<App />);
			await userEvent.type(screen.getByLabelText(/Front Desk token/i), "good");
			await userEvent.click(screen.getByRole("button", { name: /sign in/i }));
			// The rest of the authenticated shell renders normally: tabs, and the
			// Members page content past its own loading state. If the ErrorBoundary
			// around QuotaStrip in App.tsx were removed, this throw would unmount
			// the whole tree and none of this would be found.
			await waitFor(() => {
				expect(
					screen.getByRole("tab", { name: /members/i }),
				).toBeInTheDocument();
			});
			await waitFor(() => {
				expect(screen.getByRole("heading", { level: 1 })).toBeInTheDocument();
			});
		} finally {
			quotaStripThrows.current = false;
			consoleErrorSpy.mockRestore();
		}
	});
});
