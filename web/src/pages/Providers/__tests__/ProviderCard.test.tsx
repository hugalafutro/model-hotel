import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Provider } from "../../../api/types";
import type { useQuotaData } from "../../../hooks/useQuotaData";
import { AllProviders } from "../../../test/utils";
import { ProviderCard } from "../ProviderCard";

const mockProvider: Provider = {
	id: "provider-001",
	name: "Test Provider",
	base_url: "https://api.test-provider.com/v1",
	masked_key: "sk_test_••••••••••••••••••••••••",
	enabled: true,
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
	onSetModalOpenRouter: vi.fn(),
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

			expect(screen.getByText("5 models")).toBeInTheDocument();
		});

		it("renders singular 'model' when count is 1", () => {
			render(<ProviderCard {...defaultProps} modelCount={1} />, {
				wrapper: AllProviders,
			});

			expect(screen.getByText("1 model")).toBeInTheDocument();
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

		it("applies opacity when disabled", () => {
			const disabledProvider: Provider = {
				...mockProvider,
				enabled: false,
			};

			render(<ProviderCard {...defaultProps} provider={disabledProvider} />, {
				wrapper: AllProviders,
			});

			const card = screen.getByText("Test Provider").closest(".ui-card");
			expect(card).toHaveClass("opacity-50");
		});
	});

	describe("model count button", () => {
		it("renders model count button", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			const modelButton = screen.getByText("5 models").closest("button");
			expect(modelButton).toBeInTheDocument();
		});

		it("calls onSetModelsProvider when model count button is clicked", () => {
			render(<ProviderCard {...defaultProps} />, { wrapper: AllProviders });

			const modelButton = screen.getByText("5 models");
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
			// eslint-disable-next-line @typescript-eslint/no-explicit-any
			mockQuotaData.nanogptUsage = { weeklyInputTokens: { used: 1000 } } as any;
			mockQuotaData.nanoWeeklyUsed = 1000;
			mockQuotaData.nanoWeeklyLimit = 10000;

			render(
				<ProviderCard
					{...defaultProps}
					provider={{
						...mockProvider,
						base_url: "https://api.nano-gpt.com/v1",
					}}
				/>,
				{ wrapper: AllProviders },
			);

			const nanoBadge = screen.getByTitle(
				"NanoGPT weekly token quota - click for details",
			);
			fireEvent.click(nanoBadge);

			expect(defaultProps.onSetModalNano).toHaveBeenCalledWith({
				weeklyInputTokens: { used: 1000 },
			});
		});

		it("does not call onSetModalNano when nanogptUsage is falsy", () => {
			mockQuotaData.showNanoBadge = true;
			mockQuotaData.nanogptUsage = null;
			mockQuotaData.nanoWeeklyUsed = null;
			mockQuotaData.nanoWeeklyLimit = null;

			render(
				<ProviderCard
					{...defaultProps}
					provider={{
						...mockProvider,
						base_url: "https://api.nano-gpt.com/v1",
					}}
				/>,
				{ wrapper: AllProviders },
			);

			const nanoBadge = screen.queryByTitle(
				"NanoGPT weekly token quota - click for details",
			);
			expect(nanoBadge).not.toBeInTheDocument();
			expect(defaultProps.onSetModalNano).not.toHaveBeenCalled();
		});

		it("calls onSetModalZaiCoding when ZAI Coding badge is clicked", () => {
			mockQuotaData.showZaiCodingBadge = true;
			// eslint-disable-next-line @typescript-eslint/no-explicit-any
			mockQuotaData.zaiCodingUsage = { success: true } as any;
			// eslint-disable-next-line @typescript-eslint/no-explicit-any
			mockQuotaData.zaiCodingFiveHour = { percentage: 50 } as any;
			// eslint-disable-next-line @typescript-eslint/no-explicit-any
			mockQuotaData.zaiCodingWeekly = { percentage: 30 } as any;

			render(
				<ProviderCard
					{...defaultProps}
					provider={{ ...mockProvider, base_url: "https://api.z.ai/v1" }}
				/>,
				{ wrapper: AllProviders },
			);

			const zaiBadge = screen.getByTitle(
				"Z.ai Coding Plan token quota - click for details",
			);
			fireEvent.click(zaiBadge);

			expect(defaultProps.onSetModalZaiCoding).toHaveBeenCalledWith({
				success: true,
			});
		});

		it("calls refetchDeepseek and toasts success when DeepSeek badge is clicked", async () => {
			mockQuotaData.showDsBadge = true;
			mockQuotaData.deepseekBalance = {
				balance_infos: [{ currency: "USD", total_balance: "10.00" }],
				// eslint-disable-next-line @typescript-eslint/no-explicit-any
			} as any;

			render(
				<ProviderCard
					{...defaultProps}
					provider={{
						...mockProvider,
						base_url: "https://api.deepseek.com/v1",
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
				// eslint-disable-next-line @typescript-eslint/no-explicit-any
			} as any;
			mockQuotaData.refetchDeepseek = vi
				.fn()
				.mockRejectedValue(new Error("fail"));

			render(
				<ProviderCard
					{...defaultProps}
					provider={{
						...mockProvider,
						base_url: "https://api.deepseek.com/v1",
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
			// eslint-disable-next-line @typescript-eslint/no-explicit-any
			mockQuotaData.openrouterBalance = { credits_remaining: 5.0 } as any;

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

			expect(defaultProps.onSetModalOpenRouter).toHaveBeenCalledWith({
				credits_remaining: 5.0,
			});
		});

		it("calls refetchOllamaCloud and toasts success when Ollama Cloud badge is clicked", async () => {
			mockQuotaData.showOllamaCloudBadge = true;
			// eslint-disable-next-line @typescript-eslint/no-explicit-any
			mockQuotaData.ollamaCloudAccount = { plan: "pro" } as any;

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
			// eslint-disable-next-line @typescript-eslint/no-explicit-any
			mockQuotaData.ollamaCloudAccount = { plan: "pro" } as any;
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
