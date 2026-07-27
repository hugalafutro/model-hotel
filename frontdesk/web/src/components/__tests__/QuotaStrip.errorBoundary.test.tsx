import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { QuotaSnapshot } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { QuotaStrip } from "../QuotaStrip";

// One modal, replaced by one that throws UNLESS the payload carries the
// "fixed" sentinel. Deliberately not a payload crafted to blow a real modal
// up: what is under test here is the WIRING (which subtree a modal-side throw
// is allowed to take down, how the operator gets back, and what does NOT get
// the operator back), which must hold for any modal and any future one, not
// just for the nested reads that happen to be reachable today. The "fixed"
// branch exists solely for the corrected-data regression test below; every
// payload the other tests in this file use (level: "pro") still throws,
// unchanged. Lives in its own file because vi.mock applies to the whole
// module graph of the file it is written in, and QuotaStrip.test.tsx opens
// this same modal for real.
vi.mock("../quota/ZAICodingQuotaModal", () => ({
	ZAICodingQuotaModal: ({
		payload,
	}: {
		payload: { data?: { level?: string } };
	}) => {
		if (payload.data?.level === "fixed") {
			return (
				<div data-testid="zai-modal-ok" role="dialog">
					fixed
				</div>
			);
		}
		throw new TypeError("Cannot read properties of undefined");
	},
}));

const nano: QuotaSnapshot = {
	provider_name: "nano",
	type: "nanogpt",
	kind: "usage",
	payload: {
		active: true,
		provider: "nanogpt",
		providerStatus: "active",
		cancelAtPeriodEnd: false,
		limits: {
			weeklyInputTokens: 1000,
			dailyInputTokens: null,
			dailyImages: null,
		},
		allowOverage: false,
		period: { currentPeriodEnd: "2026-08-01T00:00:00Z" },
		dailyImages: null,
		dailyInputTokens: null,
		weeklyInputTokens: {
			used: 400,
			remaining: 600,
			percentUsed: 0.4,
			resetAt: 0,
		},
	},
	http_status: 200,
	fetched_at: "2026-07-26T10:00:00Z",
};

const zai: QuotaSnapshot = {
	provider_name: "zai",
	type: "zai-coding",
	kind: "usage",
	payload: {
		success: true,
		data: {
			level: "pro",
			limits: [{ type: "TOKENS_LIMIT", unit: 3, percentage: 40 }],
		},
	},
	http_status: 200,
	fetched_at: "2026-07-26T10:00:00Z",
};

function renderStrip() {
	render(
		<ToastProvider>
			<QuotaStrip />
		</ToastProvider>,
	);
}

describe("QuotaStrip modal containment", () => {
	// React logs every error it hands to a boundary; the throw here is the point
	// of the test, not a surprise.
	beforeEach(() => {
		vi.spyOn(console, "error").mockImplementation(() => {});
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [nano, zai] })),
		);
	});
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("keeps the strip and its badges when a modal throws", async () => {
		renderStrip();
		await waitFor(() =>
			expect(
				screen.getByTestId("quota-badge-zai-coding:zai"),
			).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-badge-zai-coding:zai"));

		// The modal is gone (nothing rendered in its place, matching the strip's
		// own empty state) but everything around it survived the throw.
		expect(screen.queryByRole("dialog")).toBeNull();
		expect(screen.getByTestId("quota-strip")).toBeInTheDocument();
		expect(
			screen.getByTestId("quota-badge-zai-coding:zai"),
		).toBeInTheDocument();
		expect(screen.getByTestId("quota-badge-nanogpt:nano")).toBeInTheDocument();
		// The badge still carries its numbers: the badge path reads defensively,
		// so a modal that cannot render must not cost the operator the figure.
		expect(screen.getByTestId("quota-badge-zai-coding:zai")).toHaveTextContent(
			"60%",
		);
	});

	it("opens another provider's modal after one has thrown", async () => {
		renderStrip();
		await waitFor(() =>
			expect(
				screen.getByTestId("quota-badge-zai-coding:zai"),
			).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-badge-zai-coding:zai"));
		expect(screen.queryByRole("dialog")).toBeNull();

		// Recovery, with no reload: the boundary is keyed by the open provider,
		// so switching providers discards the failed one instead of inheriting
		// its latched state.
		await userEvent.click(screen.getByTestId("quota-badge-nanogpt:nano"));
		expect(screen.getByRole("dialog")).toBeInTheDocument();
		expect(screen.getByTestId("nano-weekly-fill")).toBeInTheDocument();
	});

	it("recovers a healthy modal that was opened after the broken one closed", async () => {
		renderStrip();
		await waitFor(() =>
			expect(
				screen.getByTestId("quota-badge-nanogpt:nano"),
			).toBeInTheDocument(),
		);
		// Healthy first, so the boundary in play has already rendered a modal
		// successfully; the broken provider must not poison it for the next one.
		await userEvent.click(screen.getByTestId("quota-badge-nanogpt:nano"));
		expect(screen.getByRole("dialog")).toBeInTheDocument();
		await userEvent.click(screen.getByTestId("quota-modal-close"));

		await userEvent.click(screen.getByTestId("quota-badge-zai-coding:zai"));
		expect(screen.queryByRole("dialog")).toBeNull();

		await userEvent.click(screen.getByTestId("quota-badge-nanogpt:nano"));
		expect(screen.getByTestId("nano-weekly-fill")).toBeInTheDocument();
	});
});

