import type { ReactNode } from "react";
import { type QuotaBarMode, windowPct } from "../display";

// Types and label helpers for the shared quota modal bodies. Plain .ts so the
// .tsx files beside it export only components (react-refresh's rule).

/**
 * The app's `t`, already scoped to its quota-modal key prefix
 * (`components.providerModals` on the dashboard, `quota.modal` on Front Desk),
 * so a body asks for "used", never a full key path.
 */
export type Translate = (key: string, opts?: Record<string, unknown>) => string;

/** The app's own date formatting for an ISO instant. */
export interface TimeFormat {
	/** Date and time of day. */
	absolute(iso?: string): string;
	/** Distance from now ("in 3 hours"). */
	relative(iso?: string): string;
}

export interface QuotaShellProps {
	title: string;
	subtitle?: ReactNode;
	children: ReactNode;
}

/**
 * Everything a body needs from its app: the modal chrome, the scoped `t` and
 * the date formatting.
 *
 * `shell` is a render function rather than a component: it closes over the
 * app's per-instance controls (refresh, toggle, close), and a component built
 * per render would be a new type each time, remounting the whole modal.
 */
export interface QuotaModalKit {
	shell: (props: QuotaShellProps) => ReactNode;
	t: Translate;
	time: TimeFormat;
}

/** What every shared quota modal body takes. */
export interface QuotaBodyProps<P> {
	payload: P;
	barMode: QuotaBarMode;
	providerName: string;
	/** ISO instant the payload was fetched upstream; durations anchor here. */
	fetchedAt: string;
	kit: QuotaModalKit;
}

/**
 * "Resets <absolute>\n<relative>" for an ISO reset time, or "-" when the
 * provider did not report one. Rendered inside a QuotaBar sublabel, which is
 * `white-space: pre-line`, so the newline becomes a second line.
 */
export function resetSublabel(
	iso: string | null | undefined,
	t: Translate,
	time: TimeFormat,
): string {
	if (!iso) return "-";
	const ms = new Date(iso).getTime();
	if (!Number.isFinite(ms)) return "-";
	return `${t("resets", { when: time.absolute(iso) })}\n${time.relative(iso)}`;
}

/** Same, from an epoch-milliseconds instant (Z.ai, NanoGPT) or a computed one (MiniMax). */
export function resetSublabelFromEpoch(
	ms: number | null | undefined,
	t: Translate,
	time: TimeFormat,
): string {
	// `ms == null` looks redundant at runtime (Number.isFinite does not coerce)
	// but is what narrows `ms` to number for `new Date(ms)` below.
	if (ms == null || !Number.isFinite(ms) || ms <= 0) return "-";
	return resetSublabel(new Date(ms).toISOString(), t, time);
}

/**
 * The right-hand figure on a quota bar header: "40% used" or "60% left",
 * whichever the current bar mode calls for. `used` is a percent used, matching
 * QuotaBar's `percentage`.
 */
export function quotaRightText(
	used: number,
	barMode: QuotaBarMode,
	t: Translate,
): string {
	return `${windowPct(used, barMode)} ${t(barMode === "used" ? "used" : "left")}`;
}
