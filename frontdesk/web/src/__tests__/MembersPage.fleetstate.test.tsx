import { render, screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import type { AutoSyncConfig } from "../api/types";
import { ToastProvider } from "../context/ToastContext";
import { MembersPage } from "../pages/MembersPage";
import { server } from "../test/server";
import { sseHandler } from "../test/sse";

// Base handlers every case needs: an idle SSE stream and an empty member list.
// Each test then layers its own /api/fleet/autosync response on top to drive the
// fleet-state badge. onUnhandledRequest is "error", so all three must be present.
function baseHandlers() {
	return [sseHandler(), http.get("/api/members", () => HttpResponse.json([]))];
}

function autoSync(body: Partial<AutoSyncConfig>) {
	return http.get("/api/fleet/autosync", () =>
		HttpResponse.json({ enabled: false, primary_id: "", ...body }),
	);
}

function renderPage() {
	return render(
		<ToastProvider>
			<MembersPage />
		</ToastProvider>,
	);
}

describe("MembersPage fleet-state badge", () => {
	it("renders a warn badge for a degraded fleet", async () => {
		server.use(
			...baseHandlers(),
			autoSync({
				stale: false,
				fleet_state: "degraded",
				fleet_state_reasons: ["member_down"],
			}),
		);
		renderPage();
		const badge = await screen.findByTestId("fleet-state-badge");
		expect(badge.className).toContain("ui-badge-warn");
	});

	it("renders an ok badge for a healthy fleet", async () => {
		server.use(
			...baseHandlers(),
			autoSync({ stale: false, fleet_state: "ok", fleet_state_reasons: [] }),
		);
		renderPage();
		const badge = await screen.findByTestId("fleet-state-badge");
		expect(badge.className).toContain("ui-badge-ok");
	});

	it("renders a danger badge for a faulty fleet", async () => {
		server.use(
			...baseHandlers(),
			autoSync({
				stale: false,
				fleet_state: "faulty",
				fleet_state_reasons: ["all_members_down"],
			}),
		);
		renderPage();
		const badge = await screen.findByTestId("fleet-state-badge");
		expect(badge.className).toContain("ui-badge-danger");
	});

	it("omits the badge when the backend sends no fleet_state (older build)", async () => {
		server.use(...baseHandlers(), autoSync({ stale: false }));
		renderPage();
		// The page has settled once the empty-list body shows; the badge must be absent.
		await waitFor(() =>
			expect(screen.getByText(/no members/i)).toBeInTheDocument(),
		);
		expect(screen.queryByTestId("fleet-state-badge")).not.toBeInTheDocument();
	});
});
