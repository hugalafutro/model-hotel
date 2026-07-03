import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { UserTotpStatus } from "../../../api/types";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { Security } from "../index";

function mockStatus(status: UserTotpStatus) {
	server.use(
		http.get("/api/auth/totp/status", () => HttpResponse.json(status)),
	);
}

describe("Security page", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	it("walks the enroll flow: start, verify, recovery codes", async () => {
		let enabled = false;
		server.use(
			http.get("/api/auth/totp/status", () =>
				HttpResponse.json({ enabled } satisfies UserTotpStatus),
			),
			http.post("/api/auth/totp/enroll/start", () =>
				HttpResponse.json({
					uri: "otpauth://totp/Model%20Hotel:alice?secret=JBSWY3DP",
					secret: "JBSWY3DPEHPK3PXP",
				}),
			),
			http.post("/api/auth/totp/enroll/verify", () => {
				enabled = true;
				return HttpResponse.json({
					recovery_codes: ["AAAA-BBBB", "CCCC-DDDD"],
				});
			}),
		);
		const { user } = renderWithProviders(<Security />);

		await user.click(await screen.findByTestId("security-enable-button"));

		// The provisional secret is shown for manual entry.
		expect(await screen.findByText("JBSWY3DPEHPK3PXP")).toBeInTheDocument();

		await user.type(screen.getByTestId("security-verify-code"), "123456");
		await user.click(screen.getByTestId("security-verify-button"));

		// Recovery codes revealed once after a successful verify.
		expect(await screen.findByText("AAAA-BBBB")).toBeInTheDocument();
		expect(screen.getByText("CCCC-DDDD")).toBeInTheDocument();
	});

	it("disables with a code from the enabled view", async () => {
		mockStatus({
			enabled: true,
			enabled_at: "2026-07-01T10:00:00Z",
			recovery_remaining: 9,
			recovery_total: 10,
		});
		let disableCode = "";
		server.use(
			http.post("/api/auth/totp/disable", async ({ request }) => {
				disableCode = ((await request.json()) as { code: string }).code;
				return HttpResponse.json({ ok: true });
			}),
		);
		const { user } = renderWithProviders(<Security />);

		// Recovery counter renders from the status payload.
		expect(await screen.findByText("9 / 10")).toBeInTheDocument();

		await user.click(screen.getByTestId("security-disable-toggle"));
		await user.type(
			await screen.findByTestId("security-disable-code"),
			"654321",
		);
		await user.click(screen.getByTestId("security-disable-confirm"));

		await waitFor(() => {
			expect(disableCode).toBe("654321");
		});
	});

	it("submits a password change with both passwords", async () => {
		mockStatus({ enabled: false });
		let payload: { current_password: string; new_password: string } | null =
			null;
		server.use(
			http.post("/api/auth/password", async ({ request }) => {
				payload = (await request.json()) as typeof payload;
				return HttpResponse.json({ ok: true });
			}),
		);
		const { user } = renderWithProviders(<Security />);

		const submit = await screen.findByTestId("security-password-submit");
		expect(submit).toBeDisabled();

		await user.type(
			screen.getByTestId("security-current-password"),
			"old-password",
		);
		await user.type(
			screen.getByTestId("security-new-password"),
			"new-password-1",
		);
		// A mismatching confirmation keeps the submit disabled.
		await user.type(
			screen.getByTestId("security-confirm-password"),
			"new-password-X",
		);
		expect(submit).toBeDisabled();

		await user.clear(screen.getByTestId("security-confirm-password"));
		await user.type(
			screen.getByTestId("security-confirm-password"),
			"new-password-1",
		);
		expect(submit).toBeEnabled();
		await user.click(submit);

		await waitFor(() => {
			expect(payload).toEqual({
				current_password: "old-password",
				new_password: "new-password-1",
			});
		});
	});

	it("keeps the session on a rejected current password", async () => {
		mockStatus({ enabled: false });
		server.use(
			http.post("/api/auth/password", () =>
				HttpResponse.text("current password is incorrect", { status: 401 }),
			),
		);
		const { user } = renderWithProviders(<Security />);

		await user.type(
			await screen.findByTestId("security-current-password"),
			"wrong-guess",
		);
		await user.type(
			screen.getByTestId("security-new-password"),
			"new-password-1",
		);
		await user.type(
			screen.getByTestId("security-confirm-password"),
			"new-password-1",
		);
		await user.click(screen.getByTestId("security-password-submit"));

		// The form stays usable for another attempt (no teardown happened).
		await waitFor(() => {
			expect(screen.getByTestId("security-password-submit")).toBeEnabled();
		});
	});

	it("shows the enable button when TOTP is off", async () => {
		mockStatus({ enabled: false });
		renderWithProviders(<Security />);
		expect(
			await screen.findByTestId("security-enable-button"),
		).toBeInTheDocument();
		expect(
			screen.queryByTestId("security-disable-toggle"),
		).not.toBeInTheDocument();
	});
});
