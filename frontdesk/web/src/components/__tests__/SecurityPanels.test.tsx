import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ToastProvider } from "../../context/ToastContext";
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
	localStorage.setItem("fdAuthToken", "tok");
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
					token: "session-token",
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

		// Recovery codes are revealed and the session token was installed.
		expect(await screen.findByText("code-aaa")).toBeInTheDocument();
		expect(screen.getByText("code-bbb")).toBeInTheDocument();
		expect(localStorage.getItem("fdAuthToken")).toBe("session-token");

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
});
