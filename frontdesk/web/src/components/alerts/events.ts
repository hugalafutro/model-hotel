// The severity palette the Alerts card's picker and the wizard's events step
// both read, so the two always agree on one colour per catalog severity. The
// labelling rule and the CSV reading are shared with the main dashboard in
// @web-shared/alerts/events; only the colours are Front Desk's own, because the
// two design systems name different tokens for the same four severities.

/** Dot colour per catalog severity, in the Front Desk palette. */
export const SEVERITY_COLOR: Record<string, string> = {
	success: "var(--ok)",
	info: "var(--info)",
	warning: "var(--warn)",
	error: "var(--danger)",
};

/**
 * How one apprise-api probe reads as a pill: the badge variant and the
 * translated label. The card's pill and the wizard's closing pill classify the
 * same probe, so they classify it once. The card covers a fourth state (nothing
 * configured at all) that cannot happen after the wizard has just configured
 * it, so it stays at the card.
 *
 * The label is resolved here rather than handed back as a key so all three keys
 * stay literal `t()` arguments, which is what the i18n gate scans: a key that
 * only ever appears inside a template literal could be renamed out of the
 * catalogs and ship as raw text.
 */
export function statusBadge(
	status: { reachable: boolean; healthy: boolean },
	t: (key: string) => string,
): {
	variant: "ui-badge-danger" | "ui-badge-warn" | "ui-badge-ok";
	label: string;
} {
	if (!status.reachable)
		return {
			variant: "ui-badge-danger",
			label: t("settings.alerts.statusUnreachable"),
		};
	if (!status.healthy)
		return {
			variant: "ui-badge-warn",
			label: t("settings.alerts.statusUnhealthy"),
		};
	return { variant: "ui-badge-ok", label: t("settings.alerts.statusOk") };
}
