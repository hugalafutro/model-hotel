import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { MemberView } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { AdminTokenResetPanel } from "../AdminTokenPanels";

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

describe("AdminTokenResetPanel", () => {
	function renderReset() {
		return render(
			<ToastProvider>
				<AdminTokenResetPanel members={members} onChanged={() => {}} />
			</ToastProvider>,
		);
	}

	it("requires ack + MASTER_KEY, sends the key, then reveals the new token once", async () => {
		let sentKey: string | undefined;
		server.use(
			http.post("/api/admin-token/reset", async ({ request }) => {
				const body = (await request.json()) as {
					confirm: boolean;
					master_key: string;
				};
				sentKey = body.master_key;
				return HttpResponse.json({
					token: "abcdef0123456789abcdef0123456789",
					results: [
						{ member_id: "1", name: "hotel-1", ok: true },
						{ member_id: "2", name: "hotel-2", ok: true },
					],
				});
			}),
		);
		renderReset();
		await userEvent.click(
			screen.getByRole("button", { name: /^Reset admin token$/i }),
		);
		const dialog = await screen.findByRole("dialog");
		const confirm = within(dialog).getByRole("button", {
			name: /Reset and show token/i,
		});
		// Ack alone is not enough: the MASTER_KEY field still gates the action.
		await userEvent.click(within(dialog).getByRole("checkbox"));
		expect(confirm).toBeDisabled();
		await userEvent.type(
			within(dialog).getByLabelText(/Fleet MASTER_KEY/i),
			"the-master-key",
		);
		expect(confirm).toBeEnabled();
		await userEvent.click(confirm);
		// Reveal-once modal shows the new token, and the typed key reached the API.
		expect(
			await screen.findByDisplayValue("abcdef0123456789abcdef0123456789"),
		).toBeInTheDocument();
		expect(sentKey).toBe("the-master-key");
	});

	it("keeps the dialog open and surfaces the server message on a wrong MASTER_KEY", async () => {
		server.use(
			http.post("/api/admin-token/reset", () =>
				HttpResponse.text("MASTER_KEY does not match; reset not performed", {
					status: 403,
				}),
			),
		);
		renderReset();
		await userEvent.click(
			screen.getByRole("button", { name: /^Reset admin token$/i }),
		);
		const dialog = await screen.findByRole("dialog");
		await userEvent.click(within(dialog).getByRole("checkbox"));
		await userEvent.type(
			within(dialog).getByLabelText(/Fleet MASTER_KEY/i),
			"wrong-key",
		);
		await userEvent.click(
			within(dialog).getByRole("button", { name: /Reset and show token/i }),
		);
		// The 403 message is toasted and the dialog stays open for a retry.
		expect(
			await screen.findByText(/MASTER_KEY does not match/i),
		).toBeInTheDocument();
		expect(screen.getByRole("dialog")).toBeInTheDocument();
	});
});
