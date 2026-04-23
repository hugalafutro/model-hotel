import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import { useState, useMemo, useCallback } from "react";
import { KeyRound } from "lucide-react";
import { useToast } from "../context/ToastContext";
import type { VirtualKey } from "../api/types";
import { SortableHeader, StaticHeader, Row } from "../components/DataTable";
import { CopyablePill } from "../components/CopyablePill";
import type { SortState } from "../components/DataTable";

type VKSortField = "name" | "key" | "created" | "tokens" | "last_used";

function formatRelativeTime(dateStr: string | null): string {
    if (!dateStr) return "Never";
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

function formatNumber(n: number): string {
    return n.toLocaleString();
}

function CreateKeyModal({
    onClose,
    onToast,
}: {
    onClose: () => void;
    onToast: (msg: string, type: "success" | "error" | "info") => void;
}) {
    const queryClient = useQueryClient();
    const [name, setName] = useState("");
    const [createdKey, setCreatedKey] = useState<VirtualKey | null>(null);

    const createMutation = useMutation({
        mutationFn: (n: string) => api.virtualKeys.create(n),
        onSuccess: (vk) => {
            setCreatedKey(vk);
            queryClient.invalidateQueries({ queryKey: ["virtualKeys"] });
            onToast("Virtual key created", "success");
        },
        onError: (err: Error) => {
            onToast(`Failed: ${err.message}`, "error");
        },
    });

    const handleSubmit = (e: React.FormEvent) => {
        e.preventDefault();
        if (!name.trim()) return;
        createMutation.mutate(name.trim());
    };

    return (
        <div
            role="dialog"
            aria-modal="true"
            className="fixed inset-0 bg-black/60 flex items-center justify-center z-50"
            onKeyDown={(e) => {
                if (e.key === "Escape") onClose();
            }}
        >
            <div className="ui-card relative p-6 w-full max-w-md">
                <button
                    type="button"
                    onClick={onClose}
                    className="absolute top-4 right-4 text-(--text-secondary) hover:text-(--text-primary) transition-all cursor-default text-xl leading-none hover:drop-shadow-[0_0_8px_var(--accent)]"
                    aria-label="Close"
                >
                    &times;
                </button>
                {createdKey ? (
                    <>
                        <h2 className="text-xl font-bold text-white mb-4">
                            Virtual Key Created
                        </h2>
                        <p className="text-sm text-gray-400 mb-3">
                            Copy this key now. It won't be shown again.
                        </p>
                        <div className="bg-gray-950 rounded-lg p-3 mb-4">
                            <CopyablePill
                                text={createdKey.key}
                                displayText={createdKey.key}
                                textClassName="text-sm text-green-400 font-mono break-all"
                                tooltip="Click to copy key"
                            />
                        </div>
                        <p className="text-sm text-gray-500 mb-4">
                            Use as:{" "}
                            <code className="text-gray-400">
                                Bearer {createdKey.key}
                            </code>{" "}
                            at{" "}
                            <code className="text-gray-400">
                                {window.location.origin}/v1
                            </code>
                        </p>
                        <div className="flex justify-end">
                            <button
                                type="button"
                                onClick={onClose}
                                className="ui-btn-secondary cursor-pointer"
                            >
                                Done
                            </button>
                        </div>
                    </>
                ) : (
                    <>
                        <h2 className="text-xl font-bold text-white mb-4">
                            Create Virtual Key
                        </h2>
                        <form onSubmit={handleSubmit} className="space-y-4">
                            <div>
                                <label
                                    htmlFor="vk-name"
                                    className="block text-sm font-medium text-gray-300 mb-1"
                                >
                                    Name
                                </label>
                                <input
                                    id="vk-name"
                                    type="text"
                                    required
                                    autoFocus={true}
                                    value={name}
                                    onChange={(e) => setName(e.target.value)}
                                    className="ui-input"
                                    placeholder="e.g., My App"
                                />
                            </div>
                            <div className="flex space-x-3 justify-end pt-2">
                                <button
                                    type="button"
                                    onClick={onClose}
                                    className="px-3 py-1.5 text-xs rounded-full border bg-gray-900/40 text-gray-300 border-gray-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(156,163,175,0.15)] transition-all"
                                >
                                    Cancel
                                </button>
                                <button
                                    type="submit"
                                    disabled={createMutation.isPending}
                                    className="px-4 py-2 bg-(--accent) text-white rounded-lg hover:bg-(--accent) transition-colors disabled:opacity-50 cursor-pointer"
                                >
                                    {createMutation.isPending
                                        ? "Creating..."
                                        : "Create Key"}
                                </button>
                            </div>
                        </form>
                    </>
                )}
            </div>
        </div>
    );
}

function KeyDetailModal({
    vk,
    onClose,
    onToast,
}: {
    vk: VirtualKey;
    onClose: () => void;
    onToast: (msg: string, type: "success" | "error" | "info") => void;
}) {
    const queryClient = useQueryClient();
    const [confirmDelete, setConfirmDelete] = useState(false);

    const deleteMutation = useMutation({
        mutationFn: () => api.virtualKeys.delete(vk.id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["virtualKeys"] });
            onToast("Virtual key deleted", "success");
            onClose();
        },
        onError: (err: Error) => {
            onToast(`Failed to delete: ${err.message}`, "error");
            setConfirmDelete(false);
        },
    });

    return (
        <div
            role="dialog"
            aria-modal="true"
            className="fixed inset-0 bg-black/60 flex items-center justify-center z-50"
            onKeyDown={(e) => {
                if (e.key === "Escape") onClose();
            }}
        >
            <div className="ui-card p-6 w-full max-w-md relative">
                <button
                    type="button"
                    onClick={onClose}
                    className="absolute top-4 right-4 text-(--text-secondary) hover:text-(--text-primary) transition-colors cursor-default text-xl leading-none hover:drop-shadow-[0_0_8px_var(--accent)]"
                >
                    &times;
                </button>
                <h2 className="text-xl font-bold text-white mb-4">
                    Virtual Key Details
                </h2>

                <div className="space-y-3 mb-6">
                    <div>
                        <span className="text-sm text-gray-500">Name</span>
                        <p className="text-gray-200">{vk.name}</p>
                    </div>
                    <div>
                        <span className="text-sm text-gray-500">Key</span>
                        <p className="text-gray-200 font-mono">
                            {vk.key_preview}
                        </p>
                    </div>
                    <div>
                        <span className="text-sm text-gray-500">Created</span>
                        <p className="text-gray-200">
                            {new Date(vk.created_at).toLocaleString()}
                        </p>
                    </div>
                    <div>
                        <span className="text-sm text-gray-500">
                            Tokens Consumed
                        </span>
                        <p className="text-gray-200">
                            {formatNumber(vk.tokens_used)}
                        </p>
                    </div>
                    <div>
                        <span className="text-sm text-gray-500">Last Used</span>
                        <p className="text-gray-200">
                            {vk.last_used_at
                                ? new Date(vk.last_used_at).toLocaleString()
                                : "Never"}
                        </p>
                    </div>
                </div>

                <div className="flex justify-start items-center">
                    {!confirmDelete ? (
                        <button
                            type="button"
                            onClick={() => setConfirmDelete(true)}
                            className="px-3 py-1.5 text-xs rounded-full border bg-red-900/50 text-red-400 border-red-700/50 hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(239,68,68,0.2)] cursor-pointer transition-all"
                        >
                            Delete Key
                        </button>
                    ) : (
                        <div className="flex items-center gap-2">
                            <span className="text-xs text-red-400">
                                Are you sure?
                            </span>
                            <button
                                type="button"
                                onClick={() => deleteMutation.mutate()}
                                disabled={deleteMutation.isPending}
                                className="px-3 py-1.5 text-xs rounded-full border bg-red-900/50 text-red-400 border-red-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(239,68,68,0.2)] transition-all"
                            >
                                {deleteMutation.isPending
                                    ? "Deleting..."
                                    : "Yes, delete"}
                            </button>
                            <button
                                type="button"
                                onClick={() => setConfirmDelete(false)}
                                className="px-3 py-1.5 text-xs rounded-full border bg-gray-900/40 text-gray-300 border-gray-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(156,163,175,0.15)] transition-all"
                            >
                                Cancel
                            </button>
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}

export function VirtualKeys() {
    const { toast } = useToast();
    const [showCreate, setShowCreate] = useState(false);
    const [selectedKey, setSelectedKey] = useState<VirtualKey | null>(null);
    const [sort, setSort] = useState<SortState<VKSortField>>({
        field: "name",
        dir: "asc",
    });
    const [pageSize, setPageSize] = useState(25);
    const [currentPage, setCurrentPage] = useState(1);

    const { data: keys, isLoading } = useQuery({
        queryKey: ["virtualKeys"],
        queryFn: () => api.virtualKeys.list(),
    });

    const handleSort = useCallback((field: VKSortField) => {
        setSort((prev) => ({
            field,
            dir: prev.field === field && prev.dir === "asc" ? "desc" : "asc",
        }));
        setCurrentPage(1);
    }, []);

    const sortedKeys = useMemo(() => {
        if (!keys) return [];
        const dir = sort.dir === "asc" ? 1 : -1;
        return [...keys].sort((a, b) => {
            switch (sort.field) {
                case "name":
                    return dir * a.name.localeCompare(b.name);
                case "created":
                    return (
                        dir *
                        (new Date(a.created_at).getTime() -
                            new Date(b.created_at).getTime())
                    );
                case "tokens":
                    return dir * (a.tokens_used - b.tokens_used);
                case "last_used": {
                    const aT = a.last_used_at
                        ? new Date(a.last_used_at).getTime()
                        : 0;
                    const bT = b.last_used_at
                        ? new Date(b.last_used_at).getTime()
                        : 0;
                    return dir * (aT - bT);
                }
                default:
                    return 0;
            }
        });
    }, [keys, sort]);

    const totalPages = Math.ceil(sortedKeys.length / pageSize);
    const paginatedKeys = sortedKeys.slice(
        (currentPage - 1) * pageSize,
        currentPage * pageSize,
    );

    if (isLoading) {
        return (
            <div className="flex items-center justify-center h-64">
                <div className="animate-spin rounded-full h-12 w-12 border-b-2 border(--accent)" />
            </div>
        );
    }

    return (
        <div className="space-y-6">
            <div className="flex justify-between items-center">
                <div>
                    <div className="flex items-center gap-3">
                        <KeyRound
                            size={28}
                            strokeWidth={2}
                            className="text-(--accent)"
                        />
                        <h1 className="text-3xl font-bold text-white">
                            Virtual Keys
                        </h1>
                    </div>
                    <p className="text-gray-400 mt-1">
                        Issue keys for clients to access the proxy at /v1
                    </p>
                </div>
                <button
                    type="button"
                    onClick={() => setShowCreate(true)}
                    className="px-3 py-1.5 text-xs rounded-full border bg-(--accent-light) text-(--accent) border-(--accent-lighter) cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(156,163,175,0.15)] transition-all"
                >
                    + Create Key
                </button>
            </div>

            {sortedKeys.length > 0 ? (
                <div className="ui-card overflow-hidden">
                    <table className="w-full table-fixed ui-table">
                        <colgroup>
                            <col className="w-[28%]" />
                            <col className="w-[18%]" />
                            <col className="w-[22%]" />
                            <col className="w-[18%]" />
                            <col className="w-[14%]" />
                        </colgroup>
                        <thead>
                            <tr>
                                <SortableHeader
                                    label="Name"
                                    field="name"
                                    sort={sort}
                                    onSort={handleSort}
                                    tooltip="Display name for the virtual key"
                                />
                                <StaticHeader tooltip="Preview of the API key (full key only shown once on creation)">
                                    Key
                                </StaticHeader>
                                <SortableHeader
                                    label="Created"
                                    field="created"
                                    sort={sort}
                                    onSort={handleSort}
                                    tooltip="When the key was created"
                                />
                                <SortableHeader
                                    label="Tokens"
                                    field="tokens"
                                    sort={sort}
                                    onSort={handleSort}
                                    tooltip="Total tokens consumed using this key"
                                />
                                <SortableHeader
                                    label="Last Used"
                                    field="last_used"
                                    sort={sort}
                                    onSort={handleSort}
                                    tooltip="When the key was last used for a request"
                                />
                            </tr>
                        </thead>
                        <tbody>
                            {paginatedKeys.map((vk, idx) => (
                                <Row key={vk.id} index={idx}>
                                    <td className="px-4 py-3">
                                        <button
                                            type="button"
                                            onClick={() => setSelectedKey(vk)}
                                            className="text-gray-200 hover:text-(--accent) transition-colors cursor-pointer text-sm"
                                        >
                                            {vk.name}
                                        </button>
                                    </td>
                                    <td className="px-4 py-3 text-gray-500 font-mono text-xs">
                                        {vk.key_preview}
                                    </td>
                                    <td className="px-4 py-3 text-sm text-gray-400">
                                        {new Date(
                                            vk.created_at,
                                        ).toLocaleString()}
                                    </td>
                                    <td className="px-4 py-3 text-sm text-gray-400 font-mono">
                                        {formatNumber(vk.tokens_used)}
                                    </td>
                                    <td className="px-4 py-3 text-sm text-gray-400">
                                        {formatRelativeTime(vk.last_used_at)}
                                    </td>
                                </Row>
                            ))}
                        </tbody>
                    </table>
                </div>
            ) : (
                <div className="text-center py-12 ui-card">
                    <p className="text-gray-500">
                        No virtual keys. Create one to start using the proxy.
                    </p>
                </div>
            )}

            {sortedKeys.length > 0 && (
                <div className="flex items-center justify-between">
                    <div className="text-sm text-gray-500">
                        Showing {(currentPage - 1) * pageSize + 1}-
                        {Math.min(currentPage * pageSize, sortedKeys.length)} of{" "}
                        {sortedKeys.length} keys
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
                                        setCurrentPage((p) =>
                                            Math.max(1, p - 1),
                                        )
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
                                        } else if (
                                            currentPage >=
                                            totalPages - 3
                                        ) {
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

            {showCreate && (
                <CreateKeyModal
                    onClose={() => setShowCreate(false)}
                    onToast={toast}
                />
            )}

            {selectedKey && (
                <KeyDetailModal
                    vk={selectedKey}
                    onClose={() => setSelectedKey(null)}
                    onToast={toast}
                />
            )}
        </div>
    );
}
