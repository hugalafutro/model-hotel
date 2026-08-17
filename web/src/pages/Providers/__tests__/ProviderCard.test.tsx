import { fireEvent, render, screen } from "@testing-library/react";
import i18next from "i18next";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
	DeepSeekBalance,
	KimiCodeQuotaResponse,
	MiniMaxQuotaResponse,
	NanoGPTUsage,
	OllamaCloudAccount,
	OpenRouterBalance,
	Provider,
	ZAICodingQuotaLimit,
	ZAICodingQuotaResponse,
} from "../../../api/types";
import type { useQuotaData } from "../../../hooks/useQuotaData";
import { AllProviders } from "../../../test/utils";
import { ProviderCard } from "../ProviderCard";

// The real English copy for this key ships in a later i18n task; captured at
// module load (before any test mutates it) so the "scheduled disable
// indicator" suite below can restore it exactly and a later sanity test can
// confirm the restore actually took.
const SCHEDULED_DISABLE_TOOLTIP_KEY =
	"providers.scheduled_disable_card_tooltip";
const originalScheduledDisableTooltip: string | undefined = i18next.getResource(
	"en",
	"translation",
	SCHEDULED_DISABLE_TOOLTIP_KEY,
);

const mockProvider: Provider = {
	id: "provider-001",
	name: "Test Provider",
	base_url: "https://api.test-provider.com/v1",
	provider_type: "custom",
	masked_key: "sk_test_••••••••••••••••••••••••",
	enabled: true,
	autodiscovery_enabled: true,
	scheduled_disable_on: null,
	last_discovered_at: "2026-05-10T12:00:00Z",
	last_used_at: "2026-05-11T08:30:00Z",
	created_at: "2026-01-15T10:00:00Z",
	updated_at: "2026-05-10T12:00:00Z",
	model_count: 5,
	total_tokens: 1250000,
};

const mockQuotaData = {
	refetchDeepseek: vi.fn().mockResolvedValue(undefined),
	refetchOllamaCloud: vi.fn().mockResolvedValue(undefined),
} as unknown as ReturnType<typeof useQuotaData>;

const defaultProps = {
	provider: mockProvider,
	modelCount: 5,
	quotaData: mockQuotaData,
	discoveringId: null,
	discoverAllCurrentId: null,
	discoverAllIsPending: false,
	onEdit: vi.fn(),
	onDiscover: vi.fn(),
	onDelete: vi.fn(),
	onSetModelsProvider: vi.fn(),
	onSetModalNano: vi.fn(),
	onSetModalZaiCoding: vi.fn(),
	onSetModalKimiCode: vi.fn(),
	onSetModalMiniMax: vi.fn(),
	onSetModalOpenRouter: vi.fn(),
	onSetModalNeuralwatt: vi.fn(),
	toast: vi.fn(),
};

