import type { UseMutationResult } from "@tanstack/react-query";
import { useState } from "react";

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
 * through and only the empty "select a range" placeholder is guarded.
 */
export function PurgeLogsControl({
	labels,
	mutation,
}: {
	labels: PurgeLogsLabels;
	mutation: UseMutationResult<unknown, Error, string>;
}) {
	const [confirming, setConfirming] = useState(false);
	const [selection, setSelection] = useState("");

	if (!confirming) {
		return (
			<button
				type="button"
				onClick={() => setConfirming(true)}
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
				onChange={(e) => setSelection(e.target.value)}
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
					if (!olderThan) return;
					mutation.mutate(olderThan, {
						onSuccess: () => {
							setConfirming(false);
							setSelection("");
						},
						onError: () => setConfirming(false),
					});
				}}
				className="ui-btn ui-btn-danger"
			>
				{mutation.isPending
					? (labels.deleting ?? labels.confirm)
					: labels.confirm}
			</button>
			<button
				type="button"
				onClick={() => {
					setConfirming(false);
					setSelection("");
				}}
				className="ui-btn ui-btn-secondary"
			>
				{labels.cancel}
			</button>
		</>
	);
}
