import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { KimiCodeQuotaResponse } from "../../api/types";
import { renderWithProviders } from "../../test/utils";
import { KimiCodeQuotaModal } from "../ProviderModals";

describe("KimiCodeQuotaModal", () => {
	const mockUsage: KimiCodeQuotaResponse = {
		user: { region: "REGION_OVERSEA", membership: { level: "basic" } },
		usage: {
			limit: "100",
			remaining: "50",
			resetTime: "2099-07-26T12:10:02Z",
		},
		limits: [
			{
				window: { duration: 300, timeUnit: "TIME_UNIT_MINUTE" },
				detail: {
					limit: "100",
					remaining: "80",
					resetTime: "2099-07-19T17:10:02Z",
				},
			},
		],
		parallel: { limit: "10" },
		totalQuota: { limit: "100", remaining: "99" },
		authentication: { method: "METHOD_API_KEY", scope: "FEATURE_CODING" },
		subType: "TYPE_PURCHASE",
	};

	const onClose = vi.fn();
	const onRefresh = vi.fn();
	const onToast = vi.fn();

	const defaultProps = {
		usage: mockUsage,
		onClose,
		onRefresh,
		isRefreshing: false,
		onToast,
		lastRefreshed: Date.now(),
	};

	beforeEach(() => {
		vi.clearAllMocks();
		localStorage.clear();
	});

	describe("rendering", () => {
		it("renders modal title", () => {
			renderWithProviders(<KimiCodeQuotaModal {...defaultProps} />);
			expect(
				screen.getByRole("heading", { name: "Kimi Code Plan Quota" }),
			).toBeInTheDocument();
		});

		it("renders membership level via data-testid", () => {
			renderWithProviders(<KimiCodeQuotaModal {...defaultProps} />);
			expect(screen.getByTestId("kimi-code-membership")).toHaveTextContent(
				"basic",
			);
		});

		it("renders the 5h bar", () => {
			renderWithProviders(<KimiCodeQuotaModal {...defaultProps} />);
			expect(screen.getByTestId("kimi-code-5h-bar")).toBeInTheDocument();
		});

		it("renders the weekly bar", () => {
			renderWithProviders(<KimiCodeQuotaModal {...defaultProps} />);
			expect(screen.getByTestId("kimi-code-weekly-bar")).toBeInTheDocument();
		});

		it("renders the parallel limit row", () => {
			renderWithProviders(<KimiCodeQuotaModal {...defaultProps} />);
			expect(screen.getByTestId("kimi-code-parallel")).toHaveTextContent("10");
		});

		it("renders the total quota row", () => {
			renderWithProviders(<KimiCodeQuotaModal {...defaultProps} />);
			expect(screen.getByTestId("kimi-code-total-quota")).toHaveTextContent(
				"99 / 100",
			);
		});

		it("renders last refreshed timestamp", () => {
			renderWithProviders(<KimiCodeQuotaModal {...defaultProps} />);
			expect(screen.getByText("Last refreshed")).toBeInTheDocument();
		});

		it("does not render last refreshed when undefined", () => {
			renderWithProviders(
				<KimiCodeQuotaModal {...defaultProps} lastRefreshed={undefined} />,
			);
			expect(screen.queryByText("Last refreshed")).not.toBeInTheDocument();
		});
	});

	describe("edge cases", () => {
		it("shows '-' when membership level is missing", () => {
			const usageNoLevel: KimiCodeQuotaResponse = {
				...mockUsage,
				user: { membership: {} },
			};
			renderWithProviders(
				<KimiCodeQuotaModal {...defaultProps} usage={usageNoLevel} />,
			);
			expect(screen.getByTestId("kimi-code-membership")).toHaveTextContent("-");
		});

		it("does not render bars when usage and limits are absent", () => {
			const empty: KimiCodeQuotaResponse = {
				user: { membership: { level: "basic" } },
			};
			renderWithProviders(
				<KimiCodeQuotaModal {...defaultProps} usage={empty} />,
			);
			expect(screen.queryByTestId("kimi-code-5h-bar")).not.toBeInTheDocument();
			expect(
				screen.queryByTestId("kimi-code-weekly-bar"),
			).not.toBeInTheDocument();
		});

		it("does not render the detail section when parallel limit and total quota are absent", () => {
			const empty: KimiCodeQuotaResponse = {
				user: { membership: { level: "basic" } },
			};
			renderWithProviders(
				<KimiCodeQuotaModal {...defaultProps} usage={empty} />,
			);
			expect(
				screen.queryByTestId("kimi-code-parallel"),
			).not.toBeInTheDocument();
			expect(
				screen.queryByTestId("kimi-code-total-quota"),
			).not.toBeInTheDocument();
		});

		it("shows 'N/A' reset label when the 5h window reset time is empty", () => {
			const usageEmptyReset: KimiCodeQuotaResponse = {
				...mockUsage,
				limits: [
					{
						window: { duration: 300, timeUnit: "TIME_UNIT_MINUTE" },
						detail: { limit: "100", remaining: "80", resetTime: "" },
					},
				],
			};
			renderWithProviders(
				<KimiCodeQuotaModal {...defaultProps} usage={usageEmptyReset} />,
			);
			expect(screen.getByText("N/A")).toBeInTheDocument();
		});

		it("shows 'N/A' reset label when the reset time is not a valid date", () => {
			const usageBadReset: KimiCodeQuotaResponse = {
				...mockUsage,
				limits: [
					{
						window: { duration: 300, timeUnit: "TIME_UNIT_MINUTE" },
						detail: {
							limit: "100",
							remaining: "80",
							resetTime: "not-a-date",
						},
					},
				],
			};
			renderWithProviders(
				<KimiCodeQuotaModal {...defaultProps} usage={usageBadReset} />,
			);
			expect(screen.getByText("N/A")).toBeInTheDocument();
		});

		it("renders '-' fallbacks when total quota fields are missing", () => {
			const usageNoTotals: KimiCodeQuotaResponse = {
				...mockUsage,
				totalQuota: {},
			};
			renderWithProviders(
				<KimiCodeQuotaModal {...defaultProps} usage={usageNoTotals} />,
			);
			expect(screen.getByTestId("kimi-code-total-quota")).toHaveTextContent(
				"- / -",
			);
		});

		it("toggles bar mode from remaining to used", async () => {
			const { user } = renderWithProviders(
				<KimiCodeQuotaModal {...defaultProps} />,
			);
			// 5h window: limit 100, remaining 80 => 20% used / 80% left.
			expect(screen.getByText("80% left")).toBeInTheDocument();
			await user.click(
				screen.getByRole("button", {
					name: "Toggle between remaining and used",
				}),
			);
			expect(screen.getByText("20% used")).toBeInTheDocument();
		});
	});

	describe("refresh functionality", () => {
		it("calls onRefresh when refresh button is clicked", async () => {
			const { user } = renderWithProviders(
				<KimiCodeQuotaModal {...defaultProps} />,
			);
			await user.click(screen.getByRole("button", { name: "Refresh" }));
			expect(onRefresh).toHaveBeenCalledTimes(1);
		});

		it("calls onToast with success after refresh", async () => {
			onRefresh.mockResolvedValue(undefined);
			const { user } = renderWithProviders(
				<KimiCodeQuotaModal {...defaultProps} />,
			);
			await user.click(screen.getByRole("button", { name: "Refresh" }));
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith("Quota refreshed", "success");
			});
		});

		it("calls onToast with error on refresh failure", async () => {
			onRefresh.mockRejectedValue(new Error("boom"));
			const { user } = renderWithProviders(
				<KimiCodeQuotaModal {...defaultProps} />,
			);
			await user.click(screen.getByRole("button", { name: "Refresh" }));
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					"Failed to refresh quota",
					"error",
				);
			});
		});
	});

	describe("close functionality", () => {
		it("calls onClose when close button is clicked", async () => {
			const { user } = renderWithProviders(
				<KimiCodeQuotaModal {...defaultProps} />,
			);
			await user.click(screen.getByRole("button", { name: "Close" }));
			await waitFor(() => {
				expect(onClose).toHaveBeenCalledTimes(1);
			});
		});
	});
});
