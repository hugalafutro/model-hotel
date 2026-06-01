import { type RefObject, useCallback, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ArenaPromptPreset } from "../data/presets";
import { CollapsibleToggle, useCollapsible } from "./CollapsibleToggle";
import { ConfirmDialog } from "./ConfirmDialog";
import { PresetBar } from "./PresetBar";

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

export function PromptPicker({
	prompts,
	activePromptId,
	prompt,
	onActivePromptIdChange,
	onPromptChange,
	label: labelProp,
	textareaPlaceholder: textareaPlaceholderProp,
	className,
	disabled = false,
	showPresetBar = true,
	autoFocus = false,
	maxLength = 10000,
}: PromptPickerProps) {
	const { t } = useTranslation();
	const { collapsed, toggle: toggleCollapsed } = useCollapsible();
	const label = labelProp ?? t("components.promptPicker.prompt");
	const textareaPlaceholder =
		textareaPlaceholderProp ??
		t("components.promptPicker.enterPromptPlaceholder");
	const [pendingPrompt, setPendingPrompt] = useState<ArenaPromptPreset | null>(
		null,
	);
	const textareaRef = useRef<HTMLTextAreaElement>(null);

	const autoExpand = useCallback(
		(ref: RefObject<HTMLTextAreaElement | null>) => {
			const el = ref.current;
			if (!el) return;
			el.style.height = "auto";
			requestAnimationFrame(() => {
				el.style.height = `${el.scrollHeight}px`;
			});
		},
		[],
	);

	const handleSelect = useCallback(
		(preset: ArenaPromptPreset) => {
			if (prompt.trim() && activePromptId === null) {
				// User has custom text - confirm before overwriting
				setPendingPrompt(preset);
				return;
			}
			onPromptChange(t(preset.prompt));
			onActivePromptIdChange(preset.id);
			autoExpand(textareaRef);
		},
		[
			prompt,
			activePromptId,
			onPromptChange,
			onActivePromptIdChange,
			autoExpand,
		],
	);

	const handleCustom = useCallback(() => {
		if (activePromptId !== null) {
			// A preset is active - warn that switching to custom will clear
			setPendingPrompt({
				id: "__custom__",
				icon: "✏️",
				label: t("common.custom"),
				prompt: "",
			});
			return;
		}
	}, [activePromptId, t]);

	const handleRandom = useCallback(() => {
		const available = prompts.filter((p) => p.id !== activePromptId);
		if (available.length === 0) return;
		const pick = available[Math.floor(Math.random() * available.length)];
		if (prompt.trim() && activePromptId === null) {
			setPendingPrompt(pick);
			return;
		}
		onPromptChange(t(pick.prompt));
		onActivePromptIdChange(pick.id);
		autoExpand(textareaRef);
	}, [
		prompts,
		activePromptId,
		prompt,
		onPromptChange,
		onActivePromptIdChange,
		autoExpand,
	]);

	const handleTextareaChange = useCallback(
		(value: string) => {
			onPromptChange(value);
			// If user edits away from a preset, switch to custom
			const current = prompts.find((p) => p.id === activePromptId);
			if (current && value !== t(current.prompt)) {
				onActivePromptIdChange(null);
			}
		},
		[prompts, activePromptId, onPromptChange, onActivePromptIdChange],
	);

	const handleConfirmOverwrite = useCallback(() => {
		if (!pendingPrompt) return;
		if (pendingPrompt.id === "__custom__") {
			onPromptChange("");
			onActivePromptIdChange(null);
		} else {
			onPromptChange(t(pendingPrompt.prompt));
			onActivePromptIdChange(pendingPrompt.id);
			autoExpand(textareaRef);
		}
		setPendingPrompt(null);
	}, [pendingPrompt, onPromptChange, onActivePromptIdChange, autoExpand]);

	return (
		<div className={className}>
			<div className="flex items-center justify-between mb-2">
				<label
					htmlFor="prompt-picker-textarea"
					className="text-sm font-semibold text-(--accent)"
				>
					{label}
				</label>
				<CollapsibleToggle collapsed={collapsed} onToggle={toggleCollapsed} />
			</div>
			<div
				className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${collapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"}`}
			>
				<div className="overflow-hidden">
					{showPresetBar && (
						<PresetBar
							items={prompts}
							activeId={activePromptId}
							onSelect={handleSelect}
							onCustom={handleCustom}
							onRandom={handleRandom}
						/>
					)}
					<textarea
						ref={textareaRef}
						value={prompt}
						onChange={(e) => {
							handleTextareaChange(e.target.value);
							if (!e.target.value) {
								e.target.style.height = "auto";
							} else {
								e.target.style.height = "auto";
								const el = e.target;
								requestAnimationFrame(() => {
									if (el.scrollHeight > el.clientHeight) {
										el.style.height = `${el.scrollHeight}px`;
									}
								});
							}
						}}
						placeholder={textareaPlaceholder}
						rows={1}
						maxLength={maxLength}
						className="ui-input w-full resize-y max-h-32 min-h-11 overflow-y-auto mt-1.5"
						disabled={disabled}
						// biome-ignore lint/a11y/noAutofocus: intentional UX - auto-focuses the input when the modal/picker opens
						autoFocus={autoFocus}
						id="prompt-picker-textarea"
					/>
				</div>
			</div>

			{/* Prompt Overwrite Confirmation */}
			{pendingPrompt && (
				<ConfirmDialog
					title={
						pendingPrompt.id === "__custom__"
							? t("components.promptPicker.switchToCustom")
							: t("components.promptPicker.overwritePrompt")
					}
					fields={[t("components.promptPicker.promptField")]}
					onConfirm={handleConfirmOverwrite}
					onCancel={() => setPendingPrompt(null)}
				/>
			)}
		</div>
	);
}
