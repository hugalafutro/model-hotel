import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { api } from "../api/client";
import { SETTING_DEFAULTS } from "../pages/Settings/defaults";
import { startIdleLogout } from "../utils/idleLogout";

/**
 * useIdleLogout signs the admin out after a configurable period of inactivity.
 *
 * The window comes from the `session_idle_timeout_minutes` setting (minutes;
 * 0 disables auto-logout, the default is 60). The actual logout is delegated to
 * `onLogout` so this hook reuses the caller's existing sign-out path (token
 * clear + best-effort server-side revoke + reload). Mounted from Layout, which
 * only renders while authenticated, so the timer runs exactly when a session
 * exists.
 */
export function useIdleLogout(onLogout: () => void) {
	const { data: settings } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	const minutes = Number(
		settings?.session_idle_timeout_minutes ??
			SETTING_DEFAULTS.session_idle_timeout_minutes,
	);
	const timeoutMs =
		Number.isFinite(minutes) && minutes > 0 ? minutes * 60_000 : 0;

	// Hold the latest callback in a ref so the timer re-arms only when the
	// timeout value changes, not on every render that hands us a new closure.
	const onLogoutRef = useRef(onLogout);
	useEffect(() => {
		onLogoutRef.current = onLogout;
	}, [onLogout]);

	useEffect(() => {
		return startIdleLogout({
			timeoutMs,
			onTimeout: () => onLogoutRef.current(),
		});
	}, [timeoutMs]);
}
