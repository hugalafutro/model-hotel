import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { MemberView } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { AdminTokenResetPanel, AdminTokenSyncPanel } from "../AdminTokenPanels";

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

describe("AdminTokenSyncPanel", () => {
	function renderSync() {
		return render(
			<ToastProvider>
				<AdminTokenSyncPanel members={members} onChanged={() => {}} />
			</ToastProvider>,
		);
	}

	it("previews, then double-confirms and runs the sync", async () => {
		let synced = false;
		server.use(
			http.get("/api/admin-token/preview", () =>
				HttpResponse.json({
					primary_id: "1",
					items: [
						{ member_id: "1", name: "hotel-1", disposition: "matches" },
						{ member_id: "2", name: "hotel-2", disposition: "overwrite" },
					],
				}),
			),
			http.post("/api/admin-token/sync", async ({ request }) => {
				const body = (await request.json()) as { primary_id: string };
				expect(body.primary_id).toBe("1");
				synced = true;
				return HttpResponse.json({
					results: [{ member_id: "2", name: "hotel-2", ok: true }],
				});
			}),
		);
		renderSync();
		await userEvent.selectOptions(screen.getByLabelText(/Primary/i), "1");
		// Preview shows hotel-2 as overwrite.
		expect(await screen.findByText(/Will be overwritten/i)).toBeInTheDocument();

		// Open confirm; the action button is disabled until the ack is ticked.
		await userEvent.click(
			screen.getByRole("button", { name: /^Sync admin token$/i }),
		);
		const dialog = await screen.findByRole("dialog");
		const confirm = within(dialog).getByRole("button", {
			name: /Overwrite 1 member/i,
		});
		expect(confirm).toBeDisabled();
		await userEvent.click(within(dialog).getByRole("checkbox"));
		expect(confirm).toBeEnabled();
		await userEvent.click(confirm);
		await waitFor(() => expect(synced).toBe(true));
	});

	it("disables the sync button when nothing needs overwriting", async () => {
		server.use(
			http.get("/api/admin-token/preview", () =>
				HttpResponse.json({
					primary_id: "1",
					items: [
						{ member_id: "1", name: "hotel-1", disposition: "matches" },
						{ member_id: "2", name: "hotel-2", disposition: "matches" },
					],
				}),
			),
		);
		renderSync();
		await userEvent.selectOptions(screen.getByLabelText(/Primary/i), "1");
		await screen.findAllByText(/Already matches/i);
		expect(
			screen.getByRole("button", { name: /^Sync admin token$/i }),
		).toBeDisabled();
	});
});

describe("AdminTokenResetPanel", () => {
	function renderReset() {
		return render(
			<ToastProvider>
				<AdminTokenResetPanel members={members} onChanged={() => {}} />
			</ToastProvider>,
		);
	}

	it("double-confirms then reveals the new token once", async () => {
		server.use(
			http.post("/api/admin-token/reset", () =>
				HttpResponse.json({
					token: "abcdef0123456789abcdef0123456789",
					results: [
						{ member_id: "1", name: "hotel-1", ok: true },
						{ member_id: "2", name: "hotel-2", ok: true },
					],
				}),
			),
		);
		renderReset();
		await userEvent.click(
			screen.getByRole("button", { name: /^Reset admin token$/i }),
		);
		const dialog = await screen.findByRole("dialog");
		const confirm = within(dialog).getByRole("button", {
			name: /Reset and show token/i,
		});
		expect(confirm).toBeDisabled();
		await userEvent.click(within(dialog).getByRole("checkbox"));
		await userEvent.click(confirm);
		// Reveal-once modal shows the new token.
		expect(
			await screen.findByDisplayValue("abcdef0123456789abcdef0123456789"),
		).toBeInTheDocument();
	});
});
