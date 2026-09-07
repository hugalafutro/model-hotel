import { useTranslation } from "react-i18next";
import type { PersonaPreset } from "../data/presets";
import { PresetTextPicker } from "./PresetTextPicker";

interface PersonaPickerProps {
	/** Available persona presets to show in the bar */
	personas: PersonaPreset[];
	/** Currently active persona id, or null for custom */
	activePersonaId: string | null;
	/** Current system prompt text */
	systemPrompt: string;
	/** Called when the active persona id changes */
	onActivePersonaChange: (id: string | null) => void;
	/** Called when the system prompt text changes */
	onSystemPromptChange: (prompt: string) => void;
	/** Label shown above the component (defaults to "Persona") */
	label?: string;
	/** Placeholder for the textarea */
	textareaPlaceholder?: string;
	/** Additional class names for the root element */
	className?: string;
	/** Whether the textarea is disabled */
	disabled?: boolean;
	/** Called when the random button is clicked */
	onRandom?: () => void;
}

/** The persona half of PresetTextPicker: presets carrying a `systemPrompt`. */
export function PersonaPicker({
	personas,
	activePersonaId,
	systemPrompt,
	onActivePersonaChange,
	onSystemPromptChange,
	label,
	textareaPlaceholder,
	className,
	disabled = false,
	onRandom,
}: PersonaPickerProps) {
	const { t } = useTranslation();
	return (
		<PresetTextPicker
			presets={personas}
			activeId={activePersonaId}
			text={systemPrompt}
			textOf={(p) => p.systemPrompt}
			onActiveChange={onActivePersonaChange}
			onTextChange={onSystemPromptChange}
			confirmCopy={(isCustom) => ({
				title: isCustom
					? t("components.personaPicker.switchToCustom")
					: t("components.personaPicker.overwritePrompt"),
				message: isCustom
					? t("components.personaPicker.clearPersonaPrompt")
					: undefined,
				fields: isCustom ? [] : [t("components.personaPicker.systemPrompt")],
			})}
			label={label ?? t("components.personaPicker.persona")}
			textareaId="persona-picker-textarea"
			textareaPlaceholder={
				textareaPlaceholder ??
				t("components.personaPicker.customPersonaPlaceholder")
			}
			maxLength={5000}
			className={className}
			disabled={disabled}
			textareaStyle={{ height: "auto" }}
			onRandom={onRandom}
		/>
	);
}
