import type {
	PublicKeyCredentialCreationOptionsJSON,
	PublicKeyCredentialRequestOptionsJSON,
} from "@simplewebauthn/browser";
import * as simplewebauthn from "@simplewebauthn/browser";
import { HttpResponse, http } from "msw";
import { server } from "../../test/mocks/server";
import * as webauthn from "../webauthn";

vi.mock("@simplewebauthn/browser", () => ({
	browserSupportsWebAuthn: vi.fn(() => true),
	startRegistration: vi.fn(),
	startAuthentication: vi.fn(),
}));

// jsdom's DOMException does not consistently set .name on the instance,
// so the "instanceof Error && err.name === X" check in the source code
// never matches. Use a plain Error with the name set manually instead.
class MockDOMError extends Error {
	constructor(message: string, name: string) {
		super(message);
		this.name = name;
	}
}

describe("webauthn utils", () => {
	beforeEach(() => {
		server.resetHandlers();
		webauthn.resetWebAuthnCache();
	});

	describe("isWebAuthnAvailable", () => {
		it("returns false when browser does not support WebAuthn", async () => {
			vi.spyOn(simplewebauthn, "browserSupportsWebAuthn").mockReturnValue(
				false,
			);
			const result = await webauthn.isWebAuthnAvailable();
			expect(result).toBe(false);
		});

		it("returns false when server endpoint returns disabled", async () => {
			vi.spyOn(simplewebauthn, "browserSupportsWebAuthn").mockReturnValue(true);
			server.use(
				http.get("/api/webauthn/available", () =>
					HttpResponse.json({ enabled: false }),
				),
			);
			const result = await webauthn.isWebAuthnAvailable();
			expect(result).toBe(false);
		});

		it("returns true when browser supports and server enables WebAuthn", async () => {
			vi.spyOn(simplewebauthn, "browserSupportsWebAuthn").mockReturnValue(true);
			server.use(
				http.get("/api/webauthn/available", () =>
					HttpResponse.json({ enabled: true }),
				),
			);
			const result = await webauthn.isWebAuthnAvailable();
			expect(result).toBe(true);
		});

		it("caches the server response", async () => {
			vi.spyOn(simplewebauthn, "browserSupportsWebAuthn").mockReturnValue(true);
			let callCount = 0;
			server.use(
				http.get("/api/webauthn/available", () => {
					callCount++;
					return HttpResponse.json({ enabled: true });
				}),
			);
			await webauthn.isWebAuthnAvailable();
			await webauthn.isWebAuthnAvailable();
			expect(callCount).toBe(1);
		});

		it("returns false when server endpoint errors", async () => {
			vi.spyOn(simplewebauthn, "browserSupportsWebAuthn").mockReturnValue(true);
			server.use(
				http.get("/api/webauthn/available", () => HttpResponse.error()),
			);
			const result = await webauthn.isWebAuthnAvailable();
			expect(result).toBe(false);
		});
	});

	describe("resetWebAuthnCache", () => {
		it("resets the server cache so next call refetches", async () => {
			vi.spyOn(simplewebauthn, "browserSupportsWebAuthn").mockReturnValue(true);
			let callCount = 0;
			server.use(
				http.get("/api/webauthn/available", () => {
					callCount++;
					return HttpResponse.json({ enabled: callCount === 1 });
				}),
			);
			await webauthn.isWebAuthnAvailable();
			webauthn.resetWebAuthnCache();
			await webauthn.isWebAuthnAvailable();
			expect(callCount).toBe(2);
		});
	});

	describe("registerPasskey", () => {
		const mockSessionId = "session-123";
		const mockOptions = {
			challenge: "challenge-abc",
			rp: { name: "Model Hotel", id: "localhost" },
			user: { id: "user-123", name: "test@example.com", displayName: "Test" },
			pubKeyCredParams: [{ type: "public-key", alg: -7 }],
		} as PublicKeyCredentialCreationOptionsJSON;

		it("successfully registers a passkey", async () => {
			server.use(
				http.post("/api/webauthn/register/start", () =>
					HttpResponse.json({
						session_id: mockSessionId,
						options: mockOptions,
					}),
				),
				http.post(
					"/api/webauthn/register/finish",
					() => new HttpResponse(null, { status: 204 }),
				),
			);
			vi.spyOn(simplewebauthn, "startRegistration").mockResolvedValue({
				id: "cred-abc",
				rawId: "cred-abc",
				type: "public-key",
				response: {
					clientDataJSON: "client-data",
					attestationObject: "attestation-obj",
				},
				getClientExtensionResults: () => ({}),
				toJSON: () => ({}),
			});
			const result = await webauthn.registerPasskey();
			expect(result).toBe(true);
		});

		it("returns false when user cancels (NotAllowedError)", async () => {
			server.use(
				http.post("/api/webauthn/register/start", () =>
					HttpResponse.json({
						session_id: mockSessionId,
						options: mockOptions,
					}),
				),
			);
			vi.spyOn(simplewebauthn, "startRegistration").mockRejectedValue(
				new MockDOMError("User cancelled", "NotAllowedError"),
			);
			const result = await webauthn.registerPasskey();
			expect(result).toBe(false);
		});

		it("throws on other errors", async () => {
			server.use(
				http.post("/api/webauthn/register/start", () =>
					HttpResponse.json({
						session_id: mockSessionId,
						options: mockOptions,
					}),
				),
			);
			vi.spyOn(simplewebauthn, "startRegistration").mockRejectedValue(
				new Error("Registration failed"),
			);
			await expect(webauthn.registerPasskey()).rejects.toThrow(
				"Registration failed",
			);
		});

		it("throws InvalidStateError when credential already exists", async () => {
			server.use(
				http.post("/api/webauthn/register/start", () =>
					HttpResponse.json({
						session_id: mockSessionId,
						options: mockOptions,
					}),
				),
			);
			vi.spyOn(simplewebauthn, "startRegistration").mockRejectedValue(
				new MockDOMError("Credential already exists", "InvalidStateError"),
			);
			await expect(webauthn.registerPasskey()).rejects.toThrow(
				"Credential already exists",
			);
		});
	});

	describe("loginWithPasskey", () => {
		const mockSessionId = "session-456";
		const mockOptions = {
			challenge: "challenge-xyz",
			rpId: "localhost",
			allowCredentials: [],
			userVerification: "preferred",
		} as PublicKeyCredentialRequestOptionsJSON;

		it("successfully logs in with passkey and returns token", async () => {
			server.use(
				http.post("/api/webauthn/login/start", () =>
					HttpResponse.json({
						session_id: mockSessionId,
						options: mockOptions,
					}),
				),
				http.post("/api/webauthn/login/finish", () =>
					HttpResponse.json({ token: "mock-token-123" }),
				),
			);
			vi.spyOn(simplewebauthn, "startAuthentication").mockResolvedValue({
				id: "cred-xyz",
				rawId: "cred-xyz",
				type: "public-key",
				response: {
					clientDataJSON: "client-data",
					authenticatorData: "auth-data",
					signature: "signature",
					userHandle: "user-123",
				},
				getClientExtensionResults: () => ({}),
				toJSON: () => ({}),
			});
			const result = await webauthn.loginWithPasskey();
			expect(result).toBe("mock-token-123");
		});

		it("returns null when user cancels (NotAllowedError)", async () => {
			server.use(
				http.post("/api/webauthn/login/start", () =>
					HttpResponse.json({
						session_id: mockSessionId,
						options: mockOptions,
					}),
				),
			);
			vi.spyOn(simplewebauthn, "startAuthentication").mockRejectedValue(
				new MockDOMError("User cancelled", "NotAllowedError"),
			);
			const result = await webauthn.loginWithPasskey();
			expect(result).toBe(null);
		});

		it("throws on other errors", async () => {
			server.use(
				http.post("/api/webauthn/login/start", () =>
					HttpResponse.json({
						session_id: mockSessionId,
						options: mockOptions,
					}),
				),
			);
			vi.spyOn(simplewebauthn, "startAuthentication").mockRejectedValue(
				new Error("Authentication failed"),
			);
			await expect(webauthn.loginWithPasskey()).rejects.toThrow(
				"Authentication failed",
			);
		});

		it("throws InvalidStateError", async () => {
			server.use(
				http.post("/api/webauthn/login/start", () =>
					HttpResponse.json({
						session_id: mockSessionId,
						options: mockOptions,
					}),
				),
			);
			vi.spyOn(simplewebauthn, "startAuthentication").mockRejectedValue(
				new MockDOMError("Invalid state", "InvalidStateError"),
			);
			await expect(webauthn.loginWithPasskey()).rejects.toThrow(
				"Invalid state",
			);
		});
	});
});
