import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { RotateCcw } from "@/lib/icons";
import type { FailoverGroup } from "../../api/types";
import { FuseOutline } from "../../components/FuseOutline";
import { Toggle } from "../../components/Toggle";
import { naReasonKey } from "../../utils/failoverEntry";
import type { EntryCircuitView } from "./entryCircuit";

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
		// Set when a quota pin is in force: the cooldown was pinned to the
		// provider's quota reset deadline. next_retry_at is then that deadline
		// unless a longer backoff is also in force, which is rare (a pin is
		// floored at the backoff when it is stamped).
		quota_pinned?: boolean;
		// Set when a probe backoff is in force: the cooldown doubled once per
		// failed half-open probe. Says why the wait is longer than the setting,
		// the way quota_pinned does; next_retry_at is the longer of the two.
		backed_off?: boolean;
		// The derived verdict that the breaker is skipping this provider for
		// every model, and the model ids it is blocking. Which entries get a
		// status at all is entryCircuitStatus's decision; these two are here so
		// the tooltip can say whether the whole provider is out and name the
		// models the verdict rests on.
		provider_open?: boolean;
		open_models?: string[];
	};
	// Forces this provider's circuit closed. Omitted when the caller cannot
	// reset (read-only demo mode), which is what hides the control. Deliberately
	// NOT gated on `locked`: a breaker is local runtime health, not synced
	// config, so a managed member must still be able to clear its own.
	onResetCircuit?: (providerId: string, providerName: string) => void;
	resetPending?: boolean;
	// The entry's own circuit as the chip and tooltip read it (entryCircuitView):
	// live / busy / open / probe / pinned, with the last cause when the member
	// reports circuits[]. Optional so older callers and tests render as before.
	circuitView?: EntryCircuitView;
}

// The chip's badge class per state.
const CHIP_CLASS: Record<EntryCircuitView["chip"], string> = {
	live: "ui-badge-neutral",
	busy: "ui-badge-warning",
	open: "ui-badge-danger",
	probe: "ui-badge-warning",
	pinned: "ui-badge-purple",
};

// A quota pin can run for hours or days, and a CSS animation over that span is
// visually frozen: the operator sees a motionless fuse and reads it as broken.
// Above this much remaining cooldown the outline is static and the deadline
// lives in the tooltip instead.
const FUSE_ANIMATION_MAX_MS = 15 * 60 * 1000;

// How often a still-too-long countdown re-checks whether it has come inside the
// window above. Coarse on purpose: the threshold is a rough "is this worth
// animating" judgement, so arriving up to half a minute late costs nothing,
// while a per-second clock would re-render every open entry in every group for
// the entire cooldown.
const FUSE_THRESHOLD_TICK_MS = 30 * 1000;

