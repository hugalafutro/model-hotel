import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { MemberView } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { ConfigSyncPanel } from "../ConfigSyncPanel";

function member(id: string, name: string): MemberView {
	return {
		id,
		name,
		url: `https://${name}.example.com`,
		state: "active",
		has_token: true,
		created_at: "",
		updated_at: "",
		status: {
			health: { known: true, healthy: true, latency_ms: 1, checked_at: "" },
		},
	};
}

const members = [member("1", "hotel-1"), member("2", "hotel-2")];

beforeEach(() => {
	localStorage.setItem("fdAuthToken", "tok");
});

function renderPanel() {
	return render(
		<ToastProvider>
			<ConfigSyncPanel members={members} onChanged={() => {}} />
		</ToastProvider>,
	);
}

describe("ConfigSyncPanel", () => {
	it("previews diffs, warns about removals, then double-confirms the sync", async () => {
		let synced = false;
		server.use(
			http.get("/api/config/preview", () =>
				HttpResponse.json({
					primary_id: "1",
					items: [
						{
							member_id: "1",
							name: "hotel-1",
							disposition: "matches",
							added: 0,
							updated: 0,
							removed: 0,
						},
						{
							member_id: "2",
							name: "hotel-2",
							disposition: "overwrite",
							added: 2,
							updated: 1,
							removed: 1,
						},
					],
				}),
			),
			http.post("/api/config/sync", async ({ request }) => {
				const body = (await request.json()) as { primary_id: string };
				expect(body.primary_id).toBe("1");
				synced = true;
				return HttpResponse.json({
					results: [{ member_id: "2", name: "hotel-2", ok: true }],
				});
			}),
		);
		renderPanel();
		await userEvent.selectOptions(
			screen.getByLabelText(/Primary \(config source\)/i),
			"1",
		);
		// Preview shows the per-entity counts for the overwrite member.
		expect(await screen.findByText("+2")).toBeInTheDocument();
		expect(screen.getByText("-1")).toBeInTheDocument();

		await userEvent.click(
			screen.getByRole("button", { name: /^Sync config$/i }),
		);
		const dialog = await screen.findByRole("dialog");
		// The removal warning is surfaced before confirming.
		expect(within(dialog).getByText(/will be removed/i)).toBeInTheDocument();
		const confirm = within(dialog).getByRole("button", {
			name: /Replace config on 1 member now/i,
		});
		expect(confirm).toBeDisabled();
		await userEvent.click(within(dialog).getByRole("checkbox"));
		expect(confirm).toBeEnabled();
		await userEvent.click(confirm);
		await waitFor(() => expect(synced).toBe(true));
	});

	it("blocks a member on a MASTER_KEY mismatch and disables sync when nothing changes", async () => {
		server.use(
			http.get("/api/config/preview", () =>
				HttpResponse.json({
					primary_id: "1",
					items: [
						{
							member_id: "1",
							name: "hotel-1",
							disposition: "matches",
							added: 0,
							updated: 0,
							removed: 0,
						},
						{
							member_id: "2",
							name: "hotel-2",
							disposition: "blocked",
							added: 0,
							updated: 0,
							removed: 0,
							note: "MASTER_KEY does not match the primary",
						},
					],
				}),
			),
		);
		renderPanel();
		await userEvent.selectOptions(
			screen.getByLabelText(/Primary \(config source\)/i),
			"1",
		);
		expect(
			await screen.findByText(/MASTER_KEY does not match/i),
		).toBeInTheDocument();
		// No member is in "overwrite", so the sync button stays disabled.
		expect(
			screen.getByRole("button", { name: /^Sync config$/i }),
		).toBeDisabled();
	});
});
