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
 * card rather than the control. The card's mutation outlives the control
 * (SettingsSection remounts its children when managed mode flips), so the
 * state and the settle reset have to live where the mutation does or a purge
 * that settles after a remount would leave a reopened control actionable.
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
