import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useTranslation } from "react-i18next";
import type { FailoverGroup } from "../../api/types";
import { FuseOutline } from "../../components/FuseOutline";
import { Toggle } from "../../components/Toggle";

export interface SortableEntryProps {
	entry: FailoverGroup["entries"][0];
	groupEnabled: boolean;
	onToggle: (uuid: string, enabled: boolean) => void;
	cbStatus?: {
		state: string;
		cooldown_ms?: number;
		next_retry_at?: string;
		opened_at?: string;
		consecutive_fails: number;
	};
}

export function SortableEntry({
	entry,
	groupEnabled,
	onToggle,
	cbStatus,
}: SortableEntryProps) {
	const { t } = useTranslation();
	const {
		attributes,
		listeners,
		setNodeRef,
		transform,
		transition,
		isDragging,
	} = useSortable({ id: entry.model_uuid, disabled: !groupEnabled });

	const style: React.CSSProperties = {
		transform: CSS.Transform.toString(transform),
		transition,
		opacity: isDragging ? 0.5 : 1,
	};

	const dragProps = groupEnabled ? { ...attributes, ...listeners } : {};

	// Determine if fuse should show (circuit breaker open/half-open with enough fails)
	const showFuse =
		cbStatus &&
		entry.enabled &&
		(cbStatus.state === "open" || cbStatus.state === "half-open") &&
		cbStatus.consecutive_fails >= 5;

	// Half-open: cooldown already elapsed, provider is actively probing.
	// Show a static amber outline — no countdown animation.
	// Open: cooldown is running, show animated fuse outline.
	const isHalfOpen = showFuse && cbStatus.state === "half-open";

	let fuseColor: string | undefined;
	let remainingMs = 0;
	let fuseTitle: string | undefined;

	if (showFuse) {
		if (isHalfOpen) {
			fuseColor = "#fde68a";
			fuseTitle = t("failoverGroups.entry.circuitBreakerHalfOpen");
		} else {
			fuseColor = "#fca5a5";
			fuseTitle = t("failoverGroups.entry.circuitBreakerOpen");
		}

		// Compute remaining cooldown time (only meaningful for open state)
		if (cbStatus.next_retry_at) {
			remainingMs = new Date(cbStatus.next_retry_at).getTime() - Date.now();
		}
		remainingMs = Math.max(0, remainingMs);
	}

	return (
		<div
			ref={setNodeRef}
			style={{ ...style, overflow: showFuse ? "hidden" : undefined }}
			className={`relative flex items-center justify-between px-2 py-1 rounded group text-sm ${
				entry.enabled ? "bg-gray-700" : "failover-entry-disabled"
			}`}
			{...(fuseTitle ? { title: fuseTitle } : {})}
		>
			{showFuse && fuseColor && isHalfOpen && (
				<div
					className="absolute inset-0 rounded pointer-events-none"
					style={{ boxShadow: `inset 0 0 0 1.5px ${fuseColor}` }}
				/>
			)}
			{showFuse && fuseColor && !isHalfOpen && (
				<FuseOutline color={fuseColor} durationMs={remainingMs} />
			)}
			<div className="flex items-center gap-2 min-w-0">
				<span
					{...dragProps}
					className={`text-(--text-tertiary) shrink-0 transition-opacity ${
						groupEnabled
							? "cursor-grab active:cursor-grabbing opacity-40 hover:opacity-100"
							: "cursor-not-allowed opacity-30"
					}`}
				>
					⠿
				</span>
				<div className="truncate failover-entry-text">
					<span className="text-(--text-primary)">{entry.provider_name}</span>
					<span className="text-gray-500 mx-1">/</span>
					<span className="text-gray-400 truncate">{entry.model_id}</span>
				</div>
			</div>
			<Toggle
				size="sm"
				checked={entry.enabled}
				disabled={!groupEnabled}
				onChange={(v) => onToggle(entry.model_uuid, v)}
				ariaLabel={
					entry.enabled
						? t("failoverGroups.entry.disableProvider")
						: t("failoverGroups.entry.enableProvider")
				}
			/>
		</div>
	);
}
