import type { BadgeVariant } from "./Badge";

/** Status badge color shared by the audit table and the detail modal. */
export function auditStatusVariant(code: number): BadgeVariant {
	if (code >= 500) return "error";
	if (code >= 400) return "warning";
	return "success";
}

/** Method badge color shared by the audit table and the detail modal. */
export function auditMethodVariant(method: string): BadgeVariant {
	return method === "DELETE" ? "error" : "info";
}
