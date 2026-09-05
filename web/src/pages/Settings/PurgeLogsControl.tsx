import type { UseMutationResult } from "@tanstack/react-query";
import type { PurgeState } from "./purgeState";

export interface PurgeLogsLabels {
	button: string;
	tooltip: string;
	selectRange: string;
	olderThan1d: string;
	olderThan1w: string;
	olderThan1m: string;
	allLogs: string;
	confirm: string;
	cancel: string;
	/** Shown on the confirm button while the purge runs; falls back to `confirm`. */
	deleting?: string;
}

/**
 * The two-step "delete older than" control shared by the request-log and
 * app-log purges: a danger button that expands into a range select plus
 * confirm and cancel. The dropdown values (1d/1w/1m/all) are exactly the
 * tokens the backend's purge endpoints accept, so the selection is passed
 * through and only the empty "select a range" placeholder is guarded. The
 * confirm and range state comes from the parent's usePurgeState so it survives
 * the section remount that a managed-mode flip causes.
 */
export function PurgeLogsControl({
	labels,
	mutation,
	state,
}: {
	labels: PurgeLogsLabels;
	mutation: UseMutationResult<unknown, Error, string>;
	state: PurgeState;
}) {
	const { confirming, selection } = state;

	if (!confirming) {
		return (
			<button
				type="button"
				onClick={state.open}
				className="ui-btn ui-btn-danger"
				title={labels.tooltip}
			>
				{labels.button}
			</button>
		);
	}

	const olderThan = ["1d", "1w", "1m", "all"].includes(selection)
		? selection
		: "";

	return (
		<>
			<select
				value={selection}
				onChange={(e) => state.select(e.target.value)}
				className="ui-input px-3 py-1.5 text-xs"
			>
				<option value="">{labels.selectRange}</option>
				<option value="1d">{labels.olderThan1d}</option>
				<option value="1w">{labels.olderThan1w}</option>
				<option value="1m">{labels.olderThan1m}</option>
				<option value="all">{labels.allLogs}</option>
			</select>
			<button
				type="button"
				disabled={!selection || mutation.isPending}
				onClick={() => {
					if (olderThan) mutation.mutate(olderThan);
				}}
				className="ui-btn ui-btn-danger"
			>
				{mutation.isPending
					? (labels.deleting ?? labels.confirm)
					: labels.confirm}
			</button>
			<button
				type="button"
				onClick={state.cancel}
				className="ui-btn ui-btn-secondary"
			>
				{labels.cancel}
			</button>
		</>
	);
}