export function SortableEntry({
	entry,
	groupEnabled,
	onToggle,
	locked,
	cbStatus,
	onResetCircuit,
	resetPending,
	circuitView,
}: SortableEntryProps) {
	const { t } = useTranslation();
	// The cause line the tooltip appends when the member reports the circuit's
	// last verdict: what the breaker saw, the upstream status behind it and
	// when. Absent on an older member, where only the row is known.
	const causeLine = circuitView?.lastCause
		? t("failoverGroups.entry.circuitCause", {
				cause: circuitView.lastCause,
				status: circuitView.lastStatus ?? "-",
				when: circuitView.lastAt
					? new Date(circuitView.lastAt).toLocaleString()
					: "-",
			})
		: undefined;
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

	// The cooldown in force has been doubled by failed probes. Ranked below a
	// provider-wide skip in the tooltip: that a whole provider is out matters
	// more than why this one model's wait is long, and a backoff never implies
	// the provider verdict the way a quota pin does. Ranked below the quota pin
	// too, because a pin names the cause and is nearly always the longer of
	// the two when both are set; the status has no field for which one governs.
	const backedOff = Boolean(showFuse && cbStatus.backed_off);

	const nextRetryAt = cbStatus?.next_retry_at;

	// The instant the countdown was last measured against. It advances only on
	// the coarse tick below, never on an ordinary re-render, which is what keeps
	// remainingMs stable: without that, intermediate re-renders (drag, toggle,
	// parent refetch) would shorten it each time and the fuse would snap ahead of
	// the cooldown it visualises.
	const [measuredAt, setMeasuredAt] = useState(() => Date.now());

	// Which deadline that anchor belongs to. An entry that never unmounts can be
	// handed a *new* next_retry_at — the circuit re-opened, or a quota pin
	// replaced an ordinary cooldown — and measuring that against the anchor of
	// the deadline it replaced folds all the time that passed before it arrived
	// into the new duration: the fuse burns too slowly, or sits static for a
	// cooldown that belongs inside the animation window. So the anchor is taken
	// again whenever the deadline changes, and only then — re-taking it on every
	// render is the very thing measuredAt exists to prevent.
	//
	// Layout effect, not a plain one: the fuse restarts its CSS timeline every
	// time durationMs changes, so a frame painted from the stale anchor would
	// show a flame of the wrong length before snapping to the right one.
	const anchoredTo = useRef(nextRetryAt);
	useLayoutEffect(() => {
		if (anchoredTo.current === nextRetryAt) return;
		anchoredTo.current = nextRetryAt;
		setMeasuredAt(Date.now());
	}, [nextRetryAt]);

	// A countdown too long to animate has to keep watching the clock, because the
	// animate-vs-static decision is a function of *now* while next_retry_at never
	// changes for as long as the circuit stays open. Settled once at mount, an
	// entry that appeared at 16 minutes remaining stayed static for the rest of
	// its cooldown and never began burning as it crossed the threshold.
	//
	// The watch stops itself at the crossing rather than running for the whole
	// cooldown. Time only moves one way, so the decision cannot reverse, and
	// FuseOutline restarts its CSS timeline whenever durationMs changes: a
	// re-measured duration every tick would reset the flame to full length twice
	// a minute. The last measurement it publishes is the one at the crossing,
	// which is exactly the duration the animation should then run for.
	useEffect(() => {
		if (!showFuse || isHalfOpen || !nextRetryAt) return;
		const deadline = new Date(nextRetryAt).getTime();
		if (deadline - Date.now() <= FUSE_ANIMATION_MAX_MS) return;
		const id = setInterval(() => {
			const now = Date.now();
			if (deadline - now <= FUSE_ANIMATION_MAX_MS) clearInterval(id);
			setMeasuredAt(now);
		}, FUSE_THRESHOLD_TICK_MS);
		return () => clearInterval(id);
	}, [showFuse, isHalfOpen, nextRetryAt]);

	// Elapsed cooldown: circuit is open but the deadline has passed — the breaker
	// has not reported half-open yet (clock drift or polling delay).
	const { remainingMs, elapsedCooldown } = useMemo(() => {
		if (!showFuse || isHalfOpen || !nextRetryAt) {
			return { remainingMs: 0, elapsedCooldown: false };
		}
		const ms = Math.max(0, new Date(nextRetryAt).getTime() - measuredAt);
		return { remainingMs: ms, elapsedCooldown: ms <= 0 };
	}, [showFuse, isHalfOpen, nextRetryAt, measuredAt]);

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

	// The provider itself is skipped, so this entry is turned away whatever its
	// own model is doing. Naming the models the verdict rests on is the only way
	// an operator can tell a provider outage from two unrelated models failing.
	const openModels = cbStatus?.open_models;
	const providerSkipped = Boolean(
		showFuse && cbStatus.provider_open && openModels && openModels.length > 0,
	);

	const fuseColor = cooldownOver ? "#fde68a" : showFuse ? "#fca5a5" : undefined;
	const baseTitle = !showFuse
		? undefined
		: cooldownOver
			? t("failoverGroups.entry.circuitBreakerReadyToProbe")
			: quotaPinned && cbStatus.next_retry_at
				? t("failoverGroups.entry.circuitBreakerQuotaPinned", {
						resetTime: new Date(cbStatus.next_retry_at).toLocaleString(),
					})
				: providerSkipped
					? t("failoverGroups.entry.circuitBreakerProviderOpen", {
							models: openModels?.join(", "),
						})
					: backedOff && cbStatus.next_retry_at
						? t("failoverGroups.entry.circuitBreakerBackedOff", {
								resetTime: new Date(cbStatus.next_retry_at).toLocaleString(),
							})
						: t("failoverGroups.entry.circuitBreakerOpen");
	// A busy entry has no fuse (its circuit is closed) but still explains itself.
	const chipTitle =
		circuitView?.chip === "busy"
			? t("failoverGroups.entry.chipBusyTip")
			: undefined;
	const fuseTitle = [baseTitle ?? chipTitle, causeLine]
		.filter(Boolean)
		.join("\n");

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
				{circuitView && entry.enabled && !effectivelyDisabled && (
					<span
						className={`ui-badge ${CHIP_CLASS[circuitView.chip]} shrink-0 text-[10px] leading-[1.6] px-1.5 cursor-help`}
						data-testid="failover-entry-chip"
						data-chip={circuitView.chip}
						title={fuseTitle || undefined}
					>
						{t(`failoverGroups.entry.chip.${circuitView.chip}`)}
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
