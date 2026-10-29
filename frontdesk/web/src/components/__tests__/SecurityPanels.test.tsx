import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api, onUnauthorized } from "../../api/client";
import { ToastProvider } from "../../context/ToastContext";
import i18n from "../../i18n";
import { server } from "../../test/server";
import { registerPasskey } from "../../utils/webauthn";
import { SecurityPanels } from "../SecurityPanels";

// registerPasskey runs the real browser WebAuthn ceremony, which jsdom cannot
// perform, so mock it and drive its success/error outcomes directly.
vi.mock("../../utils/webauthn", () => ({ registerPasskey: vi.fn() }));
const mockRegisterPasskey = vi.mocked(registerPasskey);

function renderPanels() {
	return render(
		<ToastProvider>
			<SecurityPanels />
		</ToastProvider>,
	);
}

beforeEach(() => {
	mockRegisterPasskey.mockReset();
});

describe("PasskeyPanel", () => {
	it("shows the not-configured warning when WebAuthn has no relying party", async () => {
		server.use(
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: false, has_credentials: false }),
			),
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
		);
		renderPanels();
		expect(
			await screen.findByText(/Passkeys are not configured/i),
		).toBeInTheDocument();
		// No register button while unconfigured.
		expect(
			screen.queryByRole("button", { name: /Register a passkey/i }),
		).toBeNull();
	});

	it("lists registered passkeys and deletes one after confirming", async () => {
		let creds = [
			{
				id: "c1",
				name: "YubiKey",
				transports: ["usb"],
				created_at: "2026-06-01T10:00:00Z",
				aaguid: "",
				sign_count: 0,
			},
		];
		let deleted = false;
		server.use(
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: true, has_credentials: true }),
			),
			http.get("/api/webauthn/credentials", () => HttpResponse.json(creds)),
			http.delete("/api/webauthn/credentials/c1", () => {
				deleted = true;
				creds = [];
				return new HttpResponse(null, { status: 204 });
			}),
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
		);
		renderPanels();
		expect(await screen.findByText("YubiKey")).toBeInTheDocument();
		await userEvent.click(
			screen.getByRole("button", { name: /Remove passkey/i }),
		);
		// A confirm dialog appears first; nothing is deleted until it is confirmed.
		const dialog = await screen.findByRole("dialog");
		expect(deleted).toBe(false);
		await userEvent.click(
			within(dialog).getByRole("button", { name: /^Remove$/i }),
		);
		await waitFor(() => expect(deleted).toBe(true));
	});

	it("falls back to a transport-derived name for an unnamed passkey", async () => {
		server.use(
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: true, has_credentials: true }),
			),
			http.get("/api/webauthn/credentials", () =>
				HttpResponse.json([
					{
						id: "c1",
						name: "",
						transports: ["internal"],
						created_at: "2026-06-01T10:00:00Z",
						aaguid: "",
						sign_count: 0,
					},
				]),
			),
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
		);
		renderPanels();
		// name is empty, so the "internal" transport renders as "Built-in".
		expect(await screen.findByText("Built-in")).toBeInTheDocument();
	});

	it("registers a passkey and shows a success toast", async () => {
		let credsFetches = 0;
		server.use(
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: true, has_credentials: false }),
			),
			http.get("/api/webauthn/credentials", () => {
				credsFetches += 1;
				// First load: none. loadCreds re-fetches after a successful register.
				return HttpResponse.json(
					credsFetches > 1
						? [
								{
									id: "c1",
									name: "New key",
									transports: ["usb"],
									created_at: "2026-06-01T10:00:00Z",
									aaguid: "",
									sign_count: 0,
								},
							]
						: [],
				);
			}),
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
		);
		mockRegisterPasskey.mockResolvedValue(true);
		renderPanels();

		await userEvent.click(
			await screen.findByRole("button", { name: /Register a passkey/i }),
		);

		expect(mockRegisterPasskey).toHaveBeenCalledTimes(1);
		expect(await screen.findByText("Passkey registered")).toBeInTheDocument();
		// loadCreds re-fetched and the freshly-registered credential is listed.
		expect(await screen.findByText("New key")).toBeInTheDocument();
	});

	it("surfaces an already-registered error toast", async () => {
		server.use(
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: true, has_credentials: false }),
			),
			http.get("/api/webauthn/credentials", () => HttpResponse.json([])),
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
		);
		const dup = new Error("dup");
		dup.name = "InvalidStateError";
		mockRegisterPasskey.mockRejectedValue(dup);
		renderPanels();

		await userEvent.click(
			await screen.findByRole("button", { name: /Register a passkey/i }),
		);

		expect(await screen.findByText(/already registered/i)).toBeInTheDocument();
	});

	it("renames a passkey inline", async () => {
		let renamedTo = "";
		server.use(
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: true, has_credentials: true }),
			),
			http.get("/api/webauthn/credentials", () =>
				HttpResponse.json([
					{
						id: "c1",
						name: "Old name",
						transports: ["usb"],
						created_at: "2026-06-01T10:00:00Z",
						aaguid: "",
						sign_count: 0,
					},
				]),
			),
			http.patch("/api/webauthn/credentials/c1", async ({ request }) => {
				const body = (await request.json()) as { name: string };
				renamedTo = body.name;
				return new HttpResponse(null, { status: 204 });
			}),
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
		);
		renderPanels();

		// Enter edit mode via the rename button, replace the draft, save.
		await userEvent.click(
			await screen.findByRole("button", { name: /Rename passkey/i }),
		);
		const input = await screen.findByLabelText("Passkey name");
		await userEvent.clear(input);
		await userEvent.type(input, "Work laptop");
		await userEvent.click(screen.getByRole("button", { name: /^Save$/i }));

		await waitFor(() => expect(renamedTo).toBe("Work laptop"));
	});
});

