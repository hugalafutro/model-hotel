import { useState, useMemo } from "react";
import { Search, X } from "lucide-react";

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
}

interface MultiProps {
    multi: true;
    models: ModelItem[];
    selected: string[];
    onChange: (selected: string[]) => void;
    maxSelections?: number;
    label?: string;
}

type ModelPickerProps = SingleProps | MultiProps;

function proxyModelID(providerName: string, modelId: string): string {
    return providerName.replace(/ /g, "-") + "/" + modelId;
}

export function ModelPicker({
    models,
    selected,
    onChange,
    multi = false,
    maxSelections = Infinity,
    label,
}: ModelPickerProps) {
    const [search, setSearch] = useState("");
    const [providerFilter, setProviderFilter] = useState<Set<string>>(new Set());

    const selectedSet = useMemo(() => {
        if (multi) return new Set(selected as string[]);
        return new Set(selected ? [selected as string] : []);
    }, [selected, multi]);

    const enabledModels = useMemo(
        () => models.filter((m) => m.provider_name),
        [models],
    );

    const providers = useMemo(
        () => Array.from(new Set(enabledModels.map((m) => m.provider_name))).sort(),
        [enabledModels],
    );

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
        return result;
    }, [enabledModels, providerFilter, search]);

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
                (onChange as (s: string[]) => void)(current.filter((v) => v !== val));
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

            {/* Search + provider filters */}
            <div className="flex items-center gap-3 flex-wrap">
                <div className="relative">
                    <Search
                        size={14}
                        className="absolute left-2.5 top-1/2 -translate-y-1/2 text-(--text-muted)"
                    />
                    <input
                        type="text"
                        placeholder="Filter models..."
                        value={search}
                        onChange={(e) => setSearch(e.target.value)}
                        className="ui-input pl-8 pr-3 py-1.5 text-xs h-8 w-56"
                    />
                </div>
                <div className="flex flex-wrap gap-1.5">
                    {providers.map((provider) => {
                        const active = providerFilter.has(provider);
                        return (
                            <button
                                key={provider}
                                onClick={() => toggleProvider(provider)}
                                className={`inline-flex items-center px-2 py-0.5 rounded text-[10px] font-medium border transition-colors ${
                                    active
                                        ? "bg-(--accent-light) text-(--accent) border-(--accent-lighter) shadow-[0_0_6px_1px_rgba(129,140,248,0.25)]"
                                        : "bg-(--surface-hover) text-(--text-tertiary) border-(--border-subtle) hover:text-(--text-secondary)"
                                }`}
                            >
                                {provider}
                            </button>
                        );
                    })}
                    {providerFilter.size > 0 && (
                        <button
                            onClick={() => setProviderFilter(new Set())}
                            className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium text-(--text-muted) hover:text-(--text-secondary)"
                        >
                            <X size={10} />
                        </button>
                    )}
                </div>
            </div>

            {/* Model pills — clamped height */}
            <div className="flex flex-wrap gap-1.5 max-h-40 overflow-y-auto pr-1">
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
