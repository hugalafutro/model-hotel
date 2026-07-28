// Presentational primitives shared by the two discovery modals:
// DiscoverySummaryModal (a single run's diff, Providers page) and
// ModelDiscrepancyModal (the standing ledger behind the Models badge). They live
// here so the two cannot drift into showing the same thing two different ways.
// Components only, by Fast Refresh's rule; the shared value formatter lives
// next door in discoveryFormat.ts.
import type { ReactNode } from "react";

// A change category: a color-coded sign badge that encodes the direction of the
// change (+ added, − removed, ↺ back, ± edited), the human label, and the body.
export function CategoryGroup({
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
export function Chip({ label, mono }: { label: string; mono?: boolean }) {
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
// `stacked` puts the model name on its own (wrapping) line above the reason,
// so a long reason can't squeeze the name down to a few visible characters.
export function DetailRow({
	primary,
	secondary,
	stacked,
}: {
	primary: string;
	secondary: ReactNode;
	stacked?: boolean;
}) {
	if (stacked) {
		return (
			<div className="space-y-0.5">
				<div
					className="break-words font-mono text-xs text-(--text-primary)"
					title={primary}
				>
					{primary}
				</div>
				<div className="text-[11px] text-(--text-tertiary)">{secondary}</div>
			</div>
		);
	}
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