describe("TotpPanel", () => {
	it("offers enable and starts enrollment showing the secret", async () => {
		server.use(
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: false, has_credentials: false }),
			),
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
			http.post("/api/totp/enroll/start", () =>
				HttpResponse.json({
					uri: "otpauth://totp/FrontDesk:admin?secret=JBSWY3DPEHPK3PXP",
					secret: "JBSWY3DPEHPK3PXP",
				}),
			),
		);
		renderPanels();
		const enable = await screen.findByRole("button", { name: /^Enable$/i });
		await userEvent.click(enable);
		expect(await screen.findByText("JBSWY3DPEHPK3PXP")).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: /^Verify$/i }),
		).toBeInTheDocument();
	});

	it("shows enabled status with recovery counts and runs disable", async () => {
		let enabled = true;
		let disabled = false;
		server.use(
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: false, has_credentials: false }),
			),
			http.get("/api/totp/status", () =>
				HttpResponse.json({
					enabled,
					enabled_at: "2026-06-10T08:00:00Z",
				}),
			),
			http.get("/api/totp/info", () =>
				HttpResponse.json({ recovery_remaining: 8, recovery_total: 10 }),
			),
			http.post("/api/totp/disable", () => {
				disabled = true;
				enabled = false;
				return new HttpResponse(null, { status: 204 });
			}),
		);
		renderPanels();
		expect(await screen.findByText("Enabled")).toBeInTheDocument();
		expect(await screen.findByText(/8 \/ 10/)).toBeInTheDocument();

		// Reveal the disable field, enter a code, confirm.
		await userEvent.click(screen.getByRole("button", { name: /^Disable$/i }));
		const input = await screen.findByLabelText(/Code or recovery code/i);
		await userEvent.type(input, "123456");
		// Two Disable buttons now (header toggle + confirm); the last is confirm.
		const disableButtons = screen.getAllByRole("button", {
			name: /^Disable$/i,
		});
		await userEvent.click(disableButtons[disableButtons.length - 1]);
		await waitFor(() => expect(disabled).toBe(true));
	});

	it("keeps the operator signed in when the disable code is wrong", async () => {
		// The server answers 403 for a wrong code; only a 401 means the session
		// is dead, and that is what the client logs out on.
		const loggedOut = vi.fn();
		const stop = onUnauthorized(loggedOut);
		server.use(
			http.get("/api/totp/status", () =>
				HttpResponse.json({
					enabled: true,
					recovery_remaining: 8,
					recovery_total: 10,
				}),
			),
			http.post("/api/totp/disable", () =>
				HttpResponse.text("invalid TOTP or recovery code", { status: 403 }),
			),
		);
		renderPanels();
		expect(await screen.findByText("Enabled")).toBeInTheDocument();
		await userEvent.click(screen.getByRole("button", { name: /^Disable$/i }));
		await userEvent.type(
			await screen.findByLabelText(/Code or recovery code/i),
			"000000",
		);
		const disableButtons = screen.getAllByRole("button", {
			name: /^Disable$/i,
		});
		await userEvent.click(disableButtons[disableButtons.length - 1]);

		expect(
			await screen.findByText("Could not disable, check the code"),
		).toBeInTheDocument();
		expect(screen.getByText("Enabled")).toBeInTheDocument();
		expect(loggedOut).not.toHaveBeenCalled();
		stop();
	});

	it("verifies a code, reveals recovery codes, and copies/downloads/saves them", async () => {
		const writeText = vi.fn().mockResolvedValue(undefined);
		Object.defineProperty(navigator, "clipboard", {
			value: { writeText },
			configurable: true,
		});
		// jsdom logs "Not implemented: navigation" on a real anchor click; stub it.
		const clickSpy = vi
			.spyOn(HTMLAnchorElement.prototype, "click")
			.mockImplementation(() => {});
		const createObjectURL = vi.fn(() => "blob:codes");
		const revokeObjectURL = vi.fn();
		Object.defineProperty(URL, "createObjectURL", {
			value: createObjectURL,
			configurable: true,
		});
		Object.defineProperty(URL, "revokeObjectURL", {
			value: revokeObjectURL,
			configurable: true,
		});

		server.use(
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: false, has_credentials: false }),
			),
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
			http.post("/api/totp/enroll/start", () =>
				HttpResponse.json({
					uri: "otpauth://totp/FrontDesk:admin?secret=JBSWY3DPEHPK3PXP",
					secret: "JBSWY3DPEHPK3PXP",
				}),
			),
			http.post("/api/totp/enroll/verify", () =>
				HttpResponse.json({
					recovery_codes: ["code-aaa", "code-bbb"],
				}),
			),
		);
		renderPanels();

		await userEvent.click(
			await screen.findByRole("button", { name: /^Enable$/i }),
		);
		const codeInput = await screen.findByLabelText(/Enter the 6-digit code/i);
		await userEvent.type(codeInput, "123456");
		await userEvent.click(screen.getByRole("button", { name: /^Verify$/i }));

		// Recovery codes are revealed; the server rotated the session cookie itself.
		expect(await screen.findByText("code-aaa")).toBeInTheDocument();
		expect(screen.getByText("code-bbb")).toBeInTheDocument();

		// Copy all pushes the joined codes to the clipboard.
		await userEvent.click(screen.getByRole("button", { name: /Copy all/i }));
		await waitFor(() =>
			expect(writeText).toHaveBeenCalledWith("code-aaa\ncode-bbb"),
		);

		// Download builds a blob object URL and revokes it.
		await userEvent.click(screen.getByRole("button", { name: /^Download$/i }));
		expect(createObjectURL).toHaveBeenCalledTimes(1);
		expect(clickSpy).toHaveBeenCalledTimes(1);
		expect(revokeObjectURL).toHaveBeenCalledWith("blob:codes");

		// "I have saved these" dismisses the reveal and returns to the enable view.
		await userEvent.click(
			screen.getByRole("button", { name: /I have saved these/i }),
		);
		expect(
			await screen.findByRole("button", { name: /^Enable$/i }),
		).toBeInTheDocument();
	});

	it("shows an error toast when enrollment verification fails", async () => {
		server.use(
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: false, has_credentials: false }),
			),
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
			http.post("/api/totp/enroll/start", () =>
				HttpResponse.json({
					uri: "otpauth://totp/FrontDesk:admin?secret=JBSWY3DPEHPK3PXP",
					secret: "JBSWY3DPEHPK3PXP",
				}),
			),
			http.post("/api/totp/enroll/verify", () =>
				HttpResponse.json({ error: "bad code" }, { status: 400 }),
			),
		);
		renderPanels();

		await userEvent.click(
			await screen.findByRole("button", { name: /^Enable$/i }),
		);
		const codeInput = await screen.findByLabelText(/Enter the 6-digit code/i);
		await userEvent.type(codeInput, "000000");
		await userEvent.click(screen.getByRole("button", { name: /^Verify$/i }));

		expect(await screen.findByText(/Invalid code/i)).toBeInTheDocument();
	});

	it("tells the operator to wait when verify is throttled", async () => {
		server.use(
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: false, has_credentials: false }),
			),
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
			http.post("/api/totp/enroll/start", () =>
				HttpResponse.json({
					uri: "otpauth://totp/FrontDesk:admin?secret=JBSWY3DPEHPK3PXP",
					secret: "JBSWY3DPEHPK3PXP",
				}),
			),
			http.post("/api/totp/enroll/verify", () =>
				HttpResponse.text("too many attempts", { status: 429 }),
			),
		);
		renderPanels();

		await userEvent.click(
			await screen.findByRole("button", { name: /^Enable$/i }),
		);
		await userEvent.type(
			await screen.findByLabelText(/Enter the 6-digit code/i),
			"000000",
		);
		await userEvent.click(screen.getByRole("button", { name: /^Verify$/i }));

		expect(
			await screen.findByText("Too many attempts, wait a moment and try again"),
		).toBeInTheDocument();
	});

	it("tells the operator to wait when disable is throttled", async () => {
		server.use(
			http.get("/api/totp/status", () =>
				HttpResponse.json({
					enabled: true,
					recovery_remaining: 8,
					recovery_total: 10,
				}),
			),
			http.post("/api/totp/disable", () =>
				HttpResponse.text("too many attempts", { status: 429 }),
			),
		);
		renderPanels();
		expect(await screen.findByText("Enabled")).toBeInTheDocument();
		await userEvent.click(screen.getByRole("button", { name: /^Disable$/i }));
		await userEvent.type(
			await screen.findByLabelText(/Code or recovery code/i),
			"000000",
		);
		const disableButtons = screen.getAllByRole("button", {
			name: /^Disable$/i,
		});
		await userEvent.click(disableButtons[disableButtons.length - 1]);

		expect(
			await screen.findByText("Too many attempts, wait a moment and try again"),
		).toBeInTheDocument();
	});

	it("copies the enrollment secret and toasts either outcome", async () => {
		const writeText = vi
			.fn()
			.mockResolvedValueOnce(undefined)
			.mockRejectedValueOnce(new Error("denied"));
		Object.defineProperty(navigator, "clipboard", {
			value: { writeText },
			configurable: true,
		});
		server.use(
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: false, has_credentials: false }),
			),
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
			http.post("/api/totp/enroll/start", () =>
				HttpResponse.json({
					uri: "otpauth://totp/FrontDesk:admin?secret=JBSWY3DPEHPK3PXP",
					secret: "JBSWY3DPEHPK3PXP",
				}),
			),
		);
		renderPanels();

		await userEvent.click(
			await screen.findByRole("button", { name: /^Enable$/i }),
		);
		const copyButton = await screen.findByRole("button", { name: /^Secret$/i });

		// Both outcomes are asserted through the toast region and the i18n keys
		// themselves, so neither a reworded string nor another "Copy" elsewhere
		// on the page can decide the result.
		const toasts = () => within(screen.getByRole("status"));

		await userEvent.click(copyButton);
		expect(writeText).toHaveBeenCalledWith("JBSWY3DPEHPK3PXP");
		await waitFor(() =>
			expect(
				toasts().getByText(i18n.t("settings.totp.secretCopied")),
			).toBeInTheDocument(),
		);

		// A refused clipboard says so instead of leaving the operator to guess.
		await userEvent.click(copyButton);
		await waitFor(() =>
			expect(
				toasts().getByText(i18n.t("common.failedToCopy")),
			).toBeInTheDocument(),
		);
	});
});

