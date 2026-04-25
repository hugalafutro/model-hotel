import { useState, useMemo } from "react";
import { FilterInput } from "./FilterInput";

interface ProviderInfo {
    name: string;
    base_url: string;
}

interface ModelItem {
    provider_name: string;
    model_id: string;
    display_name?: string;
}

interface SingleProps {
    multi?: false;
    models: ModelItem[];
    selected: string;
    onChange: (selected: string) => void;
    maxSelections?: number;
    label?: string;
    providers?: ProviderInfo[];
    align?: "left" | "right";
}

interface MultiProps {
    multi: true;
    models: ModelItem[];
    selected: string[];
    onChange: (selected: string[]) => void;
    maxSelections?: number;
    label?: string;
    providers?: ProviderInfo[];
    align?: "left" | "right";
}

type ModelPickerProps = SingleProps | MultiProps;

function proxyModelID(providerName: string, modelId: string): string {
    return providerName.replace(/ /g, "-") + "/" + modelId;
}

type SortMode = "provider" | "model";

function getProviderStyle(baseUrl: string, active: boolean) {
    const isNanoGPT = baseUrl.includes("nano-gpt.com");
    const isDeepSeek = baseUrl.includes("deepseek.com");
    const isOllama = baseUrl.includes("ollama.com");
    const isZAI = baseUrl.includes("z.ai");
    if (active) {
        if (isNanoGPT)
            return "bg-[#0690a8] text-white border-[#0690a8] shadow-[0_0_6px_1px_rgba(6,144,168,0.35)]";
        if (isDeepSeek)
            return "bg-[#36aaff] text-white border-[#36aaff] shadow-[0_0_6px_1px_rgba(54,170,255,0.35)]";
        if (isZAI)
            return "bg-[#18181b] text-white border-[#18181b] shadow-[0_0_6px_1px_rgba(255,255,255,0.2)]";
        if (isOllama)
            return "bg-[#71717a] text-white border-[#71717a] shadow-[0_0_6px_1px_rgba(113,113,122,0.35)]";
        return "bg-gray-900 text-white border-gray-700 shadow-[0_0_6px_1px_rgba(255,255,255,0.15)]";
    }
    if (isNanoGPT)
        return "bg-[#0690a8]/20 text-[#0690a8] border-[#0690a8]/50 hover:bg-[#0690a8]/30";
    if (isDeepSeek)
        return "bg-[#36aaff]/20 text-[#36aaff] border-[#36aaff]/50 hover:bg-[#36aaff]/30";
    if (isZAI)
        return "bg-[#18181b]/25 text-[#d4d4d8] border-[#3f3f46]/60 hover:bg-[#18181b]/40";
    if (isOllama)
        return "bg-[#71717a]/20 text-[#a1a1aa] border-[#71717a]/40 hover:bg-[#71717a]/30";
    return "bg-gray-700 text-gray-300 border-gray-600 hover:bg-gray-600";
}

