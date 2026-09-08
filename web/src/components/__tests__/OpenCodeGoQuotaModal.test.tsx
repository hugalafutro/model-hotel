import { screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { OpenCodeGoUsageResponse } from "../../api/types";
import { renderWithProviders } from "../../test/utils";
import { OpenCodeGoQuotaModal } from "../ProviderModals";

describe("OpenCodeGoQuotaModal", () => {
	const usage: OpenCodeGoUsageResponse = {
		usage: {
			rolling: { status: "ok", percent: 12, resetsAt: "2099-09-08T18:25:46Z" },
			weekly: { status: "ok", percent: 34, resetsAt: "2099-09-14T00:00:00Z" },
			monthly: { status: "ok", percent: 56, resetsAt: "2099-10-08T13:25:13Z" },
		},
	};

	const defaultProps = {
		usage,
		onClose: vi.fn(),
		onRefresh: vi.fn(),
		isRefreshing: false,
		onToast: vi.fn(),
		lastRefreshed: Date.now(),
	};

	beforeEach(() => {
		vi.clearAllMocks();
		localStorage.clear();
	});

	it("renders one bar per window, in report order", () => {
		renderWithProviders(<OpenCodeGoQuotaModal {...defaultProps} />);
		expect(
			screen.getByRole("heading", { name: "OpenCode Go Plan Quota" }),
		).toBeInTheDocument();
		for (const key of ["rolling", "weekly", "monthly"]) {
			expect(screen.getByTestId(`opencode-go-${key}-bar`)).toBeInTheDocument();
		}
		expect(screen.getByText("Rolling Quota (5h)")).toBeInTheDocument();
		expect(screen.getByText("Weekly Quota")).toBeInTheDocument();
		expect(screen.getByText("Monthly Quota")).toBeInTheDocument();
	});

	it("reads each window as remaining by default", () => {
		renderWithProviders(<OpenCodeGoQuotaModal {...defaultProps} />);
		expect(screen.getByText("88% left")).toBeInTheDocument();
		expect(screen.getByText("66% left")).toBeInTheDocument();
		expect(screen.getByText("44% left")).toBeInTheDocument();
	});

	it("omits the bar for a window the payload does not carry", () => {
		renderWithProviders(
			<OpenCodeGoQuotaModal
				{...defaultProps}
				usage={{ usage: { weekly: { status: "ok", percent: 7 } } }}
			/>,
		);
		expect(screen.getByTestId("opencode-go-weekly-bar")).toBeInTheDocument();
		expect(screen.queryByTestId("opencode-go-rolling-bar")).toBeNull();
		expect(screen.queryByTestId("opencode-go-monthly-bar")).toBeNull();
	});
});
