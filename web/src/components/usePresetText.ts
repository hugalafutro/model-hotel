import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { pickRandom } from "../utils/random";

/** The sentinel preset PresetBar's "custom" button stands for. */
export const CUSTOM_PRESET_ID = "__custom__";

interface Preset {
	id: string;
	icon: string;
	label: string;
}

/**
 * The preset-or-hand-written-text flow behind every preset bar: picking a
 * preset over text the user typed, or switching back to custom while a preset
 * is active, asks first. `onChange` is called with `id === null` for custom.
 *
 * `textOf` reads the text a preset carries, which is a translation key: the
 * value handed to `onChange` is already translated.
 *
 * The confirm dialog itself stays with the caller, which owns its copy.
 */
export function usePresetText<T extends Preset>({
	activeId,
	text,
	textOf,
	onChange,
}: {
	activeId: string | null;
	text: string;
	textOf: (preset: T) => string;
	onChange: (id: string | null, text: string) => void;
}) {
	const { t } = useTranslation();
	const [pending, setPending] = useState<T | null>(null);

	const select = useCallback(
		(preset: T) => {
			// Hand-written text would be lost, so confirm before replacing it.
			if (text.trim() && activeId === null) {
				setPending(preset);
				return;
			}
			onChange(preset.id, t(textOf(preset)));
		},
		[text, activeId, onChange, textOf, t],
	);

	const custom = useCallback(() => {
		if (activeId === null) return;
		setPending({
			id: CUSTOM_PRESET_ID,
			icon: "✏️",
			label: t("common.custom"),
		} as T);
	}, [activeId, t]);

	const random = useCallback(
		(presets: readonly T[]) => {
			const pick = pickRandom(presets.filter((p) => p.id !== activeId));
			if (pick) select(pick);
		},
		[activeId, select],
	);

	const confirm = useCallback(() => {
		if (!pending) return;
		if (pending.id === CUSTOM_PRESET_ID) onChange(null, "");
		else onChange(pending.id, t(textOf(pending)));
		setPending(null);
	}, [pending, onChange, textOf, t]);

	const cancel = useCallback(() => setPending(null), []);

	return {
		pending,
		isCustomPending: pending?.id === CUSTOM_PRESET_ID,
		select,
		custom,
		random,
		confirm,
		cancel,
	};
}