// Regression test: a modal that has thrown must recover the moment a LATER
// poll delivers a CORRECTED snapshot for the SAME provider, not only when the
// operator opens a different provider, collapses the strip, or the provider
// disappears from the export. Uses fake timers to drive the 60 second poll
// interval directly, matching the pattern in QuotaStrip.test.tsx's
// "QuotaStrip polling" describe.
describe("QuotaStrip modal recovery on corrected data", () => {
	beforeEach(() => {
		vi.spyOn(console, "error").mockImplementation(() => {});
		vi.useFakeTimers({ shouldAdvanceTime: true });
	});
	afterEach(() => {
		vi.useRealTimers();
		vi.restoreAllMocks();
	});

	// Same provider (same key: "zai-coding:zai") as `zai` above, but with the
	// mock's "fixed" sentinel and a later `fetched_at`, standing in for the
	// primary having re-fetched this provider successfully in between.
	const zaiFixed: QuotaSnapshot = {
		...zai,
		payload: {
			success: true,
			data: {
				level: "fixed",
				limits: [{ type: "TOKENS_LIMIT", unit: 3, percentage: 10 }],
			},
		},
		fetched_at: "2026-07-26T10:05:00Z",
	};

	it("recovers on its own once a later poll corrects the same provider, with no further click", async () => {
		let call = 0;
		server.use(
			http.get("/api/quota", () => {
				call++;
				// Poll 1 (mount): the broken payload the mocked modal always throws
				// on. Poll 2 onward: the same provider, corrected.
				return HttpResponse.json({
					quota: [nano, call === 1 ? zai : zaiFixed],
				});
			}),
		);
		renderStrip();
		await waitFor(() =>
			expect(
				screen.getByTestId("quota-badge-zai-coding:zai"),
			).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-badge-zai-coding:zai"));
		expect(screen.queryByRole("dialog")).toBeNull();

		// Poll 2 lands: same provider, corrected snapshot, the modal still
		// "open" in state (nothing closed it). No further click, no other
		// provider, no collapse: the only thing that changed is the data.
		await act(async () => {
			await vi.advanceTimersByTimeAsync(60_000);
		});
		await waitFor(() => expect(call).toBe(2));

		expect(screen.getByTestId("zai-modal-ok")).toBeInTheDocument();
		expect(screen.getByRole("dialog")).toBeInTheDocument();
	});

	it("re-clicking the same badge with no new data does not retry on its own", async () => {
		// Control for the fix's other half: a repeated click on the SAME badge
		// must not be what recovers the modal, since with no new data the
		// modal would just throw again. `openKey` is already this key, so
		// React bails out of the state update and nothing re-renders at all.
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [nano, zai] })),
		);
		renderStrip();
		await waitFor(() =>
			expect(
				screen.getByTestId("quota-badge-zai-coding:zai"),
			).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-badge-zai-coding:zai"));
		expect(screen.queryByRole("dialog")).toBeNull();

		await userEvent.click(screen.getByTestId("quota-badge-zai-coding:zai"));
		expect(screen.queryByRole("dialog")).toBeNull();
		expect(screen.queryByTestId("zai-modal-ok")).toBeNull();
	});
});
