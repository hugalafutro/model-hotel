import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { DiscoveryDiff } from "../../api/types";
import { Modal } from "../../components/Modal";
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
		!diff.failover_updated_groups?.length
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
			className={`inline-flex max-w-full items-center truncate rounded-md border border-(--border-default) bg-(--surface-elevated) px-1.5 py-0.5 text-[11px] text-(--text-secondary) ${
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
}: {
	results: DiscoverySummaryEntry[];
	onClose: () => void;
}) {
	const { t } = useTranslation();

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
					className="flex items-start gap-2 rounded-md border border-(--border-default) bg-(--surface-elevated) px-2.5 py-2 text-sm"
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
									className="rounded-md border border-(--border-default) bg-(--surface-elevated) px-2.5 py-2"
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
						<div className="space-y-1 rounded-md border border-(--border-default) bg-(--surface-elevated) px-2.5 py-2">
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
						<div className="space-y-1 rounded-md border border-(--border-default) bg-(--surface-elevated) px-2.5 py-2">
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
								{showHeaders && (
									<div className="flex items-center gap-2">
										<span className="text-sm font-semibold text-(--text-primary)">
											{r.providerName}
										</span>
										<span className="h-px flex-1 bg-(--border-default)" />
									</div>
								)}
								{renderDiffBody(r)}
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
