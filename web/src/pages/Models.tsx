import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import { useState, useMemo, useCallback, useEffect, useRef } from "react";
import type { Model, ModelCapabilities } from "../api/types";
import { useToast } from "../context/ToastContext";
import { SortableHeader, Row, EmptyRow } from "../components/DataTable";
import { ConfirmDialog } from "../components/ConfirmDialog";
import type { SortState } from "../components/DataTable";

function normalizeProviderName(name: string): string {
    return name.replace(/ /g, "-");
}

function proxyModelID(providerName: string, modelId: string): string {
    return normalizeProviderName(providerName) + "/" + modelId;
}

function formatRelativeTime(dateStr: string): string {
    const date = new Date(dateStr);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMin = Math.floor(diffMs / 60000);
    if (diffMin < 1) return "just now";
    if (diffMin < 60) return `${diffMin}m ago`;
    const diffHr = Math.floor(diffMin / 60);
    if (diffHr < 24) return `${diffHr}h ago`;
    const diffDay = Math.floor(diffHr / 24);
    return `${diffDay}d ago`;
}

function formatNumber(n: number | null | undefined): string {
    if (n == null) return "-";
    return n.toLocaleString();
}

function parseCapabilities(raw: string): ModelCapabilities | null {
    try {
        return JSON.parse(raw);
    } catch {
        return null;
    }
}

function parseParams(raw: string): Record<string, unknown> | null {
    try {
        return JSON.parse(raw);
    } catch {
        return null;
    }
}

type CapKey =
    | "vision"
    | "reasoning"
    | "tool_calling"
    | "structured_output"
    | "pdf_upload"
    | "video_input"
    | "audio_input"
    | "parallel_tool_calls";

const CAP_META: {
    key: CapKey;
    label: string;
    style: string;
    muted: string;
    disabled: string;
}[] = [
    {
        key: "vision",
        label: "Vision",
        style: "bg-purple-900/40 text-purple-300 border-purple-700/50 shadow-[0_0_6px_1px_rgba(147,51,234,0.35)]",
        muted: "bg-purple-900/15 text-purple-500/60 border-purple-700/25 hover:bg-purple-900/25 hover:text-purple-400",
        disabled:
            "bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50",
    },
    {
        key: "reasoning",
        label: "Reasoning",
        style: "bg-amber-900/40 text-amber-300 border-amber-700/50 shadow-[0_0_6px_1px_rgba(245,158,11,0.35)]",
        muted: "bg-amber-900/15 text-amber-500/60 border-amber-700/25 hover:bg-amber-900/25 hover:text-amber-400",
        disabled:
            "bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50",
    },
    {
        key: "tool_calling",
        label: "Tools",
        style: "bg-cyan-900/40 text-cyan-300 border-cyan-700/50 shadow-[0_0_6px_1px_rgba(6,182,212,0.35)]",
        muted: "bg-cyan-900/15 text-cyan-500/60 border-cyan-700/25 hover:bg-cyan-900/25 hover:text-cyan-400",
        disabled:
            "bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50",
    },
    {
        key: "structured_output",
        label: "Structured",
        style: "bg-emerald-900/40 text-emerald-300 border-emerald-700/50 shadow-[0_0_6px_1px_rgba(16,185,129,0.35)]",
        muted: "bg-emerald-900/15 text-emerald-500/60 border-emerald-700/25 hover:bg-emerald-900/25 hover:text-emerald-400",
        disabled:
            "bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50",
    },
    {
        key: "pdf_upload",
        label: "PDF",
        style: "bg-red-900/40 text-red-300 border-red-700/50 shadow-[0_0_6px_1px_rgba(239,68,68,0.35)]",
        muted: "bg-red-900/15 text-red-500/60 border-red-700/25 hover:bg-red-900/25 hover:text-red-400",
        disabled:
            "bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50",
    },
    {
        key: "video_input",
        label: "Video",
        style: "bg-pink-900/40 text-pink-300 border-pink-700/50 shadow-[0_0_6px_1px_rgba(236,72,153,0.35)]",
        muted: "bg-pink-900/15 text-pink-500/60 border-pink-700/25 hover:bg-pink-900/25 hover:text-pink-400",
        disabled:
            "bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50",
    },
    {
        key: "audio_input",
        label: "Audio",
        style: "bg-orange-900/40 text-orange-300 border-orange-700/50 shadow-[0_0_6px_1px_rgba(249,115,22,0.35)]",
        muted: "bg-orange-900/15 text-orange-500/60 border-orange-700/25 hover:bg-orange-900/25 hover:text-orange-400",
        disabled:
            "bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50",
    },
    {
        key: "parallel_tool_calls",
        label: "Parallel",
        style: "bg-teal-900/40 text-teal-300 border-teal-700/50 shadow-[0_0_6px_1px_rgba(20,184,166,0.35)]",
        muted: "bg-teal-900/15 text-teal-500/60 border-teal-700/25 hover:bg-teal-900/25 hover:text-teal-400",
        disabled:
            "bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50",
    },
];

function hasCap(caps: ModelCapabilities | null, key: CapKey): boolean {
    if (!caps) return false;
    return !!caps[key];
}

function CapBadge({
    caps,
    capKey,
}: {
    caps: ModelCapabilities | null;
    capKey: CapKey;
}) {
    const meta = CAP_META.find((m) => m.key === capKey);
    if (!meta || !hasCap(caps, capKey)) return null;
    return (
        <span
            className={`inline-flex items-center px-1.5 py-0.5 rounded text-[11px] font-medium border mr-1 ${meta.style}`}
        >
            {meta.label}
        </span>
    );
}

type SortField =
    | "name"
    | "capabilities"
    | "provider"
    | "discovered"
    | "context"
    | "output"
    | "status";
type StatusFilter = "enabled" | "disabled";

