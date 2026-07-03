import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DashboardUser } from "../../../api/types";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { UserModal } from "../UserModal";

const withTotp: DashboardUser = {
	id: "11111111-2222-4333-8444-555555555555",
	username: "bob",
	display_name: "Bob B",
	email: null,
	role: "user",
	grants: ["chat"],
	enabled: true,
	created_at: "2026-07-01T10:00:00Z",
	updated_at: "2026-07-01T10:00:00Z",
	last_login_at: null,
	totp_enabled: true,
};

function mockGrants() {
	server.use(
		http.get("/api/users/grants", () =>
			HttpResponse.json({ grants: ["chat", "usage"] }),
		),
	);
}

describe("UserModal TOTP reset", () => {
	const onClose = vi.fn();
	const onToast = vi.fn();

	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("resets TOTP after confirmation", async () => {
		mockGrants();
		let resetCalled = false;
		server.use(
			http.post(`/api/users/${withTotp.id}/totp/reset`, () => {
				resetCalled = true;
				return HttpResponse.json({ ok: true });
			}),
		);
		const { user } = renderWithProviders(
			<UserModal user={withTotp} onClose={onClose} onToast={onToast} />,
		);

		await user.click(screen.getByTestId("user-modal-totp-reset"));
		await user.click(await screen.findByTestId("user-totp-reset-confirm"));

		await waitFor(() => {
			expect(resetCalled).toBe(true);
		});
		expect(onToast).toHaveBeenCalledWith(expect.any(String), "success");
	});

	it("hides the reset button when the user has no TOTP", async () => {
		mockGrants();
		renderWithProviders(
			<UserModal
				user={{ ...withTotp, totp_enabled: false }}
				onClose={onClose}
				onToast={onToast}
			/>,
		);
		expect(
			screen.queryByTestId("user-modal-totp-reset"),
		).not.toBeInTheDocument();
	});

	it("surfaces a reset failure in the modal error box", async () => {
		mockGrants();
		server.use(
			http.post(`/api/users/${withTotp.id}/totp/reset`, () =>
				HttpResponse.text("boom", { status: 500 }),
			),
		);
		const { user } = renderWithProviders(
			<UserModal user={withTotp} onClose={onClose} onToast={onToast} />,
		);

		await user.click(screen.getByTestId("user-modal-totp-reset"));
		await user.click(await screen.findByTestId("user-totp-reset-confirm"));

		expect(await screen.findByTestId("user-modal-error")).toBeInTheDocument();
	});
});
