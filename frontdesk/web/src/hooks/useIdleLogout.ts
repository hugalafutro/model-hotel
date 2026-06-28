import { useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import { startIdleLogout } from "../utils/idleLogout";

// Fallback used until the settings fetch resolves (and if it fails). Matches the
// server-side default in internal/frontdesk (session_idle_timeout_minutes).
const DEFAULT_MINUTES = 60;

/**
 * useIdleLogout signs the operator out after a configurable period of
 * inactivity. The window (minutes; 0 disables, default 60) is read from the
 * settings endpoint. `onLogout` performs the actual sign-out so this hook reuses
 * the app's existing path (clearAuthToken + drop to the login screen).
 *
 * `enabled` gates the whole thing to the authenticated state: the hook is always
 * called (Rules of Hooks) but wires nothing while logged out.
 */
export function useIdleLogout(enabled: boolean, onLogout: () => void) {
	const [minutes, setMinutes] = useState(DEFAULT_MINUTES);

	const onLogoutRef = useRef(onLogout);
	useEffect(() => {
		onLogoutRef.current = onLogout;
	}, [onLogout]);

	useEffect(() => {
		if (!enabled) return;
		let cancelled = false;
		api
			.getSettings()
			.then((s) => {
				if (!cancelled) setMinutes(s.session_idle_timeout_minutes);
			})
			.catch(() => {
				// Keep the default window if settings can't be read.
			});
		return () => {
			cancelled = true;
		};
	}, [enabled]);

	const timeoutMs = minutes > 0 ? minutes * 60_000 : 0;

	useEffect(() => {
		if (!enabled) return;
		return startIdleLogout({
			timeoutMs,
			onTimeout: () => onLogoutRef.current(),
		});
	}, [enabled, timeoutMs]);
}
