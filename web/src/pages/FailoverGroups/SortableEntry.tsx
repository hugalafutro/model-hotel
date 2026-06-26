import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { FailoverGroup } from "../../api/types";
import { FuseOutline } from "../../components/FuseOutline";
import { Toggle } from "../../components/Toggle";
import { naReasonKey } from "../../utils/failoverEntry";

export interface SortableEntryProps {
	entry: FailoverGroup["entries"][0];
	groupEnabled: boolean;
	onToggle: (uuid: string, enabled: boolean) => void;
	// When true the group is managed by the fleet primary: the per-entry toggle
	// (entry.enabled) and reordering (priority_order) are synced config that the
	// next config sync overwrites, so both are locked here.
	locked?: boolean;
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
	locked,
	cbStatus,
}: SortableEntryProps) {
	const { t } = useTranslation();
	const draggable = groupEnabled && !locked;
	const {
		attributes,
		listeners,
		setNodeRef,
		transform,
		transition,
		isDragging,
	} = useSortable({ id: entry.model_uuid, disabled: !draggable });

	const style: React.CSSProperties = {
		transform: CSS.Transform.toString(transform),
		transition,
		opacity: isDragging ? 0.5 : 1,
	};

	const dragProps = draggable ? { ...attributes, ...listeners } : {};

	// The router skips entries whose model or provider is disabled regardless
	// of the per-entry toggle; reflect that effective state in the UI. Only an
	// explicit false counts as disabled (the backend always sends real
	// booleans) so missing/partial data never mislabels an entry as dead.
	const effectivelyDisabled =
		entry.model_enabled === false || entry.provider_enabled === false;

	// Why the member is N/A, shown on the badge and the locked toggle. The
	// operator wants the cause (provider off / disabled by hand / dropped by
	// discovery), not a restatement that it is unavailable.
	const naReason = naReasonKey(entry);
	const naReasonText = naReason ? t(naReason) : undefined;

	// Determine if fuse should show (circuit breaker open/half-open).
	// We trust the circuit breaker's own state — the backend already enforces
	// the configured threshold before transitioning to open/half-open.
	const showFuse =
		cbStatus &&
		entry.enabled &&
		(cbStatus.state === "open" || cbStatus.state === "half-open");

	// Half-open: cooldown already elapsed, provider is actively probing.
	// Show a static amber outline — no countdown animation.
	// Open: cooldown is running, show animated fuse outline.
	const isHalfOpen = showFuse && cbStatus.state === "half-open";

	// Compute remaining cooldown so it only changes when next_retry_at
	// changes, not on every render. Without this, intermediate re-renders
	// (drag, toggle, parent) shorten remainingMs each time, causing the
	// fuse animation to visually snap ahead of the actual cooldown.
	// Elapsed cooldown: circuit is open but cooldown has expired — CB hasn't
	// transitioned to half-open yet (clock drift or polling delay).
	/* eslint-disable react-hooks/preserve-manual-memoization, react-hooks/purity */
	const { remainingMs, elapsedCooldown } = useMemo(() => {
		if (!showFuse || isHalfOpen || !cbStatus?.next_retry_at) {
			return { remainingMs: 0, elapsedCooldown: false };
		}
		const ms = Math.max(
			0,
			new Date(cbStatus.next_retry_at).getTime() - Date.now(),
		);
		return { remainingMs: ms, elapsedCooldown: ms <= 0 };
	}, [showFuse, isHalfOpen, cbStatus?.next_retry_at]);
	/* eslint-enable react-hooks/preserve-manual-memoization, react-hooks/purity */

	const fuseColor =
		showFuse && isHalfOpen ? "#fde68a" : showFuse ? "#fca5a5" : undefined;
	const fuseTitle = showFuse
		? isHalfOpen
			? t("failoverGroups.entry.circuitBreakerHalfOpen")
			: t("failoverGroups.entry.circuitBreakerOpen")
		: undefined;

	return (
		<div
			ref={setNodeRef}
			style={{ ...style, overflow: showFuse ? "hidden" : undefined }}
			className={`failover-entry relative flex items-center justify-between gap-2 px-2 py-1 group text-sm ${
				entry.enabled && !effectivelyDisabled
					? "bg-gray-700"
					: "failover-entry-disabled"
			}`}
			{...(fuseTitle ? { title: fuseTitle } : {})}
		>
			{showFuse && fuseColor && isHalfOpen && (
				<div
					className="absolute inset-0 rounded-[inherit] pointer-events-none"
					style={{ boxShadow: `inset 0 0 0 1.5px ${fuseColor}` }}
				/>
			)}
			{showFuse && fuseColor && !isHalfOpen && !elapsedCooldown && (
				<FuseOutline color={fuseColor} durationMs={remainingMs} />
			)}
			{showFuse && fuseColor && !isHalfOpen && elapsedCooldown && (
				<div
					className="absolute inset-0 rounded-[inherit] pointer-events-none"
					style={{ boxShadow: `inset 0 0 0 1.5px ${fuseColor}` }}
				/>
			)}
			<div className="flex items-center gap-2 flex-1 overflow-hidden min-w-0">
				<span
					{...dragProps}
					className={`text-(--text-tertiary) shrink-0 transition-opacity ${
						draggable
							? "cursor-grab active:cursor-grabbing opacity-40 hover:opacity-100"
							: "cursor-not-allowed opacity-30"
					}`}
				>
					⠿
				</span>
				<div
					className="truncate failover-entry-text flex-1 min-w-0 text-gray-400"
					title={`${entry.provider_name} / ${entry.model_id}`}
				>
					<span className="text-(--text-primary)">{entry.provider_name}</span>
					<span className="text-gray-500 mx-1">/</span>
					<span className="text-gray-400 truncate">{entry.model_id}</span>
				</div>
				{effectivelyDisabled && (
					<span
						className="ui-badge ui-badge-warning shrink-0 cursor-help"
						data-testid="failover-entry-effective-disabled"
						title={naReasonText}
					>
						{t("failoverGroups.entry.naBadge")}
					</span>
				)}
			</div>
			<Toggle
				size="sm"
				// Reflect effective state: an entry whose model/provider is disabled
				// is not routable, so show the toggle off and lock it. Flipping the
				// per-entry flag would do nothing while the underlying model is dead,
				// which is the confusing "toggle says on but it's disabled" case.
				checked={entry.enabled && !effectivelyDisabled}
				disabled={!groupEnabled || effectivelyDisabled || locked}
				onChange={(v) => onToggle(entry.model_uuid, v)}
				title={effectivelyDisabled ? naReasonText : undefined}
				ariaLabel={
					effectivelyDisabled
						? naReasonText
						: entry.enabled
							? t("failoverGroups.entry.disableProvider")
							: t("failoverGroups.entry.enableProvider")
				}
			/>
		</div>
	);
}
