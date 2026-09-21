import { startIdleLogout } from "@web-shared/idle-logout";
import { useEffect, useRef, useState } from "react";
import { api } from "../api/client";

// Fallback used until the settings fetch resolves (and if it fails). Matches the
// server-side default in internal/frontdesk (session_idle_timeout_minutes).
const DEFAULT_MINUTES = 60;

// SETTINGS_SAVED_EVENT is dispatched on window by the settings form after a
// successful save, so hooks that cache a setting can re-read it.
export const SETTINGS_SAVED_EVENT = "fd:settings-saved";

/**
 * useIdleLogout signs the operator out after a configurable period of
 * inactivity. The window (minutes; 0 disables, default 60) is read from the
 * settings endpoint. `onLogout` performs the actual sign-out so this hook reuses
 * the app's existing logout path (server revoke + drop to the login screen).
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
		// Only the newest read applies: a save while the first read is still in
		// flight starts a second one, and the first answering last must not put
		// the pre-save window back.
		let seq = 0;
		const load = () => {
			const mine = ++seq;
			api
				.getSettings()
				.then((s) => {
					if (!cancelled && mine === seq)
						setMinutes(s.session_idle_timeout_minutes);
				})
				.catch(() => {
					// Keep the default window if settings can't be read.
				});
		};
		load();
		// The settings form announces a save (SETTINGS_SAVED_EVENT), so a changed
		// window takes effect now rather than at the next login.
		window.addEventListener(SETTINGS_SAVED_EVENT, load);
		return () => {
			cancelled = true;
			window.removeEventListener(SETTINGS_SAVED_EVENT, load);
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
