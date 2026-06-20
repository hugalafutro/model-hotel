import { type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import type { DiscoveryDiff } from "../../api/types";
import { Modal } from "../../components/Modal";
import { ChevronDown, ChevronRight, RefreshCw } from "../../lib/icons";
import { formatTokens } from "../../utils/format";
import { formatPrice } from "../../utils/model";

// Pricing fields render as "$<price>"; the rest are token counts.
const PRICE_FIELDS = new Set([
	"input_price",
	"output_price",
	"input_price_cache",
]);

// formatFieldValue renders a metadata value for the Updated section, using the
// same formatters as the Models table; a null/undefined value reads as `unset`.
function formatFieldValue(
	field: string,
	value: number | null | undefined,
	unset: string,
): string {
	if (value == null) return unset;
	return PRICE_FIELDS.has(field)
		? `$${formatPrice(value)}`
		: formatTokens(value);
}

export interface DiscoverySummaryEntry {
	providerName: string;
	diff?: DiscoveryDiff;
	error?: string;
	/** Stable React key; needed when the same provider appears more than once
	 * (e.g. several background runs recorded before review). Falls back to
	 * providerName, which is unique for a single discovery response. */
	entryKey?: string;
	/** Provider ID, when known. Enables the per-provider "Retest" action that
	 * re-runs discovery to re-probe models disabled during the original run.
	 * Background entries that only carry a provider name leave this unset. */
	providerId?: string;
}

// The backend stores failover deletion reasons as pre-existing English
// strings; map the two known values to i18n keys and fall back to raw text.
const FAILOVER_DELETE_REASON_KEYS: Record<string, string> = {
	"no enabled providers found":
		"providers.discoverySummary.failoverReason.noProviders",
	"only 1 enabled provider (need 2+ for failover)":
		"providers.discoverySummary.failoverReason.onlyOne",
};

function diffIsEmpty(diff: DiscoveryDiff): boolean {
	return (
		!diff.added?.length &&
		!diff.reenabled?.length &&
		!diff.disabled?.length &&
		!diff.updated?.length &&
		!diff.failover_deleted_groups?.length &&
		!diff.failover_updated_groups?.length &&
		!diff.failover_disabled_groups?.length
	);
}

// An entry counts as "unchanged" when it has no error and an empty/missing diff.
function entryIsUnchanged(r: DiscoverySummaryEntry): boolean {
	return !r.error && (!r.diff || diffIsEmpty(r.diff));
}

function entryKeyOf(r: DiscoverySummaryEntry): string {
	return r.entryKey ?? r.providerName;
}

// A change category: a color-coded sign badge that encodes the direction of the
// change (+ added, − removed, ↺ back, ± edited), the human label, and the body.
function CategoryGroup({
	sign,
	count,
	badgeVariant,
	label,
	testId,
	children,
}: {
	sign: string;
	count: number;
	badgeVariant: string;
	label: string;
	testId: string;
	children: ReactNode;
}) {
	return (
		<section data-testid={testId} className="space-y-1.5">
			<div className="flex items-center gap-2">
				<span
					className={`ui-badge ${badgeVariant} font-mono tabular-nums`}
					aria-hidden
				>
					{sign}
					{count}
				</span>
				<span className="text-[11px] font-semibold uppercase tracking-wider text-(--text-tertiary)">
					{label}
				</span>
			</div>
			{children}
		</section>
	);
}

// Compact wrapping chip; mono is used for model identifiers (matching the
// Models table) and left off for human-readable provider names.
function Chip({ label, mono }: { label: string; mono?: boolean }) {
	return (
		<span
			className={`inline-flex max-w-full items-center truncate rounded-(--radius-box) border border-(--border-default) bg-(--surface-elevated) px-1.5 py-0.5 text-[11px] text-(--text-secondary) ${
				mono ? "font-mono" : ""
			}`}
			title={label}
		>
			{label}
		</span>
	);
}

// A single label → value·value row (failover detail / shared layout).
function DetailRow({
	primary,
	secondary,
}: {
	primary: string;
	secondary: ReactNode;
}) {
	return (
		<div className="flex items-baseline justify-between gap-3">
			<span
				className="truncate font-mono text-xs text-(--text-primary)"
				title={primary}
			>
				{primary}
			</span>
			<span className="shrink-0 text-right text-[11px] text-(--text-tertiary)">
				{secondary}
			</span>
		</div>
	);
}

export function DiscoverySummaryModal({
	results,
	onClose,
	onRetest,
	retestingKey,
}: {
	results: DiscoverySummaryEntry[];
	onClose: () => void;
	/** Re-run discovery for one provider (re-probes models disabled during the
	 * original run). Omit to hide the per-provider Retest action. */
	onRetest?: (entry: DiscoverySummaryEntry) => void;
	/** entryKey of the provider whose retest is currently in flight, if any. */
	retestingKey?: string;
}) {
	const { t } = useTranslation();

	// Provider sections start expanded; users collapse the ones they have already
	// reviewed. Keyed by entryKey so duplicate provider names stay independent.
	const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set());
	const toggleCollapsed = (key: string) =>
		setCollapsed((prev) => {
			const next = new Set(prev);
			if (next.has(key)) next.delete(key);
			else next.add(key);
			return next;
		});

	// A small Retest button for providers that had a model disabled this run.
	// Re-running discovery re-probes those models so a transient provider hiccup
	// can be cleared without disabling/re-enabling by hand.
	const renderRetestButton = (r: DiscoverySummaryEntry) => {
		if (!onRetest || !r.providerId || !r.diff?.disabled?.length) return null;
		const isRetesting = retestingKey === entryKeyOf(r);
		return (
			<button
				type="button"
				onClick={() => onRetest(r)}
				disabled={isRetesting}
				title={t("providers.discoverySummary.retestTooltip")}
				className="ui-btn ui-btn-secondary ui-btn-compact inline-flex shrink-0 items-center gap-1.5 disabled:cursor-not-allowed disabled:opacity-50"
				data-testid="discovery-summary-retest"
			>
				<RefreshCw size={13} className={isRetesting ? "animate-spin" : ""} />
				{isRetesting
					? t("providers.discoverySummary.retesting")
					: t("providers.discoverySummary.retest")}
			</button>
		);
	};

	// Membership categories share the chip-cloud body; only the sign/color/label
	// differ. The reason is implied by the category, so it is not repeated per row.
	const chipSection = (
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

	// One provider's error banner or its stack of change categories.
	const renderDiffBody = (r: DiscoverySummaryEntry) => {
		if (r.error) {
			return (
				<div
					className="flex items-start gap-2 rounded-(--radius-box) border border-(--border-default) bg-(--surface-elevated) px-2.5 py-2 text-sm"
					data-testid="discovery-summary-error"
				>
					<span className="ui-badge ui-badge-error shrink-0">
						{t("providers.discoverySummary.error")}
					</span>
					<span className="break-words text-(--text-secondary)">{r.error}</span>
				</div>
			);
		}
		const diff = r.diff as DiscoveryDiff;
		return (
			<div className="space-y-4">
				{chipSection(
					diff.added,
					"+",
					"ui-badge-success",
					t("providers.discoverySummary.added"),
					"discovery-summary-added",
				)}
				{chipSection(
					diff.reenabled,
					"↺",
					"ui-badge-info",
					t("providers.discoverySummary.reenabled"),
					"discovery-summary-reenabled",
				)}
				{chipSection(
					diff.disabled,
					"−",
					"ui-badge-warning",
					t("providers.discoverySummary.disabled"),
					"discovery-summary-disabled",
				)}
				{diff.updated?.length ? (
					<CategoryGroup
						sign="±"
						count={diff.updated.length}
						badgeVariant="ui-badge-accent"
						label={t("providers.discoverySummary.updated")}
						testId="discovery-summary-updated"
					>
						<div className="space-y-1.5">
							{diff.updated.map((u) => (
								<div
									key={u.model_id}
									className="rounded-(--radius-box) border border-(--border-default) bg-(--surface-elevated) px-2.5 py-2"
								>
									<div
										className="truncate font-mono text-xs text-(--text-primary)"
										title={u.model_id}
									>
										{u.model_id}
									</div>
									<dl className="mt-1.5 space-y-1">
										{u.changes.map((c) => (
											<div
												key={c.field}
												className="flex items-baseline justify-between gap-3"
											>
												<dt className="text-[11px] text-(--text-tertiary)">
													{t(
														`providers.discoverySummary.field.${c.field}`,
														c.field,
													)}
												</dt>
												<dd className="whitespace-nowrap font-mono text-[11px]">
													<span
														className="text-(--text-muted)"
														data-testid={`discovery-field-old-${c.field}`}
													>
														{formatFieldValue(
															c.field,
															c.old,
															t("providers.discoverySummary.unset"),
														)}
													</span>
													<span
														className="mx-1 text-(--text-muted)"
														aria-hidden
													>
														→
													</span>
													<span
														className="font-medium text-(--text-primary)"
														data-testid={`discovery-field-new-${c.field}`}
													>
														{formatFieldValue(
															c.field,
															c.new,
															t("providers.discoverySummary.unset"),
														)}
													</span>
												</dd>
											</div>
										))}
									</dl>
								</div>
							))}
						</div>
					</CategoryGroup>
				) : null}
				{diff.failover_deleted_groups?.length ? (
					<CategoryGroup
						sign="×"
						count={diff.failover_deleted_groups.length}
						badgeVariant="ui-badge-error"
						label={t("providers.discoverySummary.failoverDeleted")}
						testId="discovery-summary-failover-deleted"
					>
						<div className="space-y-1 rounded-(--radius-box) border border-(--border-default) bg-(--surface-elevated) px-2.5 py-2">
							{diff.failover_deleted_groups.map((g) => (
								<DetailRow
									key={g.display_model}
									primary={g.display_model}
									secondary={
										FAILOVER_DELETE_REASON_KEYS[g.reason]
											? t(FAILOVER_DELETE_REASON_KEYS[g.reason])
											: g.reason
									}
								/>
							))}
						</div>
					</CategoryGroup>
				) : null}
				{diff.failover_updated_groups?.length ? (
					<CategoryGroup
						sign="⇄"
						count={diff.failover_updated_groups.length}
						badgeVariant="ui-badge-accent"
						label={t("providers.discoverySummary.failoverUpdated")}
						testId="discovery-summary-failover-updated"
					>
						<div className="space-y-1 rounded-(--radius-box) border border-(--border-default) bg-(--surface-elevated) px-2.5 py-2">
							{diff.failover_updated_groups.map((g) => (
								<DetailRow
									key={g.display_model}
									primary={g.display_model}
									secondary={
										<>
											{g.removed_model_ids?.length
												? t("providers.discoverySummary.removedCount", {
														count: g.removed_model_ids.length,
													})
												: null}
											{g.removed_model_ids?.length && g.added_model_ids?.length
												? " · "
												: null}
											{g.added_model_ids?.length
												? t("providers.discoverySummary.addedCount", {
														count: g.added_model_ids.length,
													})
												: null}
										</>
									}
								/>
							))}
						</div>
					</CategoryGroup>
				) : null}
				{diff.failover_disabled_groups?.length ? (
					<CategoryGroup
						sign="⊘"
						count={diff.failover_disabled_groups.length}
						badgeVariant="ui-badge-orange"
						label={t("providers.discoverySummary.failoverDisabled")}
						testId="discovery-summary-failover-disabled"
					>
						<div className="space-y-1 rounded-(--radius-box) border border-(--border-default) bg-(--surface-elevated) px-2.5 py-2">
							{diff.failover_disabled_groups.map((g) => (
								<DetailRow
									key={g.display_model}
									primary={g.display_model}
									secondary={t(
										"providers.discoverySummary.failoverDisabledReason",
										{ count: g.effective_count },
									)}
								/>
							))}
						</div>
					</CategoryGroup>
				) : null}
			</div>
		);
	};

	const showHeaders = results.length > 1;
	const visible = results.filter((r) => !entryIsUnchanged(r));
	const unchanged = results.filter(entryIsUnchanged);
	// A lone unchanged provider keeps the full reassuring sentence; in a batch,
	// the unchanged providers collapse into one line so they don't drown the
	// providers that actually changed.
	const singleUnchanged = results.length === 1 && unchanged.length === 1;

	return (
		<Modal
			title={t("providers.discoverySummary.title")}
			onClose={onClose}
			maxWidth="max-w-lg"
			scrollable
		>
			<div className="space-y-5" data-testid="discovery-summary">
				{singleUnchanged ? (
					<p
						className="text-sm text-(--text-tertiary)"
						data-testid="discovery-summary-no-changes"
					>
						{t("providers.discoverySummary.noChanges")}
					</p>
				) : (
					<>
						{visible.map((r) => (
							<div key={entryKeyOf(r)} className="space-y-3">
								{showHeaders ? (
									<div className="flex items-center gap-2">
										<button
											type="button"
											onClick={() => toggleCollapsed(entryKeyOf(r))}
											aria-expanded={!collapsed.has(entryKeyOf(r))}
											aria-label={t(
												"providers.discoverySummary.toggleSection",
												{ provider: r.providerName },
											)}
											className="flex min-w-0 flex-1 items-center gap-2 text-sm font-semibold text-(--text-primary)"
											data-testid="discovery-summary-toggle"
										>
											<span className="truncate">{r.providerName}</span>
											<span className="h-px flex-1 bg-(--border-default)" />
											{collapsed.has(entryKeyOf(r)) ? (
												<ChevronRight size={14} className="shrink-0" />
											) : (
												<ChevronDown size={14} className="shrink-0" />
											)}
										</button>
										{renderRetestButton(r)}
									</div>
								) : null}
								{showHeaders ? (
									<div
										className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
											collapsed.has(entryKeyOf(r))
												? "grid-rows-[0fr]"
												: "grid-rows-[1fr]"
										}`}
									>
										<div className="overflow-hidden">{renderDiffBody(r)}</div>
									</div>
								) : (
									renderDiffBody(r)
								)}
								{!showHeaders &&
									(() => {
										// Build the button once; the truthiness check and the
										// rendered node must not be two separate calls.
										const retest = renderRetestButton(r);
										return retest ? (
											<div className="flex justify-end">{retest}</div>
										) : null;
									})()}
							</div>
						))}
						{unchanged.length > 0 && (
							<section
								className="space-y-1.5"
								data-testid="discovery-summary-unchanged"
							>
								<div className="flex items-center gap-2">
									<span className="ui-badge ui-badge-neutral tabular-nums">
										{unchanged.length}
									</span>
									<span className="text-[11px] font-semibold uppercase tracking-wider text-(--text-tertiary)">
										{t("providers.discoverySummary.unchanged")}
									</span>
								</div>
								<div className="flex flex-wrap gap-1.5">
									{unchanged.map((r) => (
										<Chip key={entryKeyOf(r)} label={r.providerName} />
									))}
								</div>
							</section>
						)}
					</>
				)}
			</div>
		</Modal>
	);
}