describe("PasskeyPanel edge paths", () => {
	const cred = (over: Partial<Record<string, unknown>>) => ({
		id: "c1",
		name: "Key",
		transports: ["usb"],
		created_at: "2026-06-01T10:00:00Z",
		aaguid: "",
		sign_count: 0,
		...over,
	});
	const configured = (creds: unknown[]) =>
		server.use(
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: true, has_credentials: true }),
			),
			http.get("/api/webauthn/credentials", () => HttpResponse.json(creds)),
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
		);
	const toasts = () => within(screen.getByRole("status"));

	it("labels every known transport and passes an unknown one through", async () => {
		configured([
			cred({
				id: "c1",
				name: "",
				transports: ["usb", "nfc", "ble", "hybrid", "smart-card"],
			}),
			cred({ id: "c2", name: "", transports: [] }),
		]);
		renderPanels();
		const joined = [
			i18n.t("settings.passkeys.transportUsb"),
			i18n.t("settings.passkeys.transportNfc"),
			i18n.t("settings.passkeys.transportBle"),
			i18n.t("settings.passkeys.transportHybrid"),
			"smart-card",
		].join(", ");
		expect(await screen.findByText(joined)).toBeInTheDocument();
		// No name and no transports falls back to the generic label.
		expect(
			screen.getByText(i18n.t("settings.passkeys.securityKey")),
		).toBeInTheDocument();
	});

	it("does not load credentials when unmounted before availability resolves", async () => {
		// The availability read is held by a promise the test resolves after
		// unmount; awaiting that same promise inside act lets the panel's own
		// handler (registered first) run before the assertion.
		let resolve!: (v: { enabled: boolean; has_credentials: boolean }) => void;
		const pending = new Promise<{ enabled: boolean; has_credentials: boolean }>(
			(r) => {
				resolve = r;
			},
		);
		const available = vi
			.spyOn(api, "webauthnAvailable")
			.mockReturnValue(pending);
		const list = vi.spyOn(api, "webauthnListCredentials");
		server.use(
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
		);
		const { unmount } = renderPanels();
		expect(available).toHaveBeenCalledTimes(1);
		unmount();
		await act(async () => {
			resolve({ enabled: true, has_credentials: true });
			await pending;
		});
		expect(list).not.toHaveBeenCalled();
		available.mockRestore();
		list.mockRestore();
	});

	it("cancelling the delete confirmation removes nothing", async () => {
		let deleted = false;
		configured([cred({})]);
		server.use(
			http.delete("/api/webauthn/credentials/c1", () => {
				deleted = true;
				return new HttpResponse(null, { status: 204 });
			}),
		);
		renderPanels();
		await userEvent.click(
			await screen.findByRole("button", {
				name: i18n.t("settings.passkeys.deleteLabelFor", { name: "Key" }),
			}),
		);
		const dialog = await screen.findByRole("dialog");
		await userEvent.click(
			within(dialog).getByRole("button", { name: i18n.t("common.cancel") }),
		);
		await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
		expect(deleted).toBe(false);
	});

	it("toasts deleteFailed when the delete call fails", async () => {
		configured([cred({})]);
		server.use(
			http.delete("/api/webauthn/credentials/c1", () =>
				HttpResponse.json({ error: "boom" }, { status: 500 }),
			),
		);
		renderPanels();
		await userEvent.click(
			await screen.findByRole("button", {
				name: i18n.t("settings.passkeys.deleteLabelFor", { name: "Key" }),
			}),
		);
		const dialog = await screen.findByRole("dialog");
		await userEvent.click(
			within(dialog).getByRole("button", { name: i18n.t("common.remove") }),
		);
		await waitFor(() =>
			expect(
				toasts().getByText(i18n.t("settings.passkeys.deleteFailed")),
			).toBeInTheDocument(),
		);
	});

	it("Enter saves a rename and a failed rename toasts renameFailed", async () => {
		configured([cred({ name: "Old" })]);
		server.use(
			http.patch("/api/webauthn/credentials/c1", () =>
				HttpResponse.json({ error: "boom" }, { status: 500 }),
			),
		);
		renderPanels();
		await userEvent.click(
			await screen.findByRole("button", {
				name: i18n.t("settings.passkeys.renameLabelFor", { name: "Old" }),
			}),
		);
		const input = screen.getByLabelText(i18n.t("settings.passkeys.nameLabel"));
		await userEvent.clear(input);
		await userEvent.type(input, "New{Enter}");
		await waitFor(() =>
			expect(
				toasts().getByText(i18n.t("settings.passkeys.renameFailed")),
			).toBeInTheDocument(),
		);
	});

	it("Escape leaves rename mode without saving", async () => {
		let patched = false;
		configured([cred({ name: "Old" })]);
		server.use(
			http.patch("/api/webauthn/credentials/c1", () => {
				patched = true;
				return new HttpResponse(null, { status: 204 });
			}),
		);
		renderPanels();
		await userEvent.click(
			await screen.findByRole("button", {
				name: i18n.t("settings.passkeys.renameLabelFor", { name: "Old" }),
			}),
		);
		const input = screen.getByLabelText(i18n.t("settings.passkeys.nameLabel"));
		await userEvent.type(input, "x{Escape}");
		expect(
			screen.queryByLabelText(i18n.t("settings.passkeys.nameLabel")),
		).toBeNull();
		expect(patched).toBe(false);
	});
	it("the cancel button leaves rename mode with the saved name intact", async () => {
		configured([cred({ name: "Old" })]);
		renderPanels();
		const label = i18n.t("settings.passkeys.renameLabelFor", { name: "Old" });
		await userEvent.click(await screen.findByRole("button", { name: label }));
		await userEvent.type(
			screen.getByLabelText(i18n.t("settings.passkeys.nameLabel")),
			"zzz",
		);
		await userEvent.click(
			screen.getByRole("button", { name: i18n.t("common.cancel") }),
		);
		expect(
			screen.queryByLabelText(i18n.t("settings.passkeys.nameLabel")),
		).toBeNull();
		expect(screen.getByRole("button", { name: label })).toBeInTheDocument();
	});
});
