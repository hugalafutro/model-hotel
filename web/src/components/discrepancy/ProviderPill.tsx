import { useTranslation } from "react-i18next";
import { Broom, ChevronDown, ChevronRight, RefreshCw } from "../../lib/icons";

export interface ProviderPillProps {
	providerName: string;
	expanded: boolean;
	onToggle: () => void;
	/** Actionable-row counts per bucket; a zero renders no chip. */
	counts: { gone: number; stale: number; suspect: number };
	/** Cleared-row counts, rendered INSTEAD of `counts` when nothing is actionable. */
	cleared: { dismissed: number; resolved: number };
	/** True when no row is actionable: Clean replaces Retest all + Dismiss all. */
	isCleared: boolean;
	/** False when every actionable row is suspect, so nothing can be dismissed. */
	canDismiss: boolean;
	retestDisabled: boolean;
	retesting: boolean;
	onRetest: () => void;
	onDismissAll: () => void;
	onClean: () => void;
	describedByReadOnly?: string;
	readOnly: boolean;
	/** Id of the collapsible body this pill controls, for aria-controls. */
	regionId: string;
}

type PillChip = {
	key: string;
	variant: string;
	sign: string;
	count: number;
	label: string;
	/** What the bucket actually means. "Gone" alone does not say gone where. */
	tooltip: string;
};

/**
 * Level 1 of the claims accordion: one collapsed row per provider.
 *
 * The action buttons are SIBLINGS of the toggle rather than children of it. A
 * button inside a button is invalid HTML and browsers recover from it
 * unpredictably, so the toggle owns only the name, chips and chevron.
 */
export function ProviderPill({
	providerName,
	expanded,
	onToggle,
	counts,
	cleared,
	isCleared,
	canDismiss,
	retestDisabled,
	retesting,
	onRetest,
	onDismissAll,
	onClean,
	describedByReadOnly,
	readOnly,
	regionId,
}: ProviderPillProps) {
	const { t } = useTranslation();

	// Chips report ACTIONABLE rows. Once nothing is actionable the buckets still
	// hold every struck-through row, so a chip reading "× 60 Gone" over them would
	// advertise 60 live problems that no longer exist.
	const chips: PillChip[] = isCleared
		? [
				cleared.dismissed > 0 && {
					key: "dismissed",
					variant: "ui-badge-neutral",
					sign: "",
					count: cleared.dismissed,
					label: t("providers.discrepancies.chipDismissed"),
					tooltip: t("providers.discrepancies.chipDismissedTooltip"),
				},
				cleared.resolved > 0 && {
					key: "resolved",
					variant: "ui-badge-success",
					sign: "✓",
					count: cleared.resolved,
					label: t("providers.discrepancies.chipResolved"),
					tooltip: t("providers.discrepancies.chipResolvedTooltip"),
				},
			].filter((c): c is PillChip => c !== false)
		: [
				counts.gone > 0 && {
					key: "gone",
					variant: "ui-badge-error",
					sign: "×",
					count: counts.gone,
					label: t("providers.discrepancies.group.gone"),
					tooltip: t("providers.discrepancies.chipGoneTooltip"),
				},
				counts.suspect > 0 && {
					key: "suspect",
					variant: "ui-badge-warning",
					sign: "?",
					count: counts.suspect,
					label: t("providers.discrepancies.group.suspect"),
					tooltip: t("providers.discrepancies.chipSuspectTooltip"),
				},
				counts.stale > 0 && {
					key: "stale",
					variant: "ui-badge-neutral",
					sign: "",
					count: counts.stale,
					label: t("providers.discrepancies.group.stale"),
					tooltip: t("providers.discrepancies.chipStaleTooltip"),
				},
			].filter((c): c is PillChip => c !== false);

	return (
		// A hairline outline is what makes these read as one pill per provider in a
		// stack of eight. The open one takes the theme accent instead, so which
		// provider you are inside is answerable without scrolling up to find the
		// chevron.
		//
		// --border-input, not --border-default: the latter is 0.06 alpha, tuned for
		// large card edges, and at this size it is invisible. The pill is an
		// interactive toggle, so the token meant for interactive edges is both more
		// visible and the semantically right one, in every theme.
		<div
			className={`flex items-center gap-2 rounded-(--radius-box) border px-2 py-1 ${
				expanded ? "border-(--accent)" : "border-(--border-input)"
			}`}
		>
			<button
				type="button"
				onClick={onToggle}
				aria-expanded={expanded}
				aria-controls={regionId}
				className="flex min-w-0 flex-1 items-center gap-2 text-left"
				data-testid="discrepancy-provider-pill"
			>
				{expanded ? (
					<ChevronDown size={14} className="shrink-0" />
				) : (
					<ChevronRight size={14} className="shrink-0" />
				)}
				<span className="truncate text-sm font-semibold text-(--accent)">
					{providerName}
				</span>
				{chips.map((c) => (
					<span
						key={c.key}
						className={`ui-badge ${c.variant} shrink-0 tabular-nums`}
						data-testid={`discrepancy-chip-${c.key}`}
						title={c.tooltip}
					>
						{c.sign ? `${c.sign} ` : ""}
						{c.count} {c.label}
					</span>
				))}
				<span className="h-px flex-1 bg-white/30" />
			</button>
			{isCleared ? (
				// No confirmation: there is nothing left to write. Every row is either
				// dismissed (already persisted) or resolved (the model is healthy), so
				// this only stops rendering a provider the operator has finished with.
				<button
					type="button"
					onClick={onClean}
					title={t("providers.discrepancies.cleanTooltip")}
					aria-label={t("providers.discrepancies.clean")}
					className="ui-btn ui-btn-ghost ui-btn-compact inline-flex shrink-0 items-center"
					data-testid="discrepancy-clean"
				>
					<Broom size={14} />
				</button>
			) : (
				<>
					<button
						type="button"
						onClick={onRetest}
						disabled={retestDisabled}
						title={
							readOnly
								? t("providers.discrepancies.readOnlyTooltip")
								: t("providers.discrepancies.retestTooltip")
						}
						aria-describedby={describedByReadOnly}
						className="ui-btn ui-btn-secondary ui-btn-compact inline-flex shrink-0 items-center gap-1.5 disabled:cursor-not-allowed disabled:opacity-50"
						data-testid="discrepancy-retest"
					>
						<RefreshCw size={13} className={retesting ? "animate-spin" : ""} />
						{retesting
							? t("providers.discrepancies.retesting")
							: t("providers.discrepancies.retest")}
					</button>
					{/* Disabled rather than hidden when only suspect rows are actionable:
					    the server refuses a still-enabled model, and a control that
					    disappears reads as a missing feature. The tooltip says why. */}
					<button
						type="button"
						onClick={onDismissAll}
						disabled={readOnly || !canDismiss}
						title={
							readOnly
								? t("providers.discrepancies.readOnlyTooltip")
								: canDismiss
									? t("providers.discrepancies.dismissAllTooltip")
									: t("providers.discrepancies.dismissAllSuspectOnlyTooltip")
						}
						aria-describedby={describedByReadOnly}
						className="ui-btn ui-btn-ghost ui-btn-compact shrink-0 disabled:cursor-not-allowed disabled:opacity-50"
						data-testid="discrepancy-dismiss-all"
					>
						{t("providers.discrepancies.dismissAll")}
					</button>
				</>
			)}
		</div>
	);
}
