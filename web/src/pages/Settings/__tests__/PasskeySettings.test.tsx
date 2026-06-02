import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import * as webauthnUtils from "../../../utils/webauthn";
import { PasskeySettings } from "../PasskeySettings";

vi.mock("@simplewebauthn/browser", () => ({
	browserSupportsWebAuthn: vi.fn(() => true),
	startRegistration: vi.fn(),
	startAuthentication: vi.fn(),
}));

describe("PasskeySettings", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		server.resetHandlers();
	});

	it("renders nothing when WebAuthn is not available", () => {
		vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockReturnValue(false);

		const { container } = renderWithProviders(
			<PasskeySettings collapsed={false} onToggle={() => {}} />,
		);

		expect(container.textContent).not.toMatch(/Passkeys/i);
	});

	it("renders Passkeys section when WebAuthn is available", () => {
		vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockReturnValue(true);

		renderWithProviders(
			<PasskeySettings collapsed={false} onToggle={() => {}} />,
		);

		expect(screen.getByText("Passkeys")).toBeInTheDocument();
	});

	it("shows Register Passkey button", () => {
		vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockReturnValue(true);

		renderWithProviders(
			<PasskeySettings collapsed={false} onToggle={() => {}} />,
		);

		expect(
			screen.getByRole("button", { name: /Register a new passkey/i }),
		).toBeInTheDocument();
	});

	it("lists credentials", async () => {
		vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockReturnValue(true);

		server.use(
			http.get("/api/webauthn/credentials", () =>
				HttpResponse.json([
					{
						id: "cred-123",
						transports: ["internal"],
						created_at: "2025-01-15T10:00:00Z",
						aaguid: "00000000-0000-0000-0000-000000000000",
						sign_count: 5,
					},
					{
						id: "cred-456",
						transports: ["usb", "nfc"],
						created_at: "2025-02-20T14:30:00Z",
						aaguid: "11111111-1111-1111-1111-111111111111",
						sign_count: 10,
					},
				]),
			),
		);

		renderWithProviders(
			<PasskeySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			expect(
				screen.getByText("Platform (Windows Hello / Touch ID)"),
			).toBeInTheDocument();
		});

		expect(screen.getByText("USB Security Key, NFC")).toBeInTheDocument();
	});

	it("deletes a credential on click", async () => {
		vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockReturnValue(true);

		let deleteCalled = false;
		server.use(
			http.get("/api/webauthn/credentials", () =>
				HttpResponse.json([
					{
						id: "cred-789",
						transports: ["ble"],
						created_at: "2025-03-10T08:00:00Z",
						aaguid: "22222222-2222-2222-2222-222222222222",
						sign_count: 3,
					},
				]),
			),
			http.delete("/api/webauthn/credentials/:id", () => {
				deleteCalled = true;
				return new HttpResponse(null, { status: 204 });
			}),
		);

		renderWithProviders(
			<PasskeySettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => {
			expect(screen.getByText("Bluetooth")).toBeInTheDocument();
		});

		const deleteButton = screen.getByRole("button", {
			name: /Delete passkey/i,
		});
		await userEvent.click(deleteButton);

		await waitFor(() => {
			expect(deleteCalled).toBe(true);
		});

		expect(screen.getByText("Passkey deleted")).toBeInTheDocument();
	});
});
