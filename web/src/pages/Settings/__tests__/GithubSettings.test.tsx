import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../../../api/client";
import i18n from "../../../i18n";
import { serveSettings, toggleRowResetButton } from "../../../test/helpers";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { GithubPanel } from "../GithubSettings";

function mockGithubStatus(enabled: boolean) {
	server.use(
		http.get("/api/auth/github/status", () => HttpResponse.json({ enabled })),
	);
}

describe("GithubPanel", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("hides config inputs when GitHub SSO is disabled", async () => {
		serveSettings({ github_sso_enabled: "false" });
		renderWithProviders(<GithubPanel />);
		await screen.findByTestId("github-panel");
		expect(
			screen.queryByTestId("github-client-id-input"),
		).not.toBeInTheDocument();
		expect(
			screen.queryByTestId("github-client-secret-input"),
		).not.toBeInTheDocument();
	});

	it("shows inputs, a configured secret, and the derived callback URL when enabled", async () => {
		serveSettings({
			github_sso_enabled: "true",
			github_client_id: "Iv1.abc123",
			github_client_secret: "********",
			github_public_base_url: "https://hotel.example.com",
		});
		mockGithubStatus(true);
		renderWithProviders(<GithubPanel />);

		const clientId = (await screen.findByTestId(
			"github-client-id-input",
		)) as HTMLInputElement;
		expect(clientId.value).toBe("Iv1.abc123");

		// Secret is configured, so the clear button is shown.
		expect(
			screen.getByTestId("github-client-secret-clear"),
		).toBeInTheDocument();

		// Callback URL is derived from the public base URL (no trailing slash).
		expect(
			screen.getByText("https://hotel.example.com/api/auth/github/callback"),
		).toBeInTheDocument();
	});

	it("does not show the configured-green indicator when the client secret is blank", async () => {
		// Status ignores the secret, so it reports enabled=true with id + base URL
		// set; the panel must still show amber (incomplete) because no secret is set.
		serveSettings({
			github_sso_enabled: "true",
			github_client_id: "Iv1.abc123",
			github_public_base_url: "https://hotel.example.com",
			// github_client_secret intentionally absent
		});
		mockGithubStatus(true);
		renderWithProviders(<GithubPanel />);

		const status = await screen.findByTestId("github-status");
		await waitFor(() => {
			expect(status.querySelector(".bg-amber-500")).toBeInTheDocument();
			expect(status.querySelector(".bg-green-500")).not.toBeInTheDocument();
		});
	});

	it("derives the callback URL without a doubled slash", async () => {
		serveSettings({
			github_sso_enabled: "true",
			github_public_base_url: "https://hotel.example.com/",
		});
		mockGithubStatus(false);
		renderWithProviders(<GithubPanel />);

		expect(
			await screen.findByText(
				"https://hotel.example.com/api/auth/github/callback",
			),
		).toBeInTheDocument();
	});

	it("does NOT commit the allowed-emails draft when the blur comes from going managed", async () => {
		serveSettings({ github_sso_enabled: "true" });
		mockGithubStatus(true);
		const puts: Record<string, string>[] = [];
		server.use(
			http.put("/api/settings", async ({ request }) => {
				puts.push((await request.json()) as Record<string, string>);
				return HttpResponse.json({ ok: true });
			}),
		);
		const user = userEvent.setup();
		const { rerender } = renderWithProviders(<GithubPanel />);
		const el = await screen.findByTestId("github-allowed-emails-input");
		await user.type(el, "draft@b.test");
		// The fleet takes the allowlist over while the field has focus; the
		// forced blur must drop the draft rather than write it.
		rerender(<GithubPanel managed />);
		expect(el).toBeDisabled();
		fireEvent.blur(el);
		await new Promise((r) => setTimeout(r, 50));
		expect(puts.some((p) => "github_allowed_emails" in p)).toBe(false);
		expect(el).toHaveValue("");
	});

	it("commits each editable field, sets and clears the secret", async () => {
		serveSettings({
			github_sso_enabled: "true",
			github_client_id: "Iv1.abc123",
			github_client_secret: "********",
			github_public_base_url: "https://hotel.example.com",
		});
		mockGithubStatus(true);
		const puts: Record<string, string>[] = [];
		server.use(
			http.put("/api/settings", async ({ request }) => {
				puts.push((await request.json()) as Record<string, string>);
				return HttpResponse.json({ ok: true });
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(<GithubPanel />);

		await screen.findByTestId("github-panel");

		const commitField = async (testid: string, key: string, value: string) => {
			const el = await screen.findByTestId(testid);
			await user.clear(el);
			await user.type(el, value);
			await user.tab();
			await waitFor(() =>
				expect(puts.some((p) => p[key] === value)).toBe(true),
			);
		};

		await commitField(
			"github-client-id-input",
			"github_client_id",
			"Iv1.newid",
		);
		await commitField(
			"github-base-url-input",
			"github_public_base_url",
			"https://hotel.test",
		);
		await commitField(
			"github-allowed-emails-input",
			"github_allowed_emails",
			"a@b.test",
		);

		// Setting a new secret commits its value...
		await commitField(
			"github-client-secret-input",
			"github_client_secret",
			"new-secret",
		);
		// ...and clearing (after confirming) commits an empty string.
		await user.click(screen.getByTestId("github-client-secret-clear"));
		await user.click(screen.getByTestId("github-client-secret-confirm"));
		await waitFor(() =>
			expect(puts.some((p) => p.github_client_secret === "")).toBe(true),
		);
	});

	it("toggles enable off", async () => {
		serveSettings({
			github_sso_enabled: "true",
			github_client_id: "Iv1.abc123",
		});
		mockGithubStatus(true);
		const puts: Record<string, string>[] = [];
		server.use(
			http.put("/api/settings", async ({ request }) => {
				puts.push((await request.json()) as Record<string, string>);
				return HttpResponse.json({ ok: true });
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(<GithubPanel />);

		await screen.findByTestId("github-panel");
		await user.click(
			screen.getByRole("switch", { name: "Enable GitHub sign-in" }),
		);
		await waitFor(() =>
			expect(puts.some((p) => p.github_sso_enabled === "false")).toBe(true),
		);
	});
	it("resets each toggle row through the reset beside its label", async () => {
		const resetSpy = vi.spyOn(api.settings, "reset").mockResolvedValue({});
		const { user } = renderWithProviders(<GithubPanel />);
		const rows: [string, string][] = [
			["settings.github.enable", "github_sso_enabled"],
		];
		await screen.findByText(i18n.t(rows[0][0]));
		for (const [label, key] of rows) {
			await user.click(toggleRowResetButton(label));
			await waitFor(() => expect(resetSpy).toHaveBeenLastCalledWith([key]));
		}
		resetSpy.mockRestore();
	});
});
