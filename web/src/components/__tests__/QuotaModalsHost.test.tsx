import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useQuotaModal } from "../../context/QuotaModalContext";
import type { QuotaDataResult } from "../../hooks/useQuotaData";
import { renderWithProviders } from "../../test/utils";
import { QuotaModalsHost } from "../QuotaModalsHost";

vi.mock("../../hooks/useQuotaData", async (importOriginal) => {
	const actual =
		await importOriginal<typeof import("../../hooks/useQuotaData")>();
	return { ...actual, useQuotaData: vi.fn() };
});

const { useQuotaData } = await import("../../hooks/useQuotaData");
const mockUseQuotaData = vi.mocked(useQuotaData);

const nanogptUsage: import("../../api/types").NanoGPTUsage = {
	active: true,
	provider: "nanogpt",
	providerStatus: "active",
	providerStatusRaw: "active",
	stripeSubscriptionId: "sub_123",
	cancellationReason: null,
	canceledAt: null,
	endedAt: null,
	cancelAt: null,
	cancelAtPeriodEnd: false,
	state: "active",
	graceUntil: null,
	limits: {
		weeklyInputTokens: 1_000_000,
		dailyInputTokens: 200_000,
		dailyImages: 50,
	},
	allowOverage: false,
	period: { currentPeriodEnd: "2025-12-31" },
	dailyImages: { used: 10, remaining: 40, percentUsed: 20, resetAt: 0 },
	dailyInputTokens: {
		used: 50_000,
		remaining: 150_000,
		percentUsed: 25,
		resetAt: 0,
	},
	weeklyInputTokens: {
		used: 500_000,
		remaining: 500_000,
		percentUsed: 50,
		resetAt: 0,
	},
};

function mockQuota(overrides?: Partial<QuotaDataResult>) {
	mockUseQuotaData.mockReturnValue({
		hasAnyProvider: true,
		nanogptUsage,
		isNanoRefetching: false,
		refetchNano: vi.fn(),
		nanogptDataUpdatedAt: 0,
		...overrides,
	} as unknown as QuotaDataResult);
}

// A button that names a provider in QuotaModalContext, standing in for the
// quota badges the sidebar panel and the Providers page both render.
function OpenNano() {
	const { setOpen } = useQuotaModal();
	return (
		<button type="button" onClick={() => setOpen("nanogpt")}>
			open nano
		</button>
	);
}

function renderHost(extra?: ReactNode) {
	return renderWithProviders(
		<>
			<OpenNano />
			{extra}
			<QuotaModalsHost />
		</>,
	);
}

describe("QuotaModalsHost", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockQuota();
	});

	it("renders nothing until a provider is named", () => {
		renderHost();
		expect(
			screen.queryByRole("heading", { name: "NanoGPT Subscription" }),
		).toBeNull();
	});

	it("renders exactly one modal however many places request it", async () => {
		const user = userEvent.setup();
		// Two openers, as /providers has (the sidebar panel and the page): the
		// modal is still mounted once, so there is one dialog and one Close.
		renderHost(<OpenNano />);

		await user.click(screen.getAllByRole("button", { name: "open nano" })[0]);

		expect(
			screen.getAllByRole("heading", { name: "NanoGPT Subscription" }),
		).toHaveLength(1);
		expect(screen.getAllByRole("button", { name: "Close" })).toHaveLength(1);
	});

	it("closes when the modal asks to", async () => {
		const user = userEvent.setup();
		renderHost();

		await user.click(screen.getByRole("button", { name: "open nano" }));
		await screen.findByRole("heading", { name: "NanoGPT Subscription" });

		await user.click(screen.getByRole("button", { name: "Close" }));

		// The modal fades out before it unmounts.
		await waitFor(() => {
			expect(
				screen.queryByRole("heading", { name: "NanoGPT Subscription" }),
			).toBeNull();
		});
	});

	it("renders nothing for a provider whose quota has not loaded", async () => {
		const user = userEvent.setup();
		mockQuota({ nanogptUsage: undefined });
		renderHost();

		await user.click(screen.getByRole("button", { name: "open nano" }));

		expect(
			screen.queryByRole("heading", { name: "NanoGPT Subscription" }),
		).toBeNull();
	});
});
