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
