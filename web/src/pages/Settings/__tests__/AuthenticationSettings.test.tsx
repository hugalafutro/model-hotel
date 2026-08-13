import { screen, waitFor, within } from "@testing-library/react";
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

	it("resets exactly the breach-check key from its inline reset button", async () => {
		let capturedKeys: string[] | undefined;
		server.use(
			http.get("/api/settings", () =>
				HttpResponse.json({ pwned_password_check_enabled: "false" }),
			),
			http.delete("/api/settings", async ({ request }) => {
				capturedKeys = ((await request.json()) as { keys: string[] }).keys;
				return HttpResponse.json({});
			}),
		);

		const { user } = renderWithProviders(
			<AuthenticationSettings collapsed={false} onToggle={() => {}} />,
		);

		// Several controls share the "Reset this setting" name, so scope to the
		// reset button sitting beside the breach-check label.
		const label = await screen.findByText("Reject breached passwords", {
			selector: "p",
		});
		const row = label.closest("div") as HTMLElement;
		await user.click(within(row).getByRole("button"));

		await waitFor(() =>
			expect(capturedKeys).toEqual(["pwned_password_check_enabled"]),
		);
	});
});
