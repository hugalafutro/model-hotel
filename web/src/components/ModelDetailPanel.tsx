import { useState } from "react";
import {
    Settings,
    RotateCcw,
    ChevronsDownUp,
    ChevronsUpDown,
    X,
} from "lucide-react";
import type { Model, GenerationParams } from "../api/types";
import { CAP_META } from "./capMeta";
import { proxyModelID, parseCapabilities, formatPrice } from "../utils/model";

function ParamSlider({
    label,
    value,
    min,
    max,
    step,
    onChange,
}: {
    label: string;
    value: number | undefined;
    min: number;
    max: number;
    step: number;
    onChange: (v: number | undefined) => void;
}) {
    const isSet = value !== undefined;
    const pct = isSet ? ((value - min) / (max - min)) * 100 : 0;
    return (
        <div>
            <div className="flex items-center justify-between">
                <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                    {label}
                </span>
                <input
                    type="number"
                    value={isSet ? value : ""}
                    min={min}
                    max={max}
                    step={step}
                    onChange={(e) => {
                        const v = e.target.value;
                        if (v === "" || v === "-" || v === ".") {
                            onChange(undefined);
                            return;
                        }
                        const n = parseFloat(v);
                        if (!isNaN(n)) onChange(n);
                    }}
                    placeholder="off"
                    className="w-14 text-right px-1.5 py-0.5 rounded bg-(--surface-input) text-[10px] text-(--text-primary) border border-transparent focus:border-(--accent) outline-none placeholder:text-(--text-tertiary) no-spinner"
                />
            </div>
            <input
                type="range"
                min={min}
                max={max}
                step={step}
                value={isSet ? value : min}
                data-set={isSet ? "true" : undefined}
                onChange={(e) => onChange(parseFloat(e.target.value))}
                className="gen-slider w-full h-1 rounded-lg appearance-none cursor-pointer bg-(--surface-hover) accent-(--accent) mt-0.5"
                style={{
                    background: isSet
                        ? `linear-gradient(to right, var(--accent) ${pct}%, var(--surface-hover) ${pct}%)`
                        : undefined,
                }}
            />
        </div>
    );
}

interface ModelDetailPanelProps {
    model: Model;
    params?: GenerationParams;
    onParamsChange?: (params: GenerationParams) => void;
    /** Optional close callback — when provided, shows an X button in the header */
    onClose?: () => void;
}

