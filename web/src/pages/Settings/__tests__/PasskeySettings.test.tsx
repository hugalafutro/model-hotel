import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import * as webauthnUtils from "../../../utils/webauthn";
import { PasskeyPanel } from "../PasskeySettings";

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

	it("shows a disabled state when WebAuthn is not available", async () => {
		vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(false);

		renderWithProviders(<PasskeyPanel />);

		expect(await screen.findByText("Disabled")).toBeInTheDocument();
	});

	it("renders the passkeys panel when WebAuthn is available", async () => {
		vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(true);

		renderWithProviders(<PasskeyPanel />);

		expect(
			await screen.findByText(/Register a passkey to sign in/),
		).toBeInTheDocument();
	});

	it("shows Register Passkey button", async () => {
		vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(true);

		renderWithProviders(<PasskeyPanel />);

		expect(
			await screen.findByRole("button", { name: /Register a new passkey/i }),
		).toBeInTheDocument();
	});

	it("lists credentials", async () => {
		vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(true);

		server.use(
			http.get("/api/webauthn/credentials", () =>
				HttpResponse.json([
					{
						id: "cred-123",
						name: "",
						transports: ["internal"],
						created_at: "2025-01-15T10:00:00Z",
						aaguid: "00000000-0000-0000-0000-000000000000",
						sign_count: 5,
					},
					{
						id: "cred-456",
						name: "My YubiKey",
						transports: ["usb", "nfc"],
						created_at: "2025-02-20T14:30:00Z",
						aaguid: "11111111-1111-1111-1111-111111111111",
						sign_count: 10,
					},
				]),
			),
		);

		renderWithProviders(<PasskeyPanel />);

		await waitFor(() => {
			expect(
				screen.getByText("Platform (Windows Hello / Touch ID)"),
			).toBeInTheDocument();
		});

		expect(screen.getByText("My YubiKey")).toBeInTheDocument();
	});

	it("displays credential date in human-friendly format", async () => {
		vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(true);

		server.use(
			http.get("/api/webauthn/credentials", () =>
				HttpResponse.json([
					{
						id: "cred-date",
						name: "Test Key",
						transports: ["internal"],
						created_at: "2025-03-28T12:00:00Z",
						aaguid: "00000000-0000-0000-0000-000000000000",
						sign_count: 1,
					},
				]),
			),
		);

		renderWithProviders(<PasskeyPanel />);

		await waitFor(() => {
			// formatDateTimeShort outputs locale-aware "28 <month> 2025, <time>" style
			expect(screen.getByText(/28.*2025.*,/)).toBeInTheDocument();
		});
	});

	it("deletes a credential on click", async () => {
		vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(true);

		let deleteCalled = false;
		server.use(
			http.get("/api/webauthn/credentials", () =>
				HttpResponse.json([
					{
						id: "cred-789",
						name: "",
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

		renderWithProviders(<PasskeyPanel />);

		await waitFor(() => {
			expect(screen.getByText("Bluetooth")).toBeInTheDocument();
		});

		const deleteButton = screen.getByRole("button", {
			name: /Delete passkey/i,
		});
		await userEvent.click(deleteButton);

		// Deletion now requires confirming in a dialog first.
		const confirmButton = await screen.findByRole("button", {
			name: "Delete",
		});
		await userEvent.click(confirmButton);

		await waitFor(() => {
			expect(deleteCalled).toBe(true);
		});

		expect(screen.getByText("Passkey deleted")).toBeInTheDocument();
	});

	it("does not delete when the confirmation is cancelled", async () => {
		vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(true);

		let deleteCalled = false;
		server.use(
			http.get("/api/webauthn/credentials", () =>
				HttpResponse.json([
					{
						id: "cred-789",
						name: "",
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

		renderWithProviders(<PasskeyPanel />);

		await waitFor(() => {
			expect(screen.getByText("Bluetooth")).toBeInTheDocument();
		});

		await userEvent.click(
			screen.getByRole("button", { name: /Delete passkey/i }),
		);

		// Cancel the confirmation dialog: nothing should be deleted.
		await userEvent.click(
			await screen.findByRole("button", { name: "Cancel" }),
		);

		await waitFor(() => {
			expect(screen.queryByText("Delete passkey?")).not.toBeInTheDocument();
		});
		expect(deleteCalled).toBe(false);
	});

	describe("Register flow", () => {
		it("shows success toast and refreshes credentials on successful registration", async () => {
			vi.clearAllMocks();
			server.resetHandlers();
			vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(true);
			vi.spyOn(webauthnUtils, "registerPasskey").mockResolvedValue(true);

			server.use(
				http.get("/api/webauthn/credentials", () =>
					HttpResponse.json([
						{
							id: "existing-cred",
							name: "Existing Key",
							transports: ["usb"],
							created_at: "2025-01-01T00:00:00Z",
							aaguid: "00000000-0000-0000-0000-000000000000",
							sign_count: 1,
						},
					]),
				),
			);

			renderWithProviders(<PasskeyPanel />);

			await waitFor(() => {
				expect(screen.getByText("Existing Key")).toBeInTheDocument();
			});

			const registerButton = await screen.findByRole("button", {
				name: /Register a new passkey/i,
			});
			await userEvent.click(registerButton);

			await screen.findByText("Passkey registered successfully");
		});

		it("shows no toast when user cancels registration (NotAllowedError)", async () => {
			vi.clearAllMocks();
			server.resetHandlers();
			vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(true);
			vi.spyOn(webauthnUtils, "registerPasskey").mockResolvedValue(false);

			server.use(
				http.get("/api/webauthn/credentials", () => HttpResponse.json([])),
			);

			renderWithProviders(<PasskeyPanel />);

			const registerButton = await screen.findByRole("button", {
				name: /Register a new passkey/i,
			});
			await userEvent.click(registerButton);

			// Wait a bit to ensure no toast appears
			await new Promise((resolve) => setTimeout(resolve, 100));
			expect(screen.queryByText(/Passkey registered/i)).not.toBeInTheDocument();
		});

		it("shows error toast when registration throws", async () => {
			vi.clearAllMocks();
			server.resetHandlers();
			vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(true);
			vi.spyOn(webauthnUtils, "registerPasskey").mockRejectedValue(
				new Error("Registration failed"),
			);

			server.use(
				http.get("/api/webauthn/credentials", () => HttpResponse.json([])),
			);

			renderWithProviders(<PasskeyPanel />);

			const registerButton = await screen.findByRole("button", {
				name: /Register a new passkey/i,
			});
			await userEvent.click(registerButton);

			await waitFor(() => {
				expect(
					screen.getByText(/Registration failed|Failed to register/i),
				).toBeInTheDocument();
			});
		});
	});

	describe("Rename flow", () => {
		it("enters edit mode when clicking on credential name", async () => {
			vi.clearAllMocks();
			server.resetHandlers();
			vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(true);

			server.use(
				http.get("/api/webauthn/credentials", () =>
					HttpResponse.json([
						{
							id: "cred-123",
							name: "Old Name",
							transports: [],
							created_at: "2025-01-15T10:00:00Z",
							aaguid: "00000000-0000-0000-0000-000000000000",
							sign_count: 5,
						},
					]),
				),
			);

			renderWithProviders(<PasskeyPanel />);

			await waitFor(() => {
				expect(screen.getByText("Old Name")).toBeInTheDocument();
			});

			const nameButton = screen.getByRole("button", {
				name: /Rename passkey/i,
			});
			await userEvent.click(nameButton);

			// Should show input field
			const input = screen.getByRole("textbox", {
				name: /Passkey name/i,
			});
			expect(input).toBeInTheDocument();
			expect(input).toHaveValue("Old Name");
		});

		it("saves renamed credential on Enter key", async () => {
			vi.clearAllMocks();
			server.resetHandlers();
			vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(true);

			let renameCalled = false;
			server.use(
				http.get("/api/webauthn/credentials", () =>
					HttpResponse.json([
						{
							id: "cred-123",
							name: "Old Name",
							transports: [],
							created_at: "2025-01-15T10:00:00Z",
							aaguid: "00000000-0000-0000-0000-000000000000",
							sign_count: 5,
						},
					]),
				),
				http.patch("/api/webauthn/credentials/:id", async ({ request }) => {
					const body = await request.json();
					renameCalled = true;
					expect(body).toEqual({ name: "New Name" });
					return new HttpResponse(null, { status: 204 });
				}),
			);

			renderWithProviders(<PasskeyPanel />);

			await waitFor(() => {
				expect(screen.getByText("Old Name")).toBeInTheDocument();
			});

			const nameButton = screen.getByRole("button", {
				name: /Rename passkey/i,
			});
			await userEvent.click(nameButton);

			const input = screen.getByRole("textbox", {
				name: /Passkey name/i,
			});
			await userEvent.clear(input);
			await userEvent.type(input, "New Name");
			await userEvent.keyboard("{Enter}");

			await waitFor(() => {
				expect(renameCalled).toBe(true);
			});
		});

		it("cancels rename mode on Escape key", async () => {
			vi.clearAllMocks();
			server.resetHandlers();
			vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(true);

			server.use(
				http.get("/api/webauthn/credentials", () =>
					HttpResponse.json([
						{
							id: "cred-123",
							name: "Old Name",
							transports: [],
							created_at: "2025-01-15T10:00:00Z",
							aaguid: "00000000-0000-0000-0000-000000000000",
							sign_count: 5,
						},
					]),
				),
			);

			renderWithProviders(<PasskeyPanel />);

			await waitFor(() => {
				expect(screen.getByText("Old Name")).toBeInTheDocument();
			});

			const nameButton = screen.getByRole("button", {
				name: /Rename passkey/i,
			});
			await userEvent.click(nameButton);

			// Verify input is present
			const input = screen.getByRole("textbox", { name: /Passkey name/i });
			expect(input).toBeInTheDocument();

			// Type new value and press Escape
			await userEvent.type(input, "New Name");
			await userEvent.keyboard("{Escape}");

			// Should return to view mode - input should be gone
			await waitFor(() => {
				expect(input).not.toBeInTheDocument();
			});

			// Old name should still be visible
			expect(screen.getByText("Old Name")).toBeInTheDocument();
		});
	});

	describe("CredentialRow display", () => {
		it("shows credential name when provided", async () => {
			vi.clearAllMocks();
			server.resetHandlers();
			vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(true);

			server.use(
				http.get("/api/webauthn/credentials", () =>
					HttpResponse.json([
						{
							id: "cred-123",
							name: "My Custom Key",
							transports: ["usb"],
							created_at: "2025-01-15T10:00:00Z",
							aaguid: "00000000-0000-0000-0000-000000000000",
							sign_count: 5,
						},
					]),
				),
			);

			renderWithProviders(<PasskeyPanel />);

			await waitFor(() => {
				expect(screen.getByText("My Custom Key")).toBeInTheDocument();
			});

			// Should NOT show transport labels when name is present
			expect(screen.queryByText("USB")).not.toBeInTheDocument();
		});

		it("shows transport labels when name is empty and transports exist", async () => {
			vi.clearAllMocks();
			server.resetHandlers();
			vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(true);

			server.use(
				http.get("/api/webauthn/credentials", () =>
					HttpResponse.json([
						{
							id: "cred-123",
							name: "",
							transports: ["nfc", "ble"],
							created_at: "2025-01-15T10:00:00Z",
							aaguid: "00000000-0000-0000-0000-000000000000",
							sign_count: 5,
						},
					]),
				),
			);

			renderWithProviders(<PasskeyPanel />);

			await waitFor(() => {
				expect(screen.getByText("NFC, Bluetooth")).toBeInTheDocument();
			});

			// Should NOT show "Security Key" when transports exist
			expect(screen.queryByText("Security Key")).not.toBeInTheDocument();
		});

		it("shows 'Security Key' when name is empty and no transports", async () => {
			vi.clearAllMocks();
			server.resetHandlers();
			vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(true);

			server.use(
				http.get("/api/webauthn/credentials", () =>
					HttpResponse.json([
						{
							id: "cred-123",
							name: "",
							transports: [],
							created_at: "2025-01-15T10:00:00Z",
							aaguid: "00000000-0000-0000-0000-000000000000",
							sign_count: 5,
						},
					]),
				),
			);

			renderWithProviders(<PasskeyPanel />);

			await waitFor(() => {
				expect(screen.getByText("Security Key")).toBeInTheDocument();
			});
		});
	});
});
