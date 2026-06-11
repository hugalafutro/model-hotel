import {
	closestCenter,
	DndContext,
	type DragEndEvent,
	KeyboardSensor,
	PointerSensor,
	useSensor,
	useSensors,
} from "@dnd-kit/core";
import {
	arrayMove,
	SortableContext,
	sortableKeyboardCoordinates,
	verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type {
	CircuitBreakerProviderStatus,
	FailoverGroup,
} from "../../api/types";
import { useToast } from "../../context/ToastContext";
import { formatTokens } from "../../utils/format";
import { SortableEntry } from "./SortableEntry";

// Derive a stable key from entries so the card resets local state
// when the server data changes (after mutation/refetch).
// Includes enabled state so toggles are detected, not just UUID order.
function entriesKey(entries: FailoverGroup["entries"]): string {
	return entries.map((e) => `${e.model_uuid}:${e.enabled}`).join(",");
}

export function FailoverGroupCard({
	group,
	selected,
	onToggleSelect,
	onToggleGroup,
	onToggleEntry,
	onReorder,
	onDelete,
	onEdit,
	cbProviderMap,
}: {
	group: FailoverGroup;
	selected: boolean;
	onToggleSelect: (selected: boolean) => void;
	onToggleGroup: (enabled: boolean) => void;
	onToggleEntry: (uuid: string, enabled: boolean) => void;
	onReorder: (newOrder: string[]) => void;
	onDelete: () => void;
	onEdit?: () => void;
	cbProviderMap: Map<string, CircuitBreakerProviderStatus>;
}) {
	const { t } = useTranslation();
	const { toast } = useToast();

	// Optimistic local state: reorders immediately on dragEnd so the DOM
	// order matches the visual drag position. key-based reset ensures
	// local state re-syncs when the server data changes after mutation.
	const [localEntries, setLocalEntries] = useState(group.entries);
	const key = useMemo(() => entriesKey(group.entries), [group.entries]);

	// When server data changes, reset local state. Using key as a dep
	// avoids the lint error from setState-in-effect while still syncing.
	const [prevKey, setPrevKey] = useState(key);
	if (prevKey !== key) {
		setPrevKey(key);
		setLocalEntries(group.entries);
	}

	// Count only entries the router will actually use: the entry toggle must
	// be on AND the underlying model and provider must be enabled (matches
	// SortableEntry's effective-state display).
	const enabledCount = localEntries.filter(
		(e) => e.enabled && e.model_enabled && e.provider_enabled,
	).length;
	const totalCount = localEntries.length;

	const sensors = useSensors(
		useSensor(PointerSensor),
		useSensor(KeyboardSensor, {
			coordinateGetter: sortableKeyboardCoordinates,
		}),
	);

	const handleDragEnd = (event: DragEndEvent) => {
		if (!group.group_enabled) return;
		const { active, over } = event;
		if (over && active.id !== over.id) {
			const oldIndex = localEntries.findIndex(
				(e) => e.model_uuid === active.id,
			);
			const newIndex = localEntries.findIndex((e) => e.model_uuid === over.id);
			const reordered = arrayMove(localEntries, oldIndex, newIndex);
			setLocalEntries(reordered); // immediate optimistic update
			onReorder(reordered.map((e) => e.model_uuid));
		}
	};

	const handleCopyModel = () => {
		const modelRef = `hotel/${group.display_model}`;
		navigator.clipboard.writeText(modelRef);
		toast(t("failover.copied_model", { model: modelRef }), "success");
	};

	return (
		<div
			className={`ui-card p-3 flex flex-col ${
				group.group_enabled
					? "border-(--accent)/30"
					: "opacity-45 border-dashed border-gray-600"
			}`}
		>
			<div className="flex items-center justify-between mb-2">
				<div className="flex items-center gap-2 min-w-0">
					<input
						type="checkbox"
						checked={selected}
						onChange={(e) => onToggleSelect(e.target.checked)}
						className="rounded border-gray-600 text-(--accent) focus:ring-(--accent) shrink-0"
					/>
					{/* biome-ignore lint/a11y/useSemanticElements: cannot change to <button> without altering layout */}
					<div
						onClick={handleCopyModel}
						onKeyDown={(e) => {
							if (e.key === "Enter" || e.key === " ") {
								e.preventDefault();
								handleCopyModel();
							}
						}}
						role="button"
						tabIndex={0}
						className="flex items-center gap-1.5 min-w-0 select-none px-1.5 py-0.5 -mx-1.5 -my-0.5 rounded hover:bg-gray-700 transition-colors group cursor-default"
						title={t("failover.group.clickToCopy")}
					>
						<h3 className="text-(--accent) font-medium text-sm truncate">
							hotel/{group.display_model}
						</h3>
						<svg
							className="w-3.5 h-3.5 text-gray-500 opacity-0 group-hover:opacity-100 transition-opacity shrink-0"
							fill="none"
							stroke="currentColor"
							viewBox="0 0 24 24"
						>
							<title>{t("failoverGroups.card.copy")}</title>
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
							{t("failover.auto_created")}
						</span>
					)}
				</div>
				<button
					type="button"
					onClick={() => onToggleGroup(!group.group_enabled)}
					className={`ui-badge px-2 py-px leading-[1.6] text-xs font-medium transition-colors ${
						group.group_enabled
							? "ui-badge-accent hover:brightness-125"
							: "ui-badge-neutral hover:brightness-125"
					}`}
				>
					<span className="badge-text">
						{group.group_enabled ? t("failover.on") : t("failover.off")}
					</span>
				</button>
			</div>

			<DndContext
				sensors={sensors}
				collisionDetection={closestCenter}
				onDragEnd={handleDragEnd}
			>
				<SortableContext
					items={localEntries.map((e) => e.model_uuid)}
					strategy={verticalListSortingStrategy}
				>
					<div className="space-y-0.5">
						{localEntries.map((entry) => (
							<SortableEntry
								key={entry.model_uuid}
								entry={entry}
								groupEnabled={group.group_enabled}
								onToggle={onToggleEntry}
								cbStatus={cbProviderMap.get(entry.provider_id)}
							/>
						))}
					</div>
				</SortableContext>
			</DndContext>

			<div className="flex items-center justify-between mt-auto pt-2 text-xs text-gray-500">
				<span>
					{enabledCount}/{totalCount} {t("failoverGroups.card.active")} •{" "}
					{formatTokens(group.total_tokens)} {t("common.tokens")}
				</span>
				<div className="flex items-center gap-1">
					{!group.auto_created && onEdit && (
						<button
							type="button"
							onClick={onEdit}
							className="ui-btn ui-btn-compact text-gray-500 hover:text-amber-400 hover:bg-white/5 cursor-pointer transition-all"
						>
							{t("common.edit")}
						</button>
					)}
					<button
						type="button"
						onClick={() => onDelete()}
						className="ui-btn ui-btn-compact text-gray-500 hover:text-red-400 hover:bg-white/5 cursor-pointer transition-all"
					>
						{t("common.delete")}
					</button>
				</div>
			</div>
		</div>
	);
}
