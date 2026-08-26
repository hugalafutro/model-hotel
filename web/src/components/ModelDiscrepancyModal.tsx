import { useCallback, useEffect, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { DiscoveryChangeEntry, GroupClaim } from "../api/types";
import {
	type MergedProvider,
	providerHasNoPending,
	retestProvesNothing,
} from "../hooks/useDiscrepancies";
import { ChevronUp, RefreshCw } from "../lib/icons";
import { ConfirmDialog } from "./ConfirmDialog";
import type { ClaimRowActions } from "./discrepancy/ClaimRow";
import { GroupClaimsSection } from "./discrepancy/GroupClaimsSection";
import { ALL_GROUPS, actionableIn, type Group } from "./discrepancy/groups";
import { InformationalJournal } from "./discrepancy/InformationalJournal";
import { ProviderSection } from "./discrepancy/ProviderSection";
import { Modal } from "./Modal";

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
	const unpinTitle = readOnly
		? t("providers.discrepancies.readOnlyTooltip")
		: managed
			? t("providers.discrepancies.unpinManagedTooltip")
			: t("providers.discrepancies.unpinTooltip");

	// Everything a claim row needs to gate and route its two actions, computed
	// once here because the reasons are modal-wide (see the notes above).
	const claimActions: ClaimRowActions = {
		readOnly,
		unpinBlocked,
		unpinTitle,
		unpinNoteId,
		describedByReadOnly,
		onUnpin,
		onDismiss,
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
							{visibleProviders.map((p) => (
								<ProviderSection
									key={p.provider_id}
									provider={p}
									expanded={openPath?.providerID === p.provider_id}
									openBucket={
										openPath?.providerID === p.provider_id
											? openPath.bucket
											: null
									}
									spinning={retestingProviderId === p.provider_id}
									error={errors[p.provider_id]}
									retestBlocked={retestBlocked}
									readOnly={readOnly}
									describedByReadOnly={describedByReadOnly}
									regionIdBase={regionIdBase}
									headerRef={openHeaderRef}
									onToggleProvider={() => toggleProvider(p.provider_id)}
									onToggleBucket={(group) => toggleBucket(p.provider_id, group)}
									onRetest={() => onRetest(p.provider_id, p.provider_name)}
									onDismissAll={(modelIDs) =>
										setConfirmDismiss({
											providerID: p.provider_id,
											providerName: p.provider_name,
											modelIDs,
										})
									}
									onClean={() => onClean(p.provider_id)}
									actions={claimActions}
								/>
							))}
							<GroupClaimsSection groupClaims={groupClaims} />
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
					<InformationalJournal
						informational={informational}
						open={infoOpen}
						onToggle={toggleInfo}
						regionId={`${regionIdBase}-informational`}
					/>

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