function ModelDetailModal({
    model,
    onClose,
    onToggle,
    onDiscover,
    onTest,
    onToast,
    onUpdate,
}: {
    model: Model;
    onClose: () => void;
    onToggle: (id: string, enabled: boolean) => void;
    onDiscover: (providerId: string) => Promise<unknown>;
    onTest: (
        id: string,
    ) => Promise<{
        success: boolean;
        ttft_ms: number;
        duration_ms: number;
        response: string;
        error?: string;
    }>;
    onToast: (msg: string, type?: "success" | "error" | "info") => void;
    onUpdate: (id: string, updates: Partial<Model>) => void;
}) {
    const caps = parseCapabilities(model.capabilities);
    const params = parseParams(model.params);
    const inputMods = (() => {
        try {
            const v = JSON.parse(model.input_modalities);
            return Array.isArray(v) ? v : [v];
        } catch {
            return [];
        }
    })();
    const outputMods = (() => {
        try {
            const v = JSON.parse(model.output_modalities);
            return Array.isArray(v) ? v : [v];
        } catch {
            return [];
        }
    })();
    const [cooldown, setCooldown] = useState(0);
    const [discovering, setDiscovering] = useState(false);
    const [testing, setTesting] = useState(false);
    const [testError, setTestError] = useState(false);
    const [snippetTab, setSnippetTab] = useState<"curl" | "zed">("curl");
    const [editing, setEditing] = useState(false);
    const [confirmFields, setConfirmFields] = useState<string[] | null>(null);
    const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

    const [editData, setEditData] = useState({
        display_name: model.display_name || "",
        context_length: model.context_length?.toString() || "",
        max_output_tokens: model.max_output_tokens?.toString() || "",
        input_price_per_million:
            model.input_price_per_million?.toString() || "",
        output_price_per_million:
            model.output_price_per_million?.toString() || "",
    });

    const discoveredDefaults = useMemo(
        () => ({
            display_name: model.name || "",
            context_length: model.context_length,
            max_output_tokens: model.max_output_tokens,
            input_price_per_million: model.input_price_per_million,
            output_price_per_million: model.output_price_per_million,
        }),
        [model],
    );

    useEffect(() => {
        return () => {
            if (timerRef.current) clearInterval(timerRef.current);
        };
    }, []);

    const handleDiscover = async () => {
        if (cooldown > 0 || discovering) return;
        setDiscovering(true);
        try {
            await onDiscover(model.provider_id);
            setCooldown(30);
            timerRef.current = setInterval(() => {
                setCooldown((prev) => {
                    if (prev <= 1) {
                        if (timerRef.current) clearInterval(timerRef.current);
                        return 0;
                    }
                    return prev - 1;
                });
            }, 1000);
        } finally {
            setDiscovering(false);
        }
    };

    const handleTest = async () => {
        if (testing) return;
        setTesting(true);
        setTestError(false);
        try {
            const result = await onTest(model.id);
            if (result.success) {
                const content = result.response
                    .replace(/\n/g, " ")
                    .slice(0, 80);
                onToast(
                    `Success | Response: ${content} | TTFT: ${(result.ttft_ms / 1000).toFixed(1)}s | Duration: ${(result.duration_ms / 1000).toFixed(1)}s`,
                    "success",
                );
            } else {
                setTestError(true);
                onToast(
                    `Test failed: ${result.error || "Unknown error"}`,
                    "error",
                );
                setTimeout(() => setTestError(false), 3000);
            }
        } catch (err) {
            setTestError(true);
            onToast(
                `Test failed: ${err instanceof Error ? err.message : "Unknown error"}`,
                "error",
            );
            setTimeout(() => setTestError(false), 3000);
        } finally {
            setTesting(false);
        }
    };

    const getFieldLabel = (key: string): string => {
        const labels: Record<string, string> = {
            display_name: "Display Name",
            context_length: "Context Length",
            max_output_tokens: "Max Output Tokens",
            input_price_per_million: "Input Price",
            output_price_per_million: "Output Price",
        };
        return labels[key] || key;
    };

    const getChangedFields = (): string[] => {
        const fields: string[] = [];
        if (editData.display_name !== (model.display_name || ""))
            fields.push("display_name");
        const cl =
            editData.context_length === ""
                ? null
                : Number(editData.context_length);
        if (cl !== model.context_length) fields.push("context_length");
        const mot =
            editData.max_output_tokens === ""
                ? null
                : Number(editData.max_output_tokens);
        if (mot !== model.max_output_tokens) fields.push("max_output_tokens");
        const ipm =
            editData.input_price_per_million === ""
                ? null
                : Number(editData.input_price_per_million);
        if (ipm !== model.input_price_per_million)
            fields.push("input_price_per_million");
        const opm =
            editData.output_price_per_million === ""
                ? null
                : Number(editData.output_price_per_million);
        if (opm !== model.output_price_per_million)
            fields.push("output_price_per_million");
        return fields;
    };

    const handleCancelEdit = () => {
        const changed = getChangedFields();
        if (changed.length > 0) {
            setConfirmFields(changed.map(getFieldLabel));
        } else {
            setEditing(false);
        }
    };

    const handleSave = () => {
        const updates: Record<string, unknown> = {};
        if (editData.display_name !== (model.display_name || ""))
            updates.display_name = editData.display_name;
        const cl =
            editData.context_length === ""
                ? null
                : Number(editData.context_length);
        if (cl !== model.context_length) updates.context_length = cl;
        const mot =
            editData.max_output_tokens === ""
                ? null
                : Number(editData.max_output_tokens);
        if (mot !== model.max_output_tokens) updates.max_output_tokens = mot;
        const ipm =
            editData.input_price_per_million === ""
                ? null
                : Number(editData.input_price_per_million);
        if (ipm !== model.input_price_per_million)
            updates.input_price_per_million = ipm;
        const opm =
            editData.output_price_per_million === ""
                ? null
                : Number(editData.output_price_per_million);
        if (opm !== model.output_price_per_million)
            updates.output_price_per_million = opm;
        if (Object.keys(updates).length > 0) {
            onUpdate(model.id, updates as Partial<Model>);
        }
        setEditing(false);
    };

    const revertField = (key: keyof typeof discoveredDefaults) => {
        if (key === "display_name") {
            setEditData((prev) => ({
                ...prev,
                display_name: discoveredDefaults.display_name,
            }));
        } else if (key === "context_length") {
            setEditData((prev) => ({
                ...prev,
                context_length:
                    discoveredDefaults.context_length?.toString() ?? "",
            }));
        } else if (key === "max_output_tokens") {
            setEditData((prev) => ({
                ...prev,
                max_output_tokens:
                    discoveredDefaults.max_output_tokens?.toString() ?? "",
            }));
        } else if (key === "input_price_per_million") {
            setEditData((prev) => ({
                ...prev,
                input_price_per_million:
                    discoveredDefaults.input_price_per_million?.toString() ??
                    "",
            }));
        } else if (key === "output_price_per_million") {
            setEditData((prev) => ({
                ...prev,
                output_price_per_million:
                    discoveredDefaults.output_price_per_million?.toString() ??
                    "",
            }));
        }
    };

    const handleClose = () => {
        if (editing) {
            handleCancelEdit();
        } else {
            onClose();
        }
    };

    const pMid = proxyModelID(model.provider_name, model.model_id);

    const curlCmd = `curl -X POST ${window.location.origin}/v1/chat/completions \\\n  -H "Authorization: Bearer API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{"model":"${pMid}","messages":[{"role":"user","content":"Hello"}]}'`;

    const zedJson = JSON.stringify(
        {
            name: pMid,
            display_name: model.name,
            max_tokens: model.context_length,
            max_output_tokens: model.max_output_tokens,
            capabilities: {
                tools: hasCap(caps, "tool_calling"),
                images: hasCap(caps, "vision"),
                parallel_tool_calls: hasCap(caps, "parallel_tool_calls"),
                prompt_cache_key: false,
            },
        },
        null,
        2,
    );

    const snippetContent = snippetTab === "curl" ? curlCmd : zedJson;

    return (
        <div
            role="dialog"
            aria-modal="true"
            className="fixed inset-0 flex items-center justify-center z-50"
            onKeyDown={(e) => {
                if (e.key === "Escape") handleClose();
            }}
        >
            <button
                type="button"
                className="absolute inset-0 bg-black/60 cursor-default"
                onClick={handleClose}
                aria-label="Close dialog"
            />
            <div className="relative ui-card p-6 w-full max-w-lg max-h-[85vh] overflow-y-auto">
                <div className="flex justify-between items-start mb-4">
                    <div>
                        <h2 className="text-xl font-bold text-white">
                            {model.display_name || model.name || pMid}
                        </h2>
                        <p className="text-sm text-gray-400 mt-1 font-mono">
                            {pMid}
                        </p>
                    </div>
                    <button
                        type="button"
                        onClick={handleClose}
                        className="absolute top-4 right-4 text-(--text-secondary) hover:text-(--text-primary) transition-all cursor-default text-xl leading-none hover:drop-shadow-[0_0_8px_var(--accent)]"
                        aria-label="Close"
                    >
                        &times;
                    </button>
                </div>

                {model.description && (
                    <p className="text-sm text-gray-300 mb-4">
                        {model.description}
                    </p>
                )}

                <div className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm mb-4">
                    <div>
                        <span className="text-gray-500">Provider</span>
                        <p className="text-gray-200">{model.provider_name}</p>
                    </div>
                    <div>
                        <span className="text-gray-500">Last Discovered</span>
                        <p className="text-gray-200">
                            {formatRelativeTime(model.last_seen_at)}
                        </p>
                    </div>
                    <div>
                        <span className="text-gray-500">Display Name</span>
                        {editing ? (
                            <div className="flex items-center gap-1">
                                <input
                                    type="text"
                                    value={editData.display_name}
                                    onChange={(e) =>
                                        setEditData((prev) => ({
                                            ...prev,
                                            display_name: e.target.value,
                                        }))
                                    }
                                    className="ui-input text-sm"
                                />
                                {editData.display_name !==
                                    discoveredDefaults.display_name && (
                                    <button
                                        type="button"
                                        onClick={() =>
                                            revertField("display_name")
                                        }
                                        className="text-[10px] px-1.5 py-0.5 rounded bg-gray-700 text-gray-400 hover:text-white border border-gray-600 cursor-pointer"
                                        title="Revert to discovered value"
                                    >
                                        ↩
                                    </button>
                                )}
                            </div>
                        ) : (
                            <p className="text-gray-200">
                                {model.display_name || model.name || "-"}
                            </p>
                        )}
                    </div>
                    <div>
                        <span className="text-gray-500">Context Length</span>
                        {editing ? (
                            <div className="flex items-center gap-1">
                                <input
                                    type="number"
                                    value={editData.context_length}
                                    onChange={(e) =>
                                        setEditData((prev) => ({
                                            ...prev,
                                            context_length: e.target.value,
                                        }))
                                    }
                                    className="ui-input text-sm"
                                    placeholder="tokens"
                                />
                                {editData.context_length !==
                                    (discoveredDefaults.context_length?.toString() ??
                                        "") && (
                                    <button
                                        type="button"
                                        onClick={() =>
                                            revertField("context_length")
                                        }
                                        className="text-[10px] px-1.5 py-0.5 rounded bg-gray-700 text-gray-400 hover:text-white border border-gray-600 cursor-pointer"
                                        title="Revert to discovered value"
                                    >
                                        ↩
                                    </button>
                                )}
                            </div>
                        ) : (
                            <p className="text-gray-200">
                                {formatNumber(model.context_length)} tokens
                            </p>
                        )}
                    </div>
                    <div>
                        <span className="text-gray-500">Max Output</span>
                        {editing ? (
                            <div className="flex items-center gap-1">
                                <input
                                    type="number"
                                    value={editData.max_output_tokens}
                                    onChange={(e) =>
                                        setEditData((prev) => ({
                                            ...prev,
                                            max_output_tokens: e.target.value,
                                        }))
                                    }
                                    className="ui-input text-sm"
                                    placeholder="tokens"
                                />
                                {editData.max_output_tokens !==
                                    (discoveredDefaults.max_output_tokens?.toString() ??
                                        "") && (
                                    <button
                                        type="button"
                                        onClick={() =>
                                            revertField("max_output_tokens")
                                        }
                                        className="text-[10px] px-1.5 py-0.5 rounded bg-gray-700 text-gray-400 hover:text-white border border-gray-600 cursor-pointer"
                                        title="Revert to discovered value"
                                    >
                                        ↩
                                    </button>
                                )}
                            </div>
                        ) : (
                            <p className="text-gray-200">
                                {formatNumber(model.max_output_tokens)} tokens
                            </p>
                        )}
                    </div>
                    <div>
                        <span className="text-gray-500">Input Price</span>
                        {editing ? (
                            <div className="flex items-center gap-1">
                                <div className="relative w-full">
                                    <input
                                        type="number"
                                        step="0.01"
                                        value={editData.input_price_per_million}
                                        onChange={(e) =>
                                            setEditData((prev) => ({
                                                ...prev,
                                                input_price_per_million:
                                                    e.target.value,
                                            }))
                                        }
                                        className="ui-input text-sm pr-16"
                                        placeholder="0.00"
                                    />
                                    <span className="absolute right-2 top-1/2 -translate-y-1/2 text-[10px] text-gray-400">
                                        /1M tok
                                    </span>
                                </div>
                                {editData.input_price_per_million !==
                                    (discoveredDefaults.input_price_per_million?.toString() ??
                                        "") && (
                                    <button
                                        type="button"
                                        onClick={() =>
                                            revertField(
                                                "input_price_per_million",
                                            )
                                        }
                                        className="text-[10px] px-1.5 py-0.5 rounded bg-gray-700 text-gray-400 hover:text-white border border-gray-600 cursor-pointer shrink-0"
                                        title="Revert to discovered value"
                                    >
                                        ↩
                                    </button>
                                )}
                            </div>
                        ) : (
                            <p className="text-gray-200">
                                {model.input_price_per_million != null
                                    ? `$${model.input_price_per_million}/1M`
                                    : "-"}
                            </p>
                        )}
                    </div>
                    <div>
                        <span className="text-gray-500">Output Price</span>
                        {editing ? (
                            <div className="flex items-center gap-1">
                                <div className="relative w-full">
                                    <input
                                        type="number"
                                        step="0.01"
                                        value={
                                            editData.output_price_per_million
                                        }
                                        onChange={(e) =>
                                            setEditData((prev) => ({
                                                ...prev,
                                                output_price_per_million:
                                                    e.target.value,
                                            }))
                                        }
                                        className="ui-input text-sm pr-16"
                                        placeholder="0.00"
                                    />
                                    <span className="absolute right-2 top-1/2 -translate-y-1/2 text-[10px] text-gray-400">
                                        /1M tok
                                    </span>
                                </div>
                                {editData.output_price_per_million !==
                                    (discoveredDefaults.output_price_per_million?.toString() ??
                                        "") && (
                                    <button
                                        type="button"
                                        onClick={() =>
                                            revertField(
                                                "output_price_per_million",
                                            )
                                        }
                                        className="text-[10px] px-1.5 py-0.5 rounded bg-gray-700 text-gray-400 hover:text-white border border-gray-600 cursor-pointer shrink-0"
                                        title="Revert to discovered value"
                                    >
                                        ↩
                                    </button>
                                )}
                            </div>
                        ) : (
                            <p className="text-gray-200">
                                {model.output_price_per_million != null
                                    ? `$${model.output_price_per_million}/1M`
                                    : "-"}
                            </p>
                        )}
                    </div>
                    <div>
                        <span className="text-gray-500">Input</span>
                        <p className="text-gray-200">
                            {inputMods.join(", ") || "text"}
                        </p>
                    </div>
                    <div>
                        <span className="text-gray-500">Output</span>
                        <p className="text-gray-200">
                            {outputMods.join(", ") || "text"}
                        </p>
                    </div>
                </div>

                {caps && (
                    <div className="mb-4">
                        <h3 className="text-sm font-medium text-gray-400 mb-2">
                            Capabilities
                        </h3>
                        <div className="flex flex-wrap gap-1">
                            {CAP_META.map((m) => (
                                <CapBadge
                                    key={m.key}
                                    caps={caps}
                                    capKey={m.key}
                                />
                            ))}
                        </div>
                        {!CAP_META.some((m) => hasCap(caps, m.key)) && (
                            <p className="text-sm text-gray-500">
                                No special capabilities detected
                            </p>
                        )}
                    </div>
                )}

                {params && params.subscription_included !== undefined && (
                    <div className="mb-4">
                        <h3 className="text-sm font-medium text-gray-400 mb-2">
                            Subscription
                        </h3>
                        <div className="flex items-center gap-2">
                            <span
                                className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${
                                    params.subscription_included
                                        ? "bg-green-900/40 text-green-300 border border-green-700/50"
                                        : "bg-yellow-900/40 text-yellow-300 border border-yellow-700/50"
                                }`}
                            >
                                {params.subscription_included
                                    ? "Included"
                                    : "Not included"}
                            </span>
                            {params.subscription_note ? (
                                <span className="text-sm text-gray-500">
                                    {String(params.subscription_note)}
                                </span>
                            ) : null}
                        </div>
                    </div>
                )}

                {editing && (
                    <div className="flex gap-3 justify-end mb-4 pt-2 border-t border-gray-700">
                        <button
                            type="button"
                            onClick={handleCancelEdit}
                            className="px-4 py-2 bg-gray-700 text-gray-300 rounded-lg hover:bg-gray-600 transition-colors"
                        >
                            Cancel
                        </button>
                        <button
                            type="button"
                            onClick={handleSave}
                            className="px-4 py-2 bg-(--accent) text-white rounded-lg hover:bg-(--accent) transition-colors"
                        >
                            Save Changes
                        </button>
                    </div>
                )}

                <div className="mt-4 pt-4 border-t border-gray-700">
                    <div className="flex items-center justify-between mb-3">
                        <div className="flex items-center gap-1">
                            {(["curl", "zed"] as const).map((tab) => (
                                <button
                                    key={tab}
                                    type="button"
                                    onClick={() => setSnippetTab(tab)}
                                    className={`px-2.5 py-1 rounded text-[11px] font-medium uppercase tracking-wider cursor-pointer transition-all ${
                                        snippetTab === tab
                                            ? "bg-slate-700/60 text-slate-200 border border-slate-600/50"
                                            : "text-slate-500 hover:text-slate-400 border border-transparent"
                                    }`}
                                >
                                    {tab === "curl" ? "cURL" : "ZED"}
                                </button>
                            ))}
                        </div>
                        <button
                            type="button"
                            onClick={() => {
                                navigator.clipboard.writeText(snippetContent);
                                onToast("Copied to clipboard", "info");
                            }}
                            className="px-1.5 py-0.5 rounded text-[10px] font-medium border bg-slate-700/40 text-slate-300 border-slate-600/40 hover:brightness-125 transition-all cursor-pointer"
                        >
                            Copy
                        </button>
                    </div>
                    <pre className="bg-gray-950 rounded-lg p-3 text-[11px] text-gray-300 font-mono overflow-x-auto leading-relaxed whitespace-pre-wrap break-all">
                        {snippetContent}
                    </pre>
                </div>

                <div className="flex items-center justify-between mt-4 pt-4">
                    <div className="flex items-center gap-2">
                        <button
                            type="button"
                            onClick={() => onToggle(model.id, !model.enabled)}
                            className={`px-3 py-1.5 text-xs rounded-full border cursor-pointer transition-all ${
                                model.enabled
                                    ? "bg-green-900/50 text-green-400 border-green-700/50 hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(34,197,94,0.2)]"
                                    : "bg-red-900/50 text-red-400 border-red-700/50 hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(239,68,68,0.2)]"
                            }`}
                        >
                            {model.enabled ? "Enabled" : "Disabled"}
                        </button>
                        <button
                            type="button"
                            disabled={testing}
                            onClick={handleTest}
                            className={`px-3 py-1.5 text-xs rounded-full border transition-all flex items-center gap-1.5 ${
                                testError
                                    ? "bg-red-900/50 text-red-300 border-red-700/50"
                                    : testing
                                      ? "bg-amber-900/30 text-amber-300/70 border-amber-700/30 cursor-wait"
                                      : "bg-amber-900/40 text-amber-300 border-amber-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(245,158,11,0.2)]"
                            }`}
                        >
                            {testing && (
                                <span className="inline-block w-3 h-3 border-2 border-amber-400/50 border-t-amber-300 rounded-full animate-spin" />
                            )}
                            {testing ? "Testing..." : "Test"}
                        </button>
                    </div>
                    <div className="flex items-center gap-2">
                        {!editing && (
                            <button
                                type="button"
                                onClick={() => setEditing(true)}
                                className="px-3 py-1.5 text-xs rounded-full border bg-gray-900/40 text-gray-300 border-gray-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(156,163,175,0.15)] transition-all"
                            >
                                Edit
                            </button>
                        )}
                        <button
                            type="button"
                            disabled={cooldown > 0 || discovering}
                            onClick={handleDiscover}
                            className={`px-3 py-1.5 text-xs rounded-full border transition-all ${
                                cooldown > 0 || discovering
                                    ? "bg-(--accent-lighter) text-(--accent)/50 border-(--accent-light) cursor-not-allowed"
                                    : "bg-(--accent-light) text-(--accent) border-(--accent-lighter) cursor-pointer hover:brightness-125"
                            }`}
                        >
                            {discovering
                                ? "Updating..."
                                : cooldown > 0
                                  ? `Update (${cooldown}s)`
                                  : "Update info"}
                        </button>
                    </div>
                </div>
            </div>
            {confirmFields && (
                <ConfirmDialog
                    title="Unsaved Changes"
                    fields={confirmFields}
                    onConfirm={() => {
                        setConfirmFields(null);
                        setEditing(false);
                        setEditData({
                            display_name: model.display_name || "",
                            context_length:
                                model.context_length?.toString() || "",
                            max_output_tokens:
                                model.max_output_tokens?.toString() || "",
                            input_price_per_million:
                                model.input_price_per_million?.toString() || "",
                            output_price_per_million:
                                model.output_price_per_million?.toString() ||
                                "",
                        });
                    }}
                    onCancel={() => setConfirmFields(null)}
                />
            )}
        </div>
    );
}

function matchesAllCaps(
    caps: ModelCapabilities | null,
    keys: Set<CapKey>,
): boolean {
    if (keys.size === 0) return true;
    for (const k of keys) {
        if (!hasCap(caps, k)) return false;
    }
    return true;
}

export function Models() {
    const { toast } = useToast();
    const queryClient = useQueryClient();
    const [searchQuery, setSearchQuery] = useState("");
    const [selectedProvider, setSelectedProvider] = useState<string>("");
    const [detailModel, setDetailModel] = useState<Model | null>(null);
    const [sort, setSort] = useState<SortState<SortField>>({
        field: "name",
        dir: "asc",
    });
    const [capFilter, setCapFilter] = useState<Set<CapKey>>(new Set());
    const [statusFilter, setStatusFilter] = useState<StatusFilter>("enabled");
    const [pageSize, setPageSize] = useState(25);
    const [currentPage, setCurrentPage] = useState(1);

    const { data: models, isLoading } = useQuery({
        queryKey: ["models", selectedProvider],
        queryFn: () => api.models.list(selectedProvider || undefined),
    });

    const { data: providers } = useQuery({
        queryKey: ["providers"],
        queryFn: () => api.providers.list(),
    });

    const toggleMutation = useMutation({
        mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
            api.models.update(id, { enabled }),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["models"] });
        },
        onError: (err: Error) => {
            toast(`Failed to update model: ${err.message}`, "error");
        },
    });

    const updateMutation = useMutation({
        mutationFn: ({
            id,
            data,
        }: {
            id: string;
            data: Record<string, unknown>;
        }) =>
            api.models.update(
                id,
                data as Parameters<typeof api.models.update>[1],
            ),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["models"] });
            toast("Model updated", "success");
        },
        onError: (err: Error) => {
            toast(`Failed to update model: ${err.message}`, "error");
        },
    });

    const handleToggleModel = useCallback(
        (id: string, enabled: boolean) => {
            toggleMutation.mutate(
                { id, enabled },
                {
                    onSuccess: () => {
                        toast(
                            enabled ? "Model enabled" : "Model disabled",
                            enabled ? "success" : "error",
                        );
                        setDetailModel((prev) =>
                            prev ? { ...prev, enabled } : null,
                        );
                    },
                },
            );
        },
        [toggleMutation, toast],
    );

    const handleUpdateModel = useCallback(
        (id: string, updates: Partial<Model>) => {
            updateMutation.mutate(
                { id, data: updates },
                {
                    onSuccess: () => {
                        setDetailModel((prev) =>
                            prev ? { ...prev, ...updates } : null,
                        );
                    },
                },
            );
        },
        [updateMutation],
    );

    const handleDiscover = useCallback(
        async (providerId: string) => {
            toast("Discovering models...", "info");
            const result = await api.providers.discover(providerId);
            queryClient.invalidateQueries({ queryKey: ["models"] });
            queryClient.invalidateQueries({ queryKey: ["providers"] });
            toast(
                `Discovered ${result?.discovered ?? "new"} models`,
                "success",
            );
            return result;
        },
        [queryClient, toast],
    );

    const handleTest = useCallback(async (id: string) => {
        return api.models.test(id);
    }, []);

    const copyModelId = useCallback(
        (modelId: string) => {
            navigator.clipboard
                .writeText(modelId)
                .then(() => {
                    toast(`Copied: ${modelId}`, "info");
                })
                .catch(() => {
                    toast("Failed to copy", "error");
                });
        },
        [toast],
    );

    const toggleCapFilter = useCallback((key: CapKey) => {
        setCapFilter((prev) => {
            const next = new Set(prev);
            if (next.has(key)) next.delete(key);
            else next.add(key);
            return next;
        });
        setCurrentPage(1);
    }, []);

    const handleSort = (field: SortField) => {
        setSort((prev) => ({
            field,
            dir: prev.field === field && prev.dir === "asc" ? "desc" : "asc",
        }));
    };

    const { sortedAndFiltered, pillAvailability, existingCaps } =
        useMemo(() => {
            const baseFiltered =
                models?.filter(
                        (model) =>
                        proxyModelID(model.provider_name, model.model_id)
                            .toLowerCase()
                            .includes(searchQuery.toLowerCase()) ||
                        model.name
                            ?.toLowerCase()
                            .includes(searchQuery.toLowerCase()) ||
                        model.display_name
                            ?.toLowerCase()
                            .includes(searchQuery.toLowerCase()),
                ) || [];

            const capsInData = new Set<CapKey>();
            for (const m of baseFiltered) {
                const c = parseCapabilities(m.capabilities);
                for (const meta of CAP_META) {
                    if (hasCap(c, meta.key)) capsInData.add(meta.key);
                }
            }

            let filtered = baseFiltered;

            if (capFilter.size > 0) {
                filtered = filtered.filter((m) =>
                    matchesAllCaps(
                        parseCapabilities(m.capabilities),
                        capFilter,
                    ),
                );
            }

            filtered = filtered.filter((m) =>
                statusFilter === "enabled" ? m.enabled : !m.enabled,
            );

            const availability = new Map<CapKey, boolean>();
            for (const m of CAP_META) {
                const testFilter = new Set(capFilter);
                testFilter.add(m.key);
                const hasMatch = baseFiltered.some((model) =>
                    matchesAllCaps(
                        parseCapabilities(model.capabilities),
                        testFilter,
                    ),
                );
                availability.set(m.key, hasMatch);
            }

            const dir = sort.dir === "asc" ? 1 : -1;
            filtered.sort((a, b) => {
                switch (sort.field) {
                    case "name":
                        return (
                            dir *
                            (a.name || proxyModelID(a.provider_name, a.model_id)).localeCompare(
                                b.name || proxyModelID(b.provider_name, b.model_id),
                            )
                        );
                    case "provider":
                        return (
                            dir * a.provider_name.localeCompare(b.provider_name)
                        );
                    case "discovered":
                        return (
                            dir *
                            (new Date(a.last_seen_at).getTime() -
                                new Date(b.last_seen_at).getTime())
                        );
                    case "context":
                        return (
                            dir *
                            ((a.context_length ?? 0) - (b.context_length ?? 0))
                        );
                    case "output":
                        return (
                            dir *
                            ((a.max_output_tokens ?? 0) -
                                (b.max_output_tokens ?? 0))
                        );
                    case "status":
                        return (
                            dir *
                            (a.enabled === b.enabled ? 0 : a.enabled ? -1 : 1)
                        );
                    default:
                        return 0;
                }
            });

            return {
                sortedAndFiltered: filtered,
                pillAvailability: availability,
                existingCaps: capsInData,
            };
        }, [models, searchQuery, sort, capFilter, statusFilter]);

    const totalPages = Math.ceil(sortedAndFiltered.length / pageSize);
    const paginatedModels = sortedAndFiltered.slice(
        (currentPage - 1) * pageSize,
        currentPage * pageSize,
    );

    if (isLoading) {
        return (
            <div className="flex items-center justify-center h-64">
                <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-(--accent)"></div>
            </div>
        );
    }

    const totalEnabled = models?.filter((m) => m.enabled).length ?? 0;
    const totalDisabled = (models?.length ?? 0) - totalEnabled;
    const allSameState = totalEnabled === 0 || totalDisabled === 0;

    return (
        <div className="space-y-4">
            <div>
                <div className="flex items-center gap-3">
                    <h1 className="text-3xl font-bold text-white">
                        {models?.length ?? 0} Models
                    </h1>
                    {!allSameState && (
                        <span className="inline-flex items-center gap-2 px-2.5 py-1 rounded-full text-xs font-medium bg-gray-700/60 border border-gray-600/50">
                            <span className="text-green-400">
                                {totalEnabled} enabled
                            </span>
                            <span className="text-gray-600">/</span>
                            <span className="text-red-400">
                                {totalDisabled} disabled
                            </span>
                        </span>
                    )}
                </div>
                <p className="text-gray-400 mt-1">
                    Discovered LLM models from your providers
                </p>
            </div>

            <div className="flex flex-col md:flex-row gap-4">
                <div className="flex-1 flex gap-2">
                    <input
                        type="text"
                        placeholder="Search models..."
                        autoFocus={true}
                        value={searchQuery}
                        onChange={(e) => {
                            setSearchQuery(e.target.value);
                            setCurrentPage(1);
                        }}
                        className="ui-input"
                    />
                </div>
                <div className="md:w-64">
                    <select
                        value={selectedProvider}
                        onChange={(e) => {
                            setSelectedProvider(e.target.value);
                            setCurrentPage(1);
                        }}
                        className="hidden ui-input"
                    >
                        <option value="">All Providers</option>
                        {providers?.map((provider) => (
                            <option key={provider.id} value={provider.id}>
                                {provider.name}
                            </option>
                        ))}
                    </select>
                </div>
            </div>

            <div className="ui-card overflow-hidden">
                <table className="min-w-full table-fixed ui-table">
                    <colgroup>
                        <col className="w-[22%]" />
                        <col className="w-[26%]" />
                        <col className="w-[12%]" />
                        <col className="w-[11%]" />
                        <col className="w-[9%]" />
                        <col className="w-[9%]" />
                        <col className="w-[11%]" />
                    </colgroup>
                    <thead>
                        <tr>
                            <SortableHeader
                                label="Model"
                                field="name"
                                sort={sort}
                                onSort={handleSort}
                                tooltip="Model name and ID"
                            />
                            <th className="px-4 py-2 text-left text-xs font-medium uppercase tracking-wider whitespace-nowrap ui-table-header-text select-none hover:text-gray-200">
                                Capabilities
                            </th>
                            <th className="px-4 py-2 text-left text-xs font-medium uppercase tracking-wider ui-table-header-text select-none hover:text-gray-200">
                                Provider
                            </th>
                            <SortableHeader
                                label="Discovered"
                                field="discovered"
                                sort={sort}
                                onSort={handleSort}
                                tooltip="When the model was last seen/discovered"
                            />
                            <SortableHeader
                                label="Ctx"
                                field="context"
                                sort={sort}
                                onSort={handleSort}
                                tooltip="Maximum context length in tokens"
                            />
                            <SortableHeader
                                label="Max Out"
                                field="output"
                                sort={sort}
                                onSort={handleSort}
                                tooltip="Maximum output tokens"
                            />
                            <th className="px-4 py-2 text-left text-xs font-medium uppercase tracking-wider ui-table-header-text select-none hover:text-gray-200">
                                Status
                            </th>
                        </tr>
                        <tr className="ui-table-row-filter">
                            <th className="px-4 py-2"></th>
                            <th className="px-4 py-2">
                                <span className="flex flex-wrap gap-1">
                                    {CAP_META.filter((m) =>
                                        existingCaps.has(m.key),
                                    ).map((m) => {
                                        const isActive = capFilter.has(m.key);
                                        const isAvailable =
                                            pillAvailability.get(m.key) ??
                                            false;
                                        const isDisabled =
                                            !isActive && !isAvailable;
                                        return (
                                            <button
                                                key={m.key}
                                                type="button"
                                                disabled={isDisabled}
                                                onClick={() =>
                                                    toggleCapFilter(m.key)
                                                }
                                                className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border transition-colors ${isActive ? m.style : isDisabled ? m.disabled : m.muted}`}
                                            >
                                                {m.label}
                                            </button>
                                        );
                                    })}
                                    {capFilter.size > 0 && (
                                        <button
                                            type="button"
                                            onClick={() => {
                                                setCapFilter(new Set());
                                                setCurrentPage(1);
                                            }}
                                            className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium text-gray-400 hover:text-gray-200"
                                        >
                                            ✕
                                        </button>
                                    )}
                                </span>
                            </th>
                            <th className="px-4 py-2">
                                <span className="flex flex-wrap gap-1">
                                    {providers?.map((provider) => {
                                        const isNanoGPT =
                                            provider.base_url.includes(
                                                "nano-gpt.com",
                                            );
                                        const isDeepSeek =
                                            provider.base_url.includes(
                                                "deepseek.com",
                                            );
                                        const isSelected =
                                            selectedProvider === provider.id;
                                        const baseStyle = isNanoGPT
                                            ? "bg-[#0690a8]/20 text-[#0690a8] border-[#0690a8]/50 hover:bg-[#0690a8]/30"
                                            : isDeepSeek
                                              ? "bg-[#36aaff]/20 text-[#36aaff] border-[#36aaff]/50 hover:bg-[#36aaff]/30"
                                              : "bg-gray-700 text-gray-300 border-gray-600 hover:bg-gray-600";
                                        const activeStyle = isNanoGPT
                                            ? "bg-[#0690a8] text-white border-[#0690a8] shadow-[0_0_6px_1px_rgba(6,144,168,0.35)]"
                                            : isDeepSeek
                                              ? "bg-[#36aaff] text-white border-[#36aaff] shadow-[0_0_6px_1px_rgba(54,170,255,0.35)]"
                                              : "bg-gray-900 text-white border-gray-700 shadow-[0_0_6px_1px_rgba(255,255,255,0.15)]";
                                        return (
                                            <button
                                                key={provider.id}
                                                type="button"
                                                onClick={() => {
                                                    setSelectedProvider(
                                                        isSelected
                                                            ? ""
                                                            : provider.id,
                                                    );
                                                    setCurrentPage(1);
                                                }}
                                                className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border transition-colors ${isSelected ? activeStyle : baseStyle}`}
                                            >
                                                {provider.name}
                                            </button>
                                        );
                                    })}
                                    {selectedProvider && (
                                        <button
                                            type="button"
                                            onClick={() => {
                                                setSelectedProvider("");
                                                setCurrentPage(1);
                                            }}
                                            className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium text-gray-400 hover:text-gray-200"
                                        >
                                            ✕
                                        </button>
                                    )}
                                </span>
                            </th>
                            <th className="px-4 py-2"></th>
                            <th className="px-4 py-2"></th>
                            <th className="px-4 py-2"></th>
                            <th className="px-4 py-2">
                                <span className="flex flex-wrap gap-1">
                                    <button
                                        type="button"
                                        onClick={() => {
                                            setStatusFilter("enabled");
                                            setCurrentPage(1);
                                        }}
                                        className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border transition-colors ${
                                            statusFilter === "enabled"
                                                ? "bg-green-900/40 text-green-300 border-green-700/50 shadow-[0_0_6px_1px_rgba(34,197,94,0.35)]"
                                                : "bg-green-900/15 text-green-500/60 border-green-700/25 hover:bg-green-900/25 hover:text-green-400"
                                        }`}
                                    >
                                        Enabled
                                    </button>
                                    <button
                                        type="button"
                                        onClick={() => {
                                            setStatusFilter("disabled");
                                            setCurrentPage(1);
                                        }}
                                        className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border transition-colors ${
                                            statusFilter === "disabled"
                                                ? "bg-red-900/40 text-red-300 border-red-700/50 shadow-[0_0_6px_1px_rgba(239,68,68,0.35)]"
                                                : "bg-red-900/15 text-red-500/60 border-red-700/25 hover:bg-red-900/25 hover:text-red-400"
                                        }`}
                                    >
                                        Disabled
                                    </button>
                                </span>
                            </th>
                        </tr>
                    </thead>
                    <tbody>
                        {paginatedModels.length > 0 ? (
                            paginatedModels.map((model, idx) => {
                                const caps = parseCapabilities(
                                    model.capabilities,
                                );
                                return (
                                    <Row key={model.id} index={idx}>
                                        <td className="px-4 py-1.5">
                                            <div className="flex flex-col">
                                                <button
                                                    type="button"
                                                    onClick={() =>
                                                        setDetailModel(model)
                                                    }
                                                    title="View model details"
                                                    className="text-left text-sm font-medium text-white hover:text-gray-200 cursor-pointer transition-colors"
                                                >
                                                    {model.name ||
                                                        proxyModelID(model.provider_name, model.model_id)}
                                                </button>
                                                <button
                                                    type="button"
                                                    className="text-left text-[11px] text-gray-500 font-mono leading-tight cursor-pointer hover:text-gray-300 transition-all hover:drop-shadow-[0_0_6px_var(--accent)]"
                                                    onClick={() =>
                                                        copyModelId(
                                                            proxyModelID(model.provider_name, model.model_id),
                                                        )
                                                    }
                                                    title="Click to copy model ID"
                                                >
                                                    {proxyModelID(model.provider_name, model.model_id)}
                                                </button>
                                            </div>
                                        </td>
                                        <td className="px-4 py-1.5">
                                            <div className="flex flex-wrap">
                                                {CAP_META.map((m) => (
                                                    <CapBadge
                                                        key={m.key}
                                                        caps={caps}
                                                        capKey={m.key}
                                                    />
                                                ))}
                                            </div>
                                        </td>
                                        <td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">
                                            {model.provider_name}
                                        </td>
                                        <td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-400">
                                            {formatRelativeTime(
                                                model.last_seen_at,
                                            )}
                                        </td>
                                        <td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">
                                            {model.context_length
                                                ? model.context_length.toLocaleString()
                                                : "-"}
                                        </td>
                                        <td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">
                                            {model.max_output_tokens
                                                ? model.max_output_tokens.toLocaleString()
                                                : "-"}
                                        </td>
                                        <td className="px-4 py-1.5 whitespace-nowrap">
                                            <span
                                                className={`px-2 py-0.5 text-xs rounded-full ${model.enabled ? "bg-green-900/50 text-green-400" : "bg-red-900/50 text-red-400"}`}
                                            >
                                                {model.enabled
                                                    ? "Enabled"
                                                    : "Disabled"}
                                            </span>
                                        </td>
                                    </Row>
                                );
                            })
                        ) : (
                            <EmptyRow
                                colSpan={7}
                                message={
                                    searchQuery ||
                                    selectedProvider ||
                                    capFilter.size > 0
                                        ? "No models match your filters"
                                        : "No models discovered yet. Add a provider and discover models."
                                }
                            />
                        )}
                    </tbody>
                </table>
            </div>

            {models && models.length > 0 && (
                <div className="flex items-center justify-between">
                    <div className="text-sm text-gray-500">
                        Showing {(currentPage - 1) * pageSize + 1}-
                        {Math.min(
                            currentPage * pageSize,
                            sortedAndFiltered.length,
                        )}{" "}
                        of {sortedAndFiltered.length} models
                    </div>
                    <div className="flex items-center gap-3">
                        <select
                            value={pageSize}
                            onChange={(e) => {
                                setPageSize(Number(e.target.value));
                                setCurrentPage(1);
                            }}
                            className="ui-input ui-input-sm"
                        >
                            <option value={25}>25 / page</option>
                            <option value={50}>50 / page</option>
                            <option value={75}>75 / page</option>
                            <option value={100}>100 / page</option>
                            <option value={125}>125 / page</option>
                            <option value={150}>150 / page</option>
                            <option value={175}>175 / page</option>
                            <option value={200}>200 / page</option>
                        </select>
                        {totalPages > 1 && (
                            <div className="flex items-center gap-1">
                                <button
                                    type="button"
                                    onClick={() =>
                                        setCurrentPage((p) => Math.max(1, p - 1))
                                    }
                                    disabled={currentPage === 1}
                                    className="px-2 py-1 text-xs rounded border bg-gray-700 text-gray-300 border-gray-600 hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed"
                                >
                                    Prev
                                </button>
                                {Array.from(
                                    { length: Math.min(7, totalPages) },
                                    (_, i) => {
                                        let pageNum: number;
                                        if (totalPages <= 7) {
                                            pageNum = i + 1;
                                        } else if (currentPage <= 4) {
                                            pageNum = i + 1;
                                            if (i === 6) pageNum = totalPages;
                                        } else if (currentPage >= totalPages - 3) {
                                            pageNum = totalPages - 6 + i;
                                            if (i === 0) pageNum = 1;
                                        } else {
                                            pageNum = currentPage - 3 + i;
                                            if (i === 0) pageNum = 1;
                                            if (i === 6) pageNum = totalPages;
                                        }
                                        return (
                                            <button
                                                key={pageNum}
                                                type="button"
                                                onClick={() =>
                                                    setCurrentPage(pageNum)
                                                }
                                                className={`px-2 py-1 text-xs rounded border ${
                                                    currentPage === pageNum
                                                        ? "bg-(--accent) text-white border-(--accent)"
                                                        : "bg-gray-700 text-gray-300 border-gray-600 hover:bg-gray-600"
                                                }`}
                                            >
                                                {pageNum}
                                            </button>
                                        );
                                    },
                                )}
                                <button
                                    type="button"
                                    onClick={() =>
                                        setCurrentPage((p) =>
                                            Math.min(totalPages, p + 1),
                                        )
                                    }
                                    disabled={currentPage === totalPages}
                                    className="px-2 py-1 text-xs rounded border bg-gray-700 text-gray-300 border-gray-600 hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed"
                                >
                                    Next
                                </button>
                            </div>
                        )}
                    </div>
                </div>
            )}

            {detailModel && (
                <ModelDetailModal
                    model={detailModel}
                    onClose={() => setDetailModel(null)}
                    onToggle={handleToggleModel}
                    onDiscover={handleDiscover}
                    onTest={handleTest}
                    onToast={toast}
                    onUpdate={handleUpdateModel}
                />
            )}
        </div>
    );
}
