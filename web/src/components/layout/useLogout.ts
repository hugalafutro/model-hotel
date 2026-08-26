import { useQueryClient } from "@tanstack/react-query";
import { api, clearAuth } from "../../api/client";
import { useIdleLogout } from "../../hooks/useIdleLogout";

/**
 * The one logout path, used by the sidebar button and by idle auto-logout.
 *
 * Best-effort server-side session revoke via the always-mounted endpoint. It
 * revokes whatever session the caller presents (passkey OR TOTP session
 * token) and clears both auth cookies, so it must run for every session type
 * and works whether or not passkeys are configured. A raw admin token with
 * no server session is a harmless no-op. This matters for idle auto-logout:
 * a TOTP-only admin's session must die server-side too.
 */
export function useLogout() {
	const queryClient = useQueryClient();
	const handleLogout = async () => {
		try {
			await api.auth.logout();
		} catch {
			// Server-side logout failure is non-fatal.
		}
		// The logout call revoked the session and cleared the httpOnly session
		// cookie server-side. Drop the client-visible auth signal so
		// isAuthenticated() flips false, cancel any in-flight queries so they don't
		// race the reload, then reload into the login screen.
		clearAuth();
		queryClient.cancelQueries();
		window.location.reload();
	};

	// Sign out after the configured period of inactivity (0 = never). Reuses the
	// same logout path as the manual button.
	useIdleLogout(handleLogout);

	return handleLogout;
}
