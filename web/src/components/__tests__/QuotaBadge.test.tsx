import { act, render, screen, waitFor } from "@testing-library/react";
import type {
	DeepSeekBalance,
	NeuralWattQuotaResponse,
	OllamaCloudAccount,
	OpenRouterBalance,
	ZAICodingQuotaResponse,
} from "../../api/types";
import type { QuotaDataResult } from "../../hooks/useQuotaData";
import { QuotaBadge, QuotaBadges } from "../QuotaBadge";

describe("QuotaBadge", () => {
	const onClick = vi.fn();

	beforeEach(() => {
		onClick.mockClear();
	});

	describe("nanogpt type", () => {
		it("renders with nanogpt type and shows remaining by default", () => {
			render(
				<QuotaBadge
					type="nanogpt"
					variant="card"
					weeklyUsed={300000}
					weeklyLimit={1000000}
				/>,
			);
			expect(screen.getByText("700K/1M")).toBeInTheDocument();
		});

		it("renders used values when barMode is used", () => {
			render(
				<QuotaBadge
					type="nanogpt"
					variant="card"
					barMode="used"
					weeklyUsed={300000}
					weeklyLimit={1000000}
				/>,
			);
			expect(screen.getByText("300K/1M")).toBeInTheDocument();
		});

		it("renders remaining values when barMode is remaining", () => {
			render(
				<QuotaBadge
					type="nanogpt"
					variant="card"
					barMode="remaining"
					weeklyUsed={300000}
					weeklyLimit={1000000}
				/>,
			);
			expect(screen.getByText("700K/1M")).toBeInTheDocument();
		});

		it("renders with nanogpt sidebar variant", () => {
			render(
				<QuotaBadge
					type="nanogpt"
					variant="sidebar"
					weeklyUsed={500000}
					weeklyLimit={1000000}
				/>,
			);
			const button = screen.getByRole("button");
			expect(button).toHaveClass("sidebar-quota-pill");
			expect(button).toHaveClass("sidebar-quota-pill-nanogpt");
		});

		it("handles null weeklyUsed (remaining mode shows full limit)", () => {
			render(
				<QuotaBadge
					type="nanogpt"
					variant="card"
					weeklyUsed={null}
					weeklyLimit={1000000}
				/>,
			);
			// remaining = limit - null(→0) = 1M
			expect(screen.getByText("1M/1M")).toBeInTheDocument();
		});

		it("handles null weeklyUsed (used mode shows 0/limit)", () => {
			render(
				<QuotaBadge
					type="nanogpt"
					variant="card"
					barMode="used"
					weeklyUsed={null}
					weeklyLimit={1000000}
				/>,
			);
			expect(screen.getByText("-/1M")).toBeInTheDocument();
		});

		it("handles null weeklyLimit in remaining mode (shows 0 remaining)", () => {
			render(
				<QuotaBadge
					type="nanogpt"
					variant="card"
					barMode="remaining"
					weeklyUsed={500000}
					weeklyLimit={null}
				/>,
			);
			// remaining = max(0, 0 - 500000) = 0
			expect(screen.getByText("0/-")).toBeInTheDocument();
		});

		it("handles null weeklyLimit in used mode", () => {
			render(
				<QuotaBadge
					type="nanogpt"
					variant="card"
					barMode="used"
					weeklyUsed={500000}
					weeklyLimit={null}
				/>,
			);
			expect(screen.getByText("500K/-")).toBeInTheDocument();
		});

		it("calls onClick when clicked", async () => {
			const user = await import("@testing-library/user-event");
			render(
				<QuotaBadge
					type="nanogpt"
					variant="card"
					weeklyUsed={500000}
					weeklyLimit={1000000}
					onClick={onClick}
				/>,
			);
			await user.default.click(screen.getByRole("button"));
			expect(onClick).toHaveBeenCalledTimes(1);
		});
	});

	describe("zai-coding type", () => {
		it("renders with zai-coding type", () => {
			// zai-coding requires zaiCodingUsage prop, without it shows -/-
			render(<QuotaBadge type="zai-coding" variant="card" />);
			expect(screen.getByText("-/-")).toBeInTheDocument();
		});

		it("renders with zai-coding sidebar variant", () => {
			render(<QuotaBadge type="zai-coding" variant="sidebar" />);
			const button = screen.getByRole("button");
			expect(button).toHaveClass("sidebar-quota-pill");
			expect(button).toHaveClass("sidebar-quota-pill-zai-coding");
		});

		it("handles null zaiCodingUsage", () => {
			render(
				<QuotaBadge type="zai-coding" variant="card" zaiCodingUsage={null} />,
			);
			expect(screen.getByText("-/-")).toBeInTheDocument();
		});

		it("handles undefined zaiCodingUsage", () => {
			render(
				<QuotaBadge
					type="zai-coding"
					variant="card"
					zaiCodingUsage={undefined}
				/>,
			);
			expect(screen.getByText("-/-")).toBeInTheDocument();
		});

		it("shows remaining percentages by default (barMode=remaining)", () => {
			const usage: ZAICodingQuotaResponse = {
				code: 200,
				msg: "success",
				data: {
					limits: [
						{
							type: "TOKENS_LIMIT",
							unit: 3,
							number: 10000,
							usage: 1000,
							currentValue: 1000,
							remaining: 9000,
							percentage: 10,
							nextResetTime: Date.now() + 18000000,
						},
						{
							type: "TOKENS_LIMIT",
							unit: 6,
							number: 100000,
							usage: 20000,
							currentValue: 20000,
							remaining: 80000,
							percentage: 20,
							nextResetTime: Date.now() + 604800000,
						},
					],
					level: "pro",
				},
				success: true,
			};
			render(
				<QuotaBadge type="zai-coding" variant="card" zaiCodingUsage={usage} />,
			);
			// remaining = 100 - percentage: 5h=90%, weekly=80%
			expect(screen.getByText("90%/80%")).toBeInTheDocument();
		});

		it("shows used percentages when barMode=used", () => {
			const usage: ZAICodingQuotaResponse = {
				code: 200,
				msg: "success",
				data: {
					limits: [
						{
							type: "TOKENS_LIMIT",
							unit: 3,
							number: 10000,
							usage: 1000,
							currentValue: 1000,
							remaining: 9000,
							percentage: 10,
							nextResetTime: Date.now() + 18000000,
						},
						{
							type: "TOKENS_LIMIT",
							unit: 6,
							number: 100000,
							usage: 20000,
							currentValue: 20000,
							remaining: 80000,
							percentage: 20,
							nextResetTime: Date.now() + 604800000,
						},
					],
					level: "pro",
				},
				success: true,
			};
			render(
				<QuotaBadge
					type="zai-coding"
					variant="card"
					barMode="used"
					zaiCodingUsage={usage}
				/>,
			);
			// percentage = % used: 5h=10%, weekly=20%
			expect(screen.getByText("10%/20%")).toBeInTheDocument();
		});
	});

	describe("deepseek type", () => {
		const mockDeepSeekBalance: DeepSeekBalance = {
			is_available: true,
			balance_infos: [
				{
					currency: "USD",
					total_balance: "25.5",
					granted_balance: "25.5",
					topped_up_balance: "0",
				},
				{
					currency: "CNY",
					total_balance: "100",
					granted_balance: "100",
					topped_up_balance: "0",
				},
			],
		};

		it("renders with deepseek type and shows USD balance", () => {
			render(
				<QuotaBadge
					type="deepseek"
					variant="card"
					deepseekBalance={mockDeepSeekBalance}
				/>,
			);
			expect(screen.getByText("25.5 USD")).toBeInTheDocument();
		});

		it("renders with deepseek sidebar variant", () => {
			render(
				<QuotaBadge
					type="deepseek"
					variant="sidebar"
					deepseekBalance={mockDeepSeekBalance}
				/>,
			);
			expect(screen.getByText("$25.5")).toBeInTheDocument();
		});

		it("handles missing USD balance", () => {
			const balanceNoUSD: DeepSeekBalance = {
				is_available: true,
				balance_infos: [
					{
						currency: "CNY",
						total_balance: "100",
						granted_balance: "100",
						topped_up_balance: "0",
					},
				],
			};
			render(
				<QuotaBadge
					type="deepseek"
					variant="card"
					deepseekBalance={balanceNoUSD}
				/>,
			);
			expect(screen.getByText("- USD")).toBeInTheDocument();
		});

		it("handles null deepseekBalance", () => {
			render(<QuotaBadge type="deepseek" variant="card" />);
			expect(screen.getByText("-")).toBeInTheDocument();
		});
	});

	describe("openrouter type", () => {
		const mockOpenRouterBalance: OpenRouterBalance = {
			label: "OpenRouter",
			limit: null,
			limit_reset: "",
			limit_remaining: null,
			usage: 0,
			usage_daily: 0,
			usage_weekly: 0,
			usage_monthly: 0,
			credits_total: 0,
			credits_used: 0,
			credits_remaining: 15.75,
			is_free_tier: false,
		};

		it("renders with openrouter type and shows balance", () => {
			render(
				<QuotaBadge
					type="openrouter"
					variant="card"
					openrouterBalance={mockOpenRouterBalance}
				/>,
			);
			expect(screen.getByText("$15.75")).toBeInTheDocument();
		});

		it("renders with openrouter sidebar variant", () => {
			render(
				<QuotaBadge
					type="openrouter"
					variant="sidebar"
					openrouterBalance={mockOpenRouterBalance}
				/>,
			);
			const button = screen.getByRole("button");
			expect(button).toHaveClass("sidebar-quota-pill");
			expect(button).toHaveClass("sidebar-quota-pill-openrouter");
		});

		it("handles null openrouterBalance", () => {
			render(<QuotaBadge type="openrouter" variant="card" />);
			expect(screen.getByText("-")).toBeInTheDocument();
		});
	});

	describe("ollama-cloud type", () => {
		const mockOllamaCloudAccount: OllamaCloudAccount = {
			id: "acct-1",
			email: "user@example.com",
			name: "Test User",
			plan: "pro",
			customer_id: { string: "", valid: false },
			subscription_id: { string: "", valid: false },
			subscription_period_start: { time: "", valid: false },
			subscription_period_end: { time: "", valid: false },
			suspended_at: { time: "", valid: false },
		};

		it("renders with ollama-cloud type and shows plan", () => {
			render(
				<QuotaBadge
					type="ollama-cloud"
					variant="card"
					ollamaCloudAccount={mockOllamaCloudAccount}
				/>,
			);
			expect(screen.getByText("pro")).toBeInTheDocument();
		});

		it("renders with ollama-cloud sidebar variant", () => {
			render(
				<QuotaBadge
					type="ollama-cloud"
					variant="sidebar"
					ollamaCloudAccount={mockOllamaCloudAccount}
				/>,
			);
			const button = screen.getByRole("button");
			expect(button).toHaveClass("sidebar-quota-pill");
			expect(button).toHaveClass("sidebar-quota-pill-ollama-cloud");
		});

		it("shows subscription end date when available", () => {
			const accountWithEnd: OllamaCloudAccount = {
				...mockOllamaCloudAccount,
				plan: "enterprise",
				subscription_period_end: {
					time: "2026-12-31T23:59:59Z",
					valid: true,
				},
			};
			render(
				<QuotaBadge
					type="ollama-cloud"
					variant="card"
					ollamaCloudAccount={accountWithEnd}
				/>,
			);
			const button = screen.getByRole("button");
			expect(button).toHaveAttribute("title");
			expect(button.getAttribute("title")).toContain("2026");
		});

		it("handles null ollamaCloudAccount", () => {
			render(<QuotaBadge type="ollama-cloud" variant="card" />);
			expect(screen.getByText("-")).toBeInTheDocument();
		});
	});

	describe("neuralwatt type", () => {
		const mockNeuralWattQuota: NeuralWattQuotaResponse = {
			snapshot_at: "2026-06-03T12:00:00Z",
			balance: {
				credits_remaining_usd: 15.5,
				total_credits_usd: 25.0,
				credits_used_usd: 9.5,
				accounting_method: "prepaid",
			},
			usage: {
				lifetime: {
					cost_usd: 9.5,
					requests: 150,
					tokens: 250000,
					energy_kwh: 2.23,
				},
				current_month: {
					cost_usd: 5.25,
					requests: 80,
					tokens: 120000,
					energy_kwh: 1.15,
				},
			},
			limits: {
				overage_limit_usd: 10.0,
				rate_limit_tier: "standard",
			},
			subscription: {
				plan: "pro",
				status: "active",
				billing_interval: "monthly",
				current_period_start: "2026-06-01T00:00:00Z",
				current_period_end: "2026-06-30T23:59:59Z",
				auto_renew: true,
				kwh_included: 16,
				kwh_used: 2.23,
				kwh_remaining: 13.77,
				in_overage: false,
			},
			key: {
				name: "default-key",
				allowance: null,
			},
		};

		const onClick = vi.fn();

		beforeEach(() => {
			onClick.mockClear();
		});

		it("renders with neuralwatt type and shows kWh used/included", () => {
			render(
				<QuotaBadge
					type="neuralwatt"
					variant="card"
					neuralwattQuota={mockNeuralWattQuota}
				/>,
			);
			expect(screen.getByText("2.23/16 kWh")).toBeInTheDocument();
		});

		it("renders with neuralwatt sidebar variant", () => {
			render(
				<QuotaBadge
					type="neuralwatt"
					variant="sidebar"
					neuralwattQuota={mockNeuralWattQuota}
				/>,
			);
			const button = screen.getByRole("button");
			expect(button).toHaveClass("sidebar-quota-pill");
			expect(button).toHaveClass("sidebar-quota-pill-neuralwatt");
		});

		it("handles zero kwh_included (pay-per-use)", () => {
			const payPerUseQuota: NeuralWattQuotaResponse = {
				...mockNeuralWattQuota,
				subscription: {
					...mockNeuralWattQuota.subscription,
					kwh_included: 0,
				},
			};
			render(
				<QuotaBadge
					type="neuralwatt"
					variant="card"
					neuralwattQuota={payPerUseQuota}
				/>,
			);
			expect(screen.getByText("2.23 kWh")).toBeInTheDocument();
		});

		it("handles null neuralwattQuota", () => {
			render(<QuotaBadge type="neuralwatt" variant="card" />);
			expect(screen.getByText("-")).toBeInTheDocument();
		});

		it("calls onClick when clicked", async () => {
			const user = await import("@testing-library/user-event");
			render(
				<QuotaBadge
					type="neuralwatt"
					variant="card"
					neuralwattQuota={mockNeuralWattQuota}
					onClick={onClick}
				/>,
			);
			await user.default.click(screen.getByRole("button"));
			expect(onClick).toHaveBeenCalledTimes(1);
		});

		it("shows included kWh as integer when whole number", () => {
			const wholeNumberQuota: NeuralWattQuotaResponse = {
				...mockNeuralWattQuota,
				subscription: {
					...mockNeuralWattQuota.subscription,
					kwh_included: 16,
					kwh_used: 5.5,
				},
			};
			render(
				<QuotaBadge
					type="neuralwatt"
					variant="card"
					neuralwattQuota={wholeNumberQuota}
				/>,
			);
			expect(screen.getByText("5.5/16 kWh")).toBeInTheDocument();
		});
	});

	describe("custom props", () => {
		it("uses custom title when provided", () => {
			render(
				<QuotaBadge
					type="nanogpt"
					variant="card"
					weeklyUsed={500000}
					weeklyLimit={1000000}
					title="Custom title"
				/>,
			);
			expect(screen.getByRole("button")).toHaveAttribute(
				"title",
				"Custom title",
			);
		});
	});
});

