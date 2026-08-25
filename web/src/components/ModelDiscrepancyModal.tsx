import { useCallback, useEffect, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type {
	DiscoveryChangeEntry,
	DiscoveryDiff,
	GroupClaim,
} from "../api/types";
import {
	type MergedClaim,
	type MergedProvider,
	providerHasNoPending,
	retestProvesNothing,
} from "../hooks/useDiscrepancies";
import { ChevronDown, ChevronRight, ChevronUp, RefreshCw } from "../lib/icons";
import { formatFieldValue } from "../pages/Providers/discoveryFormat";
import {
	CategoryGroup,
	Chip,
	DetailRow,
} from "../pages/Providers/discoveryPrimitives";
import { formatDateTimeShort, formatRelativeTime } from "../utils/format";
import { ConfirmDialog } from "./ConfirmDialog";
import { ProviderPill } from "./discrepancy/ProviderPill";
import { Modal } from "./Modal";

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
type Group = "gone" | "stale" | "suspect" | "retired" | "pinned";

const ALL_GROUPS: Group[] = ["gone", "stale", "suspect", "retired", "pinned"];

/** Which provider is unrolled, and which of its bucket lines. */
type OpenPath = { providerID: string; bucket: Group | null };

export interface ModelDiscrepancyModalProps {
	providers: MergedProvider[];
	/** Failover groups discovery disabled. They already count toward the badge,
	 * so they must render, or the badge points at nothing. */
	groupClaims: GroupClaim[];
	informational: DiscoveryChangeEntry[];
	onClose: () => void;
	onRetest: (providerId: string, providerName: string) => void;
	onRetestAll: () => void;
	/** Stops the Retest-all walk. The walk finishes the provider already in
	 * flight, so this asks for no further providers rather than aborting a
	 * discovery run half-applied. */
	onCancelRetestAll: () => void;
	onDismiss: (providerId: string, modelId: string) => void;
	/** Dismisses every actionable gone/stale model on one provider in one request.
	 * Suspect ids are never included: setModelsDismissed only touches
	 * `enabled = false` rows, so a still-enabled model would be refused. */
	onDismissAll: (providerId: string, modelIds: string[]) => void;
	/** Dismisses every actionable gone/stale model across EVERY listed provider.
	 * Batched by provider because the endpoint is provider-scoped, but reported as
	 * one action: this is the "I saw the badge and do not care about the fallout"
	 * path. */
	onDismissEverything: (
		batches: { providerID: string; modelIDs: string[] }[],
	) => void;
	/** Hands one pinned model back to automatic management. One model per call,
	 * like onDismiss: the endpoint reports which ids it cleared, and asking about
	 * one makes a short answer unambiguous. */
	onUnpin: (providerId: string, modelId: string) => void;
	/** Provider whose retest is in flight; only that section spins. */
	retestingProviderId?: string;
	/** True while ANY retest is in flight: every retest control goes disabled,
	 * because discovery is a heavy upstream call the hook serializes. */
	isRetesting: boolean;
	retestAllProgress?: { done: number; total: number };
	/** Per-provider retest failures, keyed by provider id. */
	errors: Record<string, string>;
	onExpandInformational: () => void;
	/** Message from a failed status fetch or refresh. Present means "we do not
	 * know what is wrong", which must never render as "nothing is wrong". */
	loadError?: string;
	/** True while the per-open status fetch is still out. Same rule as loadError,
	 * one step earlier: "still finding out" is not "nothing is wrong" either, and
	 * an operator who clicked a badge reading 76 must not be told the opposite
	 * for the width of a request. */
	loading?: boolean;
	readOnly: boolean;
	/** True when this instance is a managed fleet member. Pins are synced config
	 * (the EnabledModels section), so the primary's list is re-applied on the next
	 * sync pass and a local unpin would silently come back. Only the pin controls
	 * take this: everything else in here is per-member listing evidence. */
	managed?: boolean;
}

// eslint-disable-next-line max-lines-per-function -- size ratchet: split this component
export function ModelDiscrepancyModal({
	providers,
	groupClaims,
	informational,
	onClose,
	onRetest,
	onRetestAll,
	onCancelRetestAll,
	onDismiss,
	onDismissAll,
	onDismissEverything,
	onUnpin,
	retestingProviderId,
	isRetesting,
	retestAllProgress,
	errors,
	onExpandInformational,
	loadError,
	loading = false,
	readOnly,
	managed = false,
}: ModelDiscrepancyModalProps) {
	const { t } = useTranslation();

	/**
	 * Providers the operator has cleaned away, honoured only WHILE they stay clear.
	 *
	 * View-only, and safe to write nothing for, because a cleaned provider has
	 * nothing left to write: every row is either dismissed (already persisted) or
	 * resolved (the model is healthy, so it is not a claim). It does not come back
	 * on the next open either, since listClaimRows excludes dismissed rows and never
	 * reports healthy ones.
	 *
	 * The `providerHasNoPending` half of the filter is the load-bearing part, and it
	 * is not decoration. Membership alone would hide the provider FOREVER for the
	 * life of the modal, so a model that flapped healthy -> gone again after the
	 * Clean click would vanish silently: precisely the false reassurance this rework
	 * exists to remove. (An earlier comment here claimed mergeClaims would "re-add
	 * it as a new provider". It does not: Clean never touches the snapshot, so the
	 * provider is merged in place and a stale id in this set kept filtering it.)
	 *
	 * Deriving it also means no effect and no pruning pass: the moment a refresh
	 * gives the provider an actionable row, the predicate goes false and it
	 * reappears, carrying both its new claim and the struck-through log above it. If
	 * the operator then clears that row too, it hides again without a second click,
	 * which is fine: by then they have seen it and acted on it, so nothing is being
	 * hidden that they have not dealt with.
	 *
	 * Declared here rather than beside its handler because `hasContent` below reads
	 * the filtered list: a modal holding nothing but cleaned providers must show the
	 * empty state, not an empty claims zone.
	 */
	const [cleaned, setCleaned] = useState<Set<string>>(() => new Set());
	const visibleProviders = providers.filter(
		(p) => !(cleaned.has(p.provider_id) && providerHasNoPending(p)),
	);

	// Recomputed every render: a refetch that brings back a claim must take the
	// empty state away again.
	const hasContent =
		groupClaims.length > 0 ||
		visibleProviders.some((p) =>
			ALL_GROUPS.some((g) => (p[g] ?? []).length > 0),
		);

	// The informational zone starts expanded only when the discrepancy zone has
	// nothing to show, since it is then the only content worth reading. Seeded
	// once and then left alone: a claim arriving mid-session must not collapse a
	// zone the operator is already reading.
	//
	// The seed waits for the first render carrying ANY payload, rather than
	// reading the very first render. `useDiscrepancies` uses a fresh query key
	// per open, so there is always at least one render with an empty snapshot
	// before the fetch lands; seeding off that would latch the zone open while
	// claims exist, and would do it without firing the expand callback, leaving
	// an expanded journal whose dot never clears. An all-empty payload needs no
	// seed at all: with no entries the zone does not render.
	const hasPayload =
		visibleProviders.length > 0 ||
		groupClaims.length > 0 ||
		informational.length > 0;
	const [infoOpen, setInfoOpen] = useState(false);
	const [seeded, setSeeded] = useState(false);
	const [autoExpanded, setAutoExpanded] = useState(false);
	if (!seeded && hasPayload) {
		// React's documented "adjust state during render" bail-out, the same
		// pattern useDiscrepancies uses to seed its snapshot: an effect would
		// paint one frame of a collapsed zone first.
		setSeeded(true);
		setInfoOpen(!hasContent);
		setAutoExpanded(!hasContent && informational.length > 0);
	}
	const notified = useRef(false);
	const notifyExpanded = useCallback(() => {
		// Expanding marks the journal seen; once per open is enough.
		if (notified.current) return;
		notified.current = true;
		onExpandInformational();
	}, [onExpandInformational]);

	useEffect(() => {
		if (autoExpanded) notifyExpanded();
	}, [autoExpanded, notifyExpanded]);

	const toggleInfo = () => {
		const next = !infoOpen;
		setInfoOpen(next);
		if (next) notifyExpanded();
	};

	// Base for the aria-controls ids on the two collapsible zones. useId rather
	// than provider_id so the ids stay unique even if the modal is ever mounted
	// twice, and legal regardless of what a provider id looks like.
	const regionIdBase = useId();

	/**
	 * The ONE open path in the modal: which provider is unrolled and, within it,
	 * which bucket line.
	 *
	 * One atom rather than two Sets, deliberately. "Only one provider and only one
	 * line" is the feature, and with two collections three handlers would each be
	 * responsible for upholding it. Replacing the atom collapses the previous
	 * provider AND whatever line it had open in a single move, so the invariant is
	 * structural and cannot drift.
	 */
	const [openPath, setOpenPath] = useState<OpenPath | null>(null);

	const toggleProvider = (providerID: string) =>
		setOpenPath((prev) =>
			prev?.providerID === providerID ? null : { providerID, bucket: null },
		);

	// Opening a line always sets the provider too, so a click on a line inside a
	// different provider can never leave two providers looking open.
	const toggleBucket = (providerID: string, bucket: Group) =>
		setOpenPath((prev) =>
			prev?.providerID === providerID && prev.bucket === bucket
				? { providerID, bucket: null }
				: { providerID, bucket },
		);

	const onClean = (providerID: string) => {
		setCleaned((prev) => new Set(prev).add(providerID));
		// Never leave the open path pointing at a provider that is no longer
		// rendered: the return-to-top observer would then watch a detached node.
		setOpenPath((prev) => (prev?.providerID === providerID ? null : prev));
	};

	/**
	 * The Dismiss-all awaiting confirmation. Held in state rather than passed
	 * straight through, so the dialog can name the provider and the EXACT count
	 * being sent rather than the provider's total row count.
	 */
	const [confirmDismiss, setConfirmDismiss] = useState<{
		providerID: string;
		providerName: string;
		modelIDs: string[];
	} | null>(null);

	/** Whether the modal-wide Dismiss all is awaiting confirmation. */
	const [confirmDismissEverything, setConfirmDismissEverything] =
		useState(false);

	/**
	 * The open provider's section, watched so the return-to-top control knows when
	 * it has scrolled out of reach. Re-observed whenever `openPath` moves, since the
	 * node it points at changes with it.
	 *
	 * The state holds WHICH provider is out of view rather than a bare boolean, so
	 * it invalidates itself: opening another provider makes the stored id stop
	 * matching, and the control disappears without anyone having to reset a flag.
	 * A boolean needed an imperative clear at the top of the effect, which would
	 * also have left the control briefly pointing at the previous provider until
	 * the new observer's first callback landed.
	 */
	const openHeaderRef = useRef<HTMLDivElement | null>(null);
	const [offscreenFor, setOffscreenFor] = useState<string | null>(null);
	const headerOffscreen =
		offscreenFor !== null && offscreenFor === openPath?.providerID;

	useEffect(() => {
		const el = openHeaderRef.current;
		if (!openPath || !el) return;
		const root = el.closest("[data-modal-scroll]");
		// Modal renders unscrollable when `scrollable` is false, in which case there
		// is nothing to scroll back to and no control to show.
		if (!root) return;
		const providerID = openPath.providerID;
		const observer = new IntersectionObserver(
			([entry]) => setOffscreenFor(entry.isIntersecting ? null : providerID),
			{ root, threshold: 0 },
		);
		observer.observe(el);
		return () => observer.disconnect();
	}, [openPath]);

	// A retest is a real discovery run, which read-only mode rejects with a 403.
	// Disabled rather than hidden, so it does not read as a missing feature.
	//
	// `retestAllProgress` is part of the condition, not a duplicate of
	// `isRetesting`: between two providers the walk is refreshing status rather
	// than discovering, so no mutation is pending and `isRetesting` goes false for
	// that window. The walk's own lock would still refuse the click, silently. A
	// button that looks enabled and does nothing is the exact complaint that
	// started this rework, so the whole walk counts as blocked.
	const retestBlocked =
		isRetesting || readOnly || retestAllProgress !== undefined;

	// The read-only reason lives only in a `title` on a DISABLED button, and a
	// disabled button is not focusable, so keyboard and screen-reader users never
	// reach it. One sr-only sentence carries it into the DOM instead (the reason
	// is modal-wide, so a single node serves every blocked control) and each such
	// button points at it with aria-describedby. Disabled buttons stay in the
	// accessibility tree, so the description is announced in browse mode; the
	// title stays for pointer users, and nothing about the visual design moves.
	const readOnlyNoteId = `${regionIdBase}-readonly`;
	const describedByReadOnly = readOnly ? readOnlyNoteId : undefined;

	// The managed reason gets the same treatment, and its own node: it blocks one
	// control (Unpin) rather than the whole modal, and the two states can hold at
	// once, so a single shared sentence could not name either accurately.
	const managedNoteId = `${regionIdBase}-managed`;
	// Unpin writes to a synced entity, so it is blocked by demo read-only mode and
	// by managed membership alike; the two differ only in what they say about why.
	const unpinBlocked = readOnly || managed;
	// Read-only wins when both hold: it blocks the write outright, so sending the
	// operator to the primary would send them somewhere just as refusing.
	const unpinNoteId = readOnly
		? readOnlyNoteId
		: managed
			? managedNoteId
			: undefined;
	const unpinTitle = () => {
		if (readOnly) return t("providers.discrepancies.readOnlyTooltip");
		if (managed) return t("providers.discrepancies.unpinManagedTooltip");
		return t("providers.discrepancies.unpinTooltip");
	};

	const flapChip = (c: MergedClaim) => {
		// Primary number is "since your last visit"; the 30-day total is shown
		// in the tooltip as extra context beyond that count.
		if (c.flap_since_review > 0) {
			return (
				<span
					className="ui-badge ui-badge-warning shrink-0 tabular-nums"
					data-testid="discrepancy-flap"
					title={t("providers.discrepancies.flapWindowTooltip", {
						count: c.flap_window,
					})}
				>
					{t("providers.discrepancies.flapped", { count: c.flap_since_review })}
				</span>
			);
		}
		if (c.flap_window > 1) {
			// Nothing has flapped since the last visit here (flap_since_review is
			// 0), so there is no complementary count worth surfacing. The tooltip
			// instead names the window the visible count is scoped to, since the
			// chip label itself never states a timeframe.
			return (
				<span
					className="ui-badge ui-badge-warning shrink-0 tabular-nums"
					data-testid="discrepancy-flap"
					title={t("providers.discrepancies.flapWindowTooltip", {
						count: c.flap_window,
					})}
				>
					{t("providers.discrepancies.flapped", { count: c.flap_window })}
				</span>
			);
		}
		return null;
	};

	/**
	 * Tooltip for a row's Dismiss control.
	 *
	 * The ordinary promise — "it comes back if the provider lists it again" — is
	 * specifically untrue for a retired model. The provider lists it on every
	 * scan, and the dismissal is deliberately kept through that (see the Upsert
	 * exception), or the claim could never be silenced at all.
	 *
	 * Written as separate statements with literal keys rather than as one t()
	 * call picking a key: the i18n source-key check only follows literal
	 * arguments, so a key chosen inside the call is invisible to it and could go
	 * missing from en.json with nothing failing.
	 */
	const dismissTitle = (group: Group) => {
		if (readOnly) return t("providers.discrepancies.readOnlyTooltip");
		if (group === "retired") {
			return t("providers.discrepancies.dismissRetiredTooltip");
		}
		return t("providers.discrepancies.dismissTooltip");
	};

	const claimMeta = (c: MergedClaim, group: Group) => {
		if (group === "suspect") {
			return t("providers.discrepancies.suspectMeta", {
				count: c.missing_scans,
			});
		}
		// A retired model is still listed, so it was "last seen" moments ago and
		// that reading would contradict the row it sits on. Date it by when the
		// proxy retired it instead.
		//
		// The whole branch is on the group, not on the timestamp: the server sets
		// retired_at on every retired claim, but if one ever arrived without it,
		// falling through would print the "last seen" wording this state exists to
		// avoid. Better to say nothing about the timing than to say that.
		if (group === "retired") {
			return c.retired_at
				? t("providers.discrepancies.retiredMeta", {
						when: formatRelativeTime(c.retired_at),
					})
				: "";
		}
		// A pinned model IS missing from the listing, so "last seen" is true but
		// says nothing about why the row exists. Date it by the operator's decision
		// instead, and stay silent rather than fall through to the wrong wording if
		// a claim ever arrives without the stamp.
		if (group === "pinned") {
			return c.pinned_at
				? t("providers.discrepancies.pinnedMeta", {
						when: formatRelativeTime(c.pinned_at),
					})
				: "";
		}
		return t("providers.discrepancies.lastSeenMeta", {
			when: formatRelativeTime(c.last_seen_at),
		});
	};

	/**
	 * One model, as a TIGHT single line rather than a bordered card.
	 *
	 * The card treatment was the whole cost of this list: a bucket with 52 rows
	 * meant 52 rounded, bordered, separately-filled boxes, each two lines tall
	 * because the meta sat under the id. Dropping to one line with a hairline
	 * divider between rows (the container owns those, see renderBucket) roughly
	 * halves the height and removes the per-row paint work that made unrolling
	 * stutter. Same idiom as Bellhop's event list.
	 */
	const renderClaim = (p: MergedProvider, c: MergedClaim, group: Group) => {
		// `status`, never `state`: this is styling only.
		const isCleared = c.status === "resolved" || c.status === "dismissed";
		return (
			<div
				key={c.model_id}
				data-testid="discrepancy-claim"
				data-model-id={c.model_id}
				data-status={c.status}
				data-state={c.state}
				className="flex items-baseline gap-2 py-1 pl-1 pr-0.5"
			>
				{c.status === "new" ? (
					<span
						className="ui-badge ui-badge-accent shrink-0"
						data-testid="discrepancy-new"
					>
						{t("providers.discrepancies.new")}
					</span>
				) : null}
				<span
					className={`min-w-0 flex-1 truncate font-mono text-xs ${
						isCleared
							? "text-(--text-muted) line-through"
							: "text-(--text-primary)"
					}`}
					title={c.model_id}
				>
					{c.model_id}
				</span>
				<span
					className={`shrink-0 text-[11px] ${
						isCleared ? "text-(--text-muted)" : "text-(--text-tertiary)"
					}`}
				>
					{claimMeta(c, group)}
				</span>
				{flapChip(c)}
				{group === "pinned" && !isCleared ? (
					<button
						type="button"
						onClick={() => onUnpin(p.provider_id, c.model_id)}
						disabled={unpinBlocked}
						title={unpinTitle()}
						aria-describedby={unpinNoteId}
						className="ui-btn ui-btn-ghost ui-btn-compact shrink-0"
						data-testid="discrepancy-unpin"
					>
						{t("providers.discrepancies.unpin")}
					</button>
				) : null}
				{(group === "gone" || group === "retired") && !isCleared ? (
					<button
						type="button"
						onClick={() => onDismiss(p.provider_id, c.model_id)}
						disabled={readOnly}
						title={dismissTitle(group)}
						aria-describedby={describedByReadOnly}
						className="ui-btn ui-btn-ghost ui-btn-compact shrink-0"
						data-testid="discrepancy-dismiss"
					>
						{t("providers.discrepancies.dismiss")}
					</button>
				) : null}
			</div>
		);
	};

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
	const renderBucket = (p: MergedProvider, group: Group) => {
		// `?? []`: a server predating the operator pin omits the bucket entirely,
		// which a rolling deploy puts behind this dashboard.
		const claims = p[group] ?? [];
		if (claims.length === 0) return null;
		const open =
			openPath?.providerID === p.provider_id && openPath.bucket === group;
		const regionId = `${regionIdBase}-${group}-${p.provider_id}`;
		return (
			<section data-testid={`discrepancy-group-${group}`} className="space-y-1">
				<button
					type="button"
					onClick={() => toggleBucket(p.provider_id, group)}
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
							{claims.map((c) => renderClaim(p, c, group))}
						</div>
					) : null}
				</div>
			</section>
		);
	};

	/**
	 * Headline over a cleared provider's buckets, reporting the TWO causes
	 * separately and both at once for a provider that had some of each.
	 *
	 * Dismissed rows get a count, not one line each: sixty lines saying "model X
	 * dismissed" is the wall of text this redesign removes, and the rows are one
	 * click away in their bucket. Resolved rows keep their per-model line, because
	 * "is listed again" says something the count cannot.
	 */
	const renderClearedSummary = (p: MergedProvider) => {
		const all = ALL_GROUPS.flatMap((g) => p[g] ?? []);
		const dismissed = all.filter((c) => c.status === "dismissed");
		const relisted = all.filter((c) => c.status === "resolved");
		return (
			<div
				data-testid="discrepancy-resolved"
				className="space-y-1 rounded-(--radius-box) border border-(--border-default) bg-(--surface-elevated) px-2.5 py-2"
			>
				{dismissed.length > 0 ? (
					<div className="flex items-center gap-2">
						<span className="ui-badge ui-badge-neutral shrink-0">✓</span>
						<span
							className="text-sm text-(--text-secondary)"
							data-testid="discrepancy-dismissed-summary"
						>
							{t("providers.discrepancies.dismissedSummary", {
								count: dismissed.length,
							})}
						</span>
					</div>
				) : null}
				{relisted.length > 0 ? (
					<div className="flex items-center gap-2">
						<span className="ui-badge ui-badge-success shrink-0">✓</span>
						<span className="text-sm text-(--text-secondary)">
							{t("providers.discrepancies.resolved")}
						</span>
					</div>
				) : null}
				{relisted.map((c) => (
					<p
						key={c.model_id}
						className="text-[11px] text-(--text-tertiary)"
						data-testid="discrepancy-resolved-detail"
					>
						{/* A retired model was never missing from the listing, so "listed
						    again" would be false for it — what changed is that it serves
						    again. The flap variant is skipped too: flapping counts a model
						    entering and leaving the listing, which this one never did. */}
						{c.state === "retired"
							? t("providers.discrepancies.resolvedRetiredPlain", {
									model: c.model_id,
								})
							: c.flap_since_review > 0
								? t("providers.discrepancies.resolvedDetail", {
										model: c.model_id,
										count: c.flap_since_review,
									})
								: t("providers.discrepancies.resolvedPlain", {
										model: c.model_id,
									})}
					</p>
				))}
			</div>
		);
	};

	/** Rows that still need the operator: `pending` or `new`, never cleared. */
	const actionableIn = (p: MergedProvider, group: Group) =>
		(p[group] ?? []).filter(
			(c) => c.status === "pending" || c.status === "new",
		);

	const renderProvider = (p: MergedProvider) => {
		const expanded = openPath?.providerID === p.provider_id;
		const spinning = retestingProviderId === p.provider_id;
		const error = errors[p.provider_id];
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
		return (
			<section
				key={p.provider_id}
				data-testid="discrepancy-provider"
				data-provider-id={p.provider_id}
				className="space-y-2"
			>
				{/* The ref goes on a wrapper around the PILL ROW, never on the section.
				    An unrolled section is as tall as its open bucket, so it keeps
				    intersecting the scroll container long after its header has left the
				    viewport, and the return-to-top control would never appear. */}
				<div ref={expanded ? openHeaderRef : undefined}>
					<ProviderPill
						providerName={p.provider_name}
						expanded={expanded}
						onToggle={() => toggleProvider(p.provider_id)}
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
						onRetest={() => onRetest(p.provider_id, p.provider_name)}
						onDismissAll={() =>
							setConfirmDismiss({
								providerID: p.provider_id,
								providerName: p.provider_name,
								modelIDs: dismissable,
							})
						}
						onClean={() => onClean(p.provider_id)}
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
				    expensive frame in the modal. See renderBucket. */}
				<div id={regionId}>
					{expanded ? (
						// A cleared provider KEEPS its buckets: the struck-through rows are
						// the log of what the operator did, and they stay reachable until
						// Clean. Dropping them here would be the vanishing-rows complaint
						// one level up.
						<div className="space-y-2 pl-5">
							{isCleared ? renderClearedSummary(p) : null}
							{renderBucket(p, "retired")}
							{renderBucket(p, "gone")}
							{renderBucket(p, "suspect")}
							{renderBucket(p, "stale")}
							{/* Last: the only bucket that is not a problem. */}
							{renderBucket(p, "pinned")}
						</div>
					) : null}
				</div>
			</section>
		);
	};

	// Failover groups discovery disabled: `hotel/<model>` routing for them is
	// dead. No Retest (a retest is provider-scoped and a group is not) and no
	// dismiss (the claim clears itself when the group is routable again).
	const renderGroupClaims = () => {
		if (groupClaims.length === 0) return null;
		return (
			<CategoryGroup
				sign="⊘"
				count={groupClaims.length}
				badgeVariant="ui-badge-orange"
				label={t("providers.discrepancies.groupClaims")}
				testId="discrepancy-group-claims"
			>
				<div className="space-y-1 rounded-(--radius-box) border border-(--border-default) bg-(--surface-elevated) px-2.5 py-2">
					{groupClaims.map((g) => (
						<div
							key={g.display_model}
							data-testid="discrepancy-group-claim"
							data-display-model={g.display_model}
						>
							<DetailRow
								stacked
								primary={g.display_model}
								secondary={
									<>
										{t("providers.discrepancies.groupRoutable", {
											routable: g.routable_count,
											members: g.member_count,
										})}
										{" · "}
										{t("providers.discrepancies.groupDisabledAt", {
											when: formatDateTimeShort(g.disabled_at),
										})}
									</>
								}
							/>
						</div>
					))}
				</div>
			</CategoryGroup>
		);
	};

	const chipCategory = (
		items: DiscoveryDiff["added"],
		sign: string,
		badgeVariant: string,
		label: string,
		testId: string,
	) =>
		items?.length ? (
			<CategoryGroup
				sign={sign}
				count={items.length}
				badgeVariant={badgeVariant}
				label={label}
				testId={testId}
			>
				<div className="flex flex-wrap gap-1.5">
					{items.map((c) => (
						<Chip key={c.model_id} label={c.model_id} mono />
					))}
				</div>
			</CategoryGroup>
		) : null;

	const renderInformationalEntry = (
		entry: DiscoveryChangeEntry,
		key: string,
	) => {
		const diff = entry.diff;
		const failover = [
			...(diff.failover_deleted_groups ?? []),
			...(diff.failover_updated_groups ?? []),
			...(diff.failover_disabled_groups ?? []),
		];
		return (
			<div
				key={key}
				data-testid="discrepancy-informational-entry"
				className="space-y-2 rounded-(--radius-box) border border-(--border-default) bg-(--surface-elevated) px-2.5 py-2"
			>
				<div className="flex items-baseline justify-between gap-2">
					<span className="truncate text-xs font-semibold text-(--accent)">
						{entry.provider_name ||
							t("providers.discoverySummary.failover", "Failover")}
					</span>
					<span className="shrink-0 text-[11px] text-(--text-tertiary)">
						{formatRelativeTime(entry.detected_at)}
					</span>
				</div>
				{chipCategory(
					diff.added,
					"+",
					"ui-badge-success",
					t("providers.discrepancies.added"),
					"discrepancy-informational-added",
				)}
				{chipCategory(
					diff.reenabled,
					"↺",
					"ui-badge-info",
					t("providers.discrepancies.reenabled"),
					"discrepancy-informational-reenabled",
				)}
				{diff.updated?.length ? (
					<CategoryGroup
						sign="±"
						count={diff.updated.length}
						badgeVariant="ui-badge-accent"
						label={t("providers.discrepancies.updated")}
						testId="discrepancy-informational-updated"
					>
						<div className="space-y-1">
							{diff.updated.map((u) => (
								<DetailRow
									key={u.model_id}
									stacked
									primary={u.model_id}
									secondary={u.changes
										.map(
											(c) =>
												`${t(`providers.discoverySummary.field.${c.field}`, c.field)}: ${formatFieldValue(
													c.field,
													c.old,
													t("providers.discoverySummary.unset"),
												)} → ${formatFieldValue(
													c.field,
													c.new,
													t("providers.discoverySummary.unset"),
												)}`,
										)
										.join(", ")}
								/>
							))}
						</div>
					</CategoryGroup>
				) : null}
				{failover.length ? (
					<CategoryGroup
						sign="⇄"
						count={failover.length}
						badgeVariant="ui-badge-orange"
						label={t("providers.discrepancies.failover")}
						testId="discrepancy-informational-failover"
					>
						<div className="flex flex-wrap gap-1.5">
							{failover.map((g) => (
								<Chip key={g.display_model} label={g.display_model} mono />
							))}
						</div>
					</CategoryGroup>
				) : null}
			</div>
		);
	};

	const retestableProviders = visibleProviders.filter(
		(p) => !providerHasNoPending(p) && !retestProvesNothing(p),
	);

	/**
	 * Every dismissible model in the modal, batched by provider.
	 *
	 * Batched because the endpoint is provider-scoped: `POST /api/discovery/dismiss`
	 * takes one provider_id and its model_ids, so a modal-wide dismiss is N requests
	 * however it is presented. Suspect ids are excluded here for the same reason as
	 * on the pill: the server refuses a still-enabled model.
	 */
	const everythingDismissable = visibleProviders
		.map((p) => ({
			providerID: p.provider_id,
			// Same buckets as the per-provider Dismiss all above, and in the same
			// order. They have to agree: if this list is narrower, the confirm
			// dialog undercounts what it is about to clear, the rows it missed
			// come back on the next refresh looking like a failed dismissal, and
			// a provider whose claims are ALL in a missing bucket produces an
			// empty batch that the filter below drops, so the button never
			// appears at all.
			modelIDs: (["retired", "gone", "stale"] as const).flatMap((g) =>
				actionableIn(p, g).map((c) => c.model_id),
			),
		}))
		.filter((b) => b.modelIDs.length > 0);
	const everythingCount = everythingDismissable.reduce(
		(n, b) => n + b.modelIDs.length,
		0,
	);

	return (
		<>
			<Modal
				onClose={onClose}
				maxWidth="max-w-2xl"
				scrollable
				header={
					<div className="mb-4 flex items-center gap-3">
						<h2 className="text-xl font-bold text-white">
							{t("providers.discrepancies.title")}
						</h2>
						{retestAllProgress ? (
							<span
								className="ui-badge ui-badge-neutral tabular-nums"
								data-testid="discrepancy-retest-progress"
							>
								{t("providers.discrepancies.retestProgress", {
									done: retestAllProgress.done,
									total: retestAllProgress.total,
								})}
							</span>
						) : null}
						<span className="flex-1" />
						{/* Cancel replaces Retest all for the duration of the walk, and is
					    the one control `retestBlocked` must not disable: it is the way
					    out of a walk across many providers, each a slow upstream call. */}
						{retestAllProgress ? (
							<button
								type="button"
								onClick={onCancelRetestAll}
								title={t("providers.discrepancies.cancelRetestAllTooltip")}
								className="ui-btn ui-btn-ghost ui-btn-compact shrink-0"
								data-testid="discrepancy-retest-all-cancel"
							>
								{t("providers.discrepancies.cancelRetestAll")}
							</button>
						) : retestableProviders.length > 0 ? (
							<button
								type="button"
								onClick={onRetestAll}
								disabled={retestBlocked}
								title={
									readOnly
										? t("providers.discrepancies.readOnlyTooltip")
										: t("providers.discrepancies.retestAllTooltip")
								}
								aria-describedby={describedByReadOnly}
								className="ui-btn ui-btn-secondary ui-btn-compact shrink-0"
								data-testid="discrepancy-retest-all"
							>
								<RefreshCw
									size={13}
									className={isRetesting ? "animate-spin" : ""}
								/>
								{t("providers.discrepancies.retestAll")}
							</button>
						) : null}
						{/* The whole point of the badge: see a number you do not care about
						    the detail of, clear it, done. Sends one request per provider
						    (the endpoint is provider-scoped) but confirms once and toasts
						    once. Hidden rather than disabled when there is nothing to
						    dismiss, matching Retest all directly above. */}
						{everythingDismissable.length > 0 ? (
							<button
								type="button"
								onClick={() => setConfirmDismissEverything(true)}
								disabled={readOnly}
								title={
									readOnly
										? t("providers.discrepancies.readOnlyTooltip")
										: t("providers.discrepancies.dismissEverythingTooltip")
								}
								aria-describedby={describedByReadOnly}
								className="ui-btn ui-btn-ghost ui-btn-compact shrink-0"
								data-testid="discrepancy-dismiss-everything"
							>
								{t("providers.discrepancies.dismissEverything")}
							</button>
						) : null}
					</div>
				}
			>
				<div className="space-y-5" data-testid="discrepancy-modal">
					{readOnly ? (
						<span
							id={readOnlyNoteId}
							className="sr-only"
							data-testid="discrepancy-readonly-note"
						>
							{t("providers.discrepancies.readOnlyTooltip")}
						</span>
					) : null}
					{!readOnly && managed ? (
						<span
							id={managedNoteId}
							className="sr-only"
							data-testid="discrepancy-managed-note"
						>
							{t("providers.discrepancies.unpinManagedTooltip")}
						</span>
					) : null}
					{/* A failed load banners at the top and, crucially, suppresses the
				    empty state below: "nothing is wrong" when we could not find out
				    is the false reassurance this whole rework exists to remove. */}
					{loadError ? (
						// role="alert": this appears asynchronously inside an already-open
						// dialog, so without a live region assistive tech never announces
						// that the modal failed to find out what is wrong.
						<div
							role="alert"
							className="flex items-start gap-2 rounded-(--radius-box) border border-(--border-default) bg-(--surface-elevated) px-2.5 py-2 text-sm"
							data-testid="discrepancy-load-error"
						>
							<span className="ui-badge ui-badge-error shrink-0">
								{t("providers.discrepancies.error")}
							</span>
							<span className="break-words text-(--text-secondary)">
								{loadError}
							</span>
						</div>
					) : null}

					{/* Zone 1: what is currently wrong. Sections render in the order the
				    backend gave them; re-sorting here would move a section out from
				    under the cursor mid-session. */}
					{hasContent ? (
						<div className="space-y-5">
							{visibleProviders.map(renderProvider)}
							{renderGroupClaims()}
						</div>
					) : loadError ? null : loading ? (
						/* Distinct from both neighbours on purpose: the error says "we
					   asked and could not find out", this says "we are still
					   asking", and the empty state below says "we asked and the
					   answer is nothing". Collapsing the first two into the third is
					   the false reassurance this rework exists to remove. */
						<p
							className="text-sm text-(--text-tertiary)"
							data-testid="discrepancy-loading"
							aria-live="polite"
						>
							{t("providers.discrepancies.loading")}
						</p>
					) : (
						<p
							className="text-sm text-(--text-tertiary)"
							data-testid="discrepancy-empty"
						>
							{t("providers.discrepancies.empty")}
						</p>
					)}

					{/* Zone 2: the informational journal. Never holds the badge open. */}
					{informational.length > 0 ? (
						<section
							data-testid="discrepancy-informational"
							className="space-y-2"
						>
							<button
								type="button"
								onClick={toggleInfo}
								aria-expanded={infoOpen}
								aria-controls={`${regionIdBase}-informational`}
								className="flex w-full items-center gap-2 text-left"
								data-testid="discrepancy-informational-toggle"
							>
								{infoOpen ? (
									<ChevronDown size={14} className="shrink-0" />
								) : (
									<ChevronRight size={14} className="shrink-0" />
								)}
								<span className="text-[11px] font-semibold uppercase tracking-wider text-(--text-tertiary)">
									{t("providers.discrepancies.recentChanges")}
								</span>
								<span className="h-px flex-1 bg-white/30" />
								<span className="ui-badge ui-badge-neutral shrink-0 tabular-nums">
									{informational.length}
								</span>
							</button>
							{/* Collapsed, the header said only "Recent changes" and a count,
						    nothing about what the entries are. The zone starts collapsed
						    whenever the claims zone has content, so this line is where the
						    operator actually meets it. Newest and oldest come straight off
						    the array ends: both journal queries end ORDER BY
						    detected_at DESC. Hidden when expanded, where the entries speak
						    for themselves. */}
							{!infoOpen ? (
								<p
									className="pl-5 text-[11px] text-(--text-tertiary)"
									data-testid="discrepancy-journal-summary"
								>
									{informational.length === 1
										? t("providers.discrepancies.journalSummaryOne", {
												when: formatRelativeTime(informational[0].detected_at),
											})
										: t("providers.discrepancies.journalSummary", {
												count: informational.length,
												newest: formatRelativeTime(
													informational[0].detected_at,
												),
												oldest: formatRelativeTime(
													informational[informational.length - 1].detected_at,
												),
											})}
								</p>
							) : null}
							<div
								id={`${regionIdBase}-informational`}
								className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
									infoOpen ? "grid-rows-[1fr]" : "grid-rows-[0fr]"
								}`}
							>
								{/* Same reason as the stale zone: collapsed here is a visual
							    state only, so the journal must be made inert or it is read
							    out in full under an aria-expanded="false" toggle. */}
								<div className="overflow-hidden" inert={!infoOpen}>
									<div className="space-y-2">
										{informational.map((entry, i) =>
											renderInformationalEntry(
												entry,
												`${entry.provider_name}-${entry.detected_at}-${i}`,
											),
										)}
									</div>
								</div>
							</div>
						</section>
					) : null}

					{/* Sticky inside the scrolling body, so it rides the bottom-right of the
				    modal rather than the page. Only exists while a provider is open:
				    there is nothing to return to otherwise. */}
					{openPath && headerOffscreen ? (
						<div className="sticky bottom-2 flex justify-end">
							<button
								type="button"
								onClick={() => {
									// Scroll only. Collapsing here would defeat the whole point:
									// the operator is deep in a list they want to keep open.
									openHeaderRef.current?.scrollIntoView({
										block: "start",
										behavior: "smooth",
									});
								}}
								className="ui-btn ui-btn-secondary ui-btn-compact shrink-0 shadow-lg"
								title={t("providers.discrepancies.returnToTop")}
								aria-label={t("providers.discrepancies.returnToTop")}
								data-testid="discrepancy-return-to-top"
							>
								{/* Accent-tinted so it reads as a live control floating over
								    the list rather than more of the list's own chrome. */}
								<ChevronUp size={14} className="text-(--accent)" />
							</button>
						</div>
					) : null}
				</div>
			</Modal>
			{/* A SIBLING of the modal, never nested inside it: ConfirmDialog renders
		    through Modal and therefore portals to document.body, and a nested
		    frosted-glass surface breaks the backdrop filter. */}
			{confirmDismiss ? (
				<ConfirmDialog
					title={t("providers.discrepancies.dismissAllConfirmTitle", {
						count: confirmDismiss.modelIDs.length,
						provider: confirmDismiss.providerName,
					})}
					message={t("providers.discrepancies.dismissAllConfirmBody", {
						count: confirmDismiss.modelIDs.length,
						provider: confirmDismiss.providerName,
					})}
					fields={[]}
					// ConfirmDialog defaults its confirm label to "Delete", which this is
					// not: nothing is deleted and the action is reversible.
					confirmLabel={t("providers.discrepancies.dismissAll")}
					confirmTestId="discrepancy-dismiss-all-confirm"
					onConfirm={() => {
						onDismissAll(confirmDismiss.providerID, confirmDismiss.modelIDs);
						setConfirmDismiss(null);
					}}
					onCancel={() => setConfirmDismiss(null)}
				/>
			) : null}
			{confirmDismissEverything ? (
				<ConfirmDialog
					title={t("providers.discrepancies.dismissEverythingConfirmTitle", {
						count: everythingCount,
					})}
					message={t("providers.discrepancies.dismissEverythingConfirmBody", {
						count: everythingCount,
					})}
					fields={[]}
					confirmLabel={t("providers.discrepancies.dismissEverything")}
					confirmTestId="discrepancy-dismiss-everything-confirm"
					onConfirm={() => {
						onDismissEverything(everythingDismissable);
						setConfirmDismissEverything(false);
					}}
					onCancel={() => setConfirmDismissEverything(false)}
				/>
			) : null}
		</>
	);
}
