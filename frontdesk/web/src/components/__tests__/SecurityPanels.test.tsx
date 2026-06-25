import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { SecurityPanels } from "../SecurityPanels";

function renderPanels() {
	return render(
		<ToastProvider>
			<SecurityPanels />
		</ToastProvider>,
	);
}

beforeEach(() => {
	localStorage.setItem("fdAuthToken", "tok");
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

	it("lists registered passkeys and deletes one", async () => {
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
		await waitFor(() => expect(deleted).toBe(true));
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
});