describe("ProviderCard", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe("rendering provider info", () => {
		it("renders provider name", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			expect(screen.getByText("Test Provider")).toBeInTheDocument();
		});

		it("renders API base URL", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			expect(
				screen.getByText("https://api.test-provider.com/v1"),
			).toBeInTheDocument();
		});

		it("renders model count", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			expect(screen.getByText(/5 model/)).toBeInTheDocument();
		});

		it("renders singular 'model' when count is 1", () => {
			render(<ProviderCard {...defaultProps} modelCount={1} />, {
				wrapper: AllProviders,
			});

			expect(screen.getByText(/1 model/)).toBeInTheDocument();
		});

		it("renders total tokens", () => {
			const { container } = render(<ProviderCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			expect(container.textContent).toContain("1.3M tokens");
		});

		it("does not render tokens when zero", () => {
			const providerNoTokens: Provider = {
				...mockProvider,
				total_tokens: 0,
			};

			render(<ProviderCard {...defaultProps} provider={providerNoTokens} />, {
				wrapper: AllProviders,
			});

			expect(screen.queryByText(/tokens/)).not.toBeInTheDocument();
		});

		it("renders created timestamp", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			expect(screen.getByText("Created")).toBeInTheDocument();
			// Date should be formatted (just check something is there)
			expect(screen.getAllByText(/2026/)).toHaveLength(3); // created, updated, and last discovery
		});

		it("renders masked API key", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			expect(screen.getByText("API Key")).toBeInTheDocument();
			expect(
				screen.getByText("sk_test_••••••••••••••••••••••••"),
			).toBeInTheDocument();
		});

		it("renders last used timestamp", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			expect(screen.getByText("Last Used")).toBeInTheDocument();
		});

		it("renders N/A for last used when null", () => {
			const providerNoLastUsed = {
				...mockProvider,
				last_used_at: null,
			} as Provider;

			render(<ProviderCard {...defaultProps} provider={providerNoLastUsed} />, {
				wrapper: AllProviders,
			});

			expect(screen.getByText("N/A")).toBeInTheDocument();
		});

		it("renders last discovery timestamp", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			expect(screen.getByText("Last Discovery")).toBeInTheDocument();
		});

		it("does not render last discovery when null", () => {
			const providerNoDiscovery = {
				...mockProvider,
				last_discovered_at: null,
			} as Provider;

			render(
				<ProviderCard {...defaultProps} provider={providerNoDiscovery} />,
				{ wrapper: AllProviders },
			);

			expect(screen.queryByText("Last Discovery")).not.toBeInTheDocument();
		});
	});

	describe("status indicator", () => {
		it("does not render disabled badge when enabled", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			expect(screen.queryByText("Disabled")).not.toBeInTheDocument();
		});

		it("renders disabled badge when disabled", () => {
			const disabledProvider: Provider = {
				...mockProvider,
				enabled: false,
			};

			render(<ProviderCard {...defaultProps} provider={disabledProvider} />, {
				wrapper: AllProviders,
			});

			expect(screen.getByText("Disabled")).toBeInTheDocument();
		});

		it("grayscales inert content but not working controls when disabled", () => {
			const disabledProvider: Provider = {
				...mockProvider,
				enabled: false,
			};

			render(<ProviderCard {...defaultProps} provider={disabledProvider} />, {
				wrapper: AllProviders,
			});

			// The card container is no longer uniformly faded.
			const card = screen.getByText("Test Provider").closest(".ui-card");
			expect(card).not.toHaveClass("opacity-50");

			// Informational content is grayscale + dimmed.
			const stats = screen.getByText("API Key").closest(".space-y-2");
			expect(stats).toHaveClass("grayscale", "opacity-50");
			const tokensBadge = screen.getByText(/tokens/).closest(".ui-badge");
			expect(tokensBadge).toHaveClass("grayscale", "opacity-50");

			// The status badge pops as a warning instead of blending in.
			const disabledBadge = screen.getByText("Disabled").closest(".ui-badge");
			expect(disabledBadge).toHaveClass("ui-badge-warning");
			expect(disabledBadge).not.toHaveClass("grayscale");

			// Controls that still work keep full color.
			const editButton = screen.getByText("Edit").closest("button");
			expect(editButton).not.toHaveClass("grayscale");
			const deleteButton = screen.getByText("Delete").closest("button");
			expect(deleteButton).not.toHaveClass("grayscale");
		});

		it("dims the model count and hides quota badges when disabled", () => {
			mockQuotaData.showNanoBadge = true;
			mockQuotaData.nanogptUsage = {
				weeklyInputTokens: { used: 1000 },
			} as NanoGPTUsage;
			mockQuotaData.nanoWeeklyUsed = 1000;
			mockQuotaData.nanoWeeklyLimit = 10000;

			try {
				render(
					<ProviderCard
						{...defaultProps}
						provider={{
							...mockProvider,
							enabled: false,
							base_url: "https://api.nano-gpt.com/v1",
							provider_type: "nanogpt",
						}}
					/>,
					{ wrapper: AllProviders },
				);

				// Model count is live data (and the only route into the provider's
				// model list), so it stays visible but dimmed.
				const modelsButton = screen.getByText(/5 model/).closest("button");
				expect(modelsButton).toHaveClass("grayscale", "opacity-50");
				// Quota data would only show the stale pre-disable value.
				expect(
					screen.queryByTitle(
						"NanoGPT weekly tokens remaining - click for details",
					),
				).not.toBeInTheDocument();
			} finally {
				// mockQuotaData is module-level shared state; undo the mutation so
				// later tests in this file see the defaults.
				mockQuotaData.showNanoBadge = false;
				mockQuotaData.nanogptUsage = undefined;
				mockQuotaData.nanoWeeklyUsed = null;
				mockQuotaData.nanoWeeklyLimit = null;
			}
		});

		it("does not grayscale content when enabled", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			const stats = screen.getByText("API Key").closest(".space-y-2");
			expect(stats).not.toHaveClass("grayscale");
			expect(stats).not.toHaveClass("opacity-50");
		});

		it("applies red tint when enabled but autodiscovery disabled", () => {
			const providerNoAutodiscovery: Provider = {
				...mockProvider,
				enabled: true,
				autodiscovery_enabled: false,
			};

			render(
				<ProviderCard {...defaultProps} provider={providerNoAutodiscovery} />,
				{
					wrapper: AllProviders,
				},
			);

			const card = screen.getByText("Test Provider").closest(".ui-card");
			expect(card).toHaveClass("border-red-500/20");
			expect(card).toHaveClass("bg-red-500/[0.03]");
		});

		it("does not apply red tint when autodiscovery is enabled", () => {
			const providerWithAutodiscovery: Provider = {
				...mockProvider,
				enabled: true,
				autodiscovery_enabled: true,
			};

			render(
				<ProviderCard {...defaultProps} provider={providerWithAutodiscovery} />,
				{
					wrapper: AllProviders,
				},
			);

			const card = screen.getByText("Test Provider").closest(".ui-card");
			expect(card).not.toHaveClass("border-red-500/20");
			expect(card).not.toHaveClass("bg-red-500/[0.03]");
		});

		it("does not apply red tint when disabled (even if autodiscovery also false)", () => {
			const providerDisabledNoAuto: Provider = {
				...mockProvider,
				enabled: false,
				autodiscovery_enabled: false,
			};

			render(
				<ProviderCard {...defaultProps} provider={providerDisabledNoAuto} />,
				{
					wrapper: AllProviders,
				},
			);

			const card = screen.getByText("Test Provider").closest(".ui-card");
			expect(card).not.toHaveClass("border-red-500/20");
			expect(card).not.toHaveClass("bg-red-500/[0.03]");
		});

		it("renders 'Autodiscovery Off' badge when enabled but autodiscovery disabled", () => {
			const providerNoAutodiscovery: Provider = {
				...mockProvider,
				enabled: true,
				autodiscovery_enabled: false,
			};

			render(
				<ProviderCard {...defaultProps} provider={providerNoAutodiscovery} />,
				{
					wrapper: AllProviders,
				},
			);

			expect(screen.getByText("Autodiscovery Off")).toBeInTheDocument();
		});

		it("does not render 'Autodiscovery Off' badge when autodiscovery is enabled", () => {
			const providerWithAutodiscovery: Provider = {
				...mockProvider,
				enabled: true,
				autodiscovery_enabled: true,
			};

			render(
				<ProviderCard {...defaultProps} provider={providerWithAutodiscovery} />,
				{
					wrapper: AllProviders,
				},
			);

			expect(screen.queryByText("Autodiscovery Off")).not.toBeInTheDocument();
		});

		it("does not render 'Autodiscovery Off' badge when provider is disabled", () => {
			const providerDisabled: Provider = {
				...mockProvider,
				enabled: false,
				autodiscovery_enabled: false,
			};

			render(<ProviderCard {...defaultProps} provider={providerDisabled} />, {
				wrapper: AllProviders,
			});

			expect(screen.queryByText("Autodiscovery Off")).not.toBeInTheDocument();
		});
	});

	describe("model count button", () => {
		it("renders model count button", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			const modelButton = screen.getByText(/5 model/).closest("button");
			expect(modelButton).toBeInTheDocument();
		});

		it("calls onSetModelsProvider when model count button is clicked", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			const modelButton = screen.getByText(/5 model/);
			fireEvent.click(modelButton);

			expect(defaultProps.onSetModelsProvider).toHaveBeenCalledWith(
				mockProvider,
			);
		});

		it("does not render model count button when modelCount is 0", () => {
			render(<ProviderCard {...defaultProps} modelCount={0} />, {
				wrapper: AllProviders,
			});

			expect(screen.queryByText("0 models")).not.toBeInTheDocument();
		});
	});

	describe("action buttons", () => {
		it("renders Edit button", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			expect(screen.getByText("Edit")).toBeInTheDocument();
		});

		it("calls onEdit when Edit is clicked", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			const editButton = screen.getByText("Edit");
			fireEvent.click(editButton);

			expect(defaultProps.onEdit).toHaveBeenCalledWith(mockProvider);
		});

		it("renders Discover Models button", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			expect(screen.getByText("Discover Models")).toBeInTheDocument();
		});

		it("calls onDiscover when Discover Models is clicked", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			const discoverButton = screen.getByText("Discover Models");
			fireEvent.click(discoverButton);

			expect(defaultProps.onDiscover).toHaveBeenCalledWith("provider-001");
		});

		it("disables Discover Models when discovering", () => {
			render(
				<ProviderCard
					{...defaultProps}
					discoveringId="provider-001"
					discoverAllIsPending={false}
				/>,
				{ wrapper: AllProviders },
			);

			const discoverButton = screen.getByText("Discovering...");
			expect(discoverButton).toBeDisabled();
		});

		it("disables Discover Models when discoverAll is pending", () => {
			render(
				<ProviderCard
					{...defaultProps}
					discoveringId={null}
					discoverAllIsPending={true}
				/>,
				{ wrapper: AllProviders },
			);

			const discoverButton = screen.getByText("Discover Models");
			expect(discoverButton).toBeDisabled();
		});

		it("disables Discover Models when autodiscovery is disabled", () => {
			const providerNoAutodiscovery: Provider = {
				...mockProvider,
				enabled: true,
				autodiscovery_enabled: false,
			};

			render(
				<ProviderCard
					{...defaultProps}
					provider={providerNoAutodiscovery}
					discoveringId={null}
					discoverAllIsPending={false}
				/>,
				{ wrapper: AllProviders },
			);

			const discoverButton = screen.getByText("Discover Models");
			expect(discoverButton).toBeDisabled();
		});

		it("enables Discover Models when both enabled and autodiscovery enabled", () => {
			const providerEnabled: Provider = {
				...mockProvider,
				enabled: true,
				autodiscovery_enabled: true,
			};

			render(
				<ProviderCard
					{...defaultProps}
					provider={providerEnabled}
					discoveringId={null}
					discoverAllIsPending={false}
				/>,
				{ wrapper: AllProviders },
			);

			const discoverButton = screen.getByText("Discover Models");
			expect(discoverButton).not.toBeDisabled();
		});

		it("shows Discovering when discovering this provider", () => {
			render(
				<ProviderCard
					{...defaultProps}
					discoveringId="provider-001"
					discoverAllCurrentId="provider-001"
				/>,
				{ wrapper: AllProviders },
			);

			expect(screen.getByText("Discovering...")).toBeInTheDocument();
		});

		it("renders Delete button", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			expect(screen.getByText("Delete")).toBeInTheDocument();
		});

		it("calls onDelete when Delete is clicked", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			const deleteButton = screen.getByText("Delete");
			fireEvent.click(deleteButton);

			expect(defaultProps.onDelete).toHaveBeenCalledWith(mockProvider);
		});
	});

	describe("QuotaBadges", () => {
		it("renders QuotaBadges component", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			// QuotaBadges should render within the card
			const card = screen.getByText("Test Provider").closest(".ui-card");
			expect(card).toBeInTheDocument();
		});
	});

	describe("QuotaBadges click handlers", () => {
		it("calls onSetModalNano when NanoGPT badge is clicked", () => {
			mockQuotaData.showNanoBadge = true;
			mockQuotaData.nanogptUsage = {
				weeklyInputTokens: { used: 1000 },
			} as NanoGPTUsage;
			mockQuotaData.nanoWeeklyUsed = 1000;
			mockQuotaData.nanoWeeklyLimit = 10000;

			render(
				<ProviderCard
					{...defaultProps}
					provider={{
						...mockProvider,
						base_url: "https://api.nano-gpt.com/v1",
						provider_type: "nanogpt",
					}}
				/>,
				{ wrapper: AllProviders },
			);

			const nanoBadge = screen.getByTitle(
				"NanoGPT weekly tokens remaining - click for details",
			);
			fireEvent.click(nanoBadge);

			expect(defaultProps.onSetModalNano).toHaveBeenCalled();
		});

		it("does not call onSetModalNano when nanogptUsage is falsy", () => {
			mockQuotaData.showNanoBadge = true;
			mockQuotaData.nanogptUsage = undefined;
			mockQuotaData.nanoWeeklyUsed = null;
			mockQuotaData.nanoWeeklyLimit = null;

			render(
				<ProviderCard
					{...defaultProps}
					provider={{
						...mockProvider,
						base_url: "https://api.nano-gpt.com/v1",
						provider_type: "nanogpt",
					}}
				/>,
				{ wrapper: AllProviders },
			);

			const nanoBadge = screen.queryByTitle(
				"NanoGPT weekly tokens remaining - click for details",
			);
			expect(nanoBadge).not.toBeInTheDocument();
			expect(defaultProps.onSetModalNano).not.toHaveBeenCalled();
		});

		it("calls onSetModalZaiCoding when ZAI Coding badge is clicked", () => {
			mockQuotaData.showZaiCodingBadge = true;
			mockQuotaData.zaiCodingUsage = {
				success: true,
			} as ZAICodingQuotaResponse;
			mockQuotaData.zaiCodingFiveHour = {
				percentage: 50,
			} as ZAICodingQuotaLimit;
			mockQuotaData.zaiCodingWeekly = {
				percentage: 30,
			} as ZAICodingQuotaLimit;

			render(
				<ProviderCard
					{...defaultProps}
					provider={{ ...mockProvider, base_url: "https://api.z.ai/v1" }}
				/>,
				{ wrapper: AllProviders },
			);

			const zaiBadge = screen.getByTitle(
				"Z.ai Coding remaining quota - click for details",
			);
			fireEvent.click(zaiBadge);

			expect(defaultProps.onSetModalZaiCoding).toHaveBeenCalled();
		});

		it("calls onSetModalKimiCode when Kimi Code badge is clicked", () => {
			mockQuotaData.showKimiCodeBadge = true;
			mockQuotaData.kimiCodeUsage = {
				success: true,
			} as unknown as KimiCodeQuotaResponse;

			render(
				<ProviderCard
					{...defaultProps}
					provider={{
						...mockProvider,
						base_url: "https://api.kimi.com/coding/v1",
						provider_type: "kimi-code",
					}}
				/>,
				{ wrapper: AllProviders },
			);

			const kimiBadge = screen.getByTitle(
				"Kimi Code remaining quota - click for details",
			);
			fireEvent.click(kimiBadge);

			expect(defaultProps.onSetModalKimiCode).toHaveBeenCalled();
		});

		it("calls onSetModalMiniMax when MiniMax badge is clicked", () => {
			mockQuotaData.showMiniMaxBadge = true;
			mockQuotaData.minimaxUsage = {
				model_remains: [
					{
						model_name: "general",
						remains_time: 1000,
						weekly_remains_time: 2000,
						current_interval_status: 1,
						current_interval_remaining_percent: 80,
						current_weekly_status: 1,
						current_weekly_remaining_percent: 60,
					},
				],
				base_resp: { status_code: 0, status_msg: "success" },
			} as unknown as MiniMaxQuotaResponse;

			render(
				<ProviderCard
					{...defaultProps}
					provider={{
						...mockProvider,
						base_url: "https://api.minimax.io/v1",
						provider_type: "minimax",
					}}
				/>,
				{ wrapper: AllProviders },
			);

			const minimaxBadge = screen.getByTitle(
				"MiniMax remaining quota - click for details",
			);
			fireEvent.click(minimaxBadge);

			expect(defaultProps.onSetModalMiniMax).toHaveBeenCalled();
		});

		it("calls refetchDeepseek and toasts success when DeepSeek badge is clicked", async () => {
			mockQuotaData.showDsBadge = true;
			mockQuotaData.deepseekBalance = {
				balance_infos: [{ currency: "USD", total_balance: "10.00" }],
			} as DeepSeekBalance;

			render(
				<ProviderCard
					{...defaultProps}
					provider={{
						...mockProvider,
						base_url: "https://api.deepseek.com/v1",
						provider_type: "deepseek",
					}}
				/>,
				{ wrapper: AllProviders },
			);

			const deepseekBadge = screen.getByTitle(/DeepSeek balance/);
			fireEvent.click(deepseekBadge);

			await vi.waitFor(() => {
				expect(mockQuotaData.refetchDeepseek).toHaveBeenCalled();
				expect(defaultProps.toast).toHaveBeenCalledWith(
					"Balance refreshed",
					"success",
				);
			});
		});

		it("toasts error when DeepSeek refetch fails", async () => {
			mockQuotaData.showDsBadge = true;
			mockQuotaData.deepseekBalance = {
				balance_infos: [{ currency: "USD", total_balance: "10.00" }],
			} as DeepSeekBalance;
			mockQuotaData.refetchDeepseek = vi
				.fn()
				.mockRejectedValue(new Error("fail"));

			render(
				<ProviderCard
					{...defaultProps}
					provider={{
						...mockProvider,
						base_url: "https://api.deepseek.com/v1",
						provider_type: "deepseek",
					}}
				/>,
				{ wrapper: AllProviders },
			);

			const deepseekBadge = screen.getByTitle(/DeepSeek balance/);
			fireEvent.click(deepseekBadge);

			await vi.waitFor(() => {
				expect(defaultProps.toast).toHaveBeenCalledWith(
					"Failed to refresh balance",
					"error",
				);
			});
		});

		it("calls onSetModalOpenRouter when OpenRouter badge is clicked", () => {
			mockQuotaData.showOrBadge = true;
			mockQuotaData.openrouterBalance = {
				credits_remaining: 5.0,
			} as OpenRouterBalance;

			render(
				<ProviderCard
					{...defaultProps}
					provider={{ ...mockProvider, base_url: "https://openrouter.ai/v1" }}
				/>,
				{ wrapper: AllProviders },
			);

			const openrouterBadge = screen.getByTitle(
				"OpenRouter key balance - click for details",
			);
			fireEvent.click(openrouterBadge);

			expect(defaultProps.onSetModalOpenRouter).toHaveBeenCalledWith();
		});

		it("calls refetchOllamaCloud and toasts success when Ollama Cloud badge is clicked", async () => {
			mockQuotaData.showOllamaCloudBadge = true;
			mockQuotaData.ollamaCloudAccount = {
				plan: "pro",
			} as OllamaCloudAccount;

			render(
				<ProviderCard
					{...defaultProps}
					provider={{ ...mockProvider, base_url: "https://api.ollama.com/v1" }}
				/>,
				{ wrapper: AllProviders },
			);

			const ollamaBadge = screen.getByTitle(/Ollama Cloud/);
			fireEvent.click(ollamaBadge);

			await vi.waitFor(() => {
				expect(mockQuotaData.refetchOllamaCloud).toHaveBeenCalled();
				expect(defaultProps.toast).toHaveBeenCalledWith(
					"Account info refreshed",
					"success",
				);
			});
		});

		it("toasts error when Ollama Cloud refetch fails", async () => {
			mockQuotaData.showOllamaCloudBadge = true;
			mockQuotaData.ollamaCloudAccount = {
				plan: "pro",
			} as OllamaCloudAccount;
			mockQuotaData.refetchOllamaCloud = vi
				.fn()
				.mockRejectedValue(new Error("fail"));

			render(
				<ProviderCard
					{...defaultProps}
					provider={{ ...mockProvider, base_url: "https://api.ollama.com/v1" }}
				/>,
				{ wrapper: AllProviders },
			);

			const ollamaBadge = screen.getByTitle(/Ollama Cloud/);
			fireEvent.click(ollamaBadge);

			await vi.waitFor(() => {
				expect(defaultProps.toast).toHaveBeenCalledWith(
					"Failed to refresh account info",
					"error",
				);
			});
		});
	});

	describe("scheduled disable indicator", () => {
		// This suite must not depend on when the real copy for
		// SCHEDULED_DISABLE_TOOLTIP_KEY lands (a later i18n task), so
		// (following the src/utils/__tests__/format.test.ts countLabel
		// convention) it registers its own value for the exact key the
		// component calls and asserts against that. Vitest does not isolate
		// the i18next singleton between describe blocks in this file, so the
		// override is undone in afterEach: it restores the value captured at
		// module load (see originalScheduledDisableTooltip above), leaving no
		// shadowing behind for later tests in this file (verified by the
		// "restores the i18next tooltip resource" test right after this
		// describe block).
		beforeEach(() => {
			i18next.addResourceBundle(
				"en",
				"translation",
				{
					providers: {
						scheduled_disable_card_tooltip: "Scheduled to disable on {{date}}",
					},
				},
				true,
				true,
			);
		});

		afterEach(() => {
			if (originalScheduledDisableTooltip === undefined) {
				const data = i18next.getDataByLanguage("en") as
					| { translation?: { providers?: Record<string, unknown> } }
					| undefined;
				delete data?.translation?.providers?.scheduled_disable_card_tooltip;
			} else {
				i18next.addResource(
					"en",
					"translation",
					SCHEDULED_DISABLE_TOOLTIP_KEY,
					originalScheduledDisableTooltip,
				);
			}
		});

		it("shows the scheduled-disable icon with a dated tooltip when scheduled", () => {
			const scheduledProvider: Provider = {
				...mockProvider,
				scheduled_disable_on: "2030-06-20",
			};

			render(<ProviderCard {...defaultProps} provider={scheduledProvider} />, {
				wrapper: AllProviders,
			});

			const icon = screen.getByTestId("scheduled-disable-icon");
			expect(icon).toBeInTheDocument();
			// Locale-independent: assert the title contains the same string formatDate produces.
			expect(icon.getAttribute("title")).toContain(
				new Date("2030-06-20T00:00:00").toLocaleDateString(undefined, {
					day: "numeric",
					month: "short",
					year: "numeric",
				}),
			);
		});

		it("shows no icon when nothing is scheduled", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			expect(screen.queryByTestId("scheduled-disable-icon")).toBeNull();
		});

		it("shows no icon when scheduled but the provider is disabled", () => {
			const scheduledDisabledProvider: Provider = {
				...mockProvider,
				enabled: false,
				scheduled_disable_on: "2030-06-20",
			};

			render(
				<ProviderCard {...defaultProps} provider={scheduledDisabledProvider} />,
				{ wrapper: AllProviders },
			);

			expect(screen.queryByTestId("scheduled-disable-icon")).toBeNull();
		});
	});

	// Outside the "scheduled disable indicator" describe on purpose: its own
	// beforeEach/afterEach don't apply here, so this observes the ambient
	// i18next state left behind once that suite has finished. Regression
	// check for the resource-bundle override in that suite actually being
	// undone rather than permanently shadowing the real key for the rest of
	// this file's run.
	it("restores the i18next tooltip resource after the scheduled disable indicator suite", () => {
		expect(
			i18next.getResource("en", "translation", SCHEDULED_DISABLE_TOOLTIP_KEY),
		).toBe(originalScheduledDisableTooltip);
	});

	describe("copyable pills", () => {
		it("renders provider name as copyable pill", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			const nameElement = screen.getByText("Test Provider");
			expect(nameElement).toBeInTheDocument();
			// The button containing the text has the tooltip
			const button = nameElement.closest("button");
			expect(button).toHaveAttribute("title", "Test Provider");
		});

		it("renders base URL as copyable pill", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			const urlElement = screen.getByText("https://api.test-provider.com/v1");
			expect(urlElement).toBeInTheDocument();
			// The button containing the text has the tooltip
			const button = urlElement.closest("button");
			expect(button).toHaveAttribute(
				"title",
				"https://api.test-provider.com/v1",
			);
		});
	});
});
