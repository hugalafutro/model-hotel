import { useRef } from "react";
import { useTranslation } from "react-i18next";
import { autoExpandTextarea } from "../utils/dom";
import {
	CollapseBody,
	CollapsibleToggle,
	useCollapsible,
} from "./CollapsibleToggle";
import { ConfirmDialog } from "./ConfirmDialog";
import { PresetBar } from "./PresetBar";
import { usePresetText } from "./usePresetText";

/** Copy for the "this would overwrite your text" dialog, per caller. */
export interface PresetConfirmCopy {
	title: string;
	message?: string;
	fields: string[];
}

interface PresetTextPickerProps<
	T extends { id: string; icon: string; label: string },
> {
	presets: readonly T[];
	activeId: string | null;
	text: string;
	/** Reads the translation key of the text a preset carries. */
	textOf: (preset: T) => string;
	onActiveChange: (id: string | null) => void;
	onTextChange: (text: string) => void;
	/** Dialog copy, asked for separately when the pending pick is "custom". */
	confirmCopy: (isCustom: boolean) => PresetConfirmCopy;
	label: string;
	textareaId: string;
	textareaPlaceholder: string;
	maxLength: number;
	className?: string;
	disabled?: boolean;
	/** False hides the preset bar, leaving the textarea alone. */
	showPresetBar?: boolean;
	autoFocus?: boolean;
	/**
	 * The random button's handler, passed straight to PresetBar, which hides the
	 * button when there is none. Callers whose random pick means more than "a
	 * different preset" supply their own.
	 */
	onRandom?: () => void;
	/** True to give the random button the built-in "another preset" pick. */
	randomFromPresets?: boolean;
	/** Inline style for the textarea. */
	textareaStyle?: React.CSSProperties;
}

/**
 * A labelled, collapsible textarea with a bar of presets above it: picking one
 * fills the text, editing the text drops back to custom, and either direction
 * confirms first when it would discard what the user typed.
 */
export function PresetTextPicker<
	T extends { id: string; icon: string; label: string },
>({
	presets,
	activeId,
	text,
	textOf,
	onActiveChange,
	onTextChange,
	confirmCopy,
	label,
	textareaId,
	textareaPlaceholder,
	maxLength,
	className,
	disabled = false,
	showPresetBar = true,
	autoFocus = false,
	onRandom,
	randomFromPresets = false,
	textareaStyle,
}: PresetTextPickerProps<T>) {
	const { t } = useTranslation();
	const { collapsed, toggle: toggleCollapsed } = useCollapsible();
	const textareaRef = useRef<HTMLTextAreaElement>(null);

	const preset = usePresetText<T>({
		activeId,
		text,
		textOf,
		onChange: (id, next) => {
			onTextChange(next);
			onActiveChange(id);
			// Nothing to grow into when the text was cleared for custom. The
			// measurement waits a frame: the textarea still holds the old value
			// until React has re-rendered it.
			if (id !== null) {
				requestAnimationFrame(() => {
					if (textareaRef.current) autoExpandTextarea(textareaRef.current);
				});
			}
		},
	});

	const copy = confirmCopy(preset.isCustomPending);

	return (
		<div className={className}>
			<div className="flex items-center justify-between mb-2">
				<label
					htmlFor={textareaId}
					className="text-sm font-semibold text-(--accent)"
				>
					{label}
				</label>
				<CollapsibleToggle collapsed={collapsed} onToggle={toggleCollapsed} />
			</div>
			<CollapseBody collapsed={collapsed}>
				{showPresetBar && (
					<PresetBar
						items={presets}
						activeId={activeId}
						onSelect={preset.select}
						onCustom={preset.custom}
						onRandom={
							onRandom ??
							(randomFromPresets ? () => preset.random(presets) : undefined)
						}
					/>
				)}
				<textarea
					id={textareaId}
					ref={textareaRef}
					value={text}
					onChange={(e) => {
						const value = e.target.value;
						onTextChange(value);
						// An edit away from the preset's own text means custom.
						const current = presets.find((p) => p.id === activeId);
						if (current && value !== t(textOf(current))) onActiveChange(null);
						autoExpandTextarea(e.target);
					}}
					placeholder={textareaPlaceholder}
					rows={1}
					maxLength={maxLength}
					className="ui-input w-full resize-y max-h-32 min-h-11 overflow-y-auto mt-1.5"
					style={textareaStyle}
					disabled={disabled}
					// biome-ignore lint/a11y/noAutofocus: intentional UX - auto-focuses the input when the modal/picker opens
					autoFocus={autoFocus}
				/>
			</CollapseBody>

			{preset.pending && (
				<ConfirmDialog
					title={copy.title}
					message={copy.message}
					fields={copy.fields}
					onConfirm={preset.confirm}
					onCancel={preset.cancel}
				/>
			)}
		</div>
	);
}