export function ModelPicker({
    models,
    selected,
    onChange,
    multi = false,
    maxSelections = Infinity,
    label,
    providers = [],
    align,
}: ModelPickerProps) {
    const [search, setSearch] = useState("");
    const [providerFilter, setProviderFilter] = useState<Set<string>>(
        new Set(),
    );
    const [sortMode, setSortMode] = useState<SortMode>("provider");

    const selectedSet = useMemo(() => {
        if (multi) return new Set(selected as string[]);
        return new Set(selected ? [selected as string] : []);
    }, [selected, multi]);

    const enabledModels = useMemo(
        () => models.filter((m) => m.provider_name),
        [models],
    );

    const providerNames = useMemo(
        () =>
            Array.from(
                new Set(enabledModels.map((m) => m.provider_name)),
            ).sort(),
        [enabledModels],
    );

    const providerBaseUrl = useMemo(() => {
        const map = new Map<string, string>();
        for (const p of providers) {
            map.set(p.name, p.base_url);
        }
        return map;
    }, [providers]);

    const filteredModels = useMemo(() => {
        let result = enabledModels;
        if (providerFilter.size > 0) {
            result = result.filter((m) => providerFilter.has(m.provider_name));
        }
        if (search.trim()) {
            const q = search.trim().toLowerCase();
            result = result.filter((m) => {
                const name = (m.display_name || m.model_id).toLowerCase();
                const pid = m.model_id.toLowerCase();
                const prov = m.provider_name.toLowerCase();
                return name.includes(q) || pid.includes(q) || prov.includes(q);
            });
        }
        result = [...result].sort((a, b) => {
            const aVal = proxyModelID(a.provider_name, a.model_id);
            const bVal = proxyModelID(b.provider_name, b.model_id);
            const aSel = selectedSet.has(aVal) ? 0 : 1;
            const bSel = selectedSet.has(bVal) ? 0 : 1;
            if (aSel !== bSel) return aSel - bSel;
            if (sortMode === "model") {
                const aName = (a.display_name || a.model_id).toLowerCase();
                const bName = (b.display_name || b.model_id).toLowerCase();
                const cmp = aName.localeCompare(bName);
                if (cmp !== 0) return cmp;
                return a.provider_name.localeCompare(b.provider_name);
            }
            const cmp = a.provider_name.localeCompare(b.provider_name);
            if (cmp !== 0) return cmp;
            return (a.display_name || a.model_id).localeCompare(
                b.display_name || b.model_id,
            );
        });
        return result;
    }, [enabledModels, providerFilter, search, selectedSet, sortMode]);

    const toggleProvider = (provider: string) => {
        setProviderFilter((prev) => {
            const next = new Set(prev);
            if (next.has(provider)) next.delete(provider);
            else next.add(provider);
            return next;
        });
    };

    const toggleModel = (val: string) => {
        if (multi) {
            const current = [...(selected as string[])];
            if (current.includes(val)) {
                (onChange as (s: string[]) => void)(
                    current.filter((v) => v !== val),
                );
            } else {
                if (current.length >= maxSelections) return;
                (onChange as (s: string[]) => void)([...current, val]);
            }
        } else {
            (onChange as (s: string) => void)(val === selected ? "" : val);
        }
    };

    return (
        <div className="space-y-3">
            {label && (
                <label className="text-sm text-(--text-secondary) block">
                    {label}
                </label>
            )}

            <div className="flex items-center gap-2 flex-wrap">
                <FilterInput
                    value={search}
                    onChange={setSearch}
                    placeholder="Filter models..."
                    className="w-[320px]"
                />
                <select
                    value={sortMode}
                    onChange={(e) => setSortMode(e.target.value as SortMode)}
                    className="ui-input h-9 py-0! w-auto! text-xs pr-6!"
                >
                    <option value="provider">By Provider</option>
                    <option value="model">By Model</option>
                </select>
                <div className="flex flex-wrap gap-1">
                    {providerNames.map((name) => {
                        const active = providerFilter.has(name);
                        const baseUrl = providerBaseUrl.get(name) || "";
                        return (
                            <button
                                key={name}
                                onClick={() => toggleProvider(name)}
                                className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border transition-colors ${getProviderStyle(baseUrl, active)}`}
                            >
                                {name}
                            </button>
                        );
                    })}
                    {providerFilter.size > 0 && (
                        <button
                            onClick={() => setProviderFilter(new Set())}
                            className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium text-gray-400 hover:text-gray-200"
                        >
                            ✕
                        </button>
                    )}
                </div>
            </div>

            <div
                className={`flex flex-wrap gap-1.5 max-h-40 overflow-y-auto pr-1 ${align === "right" ? "justify-end" : "justify-start"}`}
            >
                {filteredModels.map((m) => {
                    const val = proxyModelID(m.provider_name, m.model_id);
                    const isSelected = selectedSet.has(val);
                    return (
                        <button
                            key={val}
                            onClick={() => toggleModel(val)}
                            className={`px-2 py-0.5 text-[11px] rounded-md border transition-all whitespace-nowrap ${
                                isSelected
                                    ? "bg-(--accent)/15 border-(--accent)/40 text-(--accent)"
                                    : "bg-(--surface-hover) border-(--border-subtle) text-(--text-secondary) hover:text-(--text-primary)"
                            }`}
                            title={m.display_name || m.model_id}
                        >
                            {m.display_name || m.model_id}
                        </button>
                    );
                })}
                {filteredModels.length === 0 && (
                    <span className="text-xs text-(--text-muted)">
                        No models match
                    </span>
                )}
            </div>
        </div>
    );
}
