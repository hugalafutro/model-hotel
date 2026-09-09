import { useCallback } from "react";
import type { ModalNavProps } from "../components/ModalNav";

/**
 * Wires a list and its selected row to {@link ModalNav}. Returns undefined
 * when there is nowhere to step: nothing is open, the list holds one row, or
 * the open row is no longer in the list (a live update dropped it). That
 * hides the stepper rather than offering a dead control or stepping blind.
 *
 * getKey decides which row is open, so a key two rows can share (app-log rows
 * arrive without an id) binds the stepper to whichever comes first.
 */
export function useModalNav<T>(
	items: T[],
	selected: T | null,
	onSelect: (item: T) => void,
	getKey: (item: T) => string,
): ModalNavProps | undefined {
	const selectedKey = selected ? getKey(selected) : null;
	const index =
		selectedKey === null
			? -1
			: items.findIndex((item) => getKey(item) === selectedKey);

	const onPrev = useCallback(() => {
		if (index > 0) onSelect(items[index - 1]);
	}, [index, items, onSelect]);
	const onNext = useCallback(() => {
		if (index >= 0 && index < items.length - 1) onSelect(items[index + 1]);
	}, [index, items, onSelect]);

	if (index < 0 || items.length < 2) return undefined;
	return { index, total: items.length, onPrev, onNext };
}
