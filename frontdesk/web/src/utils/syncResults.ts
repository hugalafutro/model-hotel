import type { TFunction } from "i18next";
import type { SyncResultItem } from "../api/types";

// reportResults toasts one line per member outcome from a fleet config sync, so
// a successful row reads "<name> ✓" and a failed row carries the member's own
// error, never a generic message.
export function reportResults(
	results: SyncResultItem[],
	toast: (m: string, k: "success" | "error") => void,
	t: TFunction,
) {
	for (const r of results) {
		toast(
			r.ok
				? t("settings.memberResultOk", { name: r.name })
				: t("settings.memberResultFailed", {
						name: r.name,
						error: r.error ?? t("settings.memberResultFailedGeneric"),
					}),
			r.ok ? "success" : "error",
		);
	}
}
