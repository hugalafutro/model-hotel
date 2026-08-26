import { useTranslation } from "react-i18next";
import type { MergedProvider } from "../../hooks/useDiscrepancies";
import { ALL_GROUPS } from "./groups";

/**
 * Headline over a cleared provider's buckets, reporting the TWO causes
 * separately and both at once for a provider that had some of each.
 *
 * Dismissed rows get a count, not one line each: sixty lines saying "model X
 * dismissed" is the wall of text this redesign removes, and the rows are one
 * click away in their bucket. Resolved rows keep their per-model line, because
 * "is listed again" says something the count cannot.
 */
export function ClearedSummary({ provider }: { provider: MergedProvider }) {
	const { t } = useTranslation();
	const all = ALL_GROUPS.flatMap((g) => provider[g] ?? []);
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
}
