import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { useToast } from "../context/ToastContext";

/**
 * Mirrors a value into localStorage as JSON whenever it changes, while
 * `enabled`. A full store is warned about once per mount: the write fails on
 * every subsequent keystroke too, and a toast per keystroke is not a warning,
 * it is a wall.
 */
export function usePersistedJSON(
	key: string,
	value: unknown,
	enabled: boolean,
	warnKey: string,
): void {
	const { toast } = useToast();
	const { t } = useTranslation();
	const quotaWarnedRef = useRef(false);

	useEffect(() => {
		if (!enabled) return;
		try {
			localStorage.setItem(key, JSON.stringify(value));
		} catch {
			if (!quotaWarnedRef.current) {
				quotaWarnedRef.current = true;
				toast(t(warnKey), "warning");
			}
		}
	}, [key, value, enabled, warnKey, t, toast]);
}
