import { useState } from "react";

export interface PurgeState {
	confirming: boolean;
	selection: string;
	open: () => void;
	select: (value: string) => void;
	cancel: () => void;
	/** Mutation-level settle: closes the confirmation, clears the range on success only. */
	settled: (success: boolean) => void;
}

/**
 * Confirm and range state for one PurgeLogsControl, owned by the settings
 * card rather than the control. The card owns the purge mutation, so the
 * state it resets on settle lives beside it rather than in the control the
 * mutation callbacks would otherwise have to reach into.
 */
export function usePurgeState(): PurgeState {
	const [confirming, setConfirming] = useState(false);
	const [selection, setSelection] = useState("");
	return {
		confirming,
		selection,
		open: () => setConfirming(true),
		select: setSelection,
		cancel: () => {
			setConfirming(false);
			setSelection("");
		},
		settled: (success) => {
			setConfirming(false);
			if (success) setSelection("");
		},
	};
}
