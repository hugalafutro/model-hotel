import type { MergedClaim, MergedProvider } from "../../hooks/useDiscrepancies";

/**
 * Which bucket a claim is rendered under.
 *
 * Passed down explicitly instead of being read back off `claim.state`, because a
 * `MergedClaim` carries BOTH `state` (gone/stale/suspect, from the server) and
 * `status` (pending/resolved/new, session-local). `state` decides which group a
 * claim belongs to and therefore which controls it gets; `status` decides only
 * how the row is styled. Threading the group through as an argument makes it
 * impossible for a row to be rendered under one heading and act like another.
 */
export type Group = "gone" | "stale" | "suspect" | "retired" | "pinned";

export const ALL_GROUPS: Group[] = [
	"gone",
	"stale",
	"suspect",
	"retired",
	"pinned",
];

/** Rows that still need the operator: `pending` or `new`, never cleared. */
export function actionableIn(p: MergedProvider, group: Group): MergedClaim[] {
	return (p[group] ?? []).filter(
		(c) => c.status === "pending" || c.status === "new",
	);
}
