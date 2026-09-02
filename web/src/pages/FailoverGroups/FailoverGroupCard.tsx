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
import { useCopyToClipboard } from "../../hooks/useCopyToClipboard";
import { formatTokens } from "../../utils/format";
import {
	type EntryCircuitView,
	entryCircuitStatus,
	entryCircuitView,
	groupCircuitSummary,
} from "./entryCircuit";
import { SortableEntry } from "./SortableEntry";

// A stable key over the entries, so the card resets its local state when the
// server data changes. Includes the enabled flag, so a toggle is detected and
// not just a change of UUID order.
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
	managed,
	cbProviderMap,
	onResetCircuit,
	resetPendingProviderId,
}: {
	group: FailoverGroup;
	selected: boolean;
	onToggleSelect: (selected: boolean) => void;
	onToggleGroup: (enabled: boolean) => void;
	onToggleEntry: (uuid: string, enabled: boolean) => void;
	onReorder: (newOrder: string[]) => void;
	onDelete: () => void;
	onEdit?: () => void;
	// When true this group's config is managed by the fleet primary. Every write
	// here (edit, delete, reorder/priority_order, the group on/off flag and the
	// per-entry enabled flags) is synced config the next config sync overwrites,
	// so all of them are hidden or locked.
	managed?: boolean;
	cbProviderMap: Map<string, CircuitBreakerProviderStatus>;
	// Passed straight to the entries. Unlike every other write on this card it
	// survives `managed`: clearing a circuit is local runtime recovery, not a
	// config edit the primary overwrites.
	onResetCircuit?: (providerId: string, providerName: string) => void;
	resetPendingProviderId?: string;
}) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const { copy } = useCopyToClipboard({ trackCopied: false });

	// Optimistic local state: reorders on dragEnd so the DOM order matches the
	// visual drag position.
	const [localEntries, setLocalEntries] = useState(group.entries);
	const key = useMemo(() => entriesKey(group.entries), [group.entries]);

	// Resets the local state when the server data changes. Comparing the key
	// during render avoids the setState-in-effect lint error while still syncing.
	const [prevKey, setPrevKey] = useState(key);
	if (prevKey !== key) {
		setPrevKey(key);
		setLocalEntries(group.entries);
	}

	// The breaker's view of each entry the router will actually use: the entry
	// toggle on AND the underlying model and provider enabled. One map feeds the
	// count, each entry's chip and the header, so the three cannot drift apart.
	// The parent rebuilds cbProviderMap on every poll, which is what keeps the
	// busy window current: memoise that map upstream and this needs its own tick.
	const circuitViews = useMemo(() => {
		const views = new Map<string, EntryCircuitView>();
		for (const e of localEntries) {
			if (e.enabled && e.model_enabled && e.provider_enabled) {
				views.set(
					e.model_uuid,
					entryCircuitView(cbProviderMap.get(e.provider_id), e.model_id),
				);
			}
		}
		return views;
	}, [localEntries, cbProviderMap]);
	const enabledCount = circuitViews.size;
	const totalCount = localEntries.length;
	const summary = useMemo(
		() => groupCircuitSummary([...circuitViews.values()]),
		[circuitViews],
	);

	const sensors = useSensors(
		useSensor(PointerSensor),
		useSensor(KeyboardSensor, {
			coordinateGetter: sortableKeyboardCoordinates,
		}),
	);

	const handleDragEnd = (event: DragEndEvent) => {
		// Reorder writes priority_order, which sync replaces; locked while managed.
		if (!group.group_enabled || managed) return;
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

	const handleCopyModel = async () => {
		const modelRef = `hotel/${group.display_model}`;
		if (await copy(modelRef))
			toast(t("failover.copied_model", { model: modelRef }), "success");
		else toast(t("common.failedToCopy"), "error");
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
					{!managed && (
						<input
							type="checkbox"
							checked={selected}
							onChange={(e) => onToggleSelect(e.target.checked)}
							className="rounded border-gray-600 text-(--accent) focus:ring-(--accent) shrink-0"
						/>
					)}
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
						<h3
							className="text-(--accent) font-medium text-sm truncate"
							title={`hotel/${group.display_model}`}
						>
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
				</div>
				<div className="flex items-center gap-2 shrink-0">
					{group.auto_created && (
						<span className="text-xs text-gray-500">
							{t("failover.auto_created")}
						</span>
					)}
					{managed ? (
						// The group on/off flag (group_enabled) is synced config, so under
						// management it is a static badge, not a toggle.
						<span
							className={`ui-badge px-2 py-px leading-[1.6] text-xs font-medium ${
								group.group_enabled ? "ui-badge-accent" : "ui-badge-neutral"
							}`}
						>
							<span className="badge-text">
								{group.group_enabled ? t("failover.on") : t("failover.off")}
							</span>
						</span>
					) : (
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
					)}
				</div>
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
								locked={managed}
								cbStatus={entryCircuitStatus(
									cbProviderMap.get(entry.provider_id),
									entry.model_id,
								)}
								circuitView={circuitViews.get(entry.model_uuid)}
								onResetCircuit={onResetCircuit}
								resetPending={resetPendingProviderId === entry.provider_id}
							/>
						))}
					</div>
				</SortableContext>
			</DndContext>

			<div className="flex items-center justify-between mt-auto pt-2 text-xs text-gray-500">
				<span>
					{enabledCount}/{totalCount} {t("failoverGroups.card.active")} •{" "}
					{formatTokens(group.total_tokens)} {t("common.tokens")}
					{group.group_enabled && summary.total > 0 && (
						<>
							<br />
							{summary.allDark ? (
								<span
									className="text-red-400 font-medium"
									data-testid="failover-card-all-dark"
								>
									{summary.earliestRetryAt
										? t("failoverGroups.card.allEntriesDarkRetry", {
												when: new Date(
													summary.earliestRetryAt,
												).toLocaleTimeString(),
											})
										: t("failoverGroups.card.allEntriesDark")}
								</span>
							) : (
								<span data-testid="failover-card-live-count">
									{t("failoverGroups.card.entriesLive", {
										live: summary.live,
										total: summary.total,
									})}
								</span>
							)}
						</>
					)}
				</span>
				<div className="flex items-center gap-1">
					{!group.auto_created && onEdit && !managed && (
						<button
							type="button"
							onClick={onEdit}
							className="ui-btn ui-btn-compact text-(--text-muted) hover:text-amber-400 hover:bg-white/5"
						>
							{t("common.edit")}
						</button>
					)}
					{!managed && (
						<button
							type="button"
							onClick={() => onDelete()}
							className="ui-btn ui-btn-compact text-(--text-muted) hover:text-red-400 hover:bg-white/5"
						>
							{t("common.delete")}
						</button>
					)}
				</div>
			</div>
		</div>
	);
}
