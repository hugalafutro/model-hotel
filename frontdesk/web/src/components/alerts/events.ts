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
