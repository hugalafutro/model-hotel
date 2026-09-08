import { useTranslation } from "react-i18next";
import type { ArenaPromptPreset } from "../data/presets";
import { PresetTextPicker } from "./PresetTextPicker";

interface PromptPickerProps {
	/** Available prompt presets to show in the bar */
	prompts: ArenaPromptPreset[];
	/** Currently active prompt preset id, or null for custom */
	activePromptId: string | null;
	/** Current prompt text */
	prompt: string;
	/** Called when the active prompt preset id changes */
	onActivePromptIdChange: (id: string | null) => void;
	/** Called when the prompt text changes */
	onPromptChange: (prompt: string) => void;
	/** Label shown above the component (defaults to "Prompt") */
	label?: string;
	/** Placeholder for the textarea */
	textareaPlaceholder?: string;
	/** Additional class names for the root element */
	className?: string;
	/** Whether the textarea is disabled */
	disabled?: boolean;
	/** Whether to show the preset bar (Arena hides it outside setup phase) */
	showPresetBar?: boolean;
	/** Whether the textarea should auto-focus */
	autoFocus?: boolean;
	/** Max length for the textarea (defaults to 10000) */
	maxLength?: number;
}

/** The prompt half of PresetTextPicker: presets carrying a `prompt`. */
export function PromptPicker({
	prompts,
	activePromptId,
	prompt,
	onActivePromptIdChange,
	onPromptChange,
	label,
	textareaPlaceholder,
	className,
	disabled = false,
	showPresetBar = true,
	autoFocus = false,
	maxLength = 10000,
}: PromptPickerProps) {
	const { t } = useTranslation();
	return (
		<PresetTextPicker
			presets={prompts}
			activeId={activePromptId}
			text={prompt}
			textOf={(p) => p.prompt}
			onActiveChange={onActivePromptIdChange}
			onTextChange={onPromptChange}
			confirmCopy={(isCustom) => ({
				title: isCustom
					? t("components.promptPicker.switchToCustom")
					: t("components.promptPicker.overwritePrompt"),
				fields: [t("components.promptPicker.promptField")],
			})}
			label={label ?? t("components.promptPicker.prompt")}
			textareaId="prompt-picker-textarea"
			textareaPlaceholder={
				textareaPlaceholder ??
				t("components.promptPicker.enterPromptPlaceholder")
			}
			maxLength={maxLength}
			className={className}
			disabled={disabled}
			showPresetBar={showPresetBar}
			autoFocus={autoFocus}
			randomFromPresets
		/>
	);
}
