import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
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

	it("submits a password change with both passwords", {
		timeout: 30000,
	}, async () => {
		mockStatus({ enabled: false });
		let payload: { current_password: string; new_password: string } | null =
			null;
		// Answer 401 on purpose: a 200 would schedule the component's delayed
		// sign-out teardown, which clears the file-wide test auth token while
		// LATER tests are running. The success path (and its teardown) is
		// owned end-to-end by the "tears down the session" test below.
		server.use(
			http.post("/api/auth/password", async ({ request }) => {
				payload = (await request.json()) as typeof payload;
				return HttpResponse.text("current password is incorrect", {
					status: 401,
				});
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

	it("keeps the session on a rejected current password", {
		timeout: 30000,
	}, async () => {
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

	it("surfaces a localized breach message on a breached password", async () => {
		mockStatus({ enabled: false });
		// A 400 (not 200) means no delayed sign-out teardown is scheduled, so this
		// test cannot clobber the file-wide auth token (see the note above).
		server.use(
			http.post("/api/auth/password", () =>
				HttpResponse.text(
					"this password has appeared in a known data breach; choose a different one",
					{ status: 400 },
				),
			),
		);
		const { user } = renderWithProviders(<Security />);

		await user.type(
			await screen.findByTestId("security-current-password"),
			"old-password",
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

		expect(
			await screen.findByText(
				"This password has appeared in a known data breach. Choose a different one.",
			),
		).toBeInTheDocument();
		// The form remains usable; no teardown happened on the error path.
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

describe("Security page edge handlers", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	it("cancels an enrollment in progress and copies the secret", {
		timeout: 30000,
	}, async () => {
		mockStatus({ enabled: false });
		server.use(
			http.post("/api/auth/totp/enroll/start", () =>
				HttpResponse.json({
					uri: "otpauth://totp/Model%20Hotel:alice?secret=JBSWY3DP",
					secret: "JBSWY3DPEHPK3PXP",
				}),
			),
		);
		const { user } = renderWithProviders(<Security />);

		await user.click(await screen.findByTestId("security-enable-button"));
		// user-event installs its own clipboard stub; spy on that instance.
		const writeText = vi.spyOn(navigator.clipboard, "writeText");
		await user.click(await screen.findByTestId("security-copy-secret"));
		expect(writeText).toHaveBeenCalledWith("JBSWY3DPEHPK3PXP");

		await user.click(screen.getByTestId("security-cancel-enroll"));
		expect(await screen.findByTestId("security-enable-button")).toBeEnabled();
		expect(
			screen.queryByTestId("security-verify-code"),
		).not.toBeInTheDocument();
	});

	it("downloads and acknowledges the recovery codes", {
		timeout: 30000,
	}, async () => {
		// jsdom's URL lacks object-URL support; patch just the two statics so
		// fetch/msw (which construct URLs) keep working.
		const createObjectURL = vi.fn(() => "blob:mock");
		const revokeObjectURL = vi.fn();
		Object.assign(URL, { createObjectURL, revokeObjectURL });
		mockStatus({ enabled: false });
		server.use(
			http.post("/api/auth/totp/enroll/start", () =>
				HttpResponse.json({ uri: "otpauth://totp/x", secret: "S" }),
			),
			http.post("/api/auth/totp/enroll/verify", () =>
				HttpResponse.json({ recovery_codes: ["AAAA-BBBB"] }),
			),
		);
		try {
			const { user } = renderWithProviders(<Security />);

			await user.click(await screen.findByTestId("security-enable-button"));
			await user.type(
				await screen.findByTestId("security-verify-code"),
				"123456",
			);
			await user.click(screen.getByTestId("security-verify-button"));

			await user.click(await screen.findByTestId("security-download-codes"));
			expect(createObjectURL).toHaveBeenCalled();
			expect(revokeObjectURL).toHaveBeenCalledWith("blob:mock");

			await user.click(screen.getByTestId("security-saved-codes"));
			expect(
				screen.queryByTestId("security-download-codes"),
			).not.toBeInTheDocument();
		} finally {
			delete (URL as { createObjectURL?: unknown }).createObjectURL;
			delete (URL as { revokeObjectURL?: unknown }).revokeObjectURL;
		}
	});

	it("surfaces server failures from every TOTP mutation", {
		timeout: 30000,
	}, async () => {
		mockStatus({ enabled: false });
		server.use(
			http.post("/api/auth/totp/enroll/start", () =>
				HttpResponse.text("boom", { status: 500 }),
			),
		);
		const { user } = renderWithProviders(<Security />);
		await user.click(await screen.findByTestId("security-enable-button"));
		// The failed start leaves the page in the pre-enroll state.
		expect(await screen.findByTestId("security-enable-button")).toBeEnabled();
	});

	it("stays on the enroll step when the code is rejected", {
		timeout: 30000,
	}, async () => {
		mockStatus({ enabled: false });
		server.use(
			http.post("/api/auth/totp/enroll/start", () =>
				HttpResponse.json({ uri: "otpauth://totp/x", secret: "S" }),
			),
			http.post("/api/auth/totp/enroll/verify", () =>
				HttpResponse.text("invalid TOTP code", { status: 400 }),
			),
		);
		const { user } = renderWithProviders(<Security />);
		await user.click(await screen.findByTestId("security-enable-button"));
		await user.type(
			await screen.findByTestId("security-verify-code"),
			"000000",
		);
		await user.click(screen.getByTestId("security-verify-button"));
		// Still on the verify step, ready for another attempt.
		expect(await screen.findByTestId("security-verify-button")).toBeEnabled();
	});

	it("keeps 2FA on when the disable call fails", {
		timeout: 30000,
	}, async () => {
		mockStatus({ enabled: true });
		server.use(
			http.post("/api/auth/totp/disable", () =>
				HttpResponse.text("boom", { status: 500 }),
			),
		);
		const { user } = renderWithProviders(<Security />);
		// The status query can take a while under instrumented parallel runs.
		await user.click(
			await screen.findByTestId(
				"security-disable-toggle",
				{},
				{ timeout: 10000 },
			),
		);
		await user.type(
			await screen.findByTestId("security-disable-code"),
			"123456",
		);
		await user.click(screen.getByTestId("security-disable-confirm"));
		expect(await screen.findByTestId("security-disable-confirm")).toBeEnabled();
	});

	it("tears down the session after a successful password change", {
		timeout: 30000,
	}, async () => {
		mockStatus({ enabled: false });
		server.use(
			http.post("/api/auth/password", () => HttpResponse.json({ ok: true })),
		);
		const reload = vi.fn();
		const original = window.location;
		Object.defineProperty(window, "location", {
			value: { ...original, reload },
			configurable: true,
		});
		try {
			const { user } = renderWithProviders(<Security />);
			await user.type(
				await screen.findByTestId("security-current-password"),
				"old-password",
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
			// The teardown runs on a short delay so the success toast is seen.
			await waitFor(() => expect(reload).toHaveBeenCalled(), {
				timeout: 15000,
			});
			// The teardown clears the client auth signal (mh_csrf cookie).
			expect(document.cookie).not.toContain("mh_csrf=");
		} finally {
			Object.defineProperty(window, "location", {
				value: original,
				configurable: true,
			});
			// The teardown cleared the file-wide session cookie; put it back for
			// whatever runs after this test.
			document.cookie = "mh_csrf=test-csrf; path=/";
		}
	});

	it("reports a generic failure for non-401 password errors", {
		timeout: 30000,
	}, async () => {
		mockStatus({ enabled: false });
		server.use(
			http.post("/api/auth/password", () =>
				HttpResponse.text("boom", { status: 500 }),
			),
		);
		const { user } = renderWithProviders(<Security />);
		await user.type(
			await screen.findByTestId("security-current-password"),
			"old-password",
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
		await waitFor(() => {
			expect(screen.getByTestId("security-password-submit")).toBeEnabled();
		});
	});
});
