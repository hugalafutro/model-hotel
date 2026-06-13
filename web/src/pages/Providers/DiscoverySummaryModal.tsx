import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { DiscoveryDiff } from "../../api/types";
import { Modal } from "../../components/Modal";

export interface DiscoverySummaryEntry {
	providerName: string;
	diff?: DiscoveryDiff;
	error?: string;
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
		!diff.failover_deleted_groups?.length &&
		!diff.failover_updated_groups?.length
	);
}

function SummarySection<T>({
	label,
	items,
	badgeVariant,
	testId,
	itemKey,
	primary,
	secondary,
}: {
	label: string;
	items: T[] | undefined;
	badgeVariant: string;
	testId: string;
	itemKey: (item: T) => string;
	primary: (item: T) => ReactNode;
	secondary: (item: T) => ReactNode;
}) {
	if (!items?.length) return null;
	return (
		<div data-testid={testId}>
			<div className="flex items-center gap-2 mb-1">
				<span className={`ui-badge ${badgeVariant}`}>{items.length}</span>
				<span className="text-sm font-medium text-gray-300">{label}</span>
			</div>
			<ul className="space-y-0.5 pl-1">
				{items.map((item) => (
					<li
						key={itemKey(item)}
						className="flex items-baseline justify-between gap-3 text-sm"
					>
						<span
							className="text-(--text-primary) truncate"
							title={String(primary(item))}
						>
							{primary(item)}
						</span>
						<span className="text-(--text-tertiary) text-xs shrink-0">
							{secondary(item)}
						</span>
					</li>
				))}
			</ul>
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

	const modelSection = (
		label: string,
		items: DiscoveryDiff["added"],
		badgeVariant: string,
		testId: string,
	) => (
		<SummarySection
			label={label}
			items={items}
			badgeVariant={badgeVariant}
			testId={testId}
			itemKey={(c) => c.model_id}
			primary={(c) => c.model_id}
			secondary={(c) =>
				t(`providers.discoverySummary.reason.${c.reason}`, c.reason)
			}
		/>
	);

	return (
		<Modal
			title={t("providers.discoverySummary.title")}
			onClose={onClose}
			maxWidth="max-w-lg"
			scrollable
		>
			<div className="space-y-5" data-testid="discovery-summary">
				{results.map((r) => (
					<div key={r.providerName} className="space-y-2">
						{results.length > 1 && (
							<h3 className="text-sm font-semibold text-(--text-primary)">
								{r.providerName}
							</h3>
						)}
						{r.error ? (
							<p
								className="text-sm text-(--text-tertiary) break-words"
								data-testid="discovery-summary-error"
							>
								<span className="ui-badge ui-badge-error mr-2">
									{t("providers.discoverySummary.error")}
								</span>
								{r.error}
							</p>
						) : r.diff && !diffIsEmpty(r.diff) ? (
							<div className="space-y-3">
								{modelSection(
									t("providers.discoverySummary.added"),
									r.diff.added,
									"ui-badge-success",
									"discovery-summary-added",
								)}
								{modelSection(
									t("providers.discoverySummary.reenabled"),
									r.diff.reenabled,
									"ui-badge-info",
									"discovery-summary-reenabled",
								)}
								{modelSection(
									t("providers.discoverySummary.disabled"),
									r.diff.disabled,
									"ui-badge-warning",
									"discovery-summary-disabled",
								)}
								<SummarySection
									label={t("providers.discoverySummary.failoverDeleted")}
									items={r.diff.failover_deleted_groups}
									badgeVariant="ui-badge-error"
									testId="discovery-summary-failover-deleted"
									itemKey={(g) => g.display_model}
									primary={(g) => g.display_model}
									secondary={(g) =>
										FAILOVER_DELETE_REASON_KEYS[g.reason]
											? t(FAILOVER_DELETE_REASON_KEYS[g.reason])
											: g.reason
									}
								/>
								<SummarySection
									label={t("providers.discoverySummary.failoverUpdated")}
									items={r.diff.failover_updated_groups}
									badgeVariant="ui-badge-accent"
									testId="discovery-summary-failover-updated"
									itemKey={(g) => g.display_model}
									primary={(g) => g.display_model}
									secondary={(g) => (
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
									)}
								/>
							</div>
						) : (
							<p
								className="text-sm text-(--text-tertiary)"
								data-testid="discovery-summary-no-changes"
							>
								{t("providers.discoverySummary.noChanges")}
							</p>
						)}
					</div>
				))}
			</div>
		</Modal>
	);
}
