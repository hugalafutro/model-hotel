import { useState, useCallback, useRef, type RefObject } from "react";
import type { ArenaPromptPreset } from "../data/presets";
import { PresetBar } from "./PresetBar";
import { ConfirmDialog } from "./ConfirmDialog";

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
    /** When true, prompt buttons wrap to multiple rows instead of scrolling horizontally */
    wrap?: boolean;
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
    label = "Prompt",
    textareaPlaceholder = "Enter your prompt…",
    className,
    disabled = false,
    wrap = false,
    showPresetBar = true,
    autoFocus = false,
    maxLength = 10000,
}: PromptPickerProps) {
    const [pendingPrompt, setPendingPrompt] = useState<ArenaPromptPreset | null>(
        null,
    );
    const textareaRef = useRef<HTMLTextAreaElement>(null);

    const autoExpand = useCallback(
        (ref: RefObject<HTMLTextAreaElement | null>) => {
            requestAnimationFrame(() => {
                const el = ref.current;
                if (el) {
                    el.style.height = "auto";
                    el.style.height = el.scrollHeight + "px";
                }
            });
        },
        [],
    );

    const handleSelect = useCallback(
        (preset: ArenaPromptPreset) => {
            if (prompt.trim() && activePromptId === null) {
                // User has custom text — confirm before overwriting
                setPendingPrompt(preset);
                return;
            }
            onPromptChange(preset.prompt);
            onActivePromptIdChange(preset.id);
            autoExpand(textareaRef);
        },
        [prompt, activePromptId, onPromptChange, onActivePromptIdChange, autoExpand],
    );

    const handleCustom = useCallback(() => {
        if (activePromptId !== null) {
            // A preset is active — warn that switching to custom will clear
            setPendingPrompt({
                id: "__custom__",
                icon: "✏️",
                label: "Custom",
                prompt: "",
            });
            return;
        }
    }, [activePromptId]);

    const handleRandom = useCallback(() => {
        const available = prompts.filter((p) => p.id !== activePromptId);
        if (available.length === 0) return;
        const pick = available[Math.floor(Math.random() * available.length)];
        if (prompt.trim() && activePromptId === null) {
            setPendingPrompt(pick);
            return;
        }
        onPromptChange(pick.prompt);
        onActivePromptIdChange(pick.id);
        autoExpand(textareaRef);
    }, [prompts, activePromptId, prompt, onPromptChange, onActivePromptIdChange, autoExpand]);

    const handleTextareaChange = useCallback(
        (value: string) => {
            onPromptChange(value);
            // If user edits away from a preset, switch to custom
            const current = prompts.find((p) => p.id === activePromptId);
            if (current && value !== current.prompt) {
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
            onPromptChange(pendingPrompt.prompt);
            onActivePromptIdChange(pendingPrompt.id);
            autoExpand(textareaRef);
        }
        setPendingPrompt(null);
    }, [pendingPrompt, onPromptChange, onActivePromptIdChange, autoExpand]);

    return (
        <div className={className}>
            {label && (
                <label className="text-sm text-(--text-secondary) mb-2 block">
                    {label}
                </label>
            )}
            {showPresetBar && (
                <PresetBar
                    items={prompts}
                    activeId={activePromptId}
                    onSelect={handleSelect}
                    onCustom={handleCustom}
                    onRandom={handleRandom}
                    wrap={wrap}
                />
            )}
            <textarea
                ref={textareaRef}
                value={prompt}
                onChange={(e) => {
                    handleTextareaChange(e.target.value);
                    if (!e.target.value) {
                        e.target.style.height = "auto";
                    } else if (e.target.scrollHeight > e.target.clientHeight) {
                        e.target.style.height = e.target.scrollHeight + "px";
                    }
                }}
                placeholder={textareaPlaceholder}
                autoFocus={autoFocus}
                rows={1}
                maxLength={maxLength}
                className="ui-input w-full resize-y max-h-32 min-h-11 overflow-y-auto mt-1.5"
                disabled={disabled}
            />

            {/* Prompt Overwrite Confirmation */}
            {pendingPrompt && (
                <ConfirmDialog
                    title={
                        pendingPrompt.id === "__custom__"
                            ? "Switch to Custom"
                            : "Overwrite Prompt"
                    }
                    fields={["Prompt"]}
                    onConfirm={handleConfirmOverwrite}
                    onCancel={() => setPendingPrompt(null)}
                />
            )}
        </div>
    );
}
