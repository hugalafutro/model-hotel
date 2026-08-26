import { useTranslation } from "react-i18next";
import type { DiscoveryChangeEntry, DiscoveryDiff } from "../../api/types";
import { ChevronDown, ChevronRight } from "../../lib/icons";
import { formatFieldValue } from "../../pages/Providers/discoveryFormat";
import {
	CategoryGroup,
	Chip,
	DetailRow,
} from "../../pages/Providers/discoveryPrimitives";
import { formatRelativeTime } from "../../utils/format";

/**
 * Zone 2 of the discrepancy modal: the informational journal. Never holds the
 * badge open. Whether it is expanded is the modal's decision (it is seeded off
 * whether the claims zone has content, and expanding marks the journal seen),
 * so both the state and the toggle come in as props.
 */
export function InformationalJournal({
	informational,
	open,
	onToggle,
	regionId,
}: {
	informational: DiscoveryChangeEntry[];
	open: boolean;
	onToggle: () => void;
	regionId: string;
}) {
	const { t } = useTranslation();
	if (informational.length === 0) return null;

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

	const renderEntry = (entry: DiscoveryChangeEntry, key: string) => {
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

	return (
		<section data-testid="discrepancy-informational" className="space-y-2">
			<button
				type="button"
				onClick={onToggle}
				aria-expanded={open}
				aria-controls={regionId}
				className="flex w-full items-center gap-2 text-left"
				data-testid="discrepancy-informational-toggle"
			>
				{open ? (
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
			{!open ? (
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
								newest: formatRelativeTime(informational[0].detected_at),
								oldest: formatRelativeTime(
									informational[informational.length - 1].detected_at,
								),
							})}
				</p>
			) : null}
			<div
				id={regionId}
				className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
					open ? "grid-rows-[1fr]" : "grid-rows-[0fr]"
				}`}
			>
				{/* Same reason as the stale zone: collapsed here is a visual
				    state only, so the journal must be made inert or it is read
				    out in full under an aria-expanded="false" toggle. */}
				<div className="overflow-hidden" inert={!open}>
					<div className="space-y-2">
						{informational.map((entry, i) =>
							renderEntry(
								entry,
								`${entry.provider_name}-${entry.detected_at}-${i}`,
							),
						)}
					</div>
				</div>
			</div>
		</section>
	);
}
