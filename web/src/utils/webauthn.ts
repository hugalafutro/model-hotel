import type {
	PublicKeyCredentialCreationOptionsJSON,
	PublicKeyCredentialRequestOptionsJSON,
} from "@simplewebauthn/browser";
import {
	browserSupportsWebAuthn,
	startAuthentication,
	startRegistration,
} from "@simplewebauthn/browser";
import { api } from "../api/client";

export async function registerPasskey(): Promise<boolean> {
	const { session_id, options } = await api.webauthn.registerStart();
	const credential = await startRegistration({
		optionsJSON: options as unknown as PublicKeyCredentialCreationOptionsJSON,
	});
	await api.webauthn.registerFinish(session_id, credential);
	return true;
}

export async function loginWithPasskey(): Promise<string | null> {
	try {
		const { session_id, options } = await api.webauthn.loginStart();
		const credential = await startAuthentication({
			optionsJSON: options as unknown as PublicKeyCredentialRequestOptionsJSON,
		});
		const { token } = await api.webauthn.loginFinish(session_id, credential);
		return token;
	} catch (err) {
		if (err instanceof Error && err.name === "NotAllowedError") {
			return null;
		}
		console.error("Passkey login failed:", err);
		return null;
	}
}

export function isWebAuthnAvailable(): boolean {
	return browserSupportsWebAuthn();
}
