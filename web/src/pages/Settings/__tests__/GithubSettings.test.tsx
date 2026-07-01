import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { GithubPanel } from "../GithubSettings";

function mockSettings(values: Record<string, string>) {
	server.use(
		http.get("/api/settings", ({ request }) => {
			if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
				return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
			}
			return HttpResponse.json(values);
		}),
	);
}

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
		mockSettings({ github_sso_enabled: "false" });
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
		mockSettings({
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
		mockSettings({
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
		mockSettings({
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

	it("commits each editable field, sets and clears the secret", async () => {
		mockSettings({
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
		mockSettings({
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
});
