import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { QuotaSnapshot } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { QuotaStrip } from "../QuotaStrip";

// One modal, replaced by one that always throws. Deliberately not a payload
// crafted to blow a real modal up: what is under test here is the WIRING (which
// subtree a modal-side throw is allowed to take down, and how the operator gets
// back), which must hold for any modal and any future one, not just for the
// nested reads that happen to be reachable today. Lives in its own file because
// vi.mock applies to the whole module graph of the file it is written in, and
// QuotaStrip.test.tsx opens this same modal for real.
vi.mock("../quota/ZAICodingQuotaModal", () => ({
	ZAICodingQuotaModal: () => {
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
