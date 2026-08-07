import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { api, hasSession } from "../api/client";
import { server } from "../test/server";
import { sseHandler } from "../test/sse";

// QuotaStrip is wrapped in an ErrorBoundary in App.tsx specifically so a throw
// inside it cannot take the whole authenticated shell down. That wiring lives
// on a single line with nothing forcing it to stay; this flag lets one test
// flip QuotaStrip into throwing on demand without disturbing every other test
// in this file, which rely on QuotaStrip rendering harmlessly (the real one
// renders nothing at all, since /api/quota returns an empty list in
// authHandlers). The marker div is what makes "the strip came back" observable.
const quotaStripThrows = vi.hoisted(() => ({ current: false }));

vi.mock("../components/QuotaStrip", () => ({
	QuotaStrip: () => {
		if (quotaStripThrows.current) {
			throw new Error("malformed quota payload");
		}
		return <div data-testid="quota-strip-mock" />;
	},
}));

// The session itself rides an HttpOnly cookie the page cannot see; the readable
// fd_csrf half is what the SPA reads as "logged in", so these two stand in for
// the server's Set-Cookie pair on login and logout.
function setSessionCookie() {
	document.cookie = "fd_csrf=csrf-abc; path=/";
}
function clearSessionCookie() {
	document.cookie = "fd_csrf=; path=/; max-age=0";
}

// Cookies outlive a test in jsdom (setup.ts only clears localStorage), so an
// authenticated test would otherwise boot the next one straight into the shell.
afterEach(clearSessionCookie);

// Auth-gating handlers: TOTP off, no passkey, cookie-authenticated members list.
// The admin exchange validates the raw token and answers with the session
// cookies, which is the only thing that makes the shell reachable.
function authHandlers(validToken: string) {
	return [
		sseHandler(),
		http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
		http.get("/api/webauthn/available", () =>
			HttpResponse.json({ enabled: false }),
		),
		http.post("/api/auth/admin-exchange", async ({ request }) => {
			const body = (await request.json()) as { admin_token?: string };
			if (body.admin_token !== validToken) {
				return new HttpResponse("Invalid admin token", { status: 401 });
			}
			setSessionCookie();
			return new HttpResponse(null, { status: 204 });
		}),
		http.post("/api/logout", () => {
			clearSessionCookie();
			return new HttpResponse(null, { status: 204 });
		}),
		http.get("/api/members", () => HttpResponse.json([])),
		// The authed shell renders QuotaStrip, which reads /api/quota. An empty
		// list keeps the strip hidden (see QuotaStrip.test.tsx) so it does not
		// disturb any existing assertion here; onUnhandledRequest is "error", so
		// every test that reaches the shell needs this handler regardless.
		http.get("/api/quota", () => HttpResponse.json({ quota: [] })),
	];
}

describe("App auth gating", () => {
	it("shows the login screen when no session cookie is present", () => {
		server.use(...authHandlers("good"));
		render(<App />);
		expect(screen.getByLabelText(/Front Desk token/i)).toBeInTheDocument();
	});

	it("boots straight into the shell when the session cookie is present", async () => {
		server.use(...authHandlers("good"));
		setSessionCookie();
		render(<App />);
		await waitFor(() =>
			expect(screen.getByRole("tab", { name: /members/i })).toBeInTheDocument(),
		);
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

	it("logs out to the login screen and drops the session marker", async () => {
		const logout = vi.spyOn(api, "logout");
		server.use(...authHandlers("good"));
		setSessionCookie();
		render(<App />);
		await waitFor(() =>
			expect(screen.getByRole("tab", { name: /members/i })).toBeInTheDocument(),
		);

		await userEvent.click(screen.getByRole("button", { name: /log out/i }));

		await waitFor(() =>
			expect(screen.getByLabelText(/Front Desk token/i)).toBeInTheDocument(),
		);
		expect(logout).toHaveBeenCalled();
		expect(hasSession()).toBe(false);
	});

	it("still logs out locally when the revoke request is rate limited", async () => {
		// /api/logout sits behind a per-IP limiter and can answer 429. The UI must
		// not stay signed in on a session the operator asked to end: the local
		// clear is unconditional and the server-side revoke is best effort.
		// Overrides go FIRST: server.use keeps the order it is given and the first
		// match wins, so a 429 listed after authHandlers' 204 would never be seen.
		server.use(
			http.post("/api/logout", () =>
				HttpResponse.json({ error: "too many requests" }, { status: 429 }),
			),
			...authHandlers("good"),
		);
		setSessionCookie();
		render(<App />);
		await waitFor(() =>
			expect(screen.getByRole("tab", { name: /members/i })).toBeInTheDocument(),
		);

		await userEvent.click(screen.getByRole("button", { name: /log out/i }));

		await waitFor(() =>
			expect(screen.getByLabelText(/Front Desk token/i)).toBeInTheDocument(),
		);
		expect(hasSession()).toBe(false);
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
		setSessionCookie();
		render(<App />);
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
		// The cookie boots the shell optimistically; the first authenticated fetch
		// is what proves the session, and an expired one must bounce back to login
		// with the readable marker cleared.
		// Override first: the first matching handler wins (see the logout test).
		server.use(
			http.get(
				"/api/members",
				() => new HttpResponse("expired", { status: 401 }),
			),
			...authHandlers("good"),
		);
		setSessionCookie();
		render(<App />);
		await waitFor(() =>
			expect(screen.getByLabelText(/Front Desk token/i)).toBeInTheDocument(),
		);
		expect(hasSession()).toBe(false);
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
			setSessionCookie();
			render(<App />);
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
			// Contained means gone, not degraded in place: the boundary renders no
			// fallback, matching the strip's own empty state.
			expect(screen.queryByTestId("quota-strip-mock")).toBeNull();
		} finally {
			quotaStripThrows.current = false;
			consoleErrorSpy.mockRestore();
		}
	});

	it("retries the strip on the next tab switch after it has failed", async () => {
		// Containment alone leaves the boundary latched for the life of the
		// mount, and this shell never unmounts while signed in, so without the
		// resetKeys wiring in App.tsx the only way back would be a page reload.
		const consoleErrorSpy = vi
			.spyOn(console, "error")
			.mockImplementation(() => {});
		quotaStripThrows.current = true;
		try {
			server.use(...authHandlers("good"));
			setSessionCookie();
			render(<App />);
			await waitFor(() => {
				expect(
					screen.getByRole("tab", { name: /members/i }),
				).toBeInTheDocument();
			});
			expect(screen.queryByTestId("quota-strip-mock")).toBeNull();

			// Whatever the strip choked on is not necessarily permanent (the next
			// poll replaces the payload), so navigating gives it another go.
			quotaStripThrows.current = false;
			await userEvent.click(screen.getByRole("tab", { name: /settings/i }));
			await waitFor(() => {
				expect(screen.getByTestId("quota-strip-mock")).toBeInTheDocument();
			});
		} finally {
			quotaStripThrows.current = false;
			consoleErrorSpy.mockRestore();
		}
	});
});
