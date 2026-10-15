import { render, screen, within } from "@testing-library/react";
import i18next from "i18next";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { MemberCircuits, MemberView } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { sseHandler } from "../../test/sse";
import { MembersPage } from "../MembersPage";

function member(
	id: string,
	name: string,
	hasToken: boolean,
	circuits?: MemberCircuits,
): MemberView {
	return {
		id,
		name,
		url: `https://${name}.example.com`,
		state: "active",
		has_token: hasToken,
		created_at: new Date().toISOString(),
		updated_at: new Date().toISOString(),
		status: {
			health: {
				known: true,
				healthy: true,
				latency_ms: 12,
				checked_at: new Date().toISOString(),
			},
			circuits,
		},
	};
}

// Locale-independent: cells are found by testid, the count by its digits,
// the ledger by the provider, model and cause strings the member reported.
describe("MembersPage circuits column", () => {
	beforeEach(() => {
		server.use(
			sseHandler(),
			http.get(
				"/api/fleet/last-sync",
				() => new HttpResponse(null, { status: 204 }),
			),
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ enabled: false, primary_id: "" }),
			),
		);
	});

	it("shows the open count with the ledger on hover, none open, and not read", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member("m1", "dark", true, {
						checked_at: "2026-08-31T14:47:00Z",
						total: 3,
						open: [
							{
								provider_id: "p-zai",
								provider: "Z.ai",
								model: "glm-5.3",
								state: "open",
								cause: "upstream status 429 (exhausted)",
								status: 429,
								next_retry_at: "2026-08-31T18:41:00Z",
								quota_pinned: true,
								pin_source: "advisor",
							},
							{
								provider_id: "p-nw",
								provider: "Neuralwatt",
								model: "glm-5.3",
								state: "half-open",
								cause: "upstream status 503",
								status: 503,
							},
							{
								provider_id: "p-ocg",
								provider: "OpenCode Go",
								model: "(account quota)",
								state: "open",
								quota_pinned: true,
								pin_source: "reserve",
							},
						],
					}),
					member("m2", "fine", true, {
						checked_at: "2026-08-31T14:47:00Z",
						open: [],
						total: 0,
					}),
					member("m3", "unread", true),
					member("m4", "tokenless", false),
				]),
			),
		);
		render(
			<ToastProvider>
				<MembersPage />
			</ToastProvider>,
		);
		const open = await screen.findByTestId("member-circuits-open");
		expect(open).toHaveTextContent("3");
		const title = open.getAttribute("title") ?? "";
		expect(title).toContain("Z.ai / glm-5.3");
		// A reserve pin is a different state from an exhaustion pin: the quota is
		// still there, just kept back, and the ledger line says so.
		const lines = title.split("\n");
		const pinned = lines.find((l) => l.startsWith("Z.ai / glm-5.3")) ?? "";
		const reserved = lines.find((l) => l.startsWith("OpenCode Go /")) ?? "";
		expect(reserved).toContain(i18next.t("members.circuitStateReserved"));
		expect(pinned).toContain(i18next.t("members.circuitStatePinned"));
		expect(i18next.t("members.circuitStateReserved")).not.toEqual(
			i18next.t("members.circuitStatePinned"),
		);
		expect(title).toContain("upstream status 429 (exhausted)");
		expect(title).toContain("Neuralwatt / glm-5.3");
		expect(title).toContain("upstream status 503");
		expect(screen.getAllByTestId("member-circuits-none")).toHaveLength(1);
		const unknown = screen.getAllByTestId("member-circuits-unknown");
		expect(unknown).toHaveLength(2);
		// The tokenless member says why it cannot be read; the unread one is
		// merely not read yet. Different strings, whatever the locale.
		expect(unknown[0].textContent).not.toEqual(unknown[1].textContent);
		// The count sits in the same row as the member it belongs to.
		expect(
			within(open.closest("tr") as HTMLElement).getByText("dark"),
		).toBeInTheDocument();
	});
});
