import { useState, useCallback, useRef, type RefObject } from "react";
import { ChevronUp, ChevronDown } from "lucide-react";
import type { PersonaPreset } from "../data/presets";
import { PresetBar } from "./PresetBar";
import { ConfirmDialog } from "./ConfirmDialog";

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
    label = "Persona",
    textareaPlaceholder = "Enter your custom prompt here…",
    className,
    disabled = false,
    onRandom,
}: PersonaPickerProps) {
    const [collapsed, setCollapsed] = useState(false);
    const [pendingPersona, setPendingPersona] = useState<PersonaPreset | null>(
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
        (persona: PersonaPreset) => {
            if (systemPrompt.trim() && activePersonaId === null) {
                // User has custom text — confirm before overwriting
                setPendingPersona(persona);
                return;
            }
            onSystemPromptChange(persona.systemPrompt);
            onActivePersonaChange(persona.id);
            autoExpand(textareaRef);
        },
        [
            systemPrompt,
            activePersonaId,
            onSystemPromptChange,
            onActivePersonaChange,
            autoExpand,
        ],
    );

    const handleCustom = useCallback(() => {
        if (activePersonaId !== null) {
            // A preset is active — warn that switching to custom will clear
            setPendingPersona({
                id: "__custom__",
                icon: "✏️",
                label: "Custom",
                systemPrompt: "",
            });
            return;
        }
    }, [activePersonaId]);

    const handleTextareaChange = useCallback(
        (value: string) => {
            onSystemPromptChange(value);
            // If user edits away from a preset, switch to custom
            const current = personas.find((p) => p.id === activePersonaId);
            if (current && value !== current.systemPrompt) {
                onActivePersonaChange(null);
            }
        },
        [
            personas,
            activePersonaId,
            onSystemPromptChange,
            onActivePersonaChange,
        ],
    );

    const handleConfirmOverwrite = useCallback(() => {
        if (!pendingPersona) return;
        if (pendingPersona.id === "__custom__") {
            onSystemPromptChange("");
            onActivePersonaChange(null);
        } else {
            onSystemPromptChange(pendingPersona.systemPrompt);
            onActivePersonaChange(pendingPersona.id);
            autoExpand(textareaRef);
        }
        setPendingPersona(null);
    }, [
        pendingPersona,
        onSystemPromptChange,
        onActivePersonaChange,
        autoExpand,
    ]);

    return (
        <div className={className}>
            {collapsed ? (
                <button
                    type="button"
                    onClick={() => setCollapsed(false)}
                    className="flex items-center gap-1.5 text-sm text-(--text-secondary) hover:text-(--accent) transition-colors cursor-pointer group"
                >
                    <ChevronDown
                        size={14}
                        className="text-(--text-tertiary) group-hover:text-(--accent) group-hover:drop-shadow-[0_0_6px_var(--accent)] transition-all"
                    />
                    <span>{label}</span>
                </button>
            ) : (
                <>
                    <div className="flex items-center justify-between mb-2">
                        <label className="text-sm text-(--text-secondary)">
                            {label}
                        </label>
                        <button
                            type="button"
                            onClick={() => setCollapsed(true)}
                            className="cursor-pointer text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[0_0_6px_var(--accent)] transition-all p-0.5 -m-0.5"
                            title="Collapse"
                        >
                            <ChevronUp size={14} />
                        </button>
                    </div>
                    <PresetBar
                        items={personas}
                        activeId={activePersonaId}
                        onSelect={handleSelect}
                        onCustom={handleCustom}
                        onRandom={onRandom}
                    />
                    <textarea
                        ref={textareaRef}
                        value={systemPrompt}
                        onChange={(e) => {
                            handleTextareaChange(e.target.value);
                            if (!e.target.value) {
                                e.target.style.height = "auto";
                            } else if (
                                e.target.scrollHeight > e.target.clientHeight
                            ) {
                                e.target.style.height =
                                    e.target.scrollHeight + "px";
                            }
                        }}
                        placeholder={textareaPlaceholder}
                        rows={1}
                        maxLength={5000}
                        className="ui-input w-full resize-y max-h-32 min-h-11 overflow-y-auto mt-1.5"
                        style={{ height: "auto" }}
                        disabled={disabled}
                    />
                </>
            )}

            {/* Persona Overwrite Confirmation */}
            {pendingPersona && (
                <ConfirmDialog
                    title={
                        pendingPersona.id === "__custom__"
                            ? "Switch to Custom"
                            : "Overwrite Prompt"
                    }
                    message={
                        pendingPersona.id === "__custom__"
                            ? "This will clear the current persona prompt. Continue?"
                            : undefined
                    }
                    fields={
                        pendingPersona.id === "__custom__"
                            ? []
                            : ["System prompt"]
                    }
                    onConfirm={handleConfirmOverwrite}
                    onCancel={() => setPendingPersona(null)}
                />
            )}
        </div>
    );
}
