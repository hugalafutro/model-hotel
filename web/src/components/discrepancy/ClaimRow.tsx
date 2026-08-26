import { useTranslation } from "react-i18next";
import type { MergedClaim, MergedProvider } from "../../hooks/useDiscrepancies";
import { formatRelativeTime } from "../../utils/format";
import type { Group } from "./groups";

/**
 * The gating and the two per-row actions every claim row shares. Computed once
 * by the modal (the read-only and managed reasons are modal-wide) and threaded
 * down unchanged, so a row can never disagree with its neighbours about whether
 * a control is blocked.
 */
export interface ClaimRowActions {
	readOnly: boolean;
	unpinBlocked: boolean;
	unpinTitle: string;
	unpinNoteId?: string;
	describedByReadOnly?: string;
	onUnpin: (providerId: string, modelId: string) => void;
	onDismiss: (providerId: string, modelId: string) => void;
}

/**
 * One model, as a TIGHT single line rather than a bordered card.
 *
 * The card treatment was the whole cost of this list: a bucket with 52 rows
 * meant 52 rounded, bordered, separately-filled boxes, each two lines tall
 * because the meta sat under the id. Dropping to one line with a hairline
 * divider between rows (the container owns those, see BucketSection) roughly
 * halves the height and removes the per-row paint work that made unrolling
 * stutter. Same idiom as Bellhop's event list.
 */
export function ClaimRow({
	provider,
	claim,
	group,
	actions,
}: {
	provider: MergedProvider;
	claim: MergedClaim;
	group: Group;
	actions: ClaimRowActions;
}) {
	const { t } = useTranslation();
	const c = claim;
	const {
		readOnly,
		unpinBlocked,
		unpinTitle,
		unpinNoteId,
		describedByReadOnly,
		onUnpin,
		onDismiss,
	} = actions;

	const flapChip = () => {
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
	const dismissTitle = () => {
		if (readOnly) return t("providers.discrepancies.readOnlyTooltip");
		if (group === "retired") {
			return t("providers.discrepancies.dismissRetiredTooltip");
		}
		return t("providers.discrepancies.dismissTooltip");
	};

	const claimMeta = () => {
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

	// `status`, never `state`: this is styling only.
	const isCleared = c.status === "resolved" || c.status === "dismissed";
	return (
		<div
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
				{claimMeta()}
			</span>
			{flapChip()}
			{group === "pinned" && !isCleared ? (
				<button
					type="button"
					onClick={() => onUnpin(provider.provider_id, c.model_id)}
					disabled={unpinBlocked}
					title={unpinTitle}
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
					onClick={() => onDismiss(provider.provider_id, c.model_id)}
					disabled={readOnly}
					title={dismissTitle()}
					aria-describedby={describedByReadOnly}
					className="ui-btn ui-btn-ghost ui-btn-compact shrink-0"
					data-testid="discrepancy-dismiss"
				>
					{t("providers.discrepancies.dismiss")}
				</button>
			) : null}
		</div>
	);
}
