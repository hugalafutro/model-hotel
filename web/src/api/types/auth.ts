export interface VirtualKey {
	id: string;
	name: string;
	key?: string;
	key_preview: string;
	tokens_used: number;
	last_used_at: string | null;
	created_at: string;
	rate_limit_rps?: number | null;
	rate_limit_burst?: number | null;
	rate_limit_tpm?: number | null;
	allowed_providers?: string[] | null;
	strip_reasoning: boolean;
	owner_user_id?: string | null;
	owner_username?: string | null;
}
export interface WebAuthnCredential {
	id: string;
	name: string;
	transports: string[];
	created_at: string;
	aaguid: string;
	sign_count: number;
}
export interface TotpStatus {
	enabled: boolean;
	/** RFC3339 confirmation time; absent when TOTP is disabled. */
	enabled_at?: string;
}
/**
 * Public OIDC SSO status, read unauthenticated on the login screen and in
 * settings. Reports only whether SSO is enabled and fully configured plus a
 * display name (the IdP host) for the button label; never any secret.
 */
export interface OidcStatus {
	enabled: boolean;
	/** IdP host, shown on the sign-in button; absent when not configured. */
	display_name?: string;
}
/**
 * Public GitHub SSO status, read unauthenticated on the login screen and in
 * settings. Reports only whether GitHub SSO is enabled and fully configured;
 * the button label is fixed ("GitHub"), so there is no display name.
 */
export interface GithubStatus {
	enabled: boolean;
}
/** Admin-gated detail for the settings panel (not the polled public status). */
export interface TotpInfo {
	recovery_remaining: number;
	recovery_total: number;
	/** RFC3339 time a TOTP code was last accepted; absent if never used. */
	last_used_at?: string;
}
export interface TotpEnrollStart {
	uri: string;
	secret: string;
}
export interface TotpEnrollVerify {
	recovery_codes: string[];
	// Session token minted on enable so the admin stays logged in (the raw
	// admin token is no longer a valid bearer once 2FA is on). Absent only if
	// the server could not mint one, in which case the user must re-login.
	token?: string;
}
export interface TotpLoginResponse {
	token: string;
}
// UserTotpStatus is the caller's own second-factor state, served by
// GET /api/auth/totp/status (users-row identities only).
export interface UserTotpStatus {
	enabled: boolean;
	/** RFC3339 confirmation time; absent when disabled. */
	enabled_at?: string;
	recovery_remaining?: number;
	recovery_total?: number;
}
// AuthStatus reports whether any enabled user accounts exist, read
// unauthenticated on the login screen to decide whether to render the
// username/password form. Served by GET /api/auth/status.
export interface AuthStatus {
	enabled: boolean;
}
// Me is the caller's resolved identity, served by GET /api/auth/me. The
// sidebar and routes gate on role/grants; the server enforces regardless.
export interface Me {
	username: string;
	display_name?: string;
	role: "admin" | "user";
	grants: string[];
	/** True for users-row identities (not the env-token admin); gates the Security page. */
	user_account?: boolean;
	/**
	 * Account provider cap. null/undefined means no cap (every provider); a
	 * non-null array restricts every key this user owns to exactly its members,
	 * so an EMPTY array denies every provider. Empty is reachable on READ even
	 * though the users API refuses to write one: deleting the last provider a
	 * capped account named rewrites the stored list to `[]`
	 * (provider.PruneAllowLists), and a fleet config-sync imports one verbatim.
	 * Test this field's presence, never its length. Advisory only — the server
	 * is the enforcement point (checked at virtual-key write time and again on
	 * every proxied request), not this field.
	 */
	allowed_providers?: string[] | null;
}
// AuthSession is one row of the Settings active-sessions list, served by
// GET /api/auth/sessions: the caller's own live sessions only, with the one
// this browser rides on flagged current. Sessions minted before the device
// metadata existed carry empty user_agent/ip and no last_seen_at.
export interface AuthSession {
	id: string;
	user_agent: string;
	ip: string;
	created_at: string;
	last_seen_at?: string;
	current: boolean;
}
// DashboardUser is a managed user account (admin-only Users page). The
// password hash never leaves the backend.
export interface DashboardUser {
	id: string;
	username: string;
	display_name: string;
	email: string | null;
	role: "admin" | "user";
	grants: string[];
	enabled: boolean;
	created_at: string;
	updated_at: string;
	last_login_at: string | null;
	// Aggregate proxy limits across the user's owned virtual keys (null = no cap).
	rate_limit_rps?: number | null;
	rate_limit_burst?: number | null;
	rate_limit_tpm?: number | null;
	/** Whether the account has a confirmed TOTP second factor. */
	totp_enabled?: boolean;
	/**
	 * Account provider cap. null/undefined means no cap (every provider); a
	 * non-null array restricts every key this user owns to exactly its members,
	 * so an EMPTY array denies every provider. Empty is reachable on READ even
	 * though the users API refuses to write one: deleting the last provider a
	 * capped account named rewrites the stored list to `[]`
	 * (provider.PruneAllowLists), and a fleet config-sync imports one verbatim.
	 * Test this field's presence, never its length. Advisory only — the server
	 * is the enforcement point (checked at virtual-key write time and again on
	 * every proxied request), not this field.
	 */
	allowed_providers?: string[] | null;
}
// UserUpsertRequest is the create/update body for POST/PUT /api/users.
// password is create-only; enabled is update-only.
export interface UserUpsertRequest {
	username: string;
	display_name: string;
	email: string | null;
	password?: string;
	role: "admin" | "user";
	grants: string[];
	enabled?: boolean;
	rate_limit_rps?: number | null;
	rate_limit_burst?: number | null;
	rate_limit_tpm?: number | null;
	/**
	 * Account provider cap. Omit to leave the stored cap unchanged (update
	 * only); send null to clear it (every provider); send a non-empty array
	 * to restrict every key this user owns to exactly those provider IDs. An
	 * empty array is rejected by the API. Advisory only, the server enforces.
	 */
	allowed_providers?: string[] | null;
}
