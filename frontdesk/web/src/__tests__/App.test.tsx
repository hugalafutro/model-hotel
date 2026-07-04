import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import App from "../App";
import { server } from "../test/server";
import { sseHandler } from "../test/sse";

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
});
