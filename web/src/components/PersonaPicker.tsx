import { type RefObject, useCallback, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { PersonaPreset } from "../data/presets";
import { CollapsibleToggle, useCollapsible } from "./CollapsibleToggle";
import { ConfirmDialog } from "./ConfirmDialog";
import { PresetBar } from "./PresetBar";

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

export function PersonaPicker({
	personas,
	activePersonaId,
	systemPrompt,
	onActivePersonaChange,
	onSystemPromptChange,
	label: labelProp,
	textareaPlaceholder: textareaPlaceholderProp,
	className,
	disabled = false,
	onRandom,
}: PersonaPickerProps) {
	const { t } = useTranslation();
	const { collapsed, toggle: toggleCollapsed } = useCollapsible();
	const label = labelProp ?? t("components.personaPicker.persona");
	const textareaPlaceholder =
		textareaPlaceholderProp ??
		t("components.personaPicker.customPersonaPlaceholder");
	const [pendingPersona, setPendingPersona] = useState<PersonaPreset | null>(
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
		(persona: PersonaPreset) => {
			if (systemPrompt.trim() && activePersonaId === null) {
				// User has custom text - confirm before overwriting
				setPendingPersona(persona);
				return;
			}
			onSystemPromptChange(t(persona.systemPrompt));
			onActivePersonaChange(persona.id);
			autoExpand(textareaRef);
		},
		[
			systemPrompt,
			activePersonaId,
			onSystemPromptChange,
			onActivePersonaChange,
			autoExpand,
			t,
		],
	);

	const handleCustom = useCallback(() => {
		if (activePersonaId !== null) {
			// A preset is active - warn that switching to custom will clear
			setPendingPersona({
				id: "__custom__",
				icon: "✏️",
				label: t("common.custom"),
				systemPrompt: "",
			});
			return;
		}
	}, [activePersonaId, t]);

	const handleTextareaChange = useCallback(
		(value: string) => {
			onSystemPromptChange(value);
			// If user edits away from a preset, switch to custom
			const current = personas.find((p) => p.id === activePersonaId);
			if (current && value !== t(current.systemPrompt)) {
				onActivePersonaChange(null);
			}
		},
		[personas, activePersonaId, onSystemPromptChange, onActivePersonaChange, t],
	);

	const handleConfirmOverwrite = useCallback(() => {
		if (!pendingPersona) return;
		if (pendingPersona.id === "__custom__") {
			onSystemPromptChange("");
			onActivePersonaChange(null);
		} else {
			onSystemPromptChange(t(pendingPersona.systemPrompt));
			onActivePersonaChange(pendingPersona.id);
			autoExpand(textareaRef);
		}
		setPendingPersona(null);
	}, [
		pendingPersona,
		onSystemPromptChange,
		onActivePersonaChange,
		autoExpand,
		t,
	]);

	return (
		<div className={className}>
			<div className="flex items-center justify-between mb-2">
				<label
					htmlFor="persona-picker-textarea"
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
					<PresetBar
						items={personas}
						activeId={activePersonaId}
						onSelect={handleSelect}
						onCustom={handleCustom}
						onRandom={onRandom}
					/>
					<textarea
						id="persona-picker-textarea"
						ref={textareaRef}
						value={systemPrompt}
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
						maxLength={5000}
						className="ui-input w-full resize-y max-h-32 min-h-11 overflow-y-auto mt-1.5"
						style={{ height: "auto" }}
						disabled={disabled}
					/>
				</div>
			</div>

			{/* Persona Overwrite Confirmation */}
			{pendingPersona && (
				<ConfirmDialog
					title={
						pendingPersona.id === "__custom__"
							? t("components.personaPicker.switchToCustom")
							: t("components.personaPicker.overwritePrompt")
					}
					message={
						pendingPersona.id === "__custom__"
							? t("components.personaPicker.clearPersonaPrompt")
							: undefined
					}
					fields={
						pendingPersona.id === "__custom__"
							? []
							: [t("components.personaPicker.systemPrompt")]
					}
					onConfirm={handleConfirmOverwrite}
					onCancel={() => setPendingPersona(null)}
				/>
			)}
		</div>
	);
}
