import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { AuthenticationSettings } from "../AuthenticationSettings";

describe("AuthenticationSettings breached-password toggle", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("defaults the breach check on when the setting is unset", async () => {
		renderWithProviders(
			<AuthenticationSettings collapsed={false} onToggle={() => {}} />,
		);

		const toggle = await screen.findByRole("switch", {
			name: "Reject breached passwords",
		});
		expect(toggle).toHaveAttribute("aria-checked", "true");
	});

	it("reflects the stored value and writes the flipped value", async () => {
		let body: Record<string, string> | undefined;
		server.use(
			http.get("/api/settings", () =>
				HttpResponse.json({ pwned_password_check_enabled: "false" }),
			),
			http.put("/api/settings", async ({ request }) => {
				body = (await request.json()) as Record<string, string>;
				return HttpResponse.json(body);
			}),
		);

		const { user } = renderWithProviders(
			<AuthenticationSettings collapsed={false} onToggle={() => {}} />,
		);

		const toggle = await screen.findByRole("switch", {
			name: "Reject breached passwords",
		});
		await waitFor(() => {
			expect(toggle).toHaveAttribute("aria-checked", "false");
		});

		await user.click(toggle);

		await waitFor(() => {
			expect(body).toEqual({ pwned_password_check_enabled: "true" });
		});
	});
});
