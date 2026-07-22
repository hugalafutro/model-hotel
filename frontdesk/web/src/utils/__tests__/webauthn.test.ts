import { beforeEach, describe, expect, it, vi } from "vitest";

// registerPasskey orchestrates the browser ceremony (startRegistration) plus the
// two-step server hand-off (register/start + register/finish). Both dependencies
// are mocked so the test drives the success path and the two error branches
// (user-cancel -> false, everything else -> rethrow) deterministically.

const startRegistration = vi.fn();
vi.mock("@simplewebauthn/browser", () => ({
	startRegistration: (...args: unknown[]) => startRegistration(...args),
}));

const webauthnRegisterStart = vi.fn();
const webauthnRegisterFinish = vi.fn();
vi.mock("../../api/client", () => ({
	api: {
		webauthnRegisterStart: () => webauthnRegisterStart(),
		webauthnRegisterFinish: (sessionId: string, credential: unknown) =>
			webauthnRegisterFinish(sessionId, credential),
	},
}));

import { registerPasskey } from "../webauthn";

const options = { challenge: "abc" };

beforeEach(() => {
	vi.clearAllMocks();
	webauthnRegisterStart.mockResolvedValue({ session_id: "sess-1", options });
	webauthnRegisterFinish.mockResolvedValue({ success: true });
});

describe("registerPasskey", () => {
	it("completes the ceremony and finishes with the returned credential", async () => {
		const credential = { id: "cred-1" };
		startRegistration.mockResolvedValue(credential);

		const ok = await registerPasskey();

		expect(ok).toBe(true);
		expect(startRegistration).toHaveBeenCalledWith({ optionsJSON: options });
		expect(webauthnRegisterFinish).toHaveBeenCalledWith("sess-1", credential);
	});

	it("returns false when the user cancels (NotAllowedError)", async () => {
		const cancel = new Error("cancelled");
		cancel.name = "NotAllowedError";
		startRegistration.mockRejectedValue(cancel);

		await expect(registerPasskey()).resolves.toBe(false);
		expect(webauthnRegisterFinish).not.toHaveBeenCalled();
	});

	it("rethrows other errors so the caller can show a specific message", async () => {
		const dup = new Error("already registered");
		dup.name = "InvalidStateError";
		startRegistration.mockRejectedValue(dup);

		await expect(registerPasskey()).rejects.toBe(dup);
		expect(webauthnRegisterFinish).not.toHaveBeenCalled();
	});
});
