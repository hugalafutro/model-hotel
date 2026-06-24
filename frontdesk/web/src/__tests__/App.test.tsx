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
});
