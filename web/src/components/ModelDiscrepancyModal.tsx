import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type {
	DiscoveryChangeEntry,
	DiscoveryDiff,
	GroupClaim,
} from "../api/types";
import {
	type MergedClaim,
	type MergedProvider,
	providerIsResolved,
} from "../hooks/useDiscrepancies";
import { ChevronDown, ChevronRight, RefreshCw } from "../lib/icons";
import { formatFieldValue } from "../pages/Providers/discoveryFormat";
import {
	CategoryGroup,
	Chip,
	DetailRow,
} from "../pages/Providers/discoveryPrimitives";
import { formatDateTimeShort, formatRelativeTime } from "../utils/format";
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
type Group = "gone" | "stale" | "suspect";

const ALL_GROUPS: Group[] = ["gone", "stale", "suspect"];

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
	readOnly: boolean;
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
	retestingProviderId,
	isRetesting,
	retestAllProgress,
	errors,
	onExpandInformational,
	loadError,
	readOnly,
}: ModelDiscrepancyModalProps) {
	const { t } = useTranslation();

	// Recomputed every render: a refetch that brings back a claim must take the
	// empty state away again.
	const hasContent =
		groupClaims.length > 0 ||
		providers.some((p) => ALL_GROUPS.some((g) => p[g].length > 0));

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
		providers.length > 0 || groupClaims.length > 0 || informational.length > 0;
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

	// Stale is aged-out history, collapsed behind its count until asked for.
	const [staleOpen, setStaleOpen] = useState<Set<string>>(() => new Set());
	const toggleStale = (providerID: string) =>
		setStaleOpen((prev) => {
			const next = new Set(prev);
			if (next.has(providerID)) next.delete(providerID);
			else next.add(providerID);
			return next;
		});

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

	const claimMeta = (c: MergedClaim, group: Group) =>
		group === "suspect"
			? t("providers.discrepancies.suspectMeta", { count: c.missing_scans })
			: t("providers.discrepancies.lastSeenMeta", {
					when: formatRelativeTime(c.last_seen_at),
				});

	const renderClaim = (p: MergedProvider, c: MergedClaim, group: Group) => {
		// `status`, never `state`: this is styling only.
		const isResolved = c.status === "resolved";
		return (
			<div
				key={c.model_id}
				data-testid="discrepancy-claim"
				data-model-id={c.model_id}
				data-status={c.status}
				data-state={c.state}
				className="flex items-start justify-between gap-3 rounded-(--radius-box) border border-(--border-default) bg-(--surface-elevated) px-2.5 py-2"
			>
				<div className="min-w-0 flex-1 space-y-0.5">
					<div className="flex items-center gap-1.5">
						{c.status === "new" ? (
							<span
								className="ui-badge ui-badge-accent shrink-0"
								data-testid="discrepancy-new"
							>
								{t("providers.discrepancies.new")}
							</span>
						) : null}
						<span
							className={`truncate font-mono text-xs ${
								isResolved
									? "text-(--text-muted) line-through"
									: "text-(--text-primary)"
							}`}
							title={c.model_id}
						>
							{c.model_id}
						</span>
					</div>
					<div
						className={`text-[11px] ${
							isResolved ? "text-(--text-muted)" : "text-(--text-tertiary)"
						}`}
					>
						{claimMeta(c, group)}
					</div>
				</div>
				<div className="flex shrink-0 items-center gap-1.5">
					{flapChip(c)}
					{group === "gone" && !isResolved ? (
						<button
							type="button"
							onClick={() => onDismiss(p.provider_id, c.model_id)}
							disabled={readOnly}
							title={
								readOnly
									? t("providers.discrepancies.readOnlyTooltip")
									: t("providers.discrepancies.dismissTooltip")
							}
							className="ui-btn ui-btn-ghost ui-btn-compact shrink-0 disabled:cursor-not-allowed disabled:opacity-50"
							data-testid="discrepancy-dismiss"
						>
							{t("providers.discrepancies.dismiss")}
						</button>
					) : null}
				</div>
			</div>
		);
	};

	const renderGoneOrSuspect = (
		p: MergedProvider,
		group: "gone" | "suspect",
	) => {
		const claims = p[group];
		if (claims.length === 0) return null;
		return (
			<CategoryGroup
				sign={group === "gone" ? "×" : "?"}
				count={claims.length}
				badgeVariant={group === "gone" ? "ui-badge-error" : "ui-badge-warning"}
				label={t(`providers.discrepancies.group.${group}`)}
				testId={`discrepancy-group-${group}`}
			>
				<div className="space-y-1.5">
					{claims.map((c) => renderClaim(p, c, group))}
				</div>
			</CategoryGroup>
		);
	};

	// Stale keeps the same chevron/collapse pattern as DiscoverySummaryModal's
	// section toggle, so the two modals behave identically where they overlap.
	const renderStale = (p: MergedProvider) => {
		if (p.stale.length === 0) return null;
		const open = staleOpen.has(p.provider_id);
		return (
			<section data-testid="discrepancy-group-stale" className="space-y-1.5">
				<button
					type="button"
					onClick={() => toggleStale(p.provider_id)}
					aria-expanded={open}
					className="flex w-full items-center gap-2 text-left"
					data-testid="discrepancy-stale-toggle"
				>
					{open ? (
						<ChevronDown size={14} className="shrink-0" />
					) : (
						<ChevronRight size={14} className="shrink-0" />
					)}
					<span className="text-[11px] font-semibold uppercase tracking-wider text-(--text-tertiary)">
						{t("providers.discrepancies.group.stale")}
					</span>
					<span className="h-px flex-1 bg-white/30" />
					<span className="ui-badge ui-badge-neutral shrink-0 tabular-nums">
						{p.stale.length}
					</span>
				</button>
				<div
					className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
						open ? "grid-rows-[1fr]" : "grid-rows-[0fr]"
					}`}
				>
					<div className="overflow-hidden">
						<div className="space-y-1.5">
							{p.stale.map((c) => renderClaim(p, c, "stale"))}
						</div>
					</div>
				</div>
			</section>
		);
	};

	const renderResolved = (p: MergedProvider) => {
		// Every claim that cleared this session, named individually: "all resolved"
		// on its own does not tell the operator what fluctuated.
		const cleared = ALL_GROUPS.flatMap((g) => p[g]).filter(
			(c) => c.status === "resolved",
		);
		return (
			<div
				data-testid="discrepancy-resolved"
				className="space-y-1 rounded-(--radius-box) border border-(--border-default) bg-(--surface-elevated) px-2.5 py-2"
			>
				<div className="flex items-center gap-2">
					<span className="ui-badge ui-badge-success shrink-0">✓</span>
					<span className="text-sm text-(--text-secondary)">
						{t("providers.discrepancies.resolved")}
					</span>
				</div>
				{/* Guarded rather than left to map-over-empty: `every` on an empty
				    array is true, so a provider with three empty buckets reads as
				    resolved. Unreachable while the backend only lists providers that
				    have a claim, but the headline line then stands alone instead of
				    introducing a detail list that never comes. */}
				{cleared.length > 0
					? cleared.map((c) => (
							<p
								key={c.model_id}
								className="text-[11px] text-(--text-tertiary)"
								data-testid="discrepancy-resolved-detail"
							>
								{c.flap_since_review > 0
									? t("providers.discrepancies.resolvedDetail", {
											model: c.model_id,
											count: c.flap_since_review,
										})
									: t("providers.discrepancies.resolvedPlain", {
											model: c.model_id,
										})}
							</p>
						))
					: null}
			</div>
		);
	};

	const renderProvider = (p: MergedProvider) => {
		// One predicate for both the resolved body and the missing Retest, so the
		// two can never disagree and offer a re-probe with nothing to probe.
		const resolved = providerIsResolved(p);
		const spinning = retestingProviderId === p.provider_id;
		const error = errors[p.provider_id];
		return (
			<section
				key={p.provider_id}
				data-testid="discrepancy-provider"
				data-provider-id={p.provider_id}
				className="space-y-2"
			>
				<div className="flex items-center gap-2">
					<span className="truncate text-sm font-semibold text-(--accent)">
						{p.provider_name}
					</span>
					<span className="h-px flex-1 bg-white/30" />
					{resolved ? null : (
						<button
							type="button"
							onClick={() => onRetest(p.provider_id, p.provider_name)}
							disabled={retestBlocked}
							title={
								readOnly
									? t("providers.discrepancies.readOnlyTooltip")
									: t("providers.discrepancies.retestTooltip")
							}
							className="ui-btn ui-btn-secondary ui-btn-compact inline-flex shrink-0 items-center gap-1.5 disabled:cursor-not-allowed disabled:opacity-50"
							data-testid="discrepancy-retest"
						>
							<RefreshCw size={13} className={spinning ? "animate-spin" : ""} />
							{spinning
								? t("providers.discrepancies.retesting")
								: t("providers.discrepancies.retest")}
						</button>
					)}
				</div>
				{/* A failed retest banners inside the section and keeps its claims:
				    a toast fades before it is read, and dropping the claims would
				    read as "fixed". */}
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
				{resolved ? (
					renderResolved(p)
				) : (
					<div className="space-y-3">
						{renderGoneOrSuspect(p, "gone")}
						{renderGoneOrSuspect(p, "suspect")}
						{renderStale(p)}
					</div>
				)}
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

	const unresolvedProviders = providers.filter((p) => !providerIsResolved(p));

	return (
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
							className="ui-btn ui-btn-ghost ui-btn-compact inline-flex shrink-0 items-center gap-1.5"
							data-testid="discrepancy-retest-all-cancel"
						>
							{t("providers.discrepancies.cancelRetestAll")}
						</button>
					) : unresolvedProviders.length > 0 ? (
						<button
							type="button"
							onClick={onRetestAll}
							disabled={retestBlocked}
							title={
								readOnly
									? t("providers.discrepancies.readOnlyTooltip")
									: t("providers.discrepancies.retestAllTooltip")
							}
							className="ui-btn ui-btn-secondary ui-btn-compact inline-flex shrink-0 items-center gap-1.5 disabled:cursor-not-allowed disabled:opacity-50"
							data-testid="discrepancy-retest-all"
						>
							<RefreshCw
								size={13}
								className={isRetesting ? "animate-spin" : ""}
							/>
							{t("providers.discrepancies.retestAll")}
						</button>
					) : null}
				</div>
			}
		>
			<div className="space-y-5" data-testid="discrepancy-modal">
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
						{providers.map(renderProvider)}
						{renderGroupClaims()}
					</div>
				) : loadError ? null : (
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
						<div
							className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
								infoOpen ? "grid-rows-[1fr]" : "grid-rows-[0fr]"
							}`}
						>
							<div className="overflow-hidden">
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
			</div>
		</Modal>
	);
}
