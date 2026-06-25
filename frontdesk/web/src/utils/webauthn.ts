import { startRegistration } from "@simplewebauthn/browser";
import { api } from "../api/client";

// registerPasskey runs the browser registration ceremony for a new passkey and
// stores the resulting credential. Returns true on success, false if the user
// cancelled the ceremony (NotAllowedError). Other errors propagate so the caller
// can show a specific message (e.g. InvalidStateError for an already-registered
// authenticator).
export async function registerPasskey(): Promise<boolean> {
	try {
		const { session_id, options } = await api.webauthnRegisterStart();
		const credential = await startRegistration({ optionsJSON: options });
		await api.webauthnRegisterFinish(session_id, credential);
		return true;
	} catch (err) {
		if (err instanceof Error && err.name === "NotAllowedError") return false;
		throw err;
	}
}
