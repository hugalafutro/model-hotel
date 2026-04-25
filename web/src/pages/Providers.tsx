import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import { useState, useMemo, useEffect, useRef } from "react";
import { PlugZap, Eye, EyeOff, X, RefreshCw } from "lucide-react";
import { Spinner } from "../components/Spinner";
import type {
    DeepSeekBalance,
    DeepSeekBalanceInfo,
    NanoGPTUsage,
    Provider,
    ZAIQuotaResponse,
} from "../api/types";
import { useToast } from "../context/ToastContext";
import { useTheme } from "../context/ThemeContext";
import { ConfirmDialog } from "../components/ConfirmDialog";

const CACHE_PREFIX = "llm-proxy";

function getCachedData<T>(key: string): T | undefined {
    try {
        const raw = localStorage.getItem(`${CACHE_PREFIX}:${key}`);
        if (raw) return JSON.parse(raw) as T;
    } catch {
        /* ignore */
    }
    return undefined;
}

function setCachedData<T>(key: string, data: T) {
    try {
        localStorage.setItem(`${CACHE_PREFIX}:${key}`, JSON.stringify(data));
    } catch {
        /* ignore */
    }
}

function formatTokens(n: number | null | undefined): string {
    if (n == null) return "-";
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
    if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
    return n.toString();
}

function formatTimestamp(ts: number | string): string {
    return new Date(ts).toLocaleString(undefined, {
        day: "numeric",
        month: "short",
        year: "numeric",
        hour: "2-digit",
        minute: "2-digit",
    });
}

function formatDate(ts: number | string): string {
    return new Date(ts).toLocaleDateString(undefined, {
        day: "numeric",
        month: "short",
        year: "numeric",
    });
}

function formatTimeUntil(ts: number): string {
    const now = Date.now();
    const diff = ts - now;
    if (diff <= 0) return "now";

    const hours = Math.floor(diff / (1000 * 60 * 60));
    const days = Math.floor(hours / 24);
    const remainingHours = hours % 24;

    if (days > 0) {
        const dayLabel = days === 1 ? "day" : "days";
        const hourLabel = remainingHours === 1 ? "hour" : "hours";
        return `in ${days} ${dayLabel}, ${remainingHours} ${hourLabel}`;
    }
    const hourLabel = hours === 1 ? "hour" : "hours";
    return `in ${hours} ${hourLabel}`;
}

