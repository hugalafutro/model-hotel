import { screen, waitFor } from "@testing-library/react";
import type {
	MiniMaxQuotaResponse,
	NanoGPTUsage,
	OpenCodeGoUsageResponse,
} from "../../api/types";
import i18n from "../../i18n";
import { getByDialogName } from "../../test/helpers";
import { renderWithProviders } from "../../test/utils";
import { formatTimestamp } from "../../utils/format";
import {
	MiniMaxQuotaModal,
	NanoGPTQuotaModal,
	OpenCodeGoQuotaModal,
} from "../ProviderModals";

// The dashboard's shell around the shared quota modal bodies: header actions,
// the refresh toast, the stored bar mode, the last-refreshed row, and the
// fetch-time anchor. What each body draws is covered in the sharedQuota*
// suites.

const k = (key: string, opts?: Record<string, unknown>) =>
	i18n.t(`components.providerModals.${key}`, opts);

const ocg: OpenCodeGoUsageResponse = {
	usage: { rolling: { status: "ok", percent: 20 } },
};

function props<P>(payload: P) {
	return {
		payload,
		onClose: vi.fn(),
		onRefresh: vi.fn().mockResolvedValue(undefined),
		isRefreshing: false,
		onToast: vi.fn(),
		lastRefreshed: Date.now(),
	};
}

