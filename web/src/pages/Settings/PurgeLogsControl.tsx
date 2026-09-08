import type { UseMutationResult } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { PurgeState } from "./purgeState";

// The token set the backend's purge endpoints accept, each mapped to the i18n
// suffix its <option> label lives under.
const PURGE_RANGES = {
	"1d": "olderThan1d",
	"1w": "olderThan1w",
	"1m": "olderThan1m",
	all: "allLogs",
} as const;

/**
 * The two-step "delete older than" control shared by the request-log and
 * app-log purges: a danger button that expands into a range select plus
 * confirm and cancel. PURGE_RANGES is the token set the backend's purge
 * endpoints accept: it fills the dropdown and gates what reaches the mutation.
 * Both callers name the same nine suffixes under their own stem, so
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
	// Only a token the endpoint accepts reaches the mutation.
	const olderThan = Object.hasOwn(PURGE_RANGES, selection) ? selection : "";

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
				{Object.entries(PURGE_RANGES).map(([range, suffix]) => (
					<option key={range} value={range}>
						{t(`${i18nStem}.${suffix}`)}
					</option>
				))}
			</select>
			<button
				type="button"
				disabled={!olderThan || mutation.isPending}
				onClick={() => mutation.mutate(olderThan)}
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
