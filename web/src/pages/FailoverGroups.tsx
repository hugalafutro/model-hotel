import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import { useState } from "react";
import { Shuffle, X } from "lucide-react";
import type { FailoverGroup, CandidateModel } from "../api/types";
import { useToast } from "../context/ToastContext";
import {
    DndContext,
    closestCenter,
    KeyboardSensor,
    PointerSensor,
    useSensor,
    useSensors,
    DragEndEvent,
} from "@dnd-kit/core";
import {
    arrayMove,
    SortableContext,
    sortableKeyboardCoordinates,
    useSortable,
    verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

function formatTokens(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
    if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
    return n.toString();
}

interface SortableEntryProps {
    entry: FailoverGroup["entries"][0];
    onToggle: (uuid: string, enabled: boolean) => void;
}

function SortableEntry({ entry, onToggle }: SortableEntryProps) {
    const {
        attributes,
        listeners,
        setNodeRef,
        transform,
        transition,
        isDragging,
    } = useSortable({ id: entry.model_uuid });

    const style = {
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.5 : 1,
    };

    return (
        <div
            ref={setNodeRef}
            style={style}
            className={`flex items-center justify-between px-2 py-1.5 bg-gray-700 rounded group text-sm ${
                !entry.enabled ? "opacity-35 saturate-0" : ""
            }`}
        >
            <div className="flex items-center gap-2 min-w-0">
                <span
                    {...attributes}
                    {...listeners}
                    className="text-gray-500 cursor-grab active:cursor-grabbing opacity-15 group-hover:opacity-100 transition-opacity shrink-0"
                >
                    ⠿
                </span>
                <div className="truncate">
                    <span className="text-white">{entry.provider_name}</span>
                    <span className="text-gray-500 mx-1">/</span>
                    <span className="text-gray-400 truncate">
                        {entry.model_id}
                    </span>
                </div>
            </div>
            <button
                type="button"
                onClick={() => onToggle(entry.model_uuid, !entry.enabled)}
                className={`relative inline-flex h-4 w-7 items-center rounded-full transition-colors focus:outline-none shrink-0 ${
                    entry.enabled ? "bg-(--accent)" : "bg-gray-600"
                }`}
                aria-label={
                    entry.enabled ? "Disable provider" : "Enable provider"
                }
            >
                <span
                    className={`inline-block h-3 w-3 transform rounded-full bg-white transition-transform ${
                        entry.enabled ? "translate-x-3.5" : "translate-x-0.5"
                    }`}
                />
            </button>
        </div>
    );
}

function FailoverGroupCard({
    group,
    onToggleGroup,
    onToggleEntry,
    onReorder,
    onDelete,
}: {
    group: FailoverGroup;
    onToggleGroup: (enabled: boolean) => void;
    onToggleEntry: (uuid: string, enabled: boolean) => void;
    onReorder: (newOrder: string[]) => void;
    onDelete: () => void;
}) {
    const { toast } = useToast();
    const enabledCount = group.entries.filter((e) => e.enabled).length;
    const totalCount = group.entries.length;

    const sensors = useSensors(
        useSensor(PointerSensor),
        useSensor(KeyboardSensor, {
            coordinateGetter: sortableKeyboardCoordinates,
        }),
    );

    const handleDragEnd = (event: DragEndEvent) => {
        const { active, over } = event;
        if (over && active.id !== over.id) {
            const oldIndex = group.entries.findIndex(
                (e) => e.model_uuid === active.id,
            );
            const newIndex = group.entries.findIndex(
                (e) => e.model_uuid === over.id,
            );
            const newOrder = arrayMove(group.entries, oldIndex, newIndex).map(
                (e) => e.model_uuid,
            );
            onReorder(newOrder);
        }
    };

    const handleCopyModel = () => {
        const modelRef = `hotel/${group.display_model}`;
        navigator.clipboard.writeText(modelRef);
        toast(`Copied ${modelRef}`, "success");
    };

    return (
        <div
            className={`ui-card p-3 ${
                group.group_enabled ? "border-(--accent)/30" : "opacity-60"
            }`}
        >
            <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2 min-w-0">
                    <div
                        onClick={handleCopyModel}
                        className="flex items-center gap-1.5 min-w-0 select-none px-1.5 py-0.5 -mx-1.5 -my-0.5 rounded hover:bg-gray-700 transition-colors group cursor-default"
                        title="Click to copy"
                    >
                        <h3 className="text-white font-medium text-sm truncate">
                            hotel/{group.display_model}
                        </h3>
                        <svg
                            className="w-3.5 h-3.5 text-gray-500 opacity-0 group-hover:opacity-100 transition-opacity shrink-0"
                            fill="none"
                            stroke="currentColor"
                            viewBox="0 0 24 24"
                        >
                            <path
                                strokeLinecap="round"
                                strokeLinejoin="round"
                                strokeWidth={2}
                                d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
                            />
                        </svg>
                    </div>
                    {group.auto_created && (
                        <span className="text-xs text-gray-500 shrink-0">
                            auto
                        </span>
                    )}
                </div>
                <button
                    type="button"
                    onClick={() => onToggleGroup(!group.group_enabled)}
                    className={`px-2 py-0.5 text-xs font-medium rounded-full transition-colors ${
                        group.group_enabled
                            ? "bg-(--accent-light) text-(--accent) hover:bg-(--accent)/30"
                            : "bg-gray-600 text-gray-300 hover:bg-gray-500"
                    }`}
                >
                    {group.group_enabled ? "ON" : "OFF"}
                </button>
            </div>

            <DndContext
                sensors={sensors}
                collisionDetection={closestCenter}
                onDragEnd={handleDragEnd}
            >
                <SortableContext
                    items={group.entries.map((e) => e.model_uuid)}
                    strategy={verticalListSortingStrategy}
                >
                    <div className="space-y-1">
                        {group.entries.map((entry) => (
                            <SortableEntry
                                key={entry.model_uuid}
                                entry={entry}
                                onToggle={onToggleEntry}
                            />
                        ))}
                    </div>
                </SortableContext>
            </DndContext>

            <div className="flex items-center justify-between mt-2 text-xs text-gray-500">
                <span>
                    {enabledCount}/{totalCount} active •{" "}
                    {formatTokens(group.total_tokens)} tokens
                </span>
                <button
                    type="button"
                    onClick={() => onDelete()}
                    className="text-gray-500 hover:text-red-400 cursor-pointer px-2 py-1 rounded-md hover:bg-red-400/10 transition-all"
                >
                    delete
                </button>
            </div>
        </div>
    );
}

function CreateGroupModal({
    candidates,
    onClose,
    onCreated,
}: {
    candidates: CandidateModel[];
    onClose: () => void;
    onCreated: () => void;
}) {
    const { toast } = useToast();
    const queryClient = useQueryClient();
    const [displayModel, setDisplayModel] = useState("");
    const [displayName, setDisplayName] = useState("");
    const [selectedEntries, setSelectedEntries] = useState<string[]>([]);
    const [search, setSearch] = useState("");

    const createMutation = useMutation({
        mutationFn: (data: {
            display_model: string;
            display_name?: string;
            entry_ids: string[];
        }) => api.failoverGroups.create(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
            toast("Failover group created", "success");
            onCreated();
        },
        onError: (err: Error) => {
            toast(`Failed to create group: ${err.message}`, "error");
        },
    });

    const filteredCandidates = candidates.filter((c) =>
        `${c.provider_name.replace(/ /g, "-")}/${c.model_id}`
            .toLowerCase()
            .includes(search.toLowerCase()),
    );

    const grouped = filteredCandidates.reduce(
        (acc, c) => {
            const key = c.model_id;
            if (!acc[key]) acc[key] = [];
            acc[key].push(c);
            return acc;
        },
        {} as Record<string, CandidateModel[]>,
    );

    const handleSubmit = (e: React.FormEvent) => {
        e.preventDefault();
        if (!displayModel.trim()) {
            toast("Display model name is required", "error");
            return;
        }
        if (selectedEntries.length < 2) {
            toast("At least 2 entries required", "error");
            return;
        }
        createMutation.mutate({
            display_model: displayModel,
            display_name: displayName || undefined,
            entry_ids: selectedEntries,
        });
    };

    return (
        <div
            role="dialog"
            aria-modal="true"
            className="fixed inset-0 flex items-center justify-center z-50"
            onKeyDown={(e) => e.key === "Escape" && onClose()}
        >
            <button
                type="button"
                className="absolute inset-0 bg-black/60 cursor-default"
                onClick={onClose}
                aria-label="Close dialog"
            />
            <div className="relative ui-card p-6 w-full max-w-lg max-h-[85vh] overflow-y-auto">
                <div className="flex justify-between items-start mb-4">
                    <h2 className="text-xl font-bold text-white">
                        Create Failover Group
                    </h2>
                    <button
                        type="button"
                        onClick={onClose}
                        className="text-gray-400 hover:text-white transition-all cursor-default leading-none p-1 hover:drop-shadow-[0_0_8px_var(--accent)]"
                        aria-label="Close"
                    >
                        <X size={20} />
                    </button>
                </div>

                <form onSubmit={handleSubmit} className="space-y-4">
                    <div>
                        <label
                            htmlFor="display-model"
                            className="block text-sm font-medium text-gray-300 mb-1"
                        >
                            Display Model Name
                        </label>
                        <input
                            id="display-model"
                            type="text"
                            required
                            autoFocus
                            value={displayModel}
                            onChange={(e) => setDisplayModel(e.target.value)}
                            className="ui-input"
                            placeholder="e.g., glm-5"
                        />
                        <p className="text-gray-500 text-xs mt-1">
                            This becomes hotel/{displayModel || "model-name"} in
                            the model list
                        </p>
                    </div>

                    <div>
                        <label
                            htmlFor="display-name"
                            className="block text-sm font-medium text-gray-300 mb-1"
                        >
                            Display Name (optional)
                        </label>
                        <input
                            id="display-name"
                            type="text"
                            value={displayName}
                            onChange={(e) => setDisplayName(e.target.value)}
                            className="ui-input"
                            placeholder="e.g., GLM-5 Failover"
                        />
                    </div>

                    <div>
                        <label className="block text-sm font-medium text-gray-300 mb-1">
                            Model Entries
                        </label>
                        <input
                            type="text"
                            value={search}
                            onChange={(e) => setSearch(e.target.value)}
                            className="ui-input mb-2"
                            placeholder="Search providers/models..."
                        />
                        <div className="max-h-48 overflow-y-auto bg-gray-900 rounded-lg p-2 space-y-1">
                            {Object.entries(grouped).map(
                                ([modelId, models]) => (
                                    <div key={modelId} className="space-y-0.5">
                                        <div className="text-xs text-gray-500 px-1 pt-1">
                                            {modelId}
                                        </div>
                                        {models.map((m) => (
                                            <label
                                                key={m.model_uuid}
                                                className="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-gray-800 cursor-pointer"
                                            >
                                                <input
                                                    type="checkbox"
                                                    checked={selectedEntries.includes(
                                                        m.model_uuid,
                                                    )}
                                                    onChange={(e) => {
                                                        if (e.target.checked) {
                                                            setSelectedEntries([
                                                                ...selectedEntries,
                                                                m.model_uuid,
                                                            ]);
                                                        } else {
                                                            setSelectedEntries(
                                                                selectedEntries.filter(
                                                                    (id) =>
                                                                        id !==
                                                                        m.model_uuid,
                                                                ),
                                                            );
                                                        }
                                                    }}
                                                    className="rounded border-gray-600 text-(--accent) focus:ring-(--accent)"
                                                />
                                                <span className="text-sm text-gray-300">
                                                    {m.provider_name}
                                                    <span className="text-gray-500 ml-1 text-xs">
                                                        (
                                                        {m.display_name ||
                                                            modelId}
                                                        )
                                                    </span>
                                                </span>
                                            </label>
                                        ))}
                                    </div>
                                ),
                            )}
                        </div>
                        <p className="text-gray-500 text-xs mt-1">
                            {selectedEntries.length} selected
                        </p>
                    </div>

                    <div className="flex justify-end gap-3 pt-4">
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
                            className="px-3 py-1.5 text-xs rounded-full border bg-(--accent-light) text-(--accent) border-(--accent-lighter) cursor-pointer hover:brightness-125 transition-all disabled:opacity-50"
                        >
                            {createMutation.isPending
                                ? "Creating..."
                                : "Create Group"}
                        </button>
                    </div>
                </form>
            </div>
        </div>
    );
}

export function FailoverGroups() {
    const { toast } = useToast();
    const queryClient = useQueryClient();

    const [showCreateModal, setShowCreateModal] = useState(false);
    const [deleteGroup, setDeleteGroup] = useState<FailoverGroup | null>(null);

    const { data: groups, isLoading } = useQuery({
        queryKey: ["failover-groups"],
        queryFn: () => api.failoverGroups.list(),
    });

    const { data: candidates } = useQuery({
        queryKey: ["failover-candidates"],
        queryFn: () => api.failoverGroups.candidates(),
    });

    const syncMutation = useMutation({
        mutationFn: () => api.failoverGroups.sync(),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
            toast("Failover groups synced", "success");
        },
        onError: (err: Error) => {
            toast(`Failed to sync: ${err.message}`, "error");
        },
    });

    const updateMutation = useMutation({
        mutationFn: ({
            id,
            data,
        }: {
            id: string;
            data: Parameters<typeof api.failoverGroups.update>[1];
        }) => api.failoverGroups.update(id, data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
        },
        onError: (err: Error) => {
            toast(`Failed to update: ${err.message}`, "error");
        },
    });

    const deleteMutation = useMutation({
        mutationFn: (id: string) => api.failoverGroups.delete(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
            toast("Group deleted", "success");
        },
        onError: (err: Error) => {
            toast(`Failed to delete: ${err.message}`, "error");
        },
    });

    const handleToggleGroup = (group: FailoverGroup, enabled: boolean) => {
        updateMutation.mutate({
            id: group.id,
            data: { group_enabled: enabled },
        });
    };

    const handleToggleEntry = (
        group: FailoverGroup,
        uuid: string,
        enabled: boolean,
    ) => {
        const enabledCount = group.entries.filter((e) => e.enabled).length;
        if (!enabled && enabledCount <= 1) {
            toast("At least one provider must remain active", "error");
            return;
        }
        const entryEnabledMap: Record<string, boolean> = {};
        group.entries.forEach((e) => {
            entryEnabledMap[e.model_uuid] = e.enabled;
        });
        entryEnabledMap[uuid] = enabled;
        updateMutation.mutate({
            id: group.id,
            data: { entry_enabled: entryEnabledMap },
        });
    };

    const handleReorder = (group: FailoverGroup, newOrder: string[]) => {
        updateMutation.mutate({
            id: group.id,
            data: { priority_order: newOrder },
        });
    };

    const handleDelete = (group: FailoverGroup) => {
        setDeleteGroup(group);
    };

    const confirmDelete = () => {
        if (deleteGroup) {
            deleteMutation.mutate(deleteGroup.id);
            setDeleteGroup(null);
        }
    };

    if (isLoading) {
        return (
            <div className="flex items-center justify-center h-64">
                <div className="text-gray-500">Loading...</div>
            </div>
        );
    }

    return (
        <div>
            <div className="flex items-center justify-between mb-6">
                <div className="flex items-center gap-3">
                    <Shuffle
                        size={28}
                        strokeWidth={2}
                        className="text-(--accent)"
                    />
                    <h1 className="text-2xl font-bold text-white">
                        Failover Groups
                    </h1>
                </div>
                <div className="flex gap-3">
                    <button
                        type="button"
                        onClick={() => syncMutation.mutate()}
                        disabled={syncMutation.isPending}
                        className="px-3 py-1.5 text-xs rounded-full border bg-gray-900/40 text-gray-300 border-gray-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(156,163,175,0.15)] transition-all disabled:opacity-50"
                    >
                        {syncMutation.isPending ? "Syncing..." : "Sync"}
                    </button>
                    <button
                        type="button"
                        onClick={() => setShowCreateModal(true)}
                        className="px-3 py-1.5 text-xs rounded-full border bg-(--accent-light) text-(--accent) border-(--accent-lighter) cursor-pointer hover:brightness-125 transition-all"
                    >
                        New Group
                    </button>
                </div>
            </div>

            <p className="text-gray-400 text-sm mb-1">
                Failover groups let you route requests through multiple
                providers in priority order. Use{" "}
                <code className="text-(--accent)">hotel/model-name</code> to
                route through a group, or{" "}
                <code className="text-(--accent)">provider/model-name</code> to
                use a specific provider.
            </p>
            <p className="text-(--text-muted) text-xs mb-6 flex items-center gap-1.5">
                <span className="text-xs shrink-0" aria-hidden="true">
                    ⠿
                </span>
                Drag models by the handle (⠿) to reorder priority
            </p>

            {groups && groups.length === 0 ? (
                <div className="text-center py-12">
                    <div className="text-gray-500 mb-4">
                        No failover groups configured
                    </div>
                    <button
                        type="button"
                        onClick={() => syncMutation.mutate()}
                        className="px-3 py-1.5 text-xs rounded-full border bg-(--accent-light) text-(--accent) border-(--accent-lighter) cursor-pointer hover:brightness-125 transition-all"
                    >
                        Auto-discover from models
                    </button>
                </div>
            ) : (
                <div className="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-3 gap-4">
                    {groups?.map((group) => (
                        <FailoverGroupCard
                            key={group.id}
                            group={group}
                            onToggleGroup={(enabled) =>
                                handleToggleGroup(group, enabled)
                            }
                            onToggleEntry={(uuid, enabled) =>
                                handleToggleEntry(group, uuid, enabled)
                            }
                            onReorder={(newOrder) =>
                                handleReorder(group, newOrder)
                            }
                            onDelete={() => handleDelete(group)}
                        />
                    ))}
                </div>
            )}

            {showCreateModal && candidates && (
                <CreateGroupModal
                    candidates={candidates}
                    onClose={() => setShowCreateModal(false)}
                    onCreated={() => setShowCreateModal(false)}
                />
            )}

            {deleteGroup && (
                <div
                    role="dialog"
                    aria-modal="true"
                    className="fixed inset-0 flex items-center justify-center z-50"
                    onKeyDown={(e) =>
                        e.key === "Escape" && setDeleteGroup(null)
                    }
                >
                    <button
                        type="button"
                        className="absolute inset-0 bg-black/60 cursor-default"
                        onClick={() => setDeleteGroup(null)}
                        aria-label="Close dialog"
                    />
                    <div className="relative ui-card p-6 w-full max-w-sm">
                        <h2 className="text-lg font-bold text-white mb-2">
                            Delete Failover Group
                        </h2>
                        <p className="text-sm text-gray-300 mb-4">
                            Are you sure you want to delete{" "}
                            <span className="text-white font-medium">
                                hotel/{deleteGroup.display_model}
                            </span>
                            ? This cannot be undone.
                        </p>
                        <div className="flex gap-3 justify-end">
                            <button
                                type="button"
                                onClick={() => setDeleteGroup(null)}
                                className="px-3 py-1.5 text-xs rounded-full border bg-gray-900/40 text-gray-300 border-gray-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(156,163,175,0.15)] transition-all"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={confirmDelete}
                                disabled={deleteMutation.isPending}
                                className="px-3 py-1.5 text-xs rounded-full border bg-red-900/50 text-red-400 border-red-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(239,68,68,0.2)] transition-all disabled:opacity-50"
                            >
                                {deleteMutation.isPending
                                    ? "Deleting..."
                                    : "Delete"}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}
