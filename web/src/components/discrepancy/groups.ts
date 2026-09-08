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

/**
 * Sign and badge variant per bucket, shared by the bucket headings and the
 * provider pill's chips so one bucket reads the same at both levels.
 *
 * "+" for pinned as in "you put these back", the same sign the journal uses for
 * models that appeared. Deliberately not one of the alarm signs: a pin is a
 * decision the operator made, not something that went wrong.
 */
export const BUCKET_SIGN: Record<Group, string> = {
	gone: "×",
	suspect: "?",
	retired: "!",
	stale: "·",
	pinned: "+",
};

export const BUCKET_VARIANT: Record<Group, string> = {
	gone: "ui-badge-error",
	suspect: "ui-badge-warning",
	retired: "ui-badge-error",
	stale: "ui-badge-neutral",
	pinned: "ui-badge-info",
};

/** Rows that still need the operator: `pending` or `new`, never cleared. */
export function actionableIn(p: MergedProvider, group: Group): MergedClaim[] {
	return (p[group] ?? []).filter(
		(c) => c.status === "pending" || c.status === "new",
	);
}