describe("quota modal shell", () => {
	beforeEach(() => {
		localStorage.clear();
	});

	it("titles the dialog with this app's string and renders the shared body", () => {
		renderWithProviders(<OpenCodeGoQuotaModal {...props(ocg)} />);
		const dialog = getByDialogName(k("openCodeGoPlanQuota"));
		expect(dialog).toContainElement(
			screen.getByTestId("opencode-go-rolling-fill"),
		);
		expect(screen.getByText(k("lastRefreshed"))).toBeInTheDocument();
	});

	it("leaves out the last-refreshed row until a fetch has landed", () => {
		renderWithProviders(
			<OpenCodeGoQuotaModal {...props(ocg)} lastRefreshed={undefined} />,
		);
		expect(screen.queryByText(k("lastRefreshed"))).toBeNull();
	});

	it("toasts success after a refresh", async () => {
		const p = props(ocg);
		const { user } = renderWithProviders(<OpenCodeGoQuotaModal {...p} />);
		await user.click(
			screen.getByRole("button", { name: i18n.t("common.refresh") }),
		);
		expect(p.onRefresh).toHaveBeenCalledTimes(1);
		await waitFor(() =>
			expect(p.onToast).toHaveBeenCalledWith(k("quotaRefreshed"), "success"),
		);
	});

	it("toasts an error when the refresh fails", async () => {
		const p = props(ocg);
		p.onRefresh.mockRejectedValue(new Error("boom"));
		const { user } = renderWithProviders(<OpenCodeGoQuotaModal {...p} />);
		await user.click(
			screen.getByRole("button", { name: i18n.t("common.refresh") }),
		);
		await waitFor(() =>
			expect(p.onToast).toHaveBeenCalledWith(
				k("failedToRefreshQuota"),
				"error",
			),
		);
	});

	it("disables and spins the refresh button while a refresh is in flight", () => {
		renderWithProviders(
			<OpenCodeGoQuotaModal {...props(ocg)} isRefreshing={true} />,
		);
		const refresh = screen.getByRole("button", {
			name: i18n.t("common.refresh"),
		});
		expect(refresh).toBeDisabled();
		expect(refresh.querySelector(".animate-spin")).not.toBeNull();
	});

	it("shows the terminal spinner while refreshing in the cyber-terminal style", () => {
		localStorage.setItem("uiStyle", "cyber-terminal");
		renderWithProviders(
			<OpenCodeGoQuotaModal {...props(ocg)} isRefreshing={true} />,
		);
		expect(screen.getByTestId("spinner")).toBeInTheDocument();
	});

	it("closes from the close button", async () => {
		const p = props(ocg);
		const { user } = renderWithProviders(<OpenCodeGoQuotaModal {...p} />);
		await user.click(
			screen.getByRole("button", { name: i18n.t("common.close") }),
		);
		// The modal animates out before it reports the close.
		await waitFor(() => expect(p.onClose).toHaveBeenCalledTimes(1));
	});

	it("reads remaining by default and keeps a toggled bar mode for the next modal", async () => {
		const { user, unmount } = renderWithProviders(
			<OpenCodeGoQuotaModal {...props(ocg)} />,
		);
		expect(screen.getByTestId("opencode-go-rolling-fill")).toHaveStyle({
			width: "80%",
		});
		await user.click(
			screen.getByRole("button", { name: k("toggleRemainingUsed") }),
		);
		expect(screen.getByTestId("opencode-go-rolling-fill")).toHaveStyle({
			width: "20%",
		});
		unmount();

		renderWithProviders(<OpenCodeGoQuotaModal {...props(ocg)} />);
		expect(screen.getByTestId("opencode-go-rolling-fill")).toHaveStyle({
			width: "20%",
		});
	});

	const minimax: MiniMaxQuotaResponse = {
		base_resp: { status_code: 0, status_msg: "ok" },
		model_remains: [
			{
				model_name: "general",
				current_interval_status: 1,
				current_interval_remaining_percent: 70,
				remains_time: 3_600_000,
				current_weekly_status: 1,
				current_weekly_remaining_percent: 30,
				weekly_remains_time: 0,
			},
		],
	};

	it("anchors a payload's durations at the server snapshot's fetch time", () => {
		// The server can answer with a snapshot it fetched upstream earlier than
		// this query landed. MiniMax measured remains_time at that upstream
		// fetch, so anchoring at the query time would push the reset later by
		// the snapshot's age, and again on every refresh of the same snapshot.
		const snapshot = "2026-07-26T09:00:00Z";
		const queried = Date.parse("2026-07-26T10:00:00Z");
		renderWithProviders(
			<MiniMaxQuotaModal
				{...props(minimax)}
				lastRefreshed={queried}
				fetchedAt={snapshot}
			/>,
		);
		const bar = screen.getByTestId("minimax-general-5h-bar").parentElement;
		expect(bar).toHaveTextContent(
			formatTimestamp(Date.parse(snapshot) + 3_600_000),
		);
		expect(bar).not.toHaveTextContent(formatTimestamp(queried + 3_600_000));
	});

	it("falls back to the query time, not the render clock, without a snapshot time", () => {
		// MiniMax reports how long until each window resets; anchoring at
		// Date.now() would push the reset later on every re-render.
		const fetched = Date.parse("2026-07-26T10:00:00Z");
		renderWithProviders(
			<MiniMaxQuotaModal {...props(minimax)} lastRefreshed={fetched} />,
		);
		const bar = screen.getByTestId("minimax-general-5h-bar").parentElement;
		expect(bar).toHaveTextContent(formatTimestamp(fetched + 3_600_000));
		expect(
			screen.getByTestId("minimax-general-weekly-bar").parentElement,
		).toHaveTextContent("-");
	});

	it("renders a dash for a date the payload leaves blank", () => {
		const payload: NanoGPTUsage = {
			active: true,
			provider: "nanogpt",
			providerStatus: "active",
			providerStatusRaw: "active",
			stripeSubscriptionId: "sub",
			cancellationReason: null,
			canceledAt: null,
			endedAt: null,
			cancelAt: null,
			cancelAtPeriodEnd: true,
			limits: {
				weeklyInputTokens: 0,
				dailyInputTokens: null,
				dailyImages: null,
			},
			allowOverage: false,
			period: { currentPeriodEnd: "" },
			dailyImages: null,
			dailyInputTokens: null,
			weeklyInputTokens: null,
			state: "active",
			graceUntil: null,
		};
		renderWithProviders(<NanoGPTQuotaModal {...props(payload)} />);
		expect(screen.getByTestId("nano-cancel-notice")).toHaveTextContent(
			k("cancelAtPeriodEnd", { date: "-" }),
		);
	});
});
