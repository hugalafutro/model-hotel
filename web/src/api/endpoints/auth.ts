import { API_BASE, fetchJSON, fetchOK, getAuthHeaders } from "../http";
import type {
	AuthSession,
	AuthStatus,
	DemoLogin,
	GithubStatus,
	Me,
	OidcStatus,
	PublicConfig,
	TotpEnrollStart,
	TotpEnrollVerify,
	TotpInfo,
	TotpLoginResponse,
	TotpStatus,
	UserTotpStatus,
} from "../types";

// Unauthenticated: read before login and inside the dashboard. No auth
// headers so it works on the login screen too.
export const publicConfig = {
	get: async (): Promise<PublicConfig> => {
		return fetchJSON<PublicConfig>(
			`${API_BASE}/api/public-config`,
			undefined,
			"Failed to fetch public config",
		);
	},
};

// Unauthenticated: read on the login screen. Returns an empty token unless
// the server runs as a demo with the token-display feature enabled.
export const demoLogin = {
	get: async (): Promise<DemoLogin> => {
		return fetchJSON<DemoLogin>(
			`${API_BASE}/api/demo-login`,
			undefined,
			"Failed to fetch demo login",
		);
	},
};

export const webauthn = {
	available: async (): Promise<{
		enabled: boolean;
		has_credentials: boolean;
	}> => {
		return fetchJSON(`${API_BASE}/api/webauthn/available`);
	},
	registerStart: async (): Promise<{
		session_id: string;
		options: Record<string, unknown>;
	}> => {
		return fetchJSON(`${API_BASE}/api/webauthn/register/start`, {
			method: "POST",
			headers: getAuthHeaders(),
		});
	},
	registerFinish: async (
		sessionId: string,
		credential: unknown,
	): Promise<void> => {
		await fetchOK(
			`${API_BASE}/api/webauthn/register/finish`,
			{
				method: "POST",
				headers: getAuthHeaders(),
				body: JSON.stringify({ session_id: sessionId, credential }),
			},
			"Passkey registration failed",
		);
	},
	loginStart: async (): Promise<{
		session_id: string;
		options: Record<string, unknown>;
	}> => {
		return fetchJSON(`${API_BASE}/api/webauthn/login/start`, {
			method: "POST",
			headers: { "Content-Type": "application/json" },
		});
	},
	loginFinish: async (
		sessionId: string,
		credential: unknown,
	): Promise<{ token: string }> => {
		return fetchJSON(
			`${API_BASE}/api/webauthn/login/finish`,
			{
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ session_id: sessionId, credential }),
			},
			"Passkey login failed",
		);
	},
	listCredentials: async (): Promise<
		import("../types").WebAuthnCredential[]
	> => {
		return fetchJSON<import("../types").WebAuthnCredential[]>(
			`${API_BASE}/api/webauthn/credentials`,
			{ headers: getAuthHeaders() },
		);
	},
	deleteCredential: async (id: string): Promise<void> => {
		await fetchOK(
			`${API_BASE}/api/webauthn/credentials/${encodeURIComponent(id)}`,
			{
				method: "DELETE",
				headers: getAuthHeaders(),
			},
			"Failed to delete passkey",
		);
	},
	renameCredential: async (id: string, name: string): Promise<void> => {
		await fetchOK(
			`${API_BASE}/api/webauthn/credentials/${encodeURIComponent(id)}`,
			{
				method: "PATCH",
				headers: { ...getAuthHeaders(), "Content-Type": "application/json" },
				body: JSON.stringify({ name }),
			},
			"Failed to rename passkey",
		);
	},
};

export const totp = {
	status: async (): Promise<TotpStatus> =>
		fetchJSON<TotpStatus>(`${API_BASE}/api/totp/status`),
	info: async (): Promise<TotpInfo> =>
		fetchJSON<TotpInfo>(`${API_BASE}/api/totp/info`, {
			headers: getAuthHeaders(),
		}),
	enrollStart: async (): Promise<TotpEnrollStart> =>
		fetchJSON<TotpEnrollStart>(
			`${API_BASE}/api/totp/enroll/start`,
			{ method: "POST", headers: getAuthHeaders() },
			"TOTP enrollment failed",
		),
	enrollVerify: async (code: string): Promise<TotpEnrollVerify> =>
		fetchJSON<TotpEnrollVerify>(
			`${API_BASE}/api/totp/enroll/verify`,
			{
				method: "POST",
				headers: getAuthHeaders(),
				body: JSON.stringify({ code }),
			},
			"TOTP verification failed",
		),
	disable: async (code: string): Promise<void> => {
		await fetchOK(
			`${API_BASE}/api/totp/disable`,
			{
				method: "POST",
				headers: getAuthHeaders(),
				body: JSON.stringify({ code }),
			},
			"TOTP disable failed",
		);
	},
	login: async (token: string, code: string): Promise<TotpLoginResponse> =>
		fetchJSON<TotpLoginResponse>(
			`${API_BASE}/api/totp/login`,
			{
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ token, code }),
			},
			"TOTP login failed",
		),
};

