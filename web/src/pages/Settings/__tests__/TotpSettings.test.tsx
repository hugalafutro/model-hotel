import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { TotpSettings } from "../TotpSettings";

const ENROLL_URI =
	"otpauth://totp/Model%20Hotel:admin?secret=JBSWY3DPEHPK3PXP&issuer=Model+Hotel&algorithm=SHA1&digits=6&period=30";
const ENROLL_SECRET = "JBSWY3DPEHPK3PXP";
const RECOVERY_CODES = [
	"AAAA-BBBB-CCCC-DDDD",
	"EEEE-FFFF-GGGG-HHHH",
	"1111-2222-3333-4444",
];

/** Register a status handler returning {enabled}. First-match-wins. */
function mockStatus(enabled: boolean) {
	server.use(
		http.get("/api/totp/status", () => HttpResponse.json({ enabled })),
	);
}

describe("TotpSettings", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		server.resetHandlers();
		// The default status handler in handlers.ts has no /api/totp/* route,
		// so status resolves to undefined -> useQuery error -> enabled=false.
		// Tests that need a specific status call mockStatus() themselves.
	});

	it("renders disabled view with Enable button when status is disabled", async () => {
		mockStatus(false);

		renderWithProviders(<TotpSettings collapsed={false} onToggle={() => {}} />);

		expect(
			await screen.findByRole("button", { name: /Enable TOTP/i }),
		).toBeInTheDocument();
	});

	it("shows QR + secret + code input after clicking Enable", async () => {
		mockStatus(false);
		server.use(
			http.post("/api/totp/enroll/start", () =>
				HttpResponse.json({ uri: ENROLL_URI, secret: ENROLL_SECRET }),
			),
		);

		const { user } = renderWithProviders(
			<TotpSettings collapsed={false} onToggle={() => {}} />,
		);

		const enableBtn = await screen.findByRole("button", {
			name: /Enable TOTP/i,
		});
		await user.click(enableBtn);

		// QR image appears (by alt text)
		expect(
			await screen.findByRole("img", { name: /TOTP enrollment QR/i }),
		).toBeInTheDocument();
		// Secret shown as a <code> element with the secret text
		expect(await screen.findByText(ENROLL_SECRET)).toBeInTheDocument();
		// Verify code input present by aria-label
		expect(
			screen.getByLabelText(/TOTP verification code/i),
		).toBeInTheDocument();
	});

	it("reveals recovery codes once after successful verify", async () => {
		mockStatus(false);
		server.use(
			http.post("/api/totp/enroll/start", () =>
				HttpResponse.json({ uri: ENROLL_URI, secret: ENROLL_SECRET }),
			),
			http.post("/api/totp/enroll/verify", () =>
				HttpResponse.json({ recovery_codes: RECOVERY_CODES }),
			),
		);

		const { user } = renderWithProviders(
			<TotpSettings collapsed={false} onToggle={() => {}} />,
		);

		const enableBtn = await screen.findByRole("button", {
			name: /Enable TOTP/i,
		});
		await user.click(enableBtn);

		const codeInput = await screen.findByLabelText(/TOTP verification code/i);
		await user.type(codeInput, "123456");

		const verifyBtn = await screen.findByRole("button", {
			name: /Verify TOTP code and enable/i,
		});
		await user.click(verifyBtn);

		// After verify: status is invalidated and re-fetched (still disabled
		// in this test since mockStatus(false) holds), but the recovery view
		// is driven by local state (showRecovery && recoveryCodes.length>0).
		// All three codes should be visible.
		await waitFor(() => {
			expect(screen.getByText(RECOVERY_CODES[0])).toBeInTheDocument();
		});
		expect(screen.getByText(RECOVERY_CODES[1])).toBeInTheDocument();
		expect(screen.getByText(RECOVERY_CODES[2])).toBeInTheDocument();
		// Warning text present (recoveryCodesWarning)
		expect(
			screen.getByText(/Save these recovery codes now/i),
		).toBeInTheDocument();
	});

	it("downloads recovery codes as a .txt file", async () => {
		mockStatus(false);
		server.use(
			http.post("/api/totp/enroll/start", () =>
				HttpResponse.json({ uri: ENROLL_URI, secret: ENROLL_SECRET }),
			),
			http.post("/api/totp/enroll/verify", () =>
				HttpResponse.json({ recovery_codes: RECOVERY_CODES }),
			),
		);

		// jsdom implements neither URL.createObjectURL nor anchor downloads.
		const createObjectURL = vi.fn(() => "blob:mock");
		const revokeObjectURL = vi.fn();
		// Assign the static methods directly. Do NOT vi.stubGlobal here: it would
		// be reverted by vi.unstubAllGlobals and clobber the navigator clipboard
		// stub from test/setup.ts for later tests in this file.
		URL.createObjectURL = createObjectURL;
		URL.revokeObjectURL = revokeObjectURL;
		const clickSpy = vi
			.spyOn(HTMLAnchorElement.prototype, "click")
			.mockImplementation(() => {});

		const { user } = renderWithProviders(
			<TotpSettings collapsed={false} onToggle={() => {}} />,
		);

		await user.click(
			await screen.findByRole("button", { name: /Enable TOTP/i }),
		);
		await user.type(
			await screen.findByLabelText(/TOTP verification code/i),
			"123456",
		);
		await user.click(
			await screen.findByRole("button", {
				name: /Verify TOTP code and enable/i,
			}),
		);

		const downloadBtn = await screen.findByRole("button", {
			name: /Download recovery codes as a text file/i,
		});
		await user.click(downloadBtn);

		expect(createObjectURL).toHaveBeenCalledTimes(1);
		expect(clickSpy).toHaveBeenCalledTimes(1);

		clickSpy.mockRestore();
	});

	it("installs the session token from enroll/verify to stay logged in", async () => {
		mockStatus(false);
		server.use(
			http.post("/api/totp/enroll/start", () =>
				HttpResponse.json({ uri: ENROLL_URI, secret: ENROLL_SECRET }),
			),
			http.post("/api/totp/enroll/verify", () =>
				HttpResponse.json({
					recovery_codes: RECOVERY_CODES,
					token: "sess-tok-123",
				}),
			),
		);

		const { user } = renderWithProviders(
			<TotpSettings collapsed={false} onToggle={() => {}} />,
		);

		await user.click(
			await screen.findByRole("button", { name: /Enable TOTP/i }),
		);
		await user.type(
			await screen.findByLabelText(/TOTP verification code/i),
			"123456",
		);
		await user.click(
			await screen.findByRole("button", {
				name: /Verify TOTP code and enable/i,
			}),
		);

		await waitFor(() => {
			expect(screen.getByText(RECOVERY_CODES[0])).toBeInTheDocument();
		});
		// The minted session token is installed so the dashboard stays authed.
		expect(localStorage.getItem("adminToken")).toBe("sess-tok-123");
	});

	it("clears recovery codes after I have saved them", async () => {
		mockStatus(false);
		server.use(
			http.post("/api/totp/enroll/start", () =>
				HttpResponse.json({ uri: ENROLL_URI, secret: ENROLL_SECRET }),
			),
			http.post("/api/totp/enroll/verify", () =>
				HttpResponse.json({ recovery_codes: RECOVERY_CODES }),
			),
		);

		const { user } = renderWithProviders(
			<TotpSettings collapsed={false} onToggle={() => {}} />,
		);

		// Walk through enroll + verify to reach recovery view
		const enableBtn = await screen.findByRole("button", {
			name: /Enable TOTP/i,
		});
		await user.click(enableBtn);
		const codeInput = await screen.findByLabelText(/TOTP verification code/i);
		await user.type(codeInput, "123456");
		const verifyBtn = await screen.findByRole("button", {
			name: /Verify TOTP code and enable/i,
		});
		await user.click(verifyBtn);

		// Wait for recovery codes to appear
		await waitFor(() => {
			expect(screen.getByText(RECOVERY_CODES[0])).toBeInTheDocument();
		});

		// Click the "I have saved my recovery codes" button
		const savedBtn = await screen.findByRole("button", {
			name: /Confirm recovery codes saved/i,
		});

		// The status query is invalidated; to land on the Enabled view,
		// the re-fetched status must now report enabled:true.
		// Override the status handler to return enabled BEFORE clicking saved.
		server.use(
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: true })),
		);
		await user.click(savedBtn);

		// Recovery codes gone, Enabled badge present
		await waitFor(() => {
			expect(screen.queryByText(RECOVERY_CODES[0])).not.toBeInTheDocument();
		});
		// Enabled view: the ui-badge-success with "Enabled" text + Disable button
		expect(screen.getByText("Enabled")).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: /Disable TOTP/i }),
		).toBeInTheDocument();
	});

	it("renders enabled view with Disable button when status is enabled", async () => {
		mockStatus(true);

		renderWithProviders(<TotpSettings collapsed={false} onToggle={() => {}} />);

		expect(
			await screen.findByRole("button", { name: /Disable TOTP/i }),
		).toBeInTheDocument();
		// Enabled badge present
		expect(screen.getByText("Enabled")).toBeInTheDocument();
	});

	it("disable flow submits the code", async () => {
		mockStatus(true);
		server.use(
			http.post("/api/totp/disable", () =>
				HttpResponse.json({ disabled: true }),
			),
		);

		const { user } = renderWithProviders(
			<TotpSettings collapsed={false} onToggle={() => {}} />,
		);

		const disableBtn = await screen.findByRole("button", {
			name: /Disable TOTP/i,
		});
		await user.click(disableBtn);

		// Disable code input appears
		const disableCodeInput = await screen.findByLabelText(
			/TOTP or recovery code to disable/i,
		);
		await user.type(disableCodeInput, "123456");

		const confirmBtn = await screen.findByRole("button", {
			name: /Confirm disable TOTP/i,
		});

		// After disable, status invalidates; re-fetch must report disabled
		// to land back on the disabled (Enable) view.
		server.use(
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
		);
		await user.click(confirmBtn);

		// Back to disabled view: Enable button reappears
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /Enable TOTP/i }),
			).toBeInTheDocument();
		});
	});

	it("shows error toast on enroll/verify failure", async () => {
		mockStatus(false);
		server.use(
			http.post("/api/totp/enroll/start", () =>
				HttpResponse.json({ uri: ENROLL_URI, secret: ENROLL_SECRET }),
			),
			http.post("/api/totp/enroll/verify", () =>
				HttpResponse.json({ message: "Invalid TOTP code" }, { status: 400 }),
			),
		);

		const { user } = renderWithProviders(
			<TotpSettings collapsed={false} onToggle={() => {}} />,
		);

		const enableBtn = await screen.findByRole("button", {
			name: /Enable TOTP/i,
		});
		await user.click(enableBtn);

		const codeInput = await screen.findByLabelText(/TOTP verification code/i);
		await user.type(codeInput, "000000");

		const verifyBtn = await screen.findByRole("button", {
			name: /Verify TOTP code and enable/i,
		});
		await user.click(verifyBtn);

		// enrollVerifyMutation.onError fires a toast with failedToVerify text.
		// Toast renders a <button> whose accessible name is the message.
		expect(
			await screen.findByRole("button", { name: /Invalid TOTP code/i }),
		).toBeInTheDocument();
	});

	it("renders nothing sensitive in initial disabled state", async () => {
		mockStatus(false);

		renderWithProviders(<TotpSettings collapsed={false} onToggle={() => {}} />);

		// Wait for the disabled view to settle (Enable button present)
		await screen.findByRole("button", { name: /Enable TOTP/i });

		// No QR, no recovery codes, no verify input
		expect(
			screen.queryByRole("img", { name: /TOTP enrollment QR/i }),
		).not.toBeInTheDocument();
		expect(
			screen.queryByLabelText(/TOTP verification code/i),
		).not.toBeInTheDocument();
		expect(
			screen.queryByText(/Save these recovery codes now/i),
		).not.toBeInTheDocument();
	});

	it("copies the secret and recovery codes, and cancels enrollment", async () => {
		mockStatus(false);
		server.use(
			http.post("/api/totp/enroll/start", () =>
				HttpResponse.json({ uri: ENROLL_URI, secret: ENROLL_SECRET }),
			),
			http.post("/api/totp/enroll/verify", () =>
				HttpResponse.json({ recovery_codes: RECOVERY_CODES }),
			),
		);
		const writeText = vi.fn().mockResolvedValue(undefined);
		Object.defineProperty(navigator, "clipboard", {
			value: { writeText },
			configurable: true,
		});
		const { user } = renderWithProviders(
			<TotpSettings collapsed={false} onToggle={() => {}} />,
		);

		await user.click(
			await screen.findByRole("button", { name: /Enable TOTP/i }),
		);
		// Copy the secret from the enrolling view.
		await user.click(
			await screen.findByRole("button", { name: /Copy TOTP secret/i }),
		);
		expect(await screen.findByText(/Secret copied/i)).toBeInTheDocument();

		// Cancel enrollment returns to the disabled view.
		await user.click(
			screen.getByRole("button", { name: /Cancel TOTP enrollment/i }),
		);
		expect(
			await screen.findByRole("button", { name: /Enable TOTP/i }),
		).toBeInTheDocument();

		// Re-enroll, verify, then copy all recovery codes from the reveal view.
		await user.click(screen.getByRole("button", { name: /Enable TOTP/i }));
		await user.type(
			await screen.findByLabelText(/TOTP verification code/i),
			"123456",
		);
		await user.click(
			await screen.findByRole("button", {
				name: /Verify TOTP code and enable/i,
			}),
		);
		await user.click(
			await screen.findByRole("button", { name: /Copy all recovery codes/i }),
		);
		expect(await screen.findByText(/Copied to clipboard/i)).toBeInTheDocument();
	});
});
