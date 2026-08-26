import { useTranslation } from "react-i18next";
import type { MergedProvider } from "../../hooks/useDiscrepancies";
import { ChevronDown, ChevronRight } from "../../lib/icons";
import { ClaimRow, type ClaimRowActions } from "./ClaimRow";
import type { Group } from "./groups";

const BUCKET_SIGN: Record<Group, string> = {
	gone: "×",
	suspect: "?",
	retired: "!",
	stale: "·",
	// "+" as in "you put these back", the same sign the journal uses for models
	// that appeared. Deliberately not one of the alarm signs: a pin is a
	// decision the operator made, not something that went wrong.
	pinned: "+",
};
const BUCKET_VARIANT: Record<Group, string> = {
	gone: "ui-badge-error",
	suspect: "ui-badge-warning",
	retired: "ui-badge-error",
	stale: "ui-badge-neutral",
	pinned: "ui-badge-info",
};

/**
 * Level 2: one collapsible line per bucket, all closed when a provider opens.
 *
 * Gone and suspect previously rendered fully expanded via CategoryGroup while
 * only stale had a toggle, which is how a provider with 42 gone models produced
 * 42 rows on sight. All three now use the stale pattern, so the operator opens
 * exactly the list they asked for.
 *
 * The rows are MOUNTED ONLY WHILE OPEN, and there is no height animation. Both
 * are deliberate, and both are why unrolling is cheap:
 *
 *   - The animated `grid-template-rows: 0fr -> 1fr` this used to share with the
 *     journal zone forces the browser to lay out the entire subtree on every
 *     frame of the transition. With 52 rows that is the stutter, and it is paid
 *     on a machine fast enough to spin its fans doing it.
 *   - Keeping collapsed rows mounted only existed to give that transition
 *     something to animate. Nothing else needs them: the single-open rule means
 *     at most one bucket in the whole modal is ever open, so the modal now holds
 *     one bucket's rows instead of every bucket's rows of every provider (179 on
 *     the dev fleet).
 *
 * Unmounting also strictly beats the old `inert` trick for screen readers: rows
 * that do not exist cannot be announced under an aria-expanded="false" toggle.
 * The region wrapper stays rendered either way so `aria-controls` always
 * resolves to a real element.
 */
export function BucketSection({
	provider,
	group,
	open,
	regionId,
	onToggle,
	actions,
}: {
	provider: MergedProvider;
	group: Group;
	open: boolean;
	regionId: string;
	onToggle: () => void;
	actions: ClaimRowActions;
}) {
	const { t } = useTranslation();
	// `?? []`: a server predating the operator pin omits the bucket entirely,
	// which a rolling deploy puts behind this dashboard.
	const claims = provider[group] ?? [];
	if (claims.length === 0) return null;
	return (
		<section data-testid={`discrepancy-group-${group}`} className="space-y-1">
			<button
				type="button"
				onClick={onToggle}
				aria-expanded={open}
				aria-controls={regionId}
				className="flex w-full items-center gap-2 text-left"
				data-testid={`discrepancy-group-${group}-toggle`}
			>
				{open ? (
					<ChevronDown size={14} className="shrink-0" />
				) : (
					<ChevronRight size={14} className="shrink-0" />
				)}
				<span
					className={`ui-badge ${BUCKET_VARIANT[group]} shrink-0 tabular-nums`}
				>
					{BUCKET_SIGN[group]} {claims.length}
				</span>
				<span className="text-[11px] font-semibold uppercase tracking-wider text-(--text-tertiary)">
					{/* Literal key for the pinned bucket rather than another entry under
					    `group`: the i18n source-key check only follows literal
					    arguments, and the label is the operator's own decision read
					    back to them, not a discovery verdict like its neighbours. */}
					{group === "pinned"
						? t("providers.discrepancies.pinnedGroup")
						: t(`providers.discrepancies.group.${group}`)}
				</span>
				<span className="h-px flex-1 bg-white/30" />
			</button>
			<div id={regionId}>
				{open ? (
					// The divider belongs to the container, not to each row: one
					// hairline between neighbours reads tighter than 52 outlined boxes
					// and costs a border instead of a filled, rounded surface each.
					<div className="divide-y divide-(--border-subtle)">
						{claims.map((c) => (
							<ClaimRow
								key={c.model_id}
								provider={provider}
								claim={c}
								group={group}
								actions={actions}
							/>
						))}
					</div>
				) : null}
			</div>
		</section>
	);
}