// Unauthenticated: read on the login screen and in settings. The actual
// login is a full-page redirect to /api/auth/oidc/start, not an XHR.
export const oidc = {
	status: async (): Promise<OidcStatus> =>
		fetchJSON<OidcStatus>(`${API_BASE}/api/auth/oidc/status`),
};

// Unauthenticated: read on the login screen and in settings. The actual
// login is a full-page redirect to /api/auth/github/start, not an XHR.
export const github = {
	status: async (): Promise<GithubStatus> =>
		fetchJSON<GithubStatus>(`${API_BASE}/api/auth/github/status`),
};

// Multi-user auth: the status probe and login exchange are unauthenticated
// (login-screen surface); me reports the caller's resolved identity.
export const auth = {
	status: async (): Promise<AuthStatus> =>
		fetchJSON<AuthStatus>(`${API_BASE}/api/auth/status`),
	login: async (
		username: string,
		password: string,
		code?: string,
	): Promise<{ token: string }> =>
		fetchJSON<{ token: string }>(
			`${API_BASE}/api/auth/login`,
			{
				method: "POST",
				headers: { "Content-Type": "application/json" },
				// code rides along only when the account has TOTP enabled (the
				// 401 {"totp_required": true} answer tells the UI to ask for it).
				body: JSON.stringify(
					code ? { username, password, code } : { username, password },
				),
			},
			"Login failed",
		),
	// Admin-token bootstrap: exchanges the env admin token for a session
	// cookie pair. Returns 400 when the admin account has TOTP enabled (the
	// caller must use the TOTP login flow instead), 401 on a bad token.
	adminExchange: async (adminToken: string): Promise<{ success: boolean }> =>
		fetchJSON<{ success: boolean }>(
			`${API_BASE}/api/auth/admin-exchange`,
			{
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ admin_token: adminToken }),
			},
			"Login failed",
		),
	// Always-mounted logout: revokes whatever session the caller presents
	// (passkey OR TOTP session token) and clears both auth cookies
	// server-side. Works with or without passkeys configured, unlike the
	// passkey-gated /webauthn/logout.
	logout: async (): Promise<void> => {
		await fetchOK(
			`${API_BASE}/api/auth/logout`,
			{
				method: "POST",
				headers: getAuthHeaders(),
			},
			"Failed to logout",
		);
	},
	me: async (): Promise<Me> =>
		fetchJSON<Me>(`${API_BASE}/api/auth/me`, {
			headers: getAuthHeaders(),
		}),
	// Signs the caller's other sessions out, keeping this one, and reports
	// how many were ended so the UI can say so.
	revokeOtherSessions: async (): Promise<{ revoked: number }> =>
		fetchJSON<{ revoked: number }>(
			`${API_BASE}/api/auth/sessions/revoke-others`,
			{ method: "POST", headers: getAuthHeaders() },
			"Could not sign out other sessions",
		),
	// The caller's own live sessions, for the Settings active-sessions list.
	listSessions: async (): Promise<{ sessions: AuthSession[] }> =>
		fetchJSON<{ sessions: AuthSession[] }>(
			`${API_BASE}/api/auth/sessions`,
			{ headers: getAuthHeaders() },
			"Could not load sessions",
		),
	// Signs one of the caller's other sessions out by row id.
	revokeSession: async (id: string): Promise<void> => {
		await fetchOK(
			`${API_BASE}/api/auth/sessions/${id}`,
			{ method: "DELETE", headers: getAuthHeaders() },
			"Could not sign out the session",
		);
	},
};

// Self-service per-user TOTP (users-row identities manage their own 2FA;
// the env-token admin uses api.totp instead).
export const userTotp = {
	status: async (): Promise<UserTotpStatus> =>
		fetchJSON<UserTotpStatus>(`${API_BASE}/api/auth/totp/status`, {
			headers: getAuthHeaders(),
		}),
	enrollStart: async (): Promise<TotpEnrollStart> =>
		fetchJSON<TotpEnrollStart>(
			`${API_BASE}/api/auth/totp/enroll/start`,
			{ method: "POST", headers: getAuthHeaders() },
			"TOTP enrollment failed",
		),
	enrollVerify: async (code: string): Promise<{ recovery_codes: string[] }> =>
		fetchJSON<{ recovery_codes: string[] }>(
			`${API_BASE}/api/auth/totp/enroll/verify`,
			{
				method: "POST",
				headers: getAuthHeaders(),
				body: JSON.stringify({ code }),
			},
			"TOTP verification failed",
		),
	disable: async (code: string): Promise<void> => {
		await fetchOK(
			`${API_BASE}/api/auth/totp/disable`,
			{
				method: "POST",
				headers: getAuthHeaders(),
				body: JSON.stringify({ code }),
			},
			"TOTP disable failed",
		);
	},
	// Rotates the caller's own password; the server revokes every session
	// of the account on success, this one included.
	changePassword: async (
		currentPassword: string,
		newPassword: string,
	): Promise<void> => {
		await fetchOK(
			`${API_BASE}/api/auth/password`,
			{
				method: "POST",
				headers: getAuthHeaders(),
				body: JSON.stringify({
					current_password: currentPassword,
					new_password: newPassword,
				}),
			},
			"Password change failed",
		);
	},
};