describe("QuotaBadges", () => {
	const nanoQuotaData: QuotaDataResult = {
		showNanoBadge: true,
		nanogptUsage: {
			active: true,
			provider: "nanogpt",
			providerStatus: "active",
			providerStatusRaw: "active",
			stripeSubscriptionId: "sub_test",
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
			period: { currentPeriodEnd: "2026-12-31" },
			dailyImages: { used: 10, remaining: 90, percentUsed: 10, resetAt: 0 },
			dailyInputTokens: {
				used: 50000,
				remaining: 150000,
				percentUsed: 25,
				resetAt: 0,
			},
			weeklyInputTokens: {
				used: 200000,
				remaining: 800000,
				percentUsed: 20,
				resetAt: 0,
			},
			state: "active",
			graceUntil: null,
		},
		nanoWeeklyUsed: 200000,
		nanoWeeklyLimit: 1000000,
		showZaiCodingBadge: false,
		zaiCodingUsage: undefined,
		showDsBadge: false,
		deepseekBalance: undefined,
		showOrBadge: false,
		openrouterBalance: undefined,
		showOllamaCloudBadge: false,
		ollamaCloudAccount: undefined,
		showNeuralwattBadge: false,
		neuralwattQuota: undefined,
		nanogptProviderId: "nanogpt-1",
		zaiCodingProviderId: undefined,
		deepseekProviderId: undefined,
		openrouterProviderId: undefined,
		ollamaCloudProviderId: undefined,
		neuralwattProviderId: undefined,
		zaiCodingFiveHour: undefined,
		zaiCodingWeekly: undefined,
		hasAnyProvider: true,
		refetchNano: vi.fn(),
		refetchZaiCoding: vi.fn(),
		refetchDeepseek: vi.fn(),
		refetchOpenRouter: vi.fn(),
		refetchOllamaCloud: vi.fn(),
		refetchNeuralwatt: vi.fn(),
		isNanoRefetching: false,
		isZaiCodingRefetching: false,
		isDsRefetching: false,
		isOrRefetching: false,
		isOllamaCloudRefetching: false,
		isNeuralwattRefetching: false,
		nanogptDataUpdatedAt: 0,
		zaiCodingDataUpdatedAt: 0,
		deepseekDataUpdatedAt: 0,
		openrouterDataUpdatedAt: 0,
		ollamaCloudDataUpdatedAt: 0,
		neuralwattDataUpdatedAt: 0,
		invalidateAll: vi.fn(),
	};

	const neuralwattQuotaData: QuotaDataResult = {
		showNanoBadge: false,
		nanogptUsage: undefined,
		nanoWeeklyUsed: undefined,
		nanoWeeklyLimit: undefined,
		showZaiCodingBadge: false,
		zaiCodingUsage: undefined,
		showDsBadge: false,
		deepseekBalance: undefined,
		showOrBadge: false,
		openrouterBalance: undefined,
		showOllamaCloudBadge: false,
		ollamaCloudAccount: undefined,
		showNeuralwattBadge: true,
		neuralwattQuota: {
			snapshot_at: "2026-06-03T12:00:00Z",
			balance: {
				credits_remaining_usd: 15.5,
				total_credits_usd: 25.0,
				credits_used_usd: 9.5,
				accounting_method: "prepaid",
			},
			usage: {
				lifetime: {
					cost_usd: 9.5,
					requests: 150,
					tokens: 250000,
					energy_kwh: 2.23,
				},
				current_month: {
					cost_usd: 5.25,
					requests: 80,
					tokens: 120000,
					energy_kwh: 1.15,
				},
			},
			limits: {
				overage_limit_usd: 10.0,
				rate_limit_tier: "standard",
			},
			subscription: {
				plan: "pro",
				status: "active",
				billing_interval: "monthly",
				current_period_start: "2026-06-01T00:00:00Z",
				current_period_end: "2026-06-30T23:59:59Z",
				auto_renew: true,
				kwh_included: 16,
				kwh_used: 2.23,
				kwh_remaining: 13.77,
				in_overage: false,
			},
			key: {
				name: "default-key",
				allowance: null,
			},
		},
		nanogptProviderId: undefined,
		zaiCodingProviderId: undefined,
		deepseekProviderId: undefined,
		openrouterProviderId: undefined,
		ollamaCloudProviderId: undefined,
		neuralwattProviderId: "neuralwatt-1",
		zaiCodingFiveHour: undefined,
		zaiCodingWeekly: undefined,
		hasAnyProvider: true,
		refetchNano: vi.fn(),
		refetchZaiCoding: vi.fn(),
		refetchDeepseek: vi.fn(),
		refetchOpenRouter: vi.fn(),
		refetchOllamaCloud: vi.fn(),
		refetchNeuralwatt: vi.fn(),
		isNanoRefetching: false,
		isZaiCodingRefetching: false,
		isDsRefetching: false,
		isOrRefetching: false,
		isOllamaCloudRefetching: false,
		isNeuralwattRefetching: false,
		nanogptDataUpdatedAt: 0,
		zaiCodingDataUpdatedAt: 0,
		deepseekDataUpdatedAt: 0,
		openrouterDataUpdatedAt: 0,
		ollamaCloudDataUpdatedAt: 0,
		neuralwattDataUpdatedAt: 0,
		invalidateAll: vi.fn(),
	};

	beforeEach(() => {
		localStorage.clear();
	});

	it("renders NanoGPT badge when quotaData has nanogpt usage", () => {
		render(<QuotaBadges quotaData={nanoQuotaData} variant="card" />);
		expect(screen.getByText("800K/1M")).toBeInTheDocument();
	});

	it("passes barMode='used' from localStorage", () => {
		localStorage.setItem("quota-bar-mode", "used");
		render(<QuotaBadges quotaData={nanoQuotaData} variant="card" />);
		expect(screen.getByText("200K/1M")).toBeInTheDocument();
	});

	it("updates barMode on localStorageChange event", async () => {
		render(<QuotaBadges quotaData={nanoQuotaData} variant="card" />);
		expect(screen.getByText("800K/1M")).toBeInTheDocument();

		localStorage.setItem("quota-bar-mode", "used");
		await act(async () => {
			window.dispatchEvent(
				new CustomEvent("localStorageChange", {
					detail: { key: "quota-bar-mode" },
				}),
			);
		});

		await waitFor(() => {
			expect(screen.getByText("200K/1M")).toBeInTheDocument();
		});
	});

	it("filters by providerBaseUrl", () => {
		render(
			<QuotaBadges
				quotaData={nanoQuotaData}
				variant="card"
				providerBaseUrl="https://api.nano-gpt.com"
			/>,
		);
		expect(screen.getByText("800K/1M")).toBeInTheDocument();
	});

	it("renders NeuralWatt badge when quotaData has neuralwatt quota", () => {
		render(<QuotaBadges quotaData={neuralwattQuotaData} variant="card" />);
		expect(screen.getByText("2.23/16 kWh")).toBeInTheDocument();
	});
});