export function ModelDetailPanel({
    model,
    params,
    onParamsChange,
    onClose,
}: ModelDetailPanelProps) {
    const caps = parseCapabilities(model.capabilities);
    const [open, setOpen] = useState(false);
    const [collapsed, setCollapsed] = useState(false);

    const editable = params !== undefined && onParamsChange !== undefined;

    const hasCustom = editable
        ? params.temperature !== undefined ||
          params.max_tokens !== undefined ||
          params.top_p !== undefined ||
          params.min_p !== undefined ||
          params.top_k !== undefined ||
          params.frequency_penalty !== undefined ||
          params.presence_penalty !== undefined
        : false;

    return (
        <div className="ui-card p-3 text-xs relative overflow-y-auto max-h-full">
            {/* Header with collapse arrow + cog */}
            <div className="flex items-start justify-between">
                <div className="min-w-0">
                    <h3 className="text-sm font-semibold text-(--text-primary) leading-tight truncate">
                        {model.display_name || model.model_id}
                    </h3>
                    {!collapsed && model.description && (
                        <p
                            className="text-(--text-secondary) mt-1 line-clamp-10 text-[11px]"
                            title={model.description}
                        >
                            {model.description}
                        </p>
                    )}
                </div>
                <div className="flex items-start gap-0.5 shrink-0 ml-2">
                    {!collapsed && hasCustom && (
                        <button
                            onClick={() => onParamsChange!({})}
                            className="p-1.5 rounded-md transition-all cursor-pointer shrink-0 text-red-500/80 hover:text-red-500 hover:drop-shadow-[0_0_6px_rgba(239,68,68,0.6)]"
                            title="Reset parameters"
                        >
                            <RotateCcw size={14} />
                        </button>
                    )}
                    {!collapsed && editable && (
                        <button
                            onClick={() => setOpen((s) => !s)}
                            className={`p-1.5 rounded-md transition-all cursor-pointer shrink-0 ${
                                open || hasCustom
                                    ? "text-(--accent) drop-shadow-[0_0_6px_var(--accent)]"
                                    : "text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[0_0_6px_var(--accent)]"
                            }`}
                            title="Generation parameters"
                        >
                            <Settings size={14} />
                        </button>
                    )}
                    {onClose && (
                        <button
                            onClick={onClose}
                            className="p-1.5 rounded-md cursor-pointer text-(--text-tertiary) hover:text-(--text-primary) transition-colors"
                            title="Close"
                        >
                            <X size={14} />
                        </button>
                    )}
                    <button
                        onClick={() => setCollapsed((c) => !c)}
                        className="p-1.5 rounded-md transition-all cursor-pointer text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[0_0_6px_var(--accent)]"
                        title={
                            collapsed
                                ? "Expand model details"
                                : "Collapse model details"
                        }
                    >
                        {collapsed ? (
                            <ChevronsUpDown size={14} />
                        ) : (
                            <ChevronsDownUp size={14} />
                        )}
                    </button>
                </div>
            </div>

            <div
                className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
                    collapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"
                }`}
            >
                <div className="overflow-hidden">
                    <div className="space-y-3 pt-3">
                        {editable && (
                            <div
                                className={`overflow-hidden transition-all duration-300 ease-in-out border-t border-(--border-subtle) ${
                                    open
                                        ? "max-h-125 opacity-100 pt-2 mt-1"
                                        : "max-h-0 opacity-0 pt-0 mt-0"
                                }`}
                            >
                                <div className="space-y-2">
                                    <ParamSlider
                                        label="Temperature"
                                        value={params!.temperature}
                                        min={0}
                                        max={2}
                                        step={0.01}
                                        onChange={(v) =>
                                            onParamsChange!({
                                                ...params!,
                                                temperature: v,
                                            })
                                        }
                                    />
                                    <ParamSlider
                                        label="Max Tokens"
                                        value={params!.max_tokens}
                                        min={1}
                                        max={32768}
                                        step={1}
                                        onChange={(v) =>
                                            onParamsChange!({
                                                ...params!,
                                                max_tokens:
                                                    v === undefined
                                                        ? undefined
                                                        : Math.round(v),
                                            })
                                        }
                                    />
                                    <ParamSlider
                                        label="Top P"
                                        value={params!.top_p}
                                        min={0}
                                        max={1}
                                        step={0.01}
                                        onChange={(v) =>
                                            onParamsChange!({
                                                ...params!,
                                                top_p: v,
                                            })
                                        }
                                    />
                                    <ParamSlider
                                        label="Min P"
                                        value={params!.min_p}
                                        min={0}
                                        max={1}
                                        step={0.01}
                                        onChange={(v) =>
                                            onParamsChange!({
                                                ...params!,
                                                min_p: v,
                                            })
                                        }
                                    />
                                    <ParamSlider
                                        label="Top K"
                                        value={params!.top_k}
                                        min={1}
                                        max={100}
                                        step={1}
                                        onChange={(v) =>
                                            onParamsChange!({
                                                ...params!,
                                                top_k:
                                                    v === undefined
                                                        ? undefined
                                                        : Math.round(v),
                                            })
                                        }
                                    />
                                    <ParamSlider
                                        label="Freq Penalty"
                                        value={params!.frequency_penalty}
                                        min={-2}
                                        max={2}
                                        step={0.01}
                                        onChange={(v) =>
                                            onParamsChange!({
                                                ...params!,
                                                frequency_penalty: v,
                                            })
                                        }
                                    />
                                    <ParamSlider
                                        label="Pres Penalty"
                                        value={params!.presence_penalty}
                                        min={-2}
                                        max={2}
                                        step={0.01}
                                        onChange={(v) =>
                                            onParamsChange!({
                                                ...params!,
                                                presence_penalty: v,
                                            })
                                        }
                                    />
                                </div>
                            </div>
                        )}

                        <div className="space-y-2">
                            <div>
                                <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                                    Provider
                                </span>
                                <div className="text-(--text-primary) font-medium">
                                    {model.provider_name}
                                </div>
                            </div>
                            <div>
                                <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                                    Model ID
                                </span>
                                <div
                                    className="text-(--text-primary) font-medium truncate"
                                    title={model.model_id}
                                >
                                    {model.model_id}
                                </div>
                            </div>
                            <div className="grid grid-cols-2 gap-2">
                                <div>
                                    <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                                        Context
                                    </span>
                                    <div className="text-(--text-primary) font-medium">
                                        {model.context_length?.toLocaleString() ??
                                            "-"}
                                    </div>
                                </div>
                                <div>
                                    <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                                        Max Out
                                    </span>
                                    <div className="text-(--text-primary) font-medium">
                                        {model.max_output_tokens?.toLocaleString() ??
                                            "-"}
                                    </div>
                                </div>
                            </div>
                            <div className="grid grid-cols-2 gap-2">
                                <div>
                                    <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                                        In $/1M
                                    </span>
                                    <div className="text-(--text-primary) font-medium">
                                        $
                                        {formatPrice(
                                            model.input_price_per_million,
                                        )}
                                    </div>
                                </div>
                                <div>
                                    <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                                        Out $/1M
                                    </span>
                                    <div className="text-(--text-primary) font-medium">
                                        $
                                        {formatPrice(
                                            model.output_price_per_million,
                                        )}
                                    </div>
                                </div>
                            </div>
                        </div>

                        {CAP_META.some((m) => caps[m.key]) && (
                            <div>
                                <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                                    Capabilities
                                </span>
                                <div className="flex flex-wrap gap-1 mt-1">
                                    {CAP_META.filter((m) => caps[m.key]).map(
                                        (m) => (
                                            <span
                                                key={m.key}
                                                className={`px-1.5 py-0.5 text-[10px] rounded-full border ${m.style}`}
                                            >
                                                {m.label}
                                            </span>
                                        ),
                                    )}
                                </div>
                            </div>
                        )}

                        <div>
                            <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                                Proxy ID
                            </span>
                            <code className="block mt-0.5 p-1.5 rounded bg-(--surface-input) text-[10px] text-(--text-secondary) break-all">
                                {proxyModelID(
                                    model.provider_name,
                                    model.model_id,
                                )}
                            </code>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    );
}

interface ModelDetailModalProps {
    model: Model;
    onClose: () => void;
}

export function ModelDetailModal({ model, onClose }: ModelDetailModalProps) {
    return (
        <div
            role="dialog"
            aria-modal="true"
            className="fixed inset-0 flex items-center justify-center z-60"
            onClick={(e) => {
                if (e.target === e.currentTarget) onClose();
            }}
            onKeyDown={(e) => {
                if (e.key === "Escape") onClose();
            }}
        >
            <div className="absolute inset-0 bg-black/50" />
            <div className="relative w-full max-w-sm max-h-[80vh] mx-4">
                <ModelDetailPanel model={model} onClose={onClose} />
            </div>
        </div>
    );
}
