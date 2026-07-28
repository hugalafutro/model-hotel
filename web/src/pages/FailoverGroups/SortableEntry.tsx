import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { RotateCcw } from "@/lib/icons";
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
		// Set when the cooldown was pinned to the provider's quota reset
		// deadline instead of the ordinary retry backoff; next_retry_at is then
		// that deadline.
		quota_pinned?: boolean;
	};
	// Forces this provider's circuit closed. Omitted when the caller cannot
	// reset (read-only demo mode), which is what hides the control. Deliberately
	// NOT gated on `locked`: a breaker is local runtime health, not synced
	// config, so a managed member must still be able to clear its own.
	onResetCircuit?: (providerId: string, providerName: string) => void;
	resetPending?: boolean;
}

// A quota pin can run for hours or days, and a CSS animation over that span is
// visually frozen: the operator sees a motionless fuse and reads it as broken.
// Above this much remaining cooldown the outline is static and the deadline
// lives in the tooltip instead.
const FUSE_ANIMATION_MAX_MS = 15 * 60 * 1000;

export function SortableEntry({
	entry,
	groupEnabled,
	onToggle,
	locked,
	cbStatus,
	onResetCircuit,
	resetPending,
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

	// The backend's own report that the cooldown has elapsed. One of the two
	// inputs to cooldownOver below; the other is the client noticing first.
	const isHalfOpen = showFuse && cbStatus.state === "half-open";

	// The cooldown in force was pinned to the provider's quota reset deadline.
	// This says why the wait is long, not that the provider is unreachable now.
	const quotaPinned = Boolean(showFuse && cbStatus.quota_pinned);

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

	// Cooldown is over and the provider is ready to probe. Two paths reach this:
	// the backend reporting half-open, and the client noticing next_retry_at has
	// passed before the next poll. They are the same state, so they render the
	// same way.
	const cooldownOver = Boolean(showFuse && (isHalfOpen || elapsedCooldown));

	// Only animate a countdown short enough for the motion to read as motion,
	// and only when there is a real deadline to count down to.
	const animateFuse =
		showFuse &&
		!cooldownOver &&
		remainingMs > 0 &&
		remainingMs <= FUSE_ANIMATION_MAX_MS;

	const fuseColor = cooldownOver ? "#fde68a" : showFuse ? "#fca5a5" : undefined;
	const fuseTitle = !showFuse
		? undefined
		: cooldownOver
			? t("failoverGroups.entry.circuitBreakerReadyToProbe")
			: quotaPinned && cbStatus.next_retry_at
				? t("failoverGroups.entry.circuitBreakerQuotaPinned", {
						resetTime: new Date(cbStatus.next_retry_at).toLocaleString(),
					})
				: t("failoverGroups.entry.circuitBreakerOpen");

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
			{showFuse && fuseColor && animateFuse && (
				<FuseOutline
					data-testid="fuse-outline-animated"
					color={fuseColor}
					durationMs={remainingMs}
				/>
			)}
			{showFuse && fuseColor && !animateFuse && (
				<div
					data-testid="fuse-outline-static"
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
			{/* Offered exactly where the fuse burns, i.e. an enabled member whose
			    circuit is open or half-open. A closed circuit has nothing to reset,
			    and a member the operator has switched off shows no breaker state at
			    all, so a lone reset button there would have no context to act on. */}
			{showFuse && onResetCircuit && (
				<button
					type="button"
					data-testid="failover-entry-reset-circuit"
					className="ui-icon-btn ui-icon-btn-warning shrink-0"
					disabled={resetPending}
					onClick={() => onResetCircuit(entry.provider_id, entry.provider_name)}
					title={t("failoverGroups.entry.resetCircuitBreaker")}
					aria-label={t("failoverGroups.entry.resetCircuitBreaker")}
				>
					<RotateCcw size={14} />
				</button>
			)}
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
