import { render, screen } from "@testing-library/react";
import type {
	DeepSeekBalance,
	OllamaCloudAccount,
	OpenRouterBalance,
} from "../../api/types";
import { QuotaBadge } from "../QuotaBadge";

describe("QuotaBadge", () => {
	const onClick = vi.fn();

	beforeEach(() => {
		onClick.mockClear();
	});

	describe("nanogpt type", () => {
		it("renders with nanogpt type and shows usage", () => {
			render(
				<QuotaBadge
					type="nanogpt"
					variant="card"
					weeklyUsed={500000}
					weeklyLimit={1000000}
				/>,
			);
			expect(screen.getByText("500K/1M")).toBeInTheDocument();
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

		it("handles null weeklyUsed", () => {
			render(
				<QuotaBadge
					type="nanogpt"
					variant="card"
					weeklyUsed={null}
					weeklyLimit={1000000}
				/>,
			);
			expect(screen.getByText("-/1M")).toBeInTheDocument();
		});

		it("handles null weeklyLimit", () => {
			render(
				<QuotaBadge
					type="nanogpt"
					variant="card"
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
	});

	describe("deepseek type", () => {
		const mockDeepSeekBalance: DeepSeekBalance = {
			balance_infos: [
				{ currency: "USD", total_balance: 25.5 },
				{ currency: "CNY", total_balance: 100 },
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
				balance_infos: [{ currency: "CNY", total_balance: 100 }],
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
			credits_remaining: 15.75,
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
			plan: "pro",
			subscription_period_end: undefined,
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
