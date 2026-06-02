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

let _serverEnabled: boolean | null = null;

export async function isWebAuthnAvailable(): Promise<boolean> {
	if (!browserSupportsWebAuthn()) return false;
	if (_serverEnabled !== null) return _serverEnabled;
	try {
		const res = await api.webauthn.available();
		_serverEnabled = res.enabled;
	} catch {
		_serverEnabled = false;
	}
	return _serverEnabled;
}

export function resetWebAuthnCache(): void {
	_serverEnabled = null;
}

export async function registerPasskey(): Promise<boolean> {
	try {
		const { session_id, options } = await api.webauthn.registerStart();
		const credential = await startRegistration({
			optionsJSON: options as unknown as PublicKeyCredentialCreationOptionsJSON,
		});
		await api.webauthn.registerFinish(session_id, credential);
		return true;
	} catch (err) {
		if (err instanceof Error && err.name === "NotAllowedError") {
			return false;
		}
		throw err;
	}
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
		throw err;
	}
}
