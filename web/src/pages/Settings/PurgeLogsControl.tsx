import type { UseMutationResult } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { PurgeState } from "./purgeState";

/**
 * The two-step "delete older than" control shared by the request-log and
 * app-log purges: a danger button that expands into a range select plus
 * confirm and cancel. The dropdown values (1d/1w/1m/all) are exactly the
 * tokens the backend's purge endpoints accept, so the selection is passed
 * through. Both callers name the same nine suffixes under their own stem, so
 * the stem is what comes in rather than nine translated strings; `deleting` is
 * the one optional suffix and falls back to `confirm`. The confirm and range
 * state comes from the parent's usePurgeState, next to the mutation that
 * resets it on settle.
 */
export function PurgeLogsControl({
	i18nStem,
	mutation,
	state,
}: {
	i18nStem: string;
	mutation: UseMutationResult<unknown, Error, string>;
	/** Settling stays with the card that owns the mutation. */
	state: Omit<PurgeState, "settled">;
}) {
	const { t } = useTranslation();
	const { confirming, selection } = state;

	if (!confirming) {
		return (
			<button
				type="button"
				onClick={state.open}
				className="ui-btn ui-btn-danger"
				title={t(`${i18nStem}.tooltip`)}
			>
				{t(i18nStem)}
			</button>
		);
	}

	return (
		<>
			<select
				value={selection}
				onChange={(e) => state.select(e.target.value)}
				aria-label={t(`${i18nStem}.selectRange`)}
				className="ui-input px-3 py-1.5 text-xs"
			>
				<option value="">{t(`${i18nStem}.selectRange`)}</option>
				<option value="1d">{t(`${i18nStem}.olderThan1d`)}</option>
				<option value="1w">{t(`${i18nStem}.olderThan1w`)}</option>
				<option value="1m">{t(`${i18nStem}.olderThan1m`)}</option>
				<option value="all">{t(`${i18nStem}.allLogs`)}</option>
			</select>
			<button
				type="button"
				disabled={!selection || mutation.isPending}
				onClick={() => mutation.mutate(selection)}
				className="ui-btn ui-btn-danger"
			>
				{mutation.isPending
					? t(`${i18nStem}.deleting`, {
							defaultValue: t(`${i18nStem}.confirm`),
						})
					: t(`${i18nStem}.confirm`)}
			</button>
			<button
				type="button"
				onClick={state.cancel}
				className="ui-btn ui-btn-secondary"
			>
				{t(`${i18nStem}.cancel`)}
			</button>
		</>
	);
}