function NanoGPTQuotaModal({
    usage,
    onClose,
    onRefresh,
    isRefreshing,
    onToast,
}: {
    usage: NanoGPTUsage;
    onClose: () => void;
    onRefresh: () => Promise<unknown>;
    isRefreshing: boolean;
    onToast: (msg: string, type: "success" | "error" | "info") => void;
}) {
    const { uiStyle } = useTheme();
    const weeklyLimit = usage.limits.weeklyInputTokens ?? 0;
    const weeklyUsed = usage.weeklyInputTokens?.used ?? 0;
    const weeklyPercent =
        weeklyLimit > 0 ? (weeklyUsed / weeklyLimit) * 100 : 0;

    const handleRefresh = async () => {
        try {
            await onRefresh();
            onToast("Quota refreshed", "success");
        } catch {
            onToast("Failed to refresh quota", "error");
        }
    };

    return (
        <div
            role="dialog"
            aria-modal="true"
            className="fixed inset-0 flex items-center justify-center z-50"
            onKeyDown={(e) => {
                if (e.key === "Escape") onClose();
            }}
        >
            <button
                type="button"
                className="absolute inset-0 bg-black/60 cursor-default"
                onClick={onClose}
                aria-label="Close dialog"
            />
            <div className="relative ui-card p-6 w-full max-w-md max-h-[85vh] overflow-y-auto">
                <div className="flex justify-between items-start mb-6">
                    <div>
                        <h2 className="text-xl font-bold text-white">
                            NanoGPT Subscription
                        </h2>
                        <p className="text-sm text-gray-400 mt-1">
                            {usage.active ? (
                                <span className="inline-flex items-center gap-1.5">
                                    <span className="w-2 h-2 rounded-full bg-green-400"></span>
                                    Active
                                </span>
                            ) : (
                                <span className="inline-flex items-center gap-1.5">
                                    <span className="w-2 h-2 rounded-full bg-red-400"></span>
                                    Inactive
                                </span>
                            )}
                        </p>
                    </div>
                    <div className="flex items-center gap-2">
                        <button
                            type="button"
                            onClick={handleRefresh}
                            disabled={isRefreshing}
                            className="absolute top-4 right-10 text-gray-400 hover:text-white transition-all cursor-default leading-none p-1 hover:drop-shadow-[0_0_8px_var(--accent)]"
                            aria-label="Refresh"
                            title="Refresh quota info"
                        >
                            {isRefreshing && uiStyle === "cyber-terminal" ? (
                                <Spinner />
                            ) : (
                                <RefreshCw
                                    size={18}
                                    className={
                                        isRefreshing ? "animate-spin" : ""
                                    }
                                />
                            )}
                        </button>
                        <button
                            type="button"
                            onClick={onClose}
                            className="absolute top-4 right-4 text-gray-400 hover:text-white transition-all cursor-default leading-none p-1 hover:drop-shadow-[0_0_8px_var(--accent)]"
                            aria-label="Close"
                            title="Close"
                        >
                            <X size={20} />
                        </button>
                    </div>
                </div>

                <div className="space-y-6">
                    <div>
                        <div className="flex justify-between items-center mb-2">
                            <span className="text-sm font-medium text-gray-300">
                                Weekly Token Quota
                            </span>
                            <span className="text-sm text-gray-400">
                                {formatTokens(weeklyUsed)} /{" "}
                                {formatTokens(weeklyLimit)}
                            </span>
                        </div>
                        <div className="w-full bg-gray-700 rounded-full h-3">
                            <div
                                className="bg-[#0690a8] h-3 rounded-full transition-all"
                                style={{
                                    width: `${Math.min(weeklyPercent, 100)}%`,
                                }}
                            />
                        </div>
                        <p className="text-xs text-gray-500 mt-1">
                            {weeklyPercent.toFixed(1)}% used. Resets{" "}
                            {usage.weeklyInputTokens?.resetAt
                                ? `${formatTimestamp(usage.weeklyInputTokens.resetAt)} - ${formatTimeUntil(usage.weeklyInputTokens.resetAt)}`
                                : "N/A"}
                        </p>
                    </div>

                    {usage.dailyImages && (
                        <div>
                            <div className="flex justify-between items-center mb-2">
                                <span className="text-sm font-medium text-gray-300">
                                    Daily Images
                                </span>
                                <span className="text-sm text-gray-400">
                                    {usage.dailyImages.used} /{" "}
                                    {usage.limits.dailyImages ?? "∞"}
                                </span>
                            </div>
                            <div className="w-full bg-gray-700 rounded-full h-3">
                                <div
                                    className="bg-purple-500 h-3 rounded-full transition-all"
                                    style={{
                                        width: `${Math.min(usage.dailyImages.percentUsed * 100, 100)}%`,
                                    }}
                                />
                            </div>
                            <p className="text-xs text-gray-500 mt-1">
                                {usage.dailyImages.percentUsed.toFixed(1)}%
                                used. Resets{" "}
                                {usage.dailyImages.resetAt
                                    ? `${formatTimestamp(usage.dailyImages.resetAt)} - ${formatTimeUntil(usage.dailyImages.resetAt)}`
                                    : "N/A"}
                            </p>
                        </div>
                    )}

                    {usage.dailyInputTokens && (
                        <div>
                            <div className="flex justify-between items-center mb-2">
                                <span className="text-sm font-medium text-gray-300">
                                    Daily Input Tokens
                                </span>
                                <span className="text-sm text-gray-400">
                                    {formatTokens(usage.dailyInputTokens.used)}{" "}
                                    /{" "}
                                    {usage.limits.dailyInputTokens
                                        ? formatTokens(
                                              usage.limits.dailyInputTokens,
                                          )
                                        : "∞"}
                                </span>
                            </div>
                            <div className="w-full bg-gray-700 rounded-full h-3">
                                <div
                                    className="bg-amber-500 h-3 rounded-full transition-all"
                                    style={{
                                        width: `${Math.min(usage.dailyInputTokens.percentUsed * 100, 100)}%`,
                                    }}
                                />
                            </div>
                            <p className="text-xs text-gray-500 mt-1">
                                {usage.dailyInputTokens.percentUsed.toFixed(1)}%
                                used. Resets{" "}
                                {usage.dailyInputTokens.resetAt
                                    ? `${formatTimestamp(usage.dailyInputTokens.resetAt)} - ${formatTimeUntil(usage.dailyInputTokens.resetAt)}`
                                    : "N/A"}
                            </p>
                        </div>
                    )}

                    <div>
                        <h3 className="text-sm font-medium text-gray-300 mb-3">
                            Subscription Details
                        </h3>
                        <div className="grid grid-cols-2 gap-3 text-sm">
                            <div>
                                <span className="text-gray-500">Provider</span>
                                <p className="text-gray-200 capitalize">
                                    {usage.provider}
                                </p>
                            </div>
                            <div>
                                <span className="text-gray-500">Status</span>
                                <p className="text-gray-200 capitalize">
                                    {usage.providerStatus}
                                </p>
                            </div>
                            <div>
                                <span className="text-gray-500">
                                    Period End
                                </span>
                                <p className="text-gray-200">
                                    {formatDate(usage.period.currentPeriodEnd)}
                                </p>
                            </div>
                            <div>
                                <span className="text-gray-500">
                                    Allow Overage
                                </span>
                                <p className="text-gray-200">
                                    {usage.allowOverage ? "Yes" : "No"}
                                </p>
                            </div>
                        </div>
                    </div>

                    {usage.cancelAtPeriodEnd && (
                        <div className="p-3 bg-yellow-900/30 border border-yellow-700/50 rounded-lg">
                            <p className="text-sm text-yellow-300">
                                Subscription will cancel at period end (
                                {formatDate(usage.period.currentPeriodEnd)})
                            </p>
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}

function ZAIQuotaModal({
    usage,
    onClose,
    onRefresh,
    isRefreshing,
    onToast,
}: {
    usage: ZAIQuotaResponse;
    onClose: () => void;
    onRefresh: () => Promise<unknown>;
    isRefreshing: boolean;
    onToast: (msg: string, type: "success" | "error" | "info") => void;
}) {
    const { uiStyle } = useTheme();
    const limits = usage.data?.limits || [];

    const fiveHourLimit = limits.find(
        (l) => l.type === "TOKENS_LIMIT" && l.unit === 3,
    );
    const weeklyLimit = limits.find(
        (l) => l.type === "TOKENS_LIMIT" && l.unit === 6,
    );
    const mcpLimit = limits.find(
        (l) => l.type === "TIME_LIMIT" && l.unit === 5,
    );

    const handleRefresh = async () => {
        try {
            await onRefresh();
            onToast("Quota refreshed", "success");
        } catch {
            onToast("Failed to refresh quota", "error");
        }
    };

    return (
        <div
            role="dialog"
            aria-modal="true"
            className="fixed inset-0 flex items-center justify-center z-50"
            onKeyDown={(e) => {
                if (e.key === "Escape") onClose();
            }}
        >
            <button
                type="button"
                className="absolute inset-0 bg-black/60 cursor-default"
                onClick={onClose}
                aria-label="Close dialog"
            />
            <div className="relative ui-card p-6 w-full max-w-md max-h-[85vh] overflow-y-auto">
                <div className="flex justify-between items-start mb-6">
                    <div>
                        <h2 className="text-xl font-bold text-white">
                            Z.ai Quota
                        </h2>
                        <p className="text-sm text-gray-400 mt-1">
                            Plan:{" "}
                            <span className="text-gray-200 capitalize">
                                {usage.data?.level ?? "-"}
                            </span>
                        </p>
                    </div>
                    <div className="flex items-center gap-2">
                        <button
                            type="button"
                            onClick={handleRefresh}
                            disabled={isRefreshing}
                            className="absolute top-4 right-10 text-gray-400 hover:text-white transition-all cursor-default leading-none p-1 hover:drop-shadow-[0_0_8px_var(--accent)]"
                            aria-label="Refresh"
                            title="Refresh quota info"
                        >
                            {isRefreshing && uiStyle === "cyber-terminal" ? (
                                <Spinner />
                            ) : (
                                <RefreshCw
                                    size={18}
                                    className={
                                        isRefreshing ? "animate-spin" : ""
                                    }
                                />
                            )}
                        </button>
                        <button
                            type="button"
                            onClick={onClose}
                            className="absolute top-4 right-4 text-gray-400 hover:text-white transition-all cursor-default leading-none p-1 hover:drop-shadow-[0_0_8px_var(--accent)]"
                            aria-label="Close"
                            title="Close"
                        >
                            <X size={20} />
                        </button>
                    </div>
                </div>

                <div className="space-y-6">
                    {fiveHourLimit && (
                        <div>
                            <div className="flex justify-between items-center mb-2">
                                <span className="text-sm font-medium text-gray-300">
                                    5h Token Quota
                                </span>
                                <span className="text-sm text-gray-400">
                                    {(100 - fiveHourLimit.percentage).toFixed(
                                        0,
                                    )}
                                    % left
                                </span>
                            </div>
                            <div className="w-full bg-gray-700 rounded-full h-3">
                                <div
                                    className="bg-[#36aaff] h-3 rounded-full transition-all"
                                    style={{
                                        width: `${Math.min(100 - fiveHourLimit.percentage, 100)}%`,
                                    }}
                                />
                            </div>
                            <p className="text-xs text-gray-500 mt-1">
                                {fiveHourLimit.percentage.toFixed(0)}% used.
                                Resets{" "}
                                {fiveHourLimit.nextResetTime
                                    ? `${formatTimestamp(fiveHourLimit.nextResetTime)} - ${formatTimeUntil(fiveHourLimit.nextResetTime)}`
                                    : "N/A"}
                            </p>
                        </div>
                    )}

                    {weeklyLimit && (
                        <div>
                            <div className="flex justify-between items-center mb-2">
                                <span className="text-sm font-medium text-gray-300">
                                    Weekly Token Quota
                                </span>
                                <span className="text-sm text-gray-400">
                                    {(100 - weeklyLimit.percentage).toFixed(0)}%
                                    left
                                </span>
                            </div>
                            <div className="w-full bg-gray-700 rounded-full h-3">
                                <div
                                    className="bg-[#36aaff] h-3 rounded-full transition-all"
                                    style={{
                                        width: `${Math.min(100 - weeklyLimit.percentage, 100)}%`,
                                    }}
                                />
                            </div>
                            <p className="text-xs text-gray-500 mt-1">
                                {weeklyLimit.percentage.toFixed(0)}% used.
                                Resets{" "}
                                {weeklyLimit.nextResetTime
                                    ? `${formatTimestamp(weeklyLimit.nextResetTime)} - ${formatTimeUntil(weeklyLimit.nextResetTime)}`
                                    : "N/A"}
                            </p>
                        </div>
                    )}

                    {mcpLimit && (
                        <div>
                            <div className="flex justify-between items-center mb-2">
                                <span className="text-sm font-medium text-gray-300">
                                    MCP Time Quota
                                </span>
                                <span className="text-sm text-gray-400">
                                    {(100 - mcpLimit.percentage).toFixed(0)}%
                                    left
                                </span>
                            </div>
                            <div className="w-full bg-gray-700 rounded-full h-3">
                                <div
                                    className="bg-purple-500 h-3 rounded-full transition-all"
                                    style={{
                                        width: `${Math.min(100 - mcpLimit.percentage, 100)}%`,
                                    }}
                                />
                            </div>
                            <p className="text-xs text-gray-500 mt-1">
                                {mcpLimit.percentage.toFixed(0)}% used. Resets{" "}
                                {mcpLimit.nextResetTime
                                    ? `${formatTimestamp(mcpLimit.nextResetTime)} - ${formatTimeUntil(mcpLimit.nextResetTime)}`
                                    : "N/A"}
                            </p>
                            {mcpLimit.usageDetails &&
                                mcpLimit.usageDetails.length > 0 && (
                                    <div className="mt-2 space-y-1">
                                        {mcpLimit.usageDetails.map((detail) => (
                                            <div
                                                key={detail.modelCode}
                                                className="flex justify-between text-xs text-gray-500"
                                            >
                                                <span className="capitalize">
                                                    {detail.modelCode}
                                                </span>
                                                <span>{detail.usage} used</span>
                                            </div>
                                        ))}
                                    </div>
                                )}
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}

function EditProviderModal({
    provider,
    onClose,
    onToast,
}: {
    provider: Provider;
    onClose: () => void;
    onToast: (msg: string, type: "success" | "error" | "info") => void;
}) {
    const queryClient = useQueryClient();
    const [formData, setFormData] = useState({
        name: provider.name,
        base_url: provider.base_url,
        api_key: "",
        enabled: provider.enabled,
    });
    const [error, setError] = useState<string | null>(null);
    const [confirmFields, setConfirmFields] = useState<string[] | null>(null);

    const updateMutation = useMutation({
        mutationFn: (data: {
            name?: string;
            base_url?: string;
            api_key?: string;
            enabled?: boolean;
        }) => api.providers.update(provider.id, data),
        onSuccess: (updated: Provider) => {
            queryClient.invalidateQueries({ queryKey: ["providers"] });
            onToast(`Provider "${updated.name}" updated`, "success");
            onClose();
        },
        onError: (err: Error) => {
            setError(err.message);
            onToast(`Failed to update provider: ${err.message}`, "error");
        },
    });

    const getChangedFields = (): string[] => {
        const fields: string[] = [];
        if (formData.name !== provider.name) fields.push("name");
        if (formData.base_url !== provider.base_url) fields.push("base_url");
        if (formData.api_key !== "") fields.push("api_key");
        if (formData.enabled !== provider.enabled) fields.push("enabled");
        return fields;
    };

    const handleClose = () => {
        const changed = getChangedFields();
        if (changed.length > 0) {
            setConfirmFields(changed);
        } else {
            onClose();
        }
    };

    const handleSubmit = (e: React.FormEvent) => {
        e.preventDefault();
        setError(null);
        const payload: {
            name?: string;
            base_url?: string;
            api_key?: string;
            enabled?: boolean;
        } = {};
        if (formData.name !== provider.name) payload.name = formData.name;
        if (formData.base_url !== provider.base_url)
            payload.base_url = formData.base_url;
        if (formData.api_key !== "") payload.api_key = formData.api_key;
        if (formData.enabled !== provider.enabled)
            payload.enabled = formData.enabled;
        updateMutation.mutate(payload);
    };

    return (
        <>
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
                <div className="relative ui-card p-6 w-full max-w-md">
                    <button
                        type="button"
                        onClick={handleClose}
                        className="absolute top-4 right-4 text-(--text-secondary) hover:text-(--text-primary) transition-all cursor-default leading-none p-1 hover:drop-shadow-[0_0_8px_var(--accent)]"
                        aria-label="Close"
                    >
                        <X size={20} />
                    </button>
                    <h2 className="text-xl font-bold text-white mb-4">
                        Edit Provider
                    </h2>

                    {error && (
                        <div className="mb-4 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
                            {error}
                        </div>
                    )}

                    <form onSubmit={handleSubmit} className="space-y-4">
                        <div>
                            <label
                                htmlFor="edit-provider-name"
                                className="block text-sm font-medium text-gray-300 mb-1"
                            >
                                Name
                            </label>
                            <input
                                id="edit-provider-name"
                                type="text"
                                required
                                autoFocus={true}
                                value={formData.name}
                                onChange={(e) =>
                                    setFormData({
                                        ...formData,
                                        name: e.target.value,
                                    })
                                }
                                className="ui-input"
                                placeholder="e.g., OpenAI"
                            />
                        </div>

                        <div>
                            <label
                                htmlFor="edit-provider-base-url"
                                className="block text-sm font-medium text-gray-300 mb-1"
                            >
                                Base URL
                            </label>
                            <input
                                id="edit-provider-base-url"
                                type="url"
                                required
                                value={formData.base_url}
                                onChange={(e) =>
                                    setFormData({
                                        ...formData,
                                        base_url: e.target.value,
                                    })
                                }
                                className="ui-input"
                                placeholder="https://api.openai.com/v1"
                            />
                        </div>

                        <div>
                            <label
                                htmlFor="edit-provider-api-key"
                                className="block text-sm font-medium text-gray-300 mb-1"
                            >
                                API Key
                            </label>
                            <input
                                id="edit-provider-api-key"
                                type="password"
                                value={formData.api_key}
                                onChange={(e) =>
                                    setFormData({
                                        ...formData,
                                        api_key: e.target.value,
                                    })
                                }
                                className="ui-input"
                                placeholder="Leave blank to keep current key"
                            />
                            <p className="text-gray-500 text-xs mt-1">
                                Current: {provider.masked_key}
                            </p>
                        </div>

                        <div className="flex items-center gap-3">
                            <label
                                htmlFor="edit-provider-enabled"
                                className="text-sm font-medium text-gray-300"
                            >
                                Enabled
                            </label>
                            <button
                                type="button"
                                role="switch"
                                aria-checked={formData.enabled}
                                onClick={() =>
                                    setFormData({
                                        ...formData,
                                        enabled: !formData.enabled,
                                    })
                                }
                                className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors focus:ring-2 focus:ring-(--accent) focus:ring-offset-2 focus:ring-offset-gray-800 ${
                                    formData.enabled
                                        ? "bg-(--accent)"
                                        : "bg-gray-600"
                                }`}
                            >
                                <span
                                    className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                                        formData.enabled
                                            ? "translate-x-6"
                                            : "translate-x-1"
                                    }`}
                                />
                            </button>
                        </div>

                        <div className="flex space-x-3 justify-end pt-4">
                            <button
                                type="button"
                                onClick={handleClose}
                                className="px-3 py-1.5 text-xs rounded-full border bg-gray-900/40 text-gray-300 border-gray-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(156,163,175,0.15)] transition-all"
                            >
                                Cancel
                            </button>
                            <button
                                type="submit"
                                disabled={updateMutation.isPending}
                                className={`px-3 py-1.5 text-xs rounded-full border transition-all ${
                                    updateMutation.isPending
                                        ? "bg-(--accent-lighter) text-(--accent)/50 border-(--accent-light) cursor-not-allowed"
                                        : "bg-(--accent-light) text-(--accent) border-(--accent-lighter) cursor-pointer hover:brightness-125"
                                }`}
                            >
                                {updateMutation.isPending
                                    ? "Saving..."
                                    : "Save Changes"}
                            </button>
                        </div>
                    </form>
                </div>
            </div>
            {confirmFields && (
                <ConfirmDialog
                    title="Unsaved Changes"
                    fields={confirmFields}
                    onConfirm={onClose}
                    onCancel={() => setConfirmFields(null)}
                />
            )}
        </>
    );
}

export function Providers() {
    const queryClient = useQueryClient();
    const { toast } = useToast();
    const [showModal, setShowModal] = useState(false);
    const [editProvider, setEditProvider] = useState<Provider | null>(null);
    const [error, setError] = useState<string | null>(null);
    const [discoveringId, setDiscoveringId] = useState<string | null>(null);
    const [quotaUsage, setQuotaUsage] = useState<NanoGPTUsage | null>(null);
    const [zaiQuotaUsage, setZaiQuotaUsage] = useState<ZAIQuotaResponse | null>(
        null,
    );
    const [formData, setFormData] = useState<{
        name: string;
        base_url: string;
        api_key: string;
        provider_type: string;
    }>({
        name: "",
        base_url: "",
        api_key: "",
        provider_type: "openai",
    });
    const [showApiKey, setShowApiKey] = useState(false);

    const { data: providers, isLoading } = useQuery({
        queryKey: ["providers"],
        queryFn: () => api.providers.list(),
    });

    const { data: models } = useQuery({
        queryKey: ["models"],
        queryFn: () => api.models.list(),
        staleTime: 60_000,
    });

    const modelCounts = useMemo(() => {
        const map = new Map<string, number>();
        if (models) {
            for (const m of models) {
                if (m.provider_name) {
                    map.set(
                        m.provider_name,
                        (map.get(m.provider_name) || 0) + 1,
                    );
                }
            }
        }
        return map;
    }, [models]);

    const { data: settings } = useQuery({
        queryKey: ["settings"],
        queryFn: () => api.settings.get(),
    });

    const nanogptProviderId = useMemo(() => {
        return providers?.find((p) => {
            try {
                return new URL(p.base_url).hostname.endsWith("nano-gpt.com");
            } catch {
                return false;
            }
        })?.id;
    }, [providers]);

    const zaiProviderId = useMemo(() => {
        return providers?.find((p) => {
            try {
                const h = new URL(p.base_url).hostname;
                return h === "z.ai" || h.endsWith(".z.ai");
            } catch {
                return false;
            }
        })?.id;
    }, [providers]);

    const deepseekProviderId = useMemo(() => {
        return providers?.find((p) => {
            try {
                return new URL(p.base_url).hostname.endsWith("deepseek.com");
            } catch {
                return false;
            }
        })?.id;
    }, [providers]);

    const {
        data: nanogptUsage,
        refetch,
        isRefetching,
        isError: isNanoGPTError,
    } = useQuery({
        queryKey: ["nanogpt-usage", nanogptProviderId],
        queryFn: () =>
            api.providers.getUsage(nanogptProviderId!) as Promise<NanoGPTUsage>,
        enabled: Boolean(nanogptProviderId),
        initialData: () => getCachedData<NanoGPTUsage>("nanogpt-usage"),
    });

    useEffect(() => {
        if (nanogptUsage) setCachedData("nanogpt-usage", nanogptUsage);
    }, [nanogptUsage]);

    const {
        data: zaiUsage,
        refetch: refetchZai,
        isRefetching: isZaiRefetching,
        isError: isZAIError,
    } = useQuery({
        queryKey: ["zai-usage", zaiProviderId],
        queryFn: () =>
            api.providers.getUsage(zaiProviderId!) as Promise<ZAIQuotaResponse>,
        enabled: Boolean(zaiProviderId),
        initialData: () => getCachedData<ZAIQuotaResponse>("zai-usage"),
    });

    useEffect(() => {
        if (zaiUsage) setCachedData("zai-usage", zaiUsage);
    }, [zaiUsage]);

    const {
        data: deepseekBalanceData,
        refetch: refetchDeepseekBalance,
        isError: isDeepseekError,
    } = useQuery({
        queryKey: ["deepseek-balance", deepseekProviderId],
        queryFn: () => api.providers.getBalance(deepseekProviderId!),
        enabled: Boolean(deepseekProviderId),
        initialData: () => getCachedData<DeepSeekBalance>("deepseek-balance"),
    });

    useEffect(() => {
        if (deepseekBalanceData)
            setCachedData("deepseek-balance", deepseekBalanceData);
    }, [deepseekBalanceData]);

    const nanoGPTErrorToasted = useRef(false);
    useEffect(() => {
        if (isNanoGPTError && !nanoGPTErrorToasted.current) {
            toast("Failed to fetch NanoGPT usage quota", "warning");
            nanoGPTErrorToasted.current = true;
        }
        if (!isNanoGPTError) nanoGPTErrorToasted.current = false;
    }, [isNanoGPTError, toast]);

    const zaiErrorToasted = useRef(false);
    useEffect(() => {
        if (isZAIError && !zaiErrorToasted.current) {
            toast("Failed to fetch ZAI usage quota", "warning");
            zaiErrorToasted.current = true;
        }
        if (!isZAIError) zaiErrorToasted.current = false;
    }, [isZAIError, toast]);

    const deepseekErrorToasted = useRef(false);
    useEffect(() => {
        if (isDeepseekError && !deepseekErrorToasted.current) {
            toast("Failed to fetch DeepSeek balance", "warning");
            deepseekErrorToasted.current = true;
        }
        if (!isDeepseekError) deepseekErrorToasted.current = false;
    }, [isDeepseekError, toast]);

    const discoverAllMutation = useMutation({
        mutationFn: async () => {
            toast("Discovering models for all providers...", "info");
            return api.providers.discoverAll();
        },
        onSuccess: (data) => {
            queryClient.invalidateQueries({ queryKey: ["providers"] });
            queryClient.invalidateQueries({ queryKey: ["models"] });
            for (const r of data.results) {
                if (r.error) {
                    toast(`${r.provider_name}: ${r.error}`, "error");
                } else {
                    toast(
                        `${r.provider_name}: ${r.discovered} models`,
                        "success",
                    );
                }
            }
            if (data.discovered > 0) {
                toast(
                    `Discovered ${data.discovered} models across ${data.succeeded} providers`,
                    "success",
                );
            } else if (data.failed > 0) {
                toast(
                    `Discovery failed for all ${data.failed} providers`,
                    "error",
                );
            }
        },
        onError: (err: Error) => {
            toast(`Discover all failed: ${err.message}`, "error");
        },
    });

    const refreshQuotasMutation = useMutation({
        mutationFn: async () => {
            toast("Refreshing quotas and balances...", "info");
            return api.providers.refreshQuotas();
        },
        onSuccess: (data) => {
            queryClient.invalidateQueries({ queryKey: ["providers"] });
            if (data.failed > 0) {
                toast(
                    `Refreshed ${data.refreshed} quotas (${data.failed} failed, ${data.skipped} unsupported)`,
                    "warning",
                );
            } else if (data.refreshed === 0) {
                toast("No providers with quota/balance support found", "info");
            } else {
                toast(`Refreshed ${data.refreshed} quotas/balances`, "success");
            }
        },
        onError: (err: Error) => {
            toast(`Refresh quotas failed: ${err.message}`, "error");
        },
    });

    const discoverMutation = useMutation({
        mutationFn: async (id: string) => {
            setDiscoveringId(id);
            toast("Discovering models...", "info");
            return api.providers.discover(id);
        },
        onSuccess: (data) => {
            queryClient.invalidateQueries({ queryKey: ["providers"] });
            queryClient.invalidateQueries({ queryKey: ["models"] });
            toast(`Discovered ${data?.discovered ?? "new"} models`, "success");
        },
        onError: (err: Error) => {
            toast(`Discovery failed: ${err.message}`, "error");
        },
        onSettled: () => {
            setDiscoveringId(null);
        },
    });

    const createMutation = useMutation({
        mutationFn: (data: {
            name: string;
            base_url: string;
            api_key: string;
        }) => api.providers.create(data),
        onSuccess: async (newProvider) => {
            queryClient.invalidateQueries({ queryKey: ["providers"] });
            setShowModal(false);
            setFormData({
                name: "",
                base_url: "",
                api_key: "",
                provider_type: "custom",
            });
            setError(null);
            toast(`Provider "${newProvider.name}" added`, "success");
            const shouldDiscover =
                settings?.discovery_on_provider_create !== "false";
            if (shouldDiscover) {
                try {
                    toast("Discovering models...", "info");
                    const result = await api.providers.discover(newProvider.id);
                    queryClient.invalidateQueries({ queryKey: ["models"] });
                    queryClient.invalidateQueries({ queryKey: ["providers"] });
                    toast(
                        `Discovered ${result?.discovered ?? "new"} models`,
                        "success",
                    );
                } catch {
                    toast("Auto-discovery failed", "error");
                }
            }
        },
        onError: (err: Error) => {
            setError(err.message);
            toast(`Failed to add provider: ${err.message}`, "error");
        },
    });

    const deleteMutation = useMutation({
        mutationFn: (id: string) => api.providers.delete(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["providers"] });
            queryClient.invalidateQueries({ queryKey: ["models"] });
            toast("Provider deleted", "success");
        },
        onError: (err: Error) => {
            toast(`Failed to delete: ${err.message}`, "error");
        },
    });

    const handleSubmit = (e: React.FormEvent) => {
        e.preventDefault();
        setError(null);
        createMutation.mutate({
            name: formData.name,
            base_url: formData.base_url,
            api_key: formData.api_key,
        });
    };

    const handleProviderTypeChange = (type: string) => {
        const baseUrls: Record<string, string> = {
            nanogpt: "https://nano-gpt.com/api/subscription/v1",
            "z-ai": "https://api.z.ai/api/paas/v4",
            openai: "https://api.openai.com/v1",
            deepseek: "https://api.deepseek.com/v1",
            ollama: "https://ollama.com/v1",
        };
        setFormData((prev) => ({
            ...prev,
            provider_type: type,
            base_url: baseUrls[type] || prev.base_url,
        }));
    };

    if (isLoading) {
        return (
            <div className="flex items-center justify-center h-64">
                <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-(--accent)"></div>
            </div>
        );
    }

    return (
        <div className="space-y-6">
            <div className="flex justify-between items-center">
                <div>
                    <div className="flex items-center gap-3">
                        <PlugZap
                            size={28}
                            strokeWidth={2}
                            className="text-(--accent)"
                        />
                        <h1 className="text-3xl font-bold text-white">
                            Providers
                        </h1>
                    </div>
                    <p className="text-gray-400">
                        Manage your provider configurations
                    </p>
                </div>
                <div className="flex items-center gap-3">
                    <button
                        type="button"
                        onClick={() => discoverAllMutation.mutate()}
                        disabled={
                            discoverAllMutation.isPending ||
                            discoveringId !== null
                        }
                        className="ui-btn ui-btn-secondary"
                    >
                        {discoverAllMutation.isPending ? (
                            <>
                                <Spinner /> Discovering...
                            </>
                        ) : (
                            "Discover All Models"
                        )}
                    </button>
                    <button
                        type="button"
                        onClick={() => refreshQuotasMutation.mutate()}
                        disabled={refreshQuotasMutation.isPending}
                        className="ui-btn ui-btn-secondary"
                    >
                        {refreshQuotasMutation.isPending
                            ? "Refreshing..."
                            : "Refresh Quotas/Balances"}
                    </button>
                    <button
                        type="button"
                        onClick={() => setShowModal(true)}
                        className="ui-btn ui-btn-primary"
                    >
                        + Add Provider
                    </button>
                </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
                {providers?.map((provider) => {
                    const isNanoGPT =
                        provider.base_url.includes("nano-gpt.com");
                    const isZAI = provider.base_url.includes("z.ai");
                    const weeklyUsed = isNanoGPT
                        ? nanogptUsage?.weeklyInputTokens?.used
                        : null;
                    const weeklyLimit = isNanoGPT
                        ? nanogptUsage?.limits?.weeklyInputTokens
                        : null;
                    const showQuotaBadge =
                        isNanoGPT &&
                        weeklyUsed != null &&
                        weeklyLimit &&
                        nanogptUsage;

                    const zaiFiveHour = isZAI
                        ? zaiUsage?.data?.limits?.find(
                              (l) => l.type === "TOKENS_LIMIT" && l.unit === 3,
                          )
                        : null;
                    const zaiWeekly = isZAI
                        ? zaiUsage?.data?.limits?.find(
                              (l) => l.type === "TOKENS_LIMIT" && l.unit === 6,
                          )
                        : null;
                    const showZaiBadge =
                        isZAI &&
                        zaiUsage?.success &&
                        (zaiFiveHour || zaiWeekly);

                    return (
                        <div
                            key={provider.id}
                            className={`ui-card p-6 ${!provider.enabled ? "opacity-50" : ""}`}
                        >
                            <div className="mb-4">
                                <div className="flex items-center justify-between">
                                    <h3 className="text-lg font-semibold text-white">
                                        {provider.name}
                                    </h3>
                                    <div className="flex items-center gap-2">
                                        {!provider.enabled && (
                                            <span className="px-2 py-0.5 rounded-full bg-gray-600/40 text-gray-400 text-xs font-medium border border-gray-600/50">
                                                Disabled
                                            </span>
                                        )}
                                        {provider.total_tokens > 0 && (
                                            <span className="px-2 py-0.5 rounded-full bg-purple-500/20 text-purple-400 text-xs font-medium border border-purple-500/30">
                                                {formatTokens(
                                                    provider.total_tokens,
                                                )}{" "}
                                                tokens
                                            </span>
                                        )}
                                        {(() => {
                                            const count =
                                                modelCounts.get(
                                                    provider.name,
                                                ) ?? 0;
                                            return (
                                                count > 0 && (
                                                    <span className="px-2 py-0.5 rounded-full bg-cyan-500/20 text-cyan-400 text-xs font-medium border border-cyan-500/30">
                                                        {count}{" "}
                                                        {count === 1
                                                            ? "model"
                                                            : "models"}
                                                    </span>
                                                )
                                            );
                                        })()}
                                    </div>
                                </div>
                                <p className="text-sm text-gray-400 mt-1 truncate">
                                    {provider.base_url}
                                </p>
                            </div>

                            <div className="space-y-2 text-sm">
                                <div className="flex justify-between">
                                    <span className="text-gray-500">
                                        Created
                                    </span>
                                    <span className="text-gray-300">
                                        {formatTimestamp(provider.created_at)}
                                    </span>
                                </div>
                                <div className="flex justify-between">
                                    <span className="text-gray-500">
                                        API Key
                                    </span>
                                    <span className="font-mono text-gray-300">
                                        {provider.masked_key}
                                    </span>
                                </div>
                                <div className="flex justify-between">
                                    <span className="text-gray-500">
                                        Last Used
                                    </span>
                                    <span className="text-gray-300">
                                        {provider.last_used_at
                                            ? formatTimestamp(
                                                  provider.last_used_at,
                                              )
                                            : "N/A"}
                                    </span>
                                </div>
                                {provider.last_discovered_at && (
                                    <div className="flex justify-between">
                                        <span className="text-gray-500">
                                            Last Discovery
                                        </span>
                                        <span className="text-gray-300">
                                            {formatTimestamp(
                                                provider.last_discovered_at,
                                            )}
                                        </span>
                                    </div>
                                )}
                            </div>

                            <div className="mt-4 flex items-center justify-between gap-2">
                                <div className="flex items-center gap-2 min-h-7">
                                    {showQuotaBadge && (
                                        <button
                                            type="button"
                                            onClick={() =>
                                                nanogptUsage &&
                                                setQuotaUsage(nanogptUsage)
                                            }
                                            className="px-2 py-1.5 rounded-full bg-[#0690a8]/20 text-[#0690a8] border border-[#0690a8]/50 text-xs font-medium cursor-pointer hover:bg-[#0690a8]/30 transition-colors"
                                            title="View quota details"
                                        >
                                            {formatTokens(weeklyUsed)}/
                                            {formatTokens(weeklyLimit)}
                                        </button>
                                    )}
                                    {showZaiBadge && (
                                        <button
                                            type="button"
                                            onClick={() =>
                                                zaiUsage &&
                                                setZaiQuotaUsage(zaiUsage)
                                            }
                                            className="px-2 py-1.5 rounded-full bg-[#36aaff]/20 text-[#36aaff] border border-[#36aaff]/50 text-xs font-medium cursor-pointer hover:bg-[#36aaff]/30 transition-colors"
                                            title="View quota details"
                                        >
                                            {zaiFiveHour
                                                ? `${(100 - zaiFiveHour.percentage).toFixed(0)}%`
                                                : "-"}
                                            /
                                            {zaiWeekly
                                                ? `${(100 - zaiWeekly.percentage).toFixed(0)}%`
                                                : "-"}
                                        </button>
                                    )}
                                    {provider.base_url.includes(
                                        "deepseek.com",
                                    ) &&
                                        deepseekBalanceData && (
                                            <button
                                                type="button"
                                                onClick={async () => {
                                                    try {
                                                        await refetchDeepseekBalance();
                                                        toast(
                                                            "Balance refreshed",
                                                            "success",
                                                        );
                                                    } catch {
                                                        toast(
                                                            "Failed to refresh balance",
                                                            "error",
                                                        );
                                                    }
                                                }}
                                                className="px-2 py-1.5 rounded-full bg-[#36aaff]/20 text-[#36aaff] border border-[#36aaff]/50 text-xs font-medium cursor-pointer hover:bg-[#36aaff]/30 transition-colors"
                                                title="Refresh balance"
                                            >
                                                {deepseekBalanceData.balance_infos.find(
                                                    (b: DeepSeekBalanceInfo) =>
                                                        b.currency === "USD",
                                                )?.total_balance ?? "-"}{" "}
                                                USD
                                            </button>
                                        )}
                                </div>
                                <div className="flex gap-2">
                                    <button
                                        type="button"
                                        onClick={() =>
                                            setEditProvider(provider)
                                        }
                                        className="px-3 py-1.5 text-xs rounded-full border bg-gray-900/40 text-gray-300 border-gray-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(156,163,175,0.15)] transition-all"
                                    >
                                        Edit
                                    </button>
                                    <button
                                        type="button"
                                        onClick={() =>
                                            discoverMutation.mutate(provider.id)
                                        }
                                        disabled={
                                            discoveringId !== null ||
                                            discoverAllMutation.isPending
                                        }
                                        className={`px-3 py-1.5 text-xs rounded-full border transition-all ${
                                            discoveringId === provider.id
                                                ? "bg-(--accent-lighter) text-(--accent)/50 border-(--accent-light) cursor-not-allowed"
                                                : discoveringId !== null ||
                                                    discoverAllMutation.isPending
                                                  ? "bg-gray-800/50 text-gray-600 border-gray-700/30 cursor-not-allowed"
                                                  : "bg-(--accent-light) text-(--accent) border-(--accent-lighter) cursor-pointer hover:brightness-125"
                                        }`}
                                    >
                                        {discoveringId === provider.id ? (
                                            <>
                                                <Spinner /> Discovering...
                                            </>
                                        ) : (
                                            "Discover Models"
                                        )}
                                    </button>
                                    <button
                                        type="button"
                                        onClick={() =>
                                            deleteMutation.mutate(provider.id)
                                        }
                                        className="px-3 py-1.5 text-xs rounded-full border bg-red-900/50 text-red-400 border-red-700/50 hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(239,68,68,0.2)] cursor-pointer transition-all"
                                    >
                                        Delete
                                    </button>
                                </div>
                            </div>
                        </div>
                    );
                })}

                {providers?.length === 0 && (
                    <div className="col-span-full text-center py-12 ui-card">
                        <p className="text-gray-500">
                            No providers configured. Add your first provider to
                            get started.
                        </p>
                    </div>
                )}
            </div>

            {showModal && (
                <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50">
                    <div className="ui-card relative p-6 w-full max-w-md">
                        <button
                            type="button"
                            onClick={() => {
                                setShowModal(false);
                                setFormData({
                                    name: "",
                                    base_url: "",
                                    api_key: "",
                                    provider_type: "custom",
                                });
                                setError(null);
                            }}
                            className="absolute top-4 right-4 text-(--text-secondary) hover:text-(--text-primary) transition-all cursor-default leading-none p-1 hover:drop-shadow-[0_0_8px_var(--accent)]"
                            aria-label="Close"
                        >
                            <X size={20} />
                        </button>
                        <h2 className="text-xl font-bold text-white mb-4">
                            Add Provider
                        </h2>

                        {error && (
                            <div className="mb-4 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
                                {error}
                            </div>
                        )}

                        <form onSubmit={handleSubmit} className="space-y-4">
                            <div>
                                <label
                                    htmlFor="provider-type"
                                    className="block text-sm font-medium text-gray-300 mb-1"
                                >
                                    Type
                                </label>
                                <select
                                    id="provider-type"
                                    value={formData.provider_type}
                                    onChange={(e) =>
                                        handleProviderTypeChange(e.target.value)
                                    }
                                    className="ui-input"
                                >
                                    <option value="openai">
                                        OpenAI Compatible
                                    </option>
                                    <option value="nanogpt">NanoGPT</option>
                                    <option value="z-ai">Z.ai</option>
                                    <option value="deepseek">DeepSeek</option>
                                    <option value="ollama">Ollama Cloud</option>
                                </select>
                            </div>

                            <div>
                                <label
                                    htmlFor="provider-name"
                                    className="block text-sm font-medium text-gray-300 mb-1"
                                >
                                    Name
                                </label>
                                <input
                                    id="provider-name"
                                    type="text"
                                    required
                                    autoFocus={true}
                                    value={formData.name}
                                    onChange={(e) =>
                                        setFormData({
                                            ...formData,
                                            name: e.target.value,
                                        })
                                    }
                                    className="ui-input"
                                    placeholder="e.g., OpenAI"
                                />
                            </div>

                            <div>
                                <label
                                    htmlFor="provider-base-url"
                                    className="block text-sm font-medium text-gray-300 mb-1"
                                >
                                    Base URL
                                </label>
                                <input
                                    id="provider-base-url"
                                    type="url"
                                    required
                                    value={formData.base_url}
                                    onChange={(e) =>
                                        setFormData({
                                            ...formData,
                                            base_url: e.target.value,
                                        })
                                    }
                                    className="ui-input"
                                    placeholder="https://api.openai.com/v1"
                                />
                                <p className="text-gray-500 text-xs mt-1">
                                    Full API base URL including any path prefix.
                                    Models will be discovered from{" "}
                                    {"<base_url>"}/models
                                </p>
                            </div>

                            <div>
                                <label
                                    htmlFor="provider-api-key"
                                    className="block text-sm font-medium text-gray-300 mb-1"
                                >
                                    API Key
                                </label>
                                <div className="relative">
                                    <input
                                        id="provider-api-key"
                                        type={showApiKey ? "text" : "password"}
                                        required
                                        value={formData.api_key}
                                        onChange={(e) =>
                                            setFormData({
                                                ...formData,
                                                api_key: e.target.value,
                                            })
                                        }
                                        className="ui-input pr-10"
                                        placeholder="API key"
                                    />
                                    <button
                                        type="button"
                                        onClick={() =>
                                            setShowApiKey(!showApiKey)
                                        }
                                        className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 transition-colors"
                                        tabIndex={-1}
                                        aria-label={
                                            showApiKey
                                                ? "Hide API key"
                                                : "Show API key"
                                        }
                                    >
                                        {showApiKey ? (
                                            <EyeOff size={18} />
                                        ) : (
                                            <Eye size={18} />
                                        )}
                                    </button>
                                </div>
                            </div>

                            <div className="flex space-x-3 justify-end pt-4">
                                <button
                                    type="button"
                                    onClick={() => {
                                        setShowModal(false);
                                        setFormData({
                                            name: "",
                                            base_url: "",
                                            api_key: "",
                                            provider_type: "custom",
                                        });
                                        setShowApiKey(false);
                                        setError(null);
                                    }}
                                    className="px-3 py-1.5 text-xs rounded-full border bg-gray-900/40 text-gray-300 border-gray-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(156,163,175,0.15)] transition-all"
                                >
                                    Cancel
                                </button>
                                <button
                                    type="submit"
                                    disabled={createMutation.isPending}
                                    className="ui-btn ui-btn-primary disabled:opacity-50"
                                >
                                    {createMutation.isPending
                                        ? "Adding..."
                                        : "Add Provider"}
                                </button>
                            </div>
                        </form>
                    </div>
                </div>
            )}

            {quotaUsage && (
                <NanoGPTQuotaModal
                    usage={quotaUsage}
                    onClose={() => setQuotaUsage(null)}
                    onRefresh={refetch}
                    isRefreshing={isRefetching}
                    onToast={toast}
                />
            )}

            {zaiQuotaUsage && (
                <ZAIQuotaModal
                    usage={zaiQuotaUsage}
                    onClose={() => setZaiQuotaUsage(null)}
                    onRefresh={refetchZai}
                    isRefreshing={isZaiRefetching}
                    onToast={toast}
                />
            )}

            {editProvider && (
                <EditProviderModal
                    provider={editProvider}
                    onClose={() => setEditProvider(null)}
                    onToast={toast}
                />
            )}
        </div>
    );
}
