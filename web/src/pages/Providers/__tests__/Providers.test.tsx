import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import type { Provider } from "../../../api/types";
import { mockAllDefaults } from "../../../test/helpers";
import { mockProvider } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { Providers } from "../../Providers";

/**
 * Helper to set up baseline MSW handlers for Providers page tests.
 * Use server.use(...mockProvidersPageDefaults(), ...testSpecificOverrides)
 */
function mockProvidersPageDefaults(overrides?: { providers?: Provider[] }) {
	return mockAllDefaults(overrides);
}

describe("Providers", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	describe("SSE event handling (lines 88-93)", () => {
		it("updates discoverAllCurrentId when receiving provider_starting event", async () => {
			server.use(...mockProvidersPageDefaults());

			renderWithProviders(<Providers />);

			// Wait for providers to load
			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			// Dispatch a custom SSE event
			const event = new CustomEvent("server-event", {
				detail: {
					type: "request.discovery.provider_starting",
					metadata: { provider_id: mockProvider.id },
				},
			});
			window.dispatchEvent(event);

			// The provider card should now show "Discovering..." state
			// We can verify this by checking the button text and styling
			await waitFor(() => {
				const discoveringButton = screen.getByRole("button", {
					name: /Discovering/i,
				});
				expect(discoveringButton).toBeInTheDocument();
				// Button should have disabled styling (cursor-not-allowed class)
				expect(discoveringButton).toHaveClass("cursor-not-allowed");
			});
		});
	});

	describe("discoverAllMutation (lines 108-114)", () => {
		it("shows error toast when all providers fail (succeeded=0, failed>0)", async () => {
			server.use(
				...mockProvidersPageDefaults(),
				http.post("/api/providers/discover-all", () =>
					HttpResponse.json({ succeeded: 0, failed: 3 }),
				),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			const discoverAllButton = screen.getByRole("button", {
				name: "Discover All Models",
			});
			await user.click(discoverAllButton);

			await waitFor(() => {
				expect(
					screen.getByText("Discovery failed for all 3 providers"),
				).toBeInTheDocument();
			});
		});

		it("shows error toast when discoverAll mutation fails", async () => {
			server.use(
				...mockProvidersPageDefaults(),
				http.post("/api/providers/discover-all", () =>
					HttpResponse.json({ error: "Network error" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			const discoverAllButton = screen.getByRole("button", {
				name: "Discover All Models",
			});
			await user.click(discoverAllButton);

			await waitFor(() => {
				expect(screen.getByText(/Discovery failed:/)).toBeInTheDocument();
			});
		});
	});

	describe("refreshQuotasMutation (lines 124-137)", () => {
		it("shows warning toast when some quotas fail to refresh", async () => {
			server.use(
				...mockProvidersPageDefaults(),
				http.post("/api/providers/refresh-quotas", () =>
					HttpResponse.json({ refreshed: 2, failed: 1, skipped: 0 }),
				),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			const refreshButton = screen.getByRole("button", {
				name: "Refresh Quotas/Balances",
			});
			await user.click(refreshButton);

			await waitFor(() => {
				expect(
					screen.getByText("Refreshed 2 quotas (1 failed, 0 unsupported)"),
				).toBeInTheDocument();
			});
		});

		it("shows info toast when no providers support quota/balance", async () => {
			server.use(
				...mockProvidersPageDefaults(),
				http.post("/api/providers/refresh-quotas", () =>
					HttpResponse.json({ refreshed: 0, failed: 0, skipped: 5 }),
				),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			const refreshButton = screen.getByRole("button", {
				name: "Refresh Quotas/Balances",
			});
			await user.click(refreshButton);

			await waitFor(() => {
				expect(
					screen.getByText("No providers with quota/balance support found"),
				).toBeInTheDocument();
			});
		});

		it("shows success toast when all quotas refresh successfully", async () => {
			server.use(
				...mockProvidersPageDefaults(),
				http.post("/api/providers/refresh-quotas", () =>
					HttpResponse.json({ refreshed: 3, failed: 0, skipped: 0 }),
				),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			const refreshButton = screen.getByRole("button", {
				name: "Refresh Quotas/Balances",
			});
			await user.click(refreshButton);

			await waitFor(() => {
				expect(
					screen.getByText("Refreshed 3 quotas/balances"),
				).toBeInTheDocument();
			});
		});

		it("shows error toast when refreshQuotas mutation fails", async () => {
			server.use(
				...mockProvidersPageDefaults(),
				http.post("/api/providers/refresh-quotas", () =>
					HttpResponse.json({ error: "Connection refused" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			const refreshButton = screen.getByRole("button", {
				name: "Refresh Quotas/Balances",
			});
			await user.click(refreshButton);

			await waitFor(() => {
				expect(screen.getByText(/Refresh quotas failed:/)).toBeInTheDocument();
			});
		});
	});

	describe("discoverMutation (lines 149-151)", () => {
		it("shows error toast when single provider discovery fails", async () => {
			const testProvider = {
				...mockProvider,
				id: "provider-test",
				name: "Test Provider",
			};
			server.use(
				...mockProvidersPageDefaults({ providers: [testProvider] }),
				http.post("/api/providers/:id/discover", () =>
					HttpResponse.json({ error: "Discovery failed" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			const discoverButton = screen.getByRole("button", {
				name: "Discover Models",
			});
			await user.click(discoverButton);

			await waitFor(() => {
				expect(screen.getByText(/Discovery failed:/)).toBeInTheDocument();
			});
		});
	});

	describe("deleteMutation (lines 169-171)", () => {
		it("shows error toast when delete fails", async () => {
			const testProvider = {
				...mockProvider,
				id: "provider-test",
				name: "Test Provider",
			};
			server.use(
				...mockProvidersPageDefaults({ providers: [testProvider] }),
				http.delete("/api/providers/:id", () =>
					HttpResponse.json({ error: "Delete failed" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			const deleteButton = screen.getByRole("button", {
				name: "Delete",
			});
			await user.click(deleteButton);

			// Wait for modal and confirm deletion within it
			await waitFor(() => {
				expect(screen.getByRole("dialog")).toBeInTheDocument();
			});
			const dialog = screen.getByRole("dialog");
			const confirmButton = within(dialog).getByRole("button", {
				name: "Delete",
			});
			await user.click(confirmButton);

			await waitFor(() => {
				expect(screen.getByText(/Failed to delete:/)).toBeInTheDocument();
			});
		});

		it("shows success toast when delete succeeds", async () => {
			const testProvider = {
				...mockProvider,
				id: "provider-test",
				name: "Test Provider",
			};
			server.use(
				...mockProvidersPageDefaults({ providers: [testProvider] }),
				http.delete(
					"/api/providers/:id",
					() => new HttpResponse(null, { status: 204 }),
				),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			const deleteButton = screen.getByRole("button", {
				name: "Delete",
			});
			await user.click(deleteButton);

			// Wait for modal and confirm deletion within it
			await waitFor(() => {
				expect(screen.getByRole("dialog")).toBeInTheDocument();
			});
			const dialog = screen.getByRole("dialog");
			const confirmButton = within(dialog).getByRole("button", {
				name: "Delete",
			});
			await user.click(confirmButton);

			await waitFor(() => {
				expect(screen.getByText("Provider deleted")).toBeInTheDocument();
			});
		});
	});

	describe("ProviderCard quota modal callbacks (lines 313-316)", () => {
		it("opens NanoGPT quota modal when clicking quota badge", async () => {
			const nanogptProvider = {
				...mockProvider,
				id: "provider-nano",
				name: "NanoGPT",
				base_url: "https://nano-gpt.com/api/subscription/v1",
			};
			server.use(
				...mockProvidersPageDefaults({ providers: [nanogptProvider] }),
				http.get("/api/providers/:id/usage", () =>
					HttpResponse.json({
						active: true,
						provider: "nanogpt",
						providerStatus: "active",
						providerStatusRaw: "active",
						stripeSubscriptionId: "sub_test123",
						cancellationReason: null,
						canceledAt: null,
						endedAt: null,
						cancelAt: null,
						cancelAtPeriodEnd: false,
						limits: {
							weeklyInputTokens: 1000000,
							dailyInputTokens: 200000,
							dailyImages: 100,
						},
						allowOverage: false,
						period: {
							currentPeriodEnd: new Date(
								Date.now() + 7 * 24 * 60 * 60 * 1000,
							).toISOString(),
						},
						dailyImages: null,
						dailyInputTokens: {
							used: 50000,
							remaining: 150000,
							percentUsed: 25,
							resetAt: Date.now() + 24 * 60 * 60 * 1000,
						},
						weeklyInputTokens: {
							used: 200000,
							remaining: 800000,
							percentUsed: 20,
							resetAt: Date.now() + 7 * 24 * 60 * 60 * 1000,
						},
						state: "active",
						graceUntil: null,
					}),
				),
			);

			const { user } = renderWithProviders(<Providers />);

			// Wait for provider to load
			await waitFor(() => {
				expect(screen.getByText("NanoGPT")).toBeInTheDocument();
			});

			// Wait for quota badge to appear (shows "800K/1M" for weekly remaining/limit in default remaining mode)
			await waitFor(() => {
				const badge = screen.getByText(/800K/);
				expect(badge).toBeInTheDocument();
			});
			const quotaBadge = screen.getByText(/800K/);
			await user.click(quotaBadge);

			// Modal should open
			await waitFor(() => {
				expect(screen.getByRole("dialog")).toBeInTheDocument();
			});
		});

		it("closes NanoGPT quota modal when clicking close button", async () => {
			const nanogptProvider = {
				...mockProvider,
				id: "provider-nano",
				name: "NanoGPT",
				base_url: "https://nano-gpt.com/api/subscription/v1",
			};
			server.use(
				...mockProvidersPageDefaults({ providers: [nanogptProvider] }),
				http.get("/api/providers/:id/usage", () =>
					HttpResponse.json({
						active: true,
						provider: "nanogpt",
						providerStatus: "active",
						providerStatusRaw: "active",
						stripeSubscriptionId: "sub_test123",
						cancellationReason: null,
						canceledAt: null,
						endedAt: null,
						cancelAt: null,
						cancelAtPeriodEnd: false,
						limits: {
							weeklyInputTokens: 1000000,
							dailyInputTokens: 200000,
							dailyImages: 100,
						},
						allowOverage: false,
						period: {
							currentPeriodEnd: new Date(
								Date.now() + 7 * 24 * 60 * 60 * 1000,
							).toISOString(),
						},
						dailyImages: null,
						dailyInputTokens: {
							used: 50000,
							remaining: 150000,
							percentUsed: 25,
							resetAt: Date.now() + 24 * 60 * 60 * 1000,
						},
						weeklyInputTokens: {
							used: 200000,
							remaining: 800000,
							percentUsed: 20,
							resetAt: Date.now() + 7 * 24 * 60 * 60 * 1000,
						},
						state: "active",
						graceUntil: null,
					}),
				),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("NanoGPT")).toBeInTheDocument();
			});

			await waitFor(() => {
				const badge = screen.getByText(/800K/);
				expect(badge).toBeInTheDocument();
			});
			const quotaBadge = screen.getByText(/800K/);
			await user.click(quotaBadge);

			await waitFor(() => {
				expect(screen.getByRole("dialog")).toBeInTheDocument();
			});

			// Close the modal
			const dialog = screen.getByRole("dialog");
			const closeButton = within(dialog).getByRole("button", {
				name: "Close",
			});
			await user.click(closeButton);

			// Modal should close
			await waitFor(() => {
				expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
			});
		});

		it("closes Z.ai Coding quota modal when clicking close button", async () => {
			const zaiProvider = {
				...mockProvider,
				id: "provider-zai",
				name: "Z.ai Coding",
				base_url: "https://api.z.ai/api/coding/paas/v4",
			};
			server.use(
				...mockProvidersPageDefaults({ providers: [zaiProvider] }),
				http.get("/api/providers/:id/usage", () =>
					HttpResponse.json({
						code: 0,
						msg: "success",
						success: true,
						data: {
							level: "basic",
							limits: [
								{
									type: "TOKENS_LIMIT",
									unit: 3,
									number: 10000,
									usage: 5000,
									currentValue: 5000,
									remaining: 5000,
									percentage: 50,
									nextResetTime: Date.now() + 5 * 60 * 60 * 1000,
								},
								{
									type: "TOKENS_LIMIT",
									unit: 6,
									number: 50000,
									usage: 25000,
									currentValue: 25000,
									remaining: 25000,
									percentage: 50,
									nextResetTime: Date.now() + 7 * 24 * 60 * 60 * 1000,
								},
							],
						},
					}),
				),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Z.ai Coding")).toBeInTheDocument();
			});

			await waitFor(() => {
				const badge = screen.getByText(/50%/);
				expect(badge).toBeInTheDocument();
			});
			const quotaBadge = screen.getByText(/50%/);
			await user.click(quotaBadge);

			await waitFor(() => {
				expect(screen.getByRole("dialog")).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog");
			const closeButton = within(dialog).getByRole("button", {
				name: "Close",
			});
			await user.click(closeButton);

			await waitFor(() => {
				expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
			});
		});

		it("closes OpenRouter balance modal when clicking close button", async () => {
			const openrouterProvider = {
				...mockProvider,
				id: "provider-or",
				name: "OpenRouter",
				base_url: "https://openrouter.ai/api/v1",
			};
			server.use(
				...mockProvidersPageDefaults({ providers: [openrouterProvider] }),
				http.get("/api/providers/:id/usage", () =>
					HttpResponse.json({
						label: "OpenRouter",
						limit: null,
						limit_reset: null,
						limit_remaining: null,
						usage: 100000,
						usage_daily: 10000,
						usage_weekly: 50000,
						usage_monthly: 100000,
						credits_total: 1000000,
						credits_used: 100000,
						credits_remaining: 900000,
						is_free_tier: false,
					}),
				),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("OpenRouter")).toBeInTheDocument();
			});

			await waitFor(() => {
				const balanceBadge = screen.getByText(/\$900000/);
				expect(balanceBadge).toBeInTheDocument();
			});
			const balanceBadge = screen.getByText(/\$900000/);
			await user.click(balanceBadge);

			await waitFor(() => {
				expect(screen.getByRole("dialog")).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog");
			const closeButton = within(dialog).getByRole("button", {
				name: "Close",
			});
			await user.click(closeButton);

			await waitFor(() => {
				expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
			});
		});

		it("opens Z.ai Coding quota modal when clicking quota badge", async () => {
			const zaiProvider = {
				...mockProvider,
				id: "provider-zai",
				name: "Z.ai Coding",
				base_url: "https://api.z.ai/api/coding/paas/v4",
			};
			server.use(
				...mockProvidersPageDefaults({ providers: [zaiProvider] }),
				http.get("/api/providers/:id/usage", () =>
					HttpResponse.json({
						code: 0,
						msg: "success",
						success: true,
						data: {
							level: "basic",
							limits: [
								{
									type: "TOKENS_LIMIT",
									unit: 3,
									number: 10000,
									usage: 5000,
									currentValue: 5000,
									remaining: 5000,
									percentage: 50,
									nextResetTime: Date.now() + 5 * 60 * 60 * 1000,
								},
								{
									type: "TOKENS_LIMIT",
									unit: 6,
									number: 50000,
									usage: 25000,
									currentValue: 25000,
									remaining: 25000,
									percentage: 50,
									nextResetTime: Date.now() + 7 * 24 * 60 * 60 * 1000,
								},
							],
						},
					}),
				),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Z.ai Coding")).toBeInTheDocument();
			});

			// Wait for quota badge (shows "50%/50%" for 5-hour/weekly usage)
			await waitFor(() => {
				const badge = screen.getByText(/50%/);
				expect(badge).toBeInTheDocument();
			});
			const quotaBadge = screen.getByText(/50%/);
			await user.click(quotaBadge);

			await waitFor(() => {
				expect(screen.getByRole("dialog")).toBeInTheDocument();
			});
		});

		it("opens OpenRouter balance modal when clicking balance badge", async () => {
			const openrouterProvider = {
				...mockProvider,
				id: "provider-or",
				name: "OpenRouter",
				base_url: "https://openrouter.ai/api/v1",
			};
			server.use(
				...mockProvidersPageDefaults({ providers: [openrouterProvider] }),
				http.get("/api/providers/:id/usage", () =>
					HttpResponse.json({
						label: "OpenRouter",
						limit: null,
						limit_reset: null,
						limit_remaining: null,
						usage: 100000,
						usage_daily: 10000,
						usage_weekly: 50000,
						usage_monthly: 100000,
						credits_total: 1000000,
						credits_used: 100000,
						credits_remaining: 900000,
						is_free_tier: false,
					}),
				),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("OpenRouter")).toBeInTheDocument();
			});

			// Wait for balance badge (shows "$900000.00" for credits_remaining)
			await waitFor(() => {
				const balanceBadge = screen.getByText(/\$900000/);
				expect(balanceBadge).toBeInTheDocument();
			});
			const balanceBadge = screen.getByText(/\$900000/);
			await user.click(balanceBadge);

			await waitFor(() => {
				expect(screen.getByRole("dialog")).toBeInTheDocument();
			});
		});
	});

	describe("DeleteConfirmModal (lines 392-400)", () => {
		it("renders delete confirmation modal", async () => {
			const testProvider = {
				...mockProvider,
				id: "provider-test",
				name: "Test Provider",
			};
			server.use(...mockProvidersPageDefaults({ providers: [testProvider] }));

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			const deleteButton = screen.getByRole("button", {
				name: "Delete",
			});
			await user.click(deleteButton);

			// Modal should render
			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Delete provider" }),
				).toBeInTheDocument();
			});
			expect(
				screen.getByText(/Are you sure you want to delete/),
			).toBeInTheDocument();
			// Provider name appears in modal - query within dialog
			const dialog = screen.getByRole("dialog");
			expect(
				within(dialog).getByText(/Test Provider/, { exact: false }),
			).toBeInTheDocument();
		});

		it("calls onCancel when cancel button is clicked", async () => {
			const testProvider = {
				...mockProvider,
				id: "provider-test",
				name: "Test Provider",
			};
			server.use(...mockProvidersPageDefaults({ providers: [testProvider] }));

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			const deleteButton = screen.getByRole("button", {
				name: "Delete",
			});
			await user.click(deleteButton);

			// Click cancel
			const cancelButton = screen.getByRole("button", { name: "Cancel" });
			await user.click(cancelButton);

			// Modal should close
			await waitFor(() => {
				expect(
					screen.queryByRole("heading", { name: "Delete provider" }),
				).not.toBeInTheDocument();
			});
		});

		it("shows provider name in delete confirmation", async () => {
			const testProvider = {
				...mockProvider,
				id: "provider-custom",
				name: "My Custom Provider",
			};
			server.use(...mockProvidersPageDefaults({ providers: [testProvider] }));

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("My Custom Provider")).toBeInTheDocument();
			});

			const deleteButton = screen.getByRole("button", {
				name: "Delete",
			});
			await user.click(deleteButton);

			// Wait for modal and check provider name appears in dialog
			await waitFor(() => {
				const dialog = screen.getByRole("dialog");
				expect(
					within(dialog).getByText(/My Custom Provider/),
				).toBeInTheDocument();
			});
		});
	});

	describe("filtering and sorting", () => {
		it("filters providers by name", async () => {
			const providers = [
				{ ...mockProvider, id: "p1", name: "Alpha Provider" },
				{ ...mockProvider, id: "p2", name: "Beta Provider" },
				{ ...mockProvider, id: "p3", name: "Gamma Provider" },
			];
			server.use(...mockProvidersPageDefaults({ providers }));

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Alpha Provider")).toBeInTheDocument();
			});

			const filterInput = screen.getByPlaceholderText(/Filter providers/);
			await user.type(filterInput, "Beta");

			await waitFor(() => {
				expect(screen.getByText("Beta Provider")).toBeInTheDocument();
				expect(screen.queryByText("Alpha Provider")).not.toBeInTheDocument();
			});
		});

		it("filters providers by type", async () => {
			const providers = [
				{
					...mockProvider,
					id: "p1",
					name: "OpenAI",
					base_url: "https://api.openai.com/v1",
				},
				{
					...mockProvider,
					id: "p2",
					name: "Anthropic",
					base_url: "https://api.anthropic.com",
				},
			];
			server.use(...mockProvidersPageDefaults({ providers }));

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("OpenAI")).toBeInTheDocument();
			});

			// Click the type filter button (shows "Provider type" placeholder text)
			const typeFilter = screen.getByRole("button", {
				name: /Provider type/,
			});
			await user.click(typeFilter);

			// Select OpenAI type from the dropdown (button shows display label "OpenAI" with count)
			const openaiOption = screen.getByRole("button", { name: /OpenAI/ });
			await user.click(openaiOption);

			await waitFor(() => {
				// Filter is applied - the filter button should show "OpenAI" as selected
				const typeFilterButton = screen.getByRole("button", {
					name: /OpenAI/,
				});
				expect(typeFilterButton).toBeInTheDocument();
			});

			// Verify Anthropic provider card is not visible (only OpenAI should be shown)
			expect(screen.queryByText("Anthropic")).not.toBeInTheDocument();
		});

		it("sorts providers alphabetically", async () => {
			const providers = [
				{ ...mockProvider, id: "p1", name: "Zebra Provider" },
				{ ...mockProvider, id: "p2", name: "Alpha Provider" },
			];
			server.use(...mockProvidersPageDefaults({ providers }));

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Zebra Provider")).toBeInTheDocument();
			});

			// Initial sort should be A-Z
			const sortButton = screen.getByRole("button", {
				name: /Sorted A-Z/,
			});
			expect(sortButton).toBeInTheDocument();

			// Click to reverse to Z-A
			await user.click(sortButton);

			// Now should be Z-A
			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: /Sorted Z-A/ }),
				).toBeInTheDocument();
			});
		});
	});

	describe("empty states", () => {
		it("shows empty state when no providers exist", async () => {
			server.use(...mockProvidersPageDefaults({ providers: [] }));

			renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText(/No providers configured/)).toBeInTheDocument();
			});
		});
	});

	it("shows filter empty state when no providers match filter", async () => {
		const providers = [{ ...mockProvider, id: "p1", name: "Alpha Provider" }];
		server.use(...mockProvidersPageDefaults({ providers }));

		const { user } = renderWithProviders(<Providers />);

		await waitFor(() => {
			expect(screen.getByText("Alpha Provider")).toBeInTheDocument();
		});

		const filterInput = screen.getByPlaceholderText(/Filter providers/);
		await user.type(filterInput, "nonexistent");

		await waitFor(() => {
			expect(
				screen.getByText(/No providers match the selected filter/),
			).toBeInTheDocument();
		});
	});

	describe("modal interactions", () => {
		it("opens Add Provider modal when clicking + Add Provider button", async () => {
			server.use(...mockProvidersPageDefaults());

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			const addButton = screen.getByRole("button", {
				name: "+ Add Provider",
			});
			await user.click(addButton);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Add Provider" }),
				).toBeInTheDocument();
			});
		});

		it("opens Edit Provider modal when clicking Edit button", async () => {
			server.use(...mockProvidersPageDefaults());

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			const editButton = screen.getByRole("button", {
				name: "Edit",
			});
			await user.click(editButton);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Edit Provider" }),
				).toBeInTheDocument();
			});
		});
	});
});
