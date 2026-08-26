import type { RefObject } from "react";
import { useTranslation } from "react-i18next";
import {
	type MergedProvider,
	providerHasNoPending,
	retestProvesNothing,
} from "../../hooks/useDiscrepancies";
import { BucketSection } from "./BucketSection";
import type { ClaimRowActions } from "./ClaimRow";
import { ClearedSummary } from "./ClearedSummary";
import { ALL_GROUPS, actionableIn, type Group } from "./groups";
import { ProviderPill } from "./ProviderPill";

/**
 * Level 1: one provider, as its pill row plus (while unrolled) its buckets.
 *
 * Which provider is open and which of its buckets is the modal's ONE open
 * path, owned by the modal; this only reads it. The header ref goes on a
 * wrapper around the PILL ROW, never on the section: an unrolled section is as
 * tall as its open bucket, so it keeps intersecting the scroll container long
 * after its header has left the viewport, and the return-to-top control would
 * never appear.
 */
export function ProviderSection({
	provider: p,
	expanded,
	openBucket,
	spinning,
	error,
	retestBlocked,
	readOnly,
	describedByReadOnly,
	regionIdBase,
	headerRef,
	onToggleProvider,
	onToggleBucket,
	onRetest,
	onDismissAll,
	onClean,
	actions,
}: {
	provider: MergedProvider;
	expanded: boolean;
	/** The open bucket line, only meaningful while `expanded`. */
	openBucket: Group | null;
	/** This provider's retest is the one in flight. */
	spinning: boolean;
	/** The last retest's failure, bannered inside the section. */
	error?: string;
	retestBlocked: boolean;
	readOnly: boolean;
	describedByReadOnly?: string;
	regionIdBase: string;
	headerRef: RefObject<HTMLDivElement | null>;
	onToggleProvider: () => void;
	onToggleBucket: (group: Group) => void;
	onRetest: () => void;
	/** Asks the modal to confirm dismissing exactly these ids. */
	onDismissAll: (modelIDs: string[]) => void;
	onClean: () => void;
	actions: ClaimRowActions;
}) {
	const { t } = useTranslation();
	const gone = actionableIn(p, "gone");
	const stale = actionableIn(p, "stale");
	const suspect = actionableIn(p, "suspect");
	const retired = actionableIn(p, "retired");
	// One predicate behind the pill's either-or controls and behind whether the
	// cleared summary renders, so the two can never disagree and offer a
	// re-probe with nothing to probe.
	const isCleared = providerHasNoPending(p);
	// Suspect ids are deliberately excluded: setModelsDismissed only touches
	// `enabled = false` rows, and a suspect model is still enabled, so sending
	// one would undercount `updated` and report an unknown model.
	const dismissable = [...retired, ...gone, ...stale].map((c) => c.model_id);
	const pointlessRetest = retestProvesNothing(p);
	const all = ALL_GROUPS.flatMap((g) => p[g] ?? []);
	const regionId = `${regionIdBase}-provider-${p.provider_id}`;
	const bucket = (group: Group) => (
		<BucketSection
			provider={p}
			group={group}
			open={expanded && openBucket === group}
			regionId={`${regionIdBase}-${group}-${p.provider_id}`}
			onToggle={() => onToggleBucket(group)}
			actions={actions}
		/>
	);
	return (
		<section
			data-testid="discrepancy-provider"
			data-provider-id={p.provider_id}
			className="space-y-2"
		>
			<div ref={expanded ? headerRef : undefined}>
				<ProviderPill
					providerName={p.provider_name}
					expanded={expanded}
					onToggle={onToggleProvider}
					// Pinned rows are deliberately absent: the chips are this
					// provider's problem count, and a pin is a decision the operator
					// made. Counting it would put a number on the pill (and, one level
					// up, on the badge) for something nobody needs to act on.
					counts={{
						gone: gone.length,
						stale: stale.length,
						suspect: suspect.length,
						retired: retired.length,
					}}
					cleared={{
						dismissed: all.filter((c) => c.status === "dismissed").length,
						resolved: all.filter((c) => c.status === "resolved").length,
					}}
					isCleared={isCleared}
					canDismiss={dismissable.length > 0}
					retestDisabled={retestBlocked || pointlessRetest}
					retestProvesNothing={pointlessRetest}
					retesting={spinning}
					onRetest={onRetest}
					onDismissAll={() => onDismissAll(dismissable)}
					onClean={onClean}
					describedByReadOnly={describedByReadOnly}
					readOnly={readOnly}
					regionId={regionId}
				/>
			</div>
			{/* A failed retest banners inside the section and keeps its claims: a
			    toast fades before it is read, and dropping the claims would read as
			    "fixed". OUTSIDE the collapsible region, so it is visible on a
			    collapsed pill, which is where the operator clicked Retest. */}
			{error ? (
				<div
					className="flex items-start gap-2 rounded-(--radius-box) border border-(--border-default) bg-(--surface-elevated) px-2.5 py-2 text-sm"
					data-testid="discrepancy-error"
				>
					<span className="ui-badge ui-badge-error shrink-0">
						{t("providers.discrepancies.error")}
					</span>
					<span className="break-words text-(--text-secondary)">{error}</span>
				</div>
			) : null}
			{/* Mounted only while open, and unanimated, for the same reason as the
			    bucket bodies: closing a provider that has a bucket open would
			    otherwise animate the whole open list collapsing, which is the most
			    expensive frame in the modal. See BucketSection. */}
			<div id={regionId}>
				{expanded ? (
					// A cleared provider KEEPS its buckets: the struck-through rows are
					// the log of what the operator did, and they stay reachable until
					// Clean. Dropping them here would be the vanishing-rows complaint
					// one level up.
					<div className="space-y-2 pl-5">
						{isCleared ? <ClearedSummary provider={p} /> : null}
						{bucket("retired")}
						{bucket("gone")}
						{bucket("suspect")}
						{bucket("stale")}
						{/* Last: the only bucket that is not a problem. */}
						{bucket("pinned")}
					</div>
				) : null}
			</div>
		</section>
	);
}
