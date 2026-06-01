import { useCallback, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	Area,
	AreaChart,
	CartesianGrid,
	ResponsiveContainer,
	Tooltip,
	XAxis,
	YAxis,
} from "recharts";
import { Spinner } from "../../components/Spinner";
import { RangeToggle } from "./ToggleGroup";
import type { GaugeDataKey, Range, TimeSeriesDataPoint } from "./types";

export function TimeSeriesChart({
	data,
	range,
	onRangeChange,
	metric,
	icon: Icon,
	color,
	label,
	dataKey,
	allowDecimals = false,
	height = 240,
	showToggle = true,
	scale = 1,
	loading,
}: {
	data: TimeSeriesDataPoint[];
	range: Range;
	onRangeChange: (r: Range) => void;
	metric: string;
	icon: React.ElementType;
	color: string;
	label: string;
	dataKey: GaugeDataKey;
	allowDecimals?: boolean;
	height?: number;
	showToggle?: boolean;
	scale?: number;
	loading?: boolean;
}) {
	const { t } = useTranslation();
	const { grid, text } = useMemo(() => {
		const style = getComputedStyle(document.documentElement);
		return {
			grid:
				style.getPropertyValue("--border-default").trim() ||
				"rgba(255,255,255,0.06)",
			text: style.getPropertyValue("--text-tertiary").trim() || "#7a7e8c",
		};
	}, []);

	// Compute viewport size based on range
	const viewportSize = range === "1h" ? 12 : range === "24h" ? 24 : 7;

	// Last bucket is always the current time (backend fills to now),
	// so panning is bounded by the actual data range.
	const lastRealIndex = data.length - 1;

	// Drag-to-pan state: enabled when data exceeds viewport
	const pannable = data.length > viewportSize;
	const maxStart = Math.max(0, lastRealIndex - viewportSize + 1);
	// userStart is null until the user explicitly pans; null = snap to latest
	const [userStart, setUserStart] = useState<number | null>(null);
	const [viewportRange, setViewportRange] = useState(range);
	if (viewportRange !== range) {
		setViewportRange(range);
		setUserStart(null);
	}
	const [isDragging, setIsDragging] = useState(false);
	const dragRef = useRef<{
		startX: number;
		startOffset: number;
		containerWidth: number;
	} | null>(null);

	// If user hasn't panned, always show latest data (maxStart).
	// Otherwise, clamp their position to the valid range.
	const effectiveStart =
		userStart !== null ? Math.max(0, Math.min(userStart, maxStart)) : maxStart;

	const visibleData = pannable
		? data.slice(effectiveStart, effectiveStart + viewportSize)
		: data;

	const canPanLeft = pannable && effectiveStart > 0;
	const canPanRight = pannable && effectiveStart < maxStart;

	const onPointerDown = useCallback(
		(e: React.PointerEvent<HTMLDivElement>) => {
			if (!pannable) return;
			e.preventDefault();
			const container = e.currentTarget;
			container.setPointerCapture(e.pointerId);
			dragRef.current = {
				startX: e.clientX,
				startOffset: effectiveStart,
				containerWidth: container.getBoundingClientRect().width,
			};
			setIsDragging(true);
		},
		[pannable, effectiveStart],
	);

	const onPointerMove = useCallback(
		(e: React.PointerEvent<HTMLDivElement>) => {
			if (!dragRef.current) return;
			const { startX, startOffset, containerWidth } = dragRef.current;
			const dx = e.clientX - startX;
			const pxPerBucket = containerWidth / viewportSize;
			// Drag right = see older data (lower start), drag left = see newer data
			const bucketShift = Math.round(-dx / pxPerBucket);
			const newStart = Math.max(
				0,
				Math.min(maxStart, startOffset + bucketShift),
			);
			setUserStart(newStart);
		},
		[maxStart, viewportSize],
	);

	const onPointerUp = useCallback(() => {
		if (!dragRef.current) return;
		dragRef.current = null;
		setIsDragging(false);
	}, []);

	// Mouse wheel / trackpad horizontal scroll
	const onWheel = useCallback(
		(e: React.WheelEvent<HTMLDivElement>) => {
			if (!pannable) return;
			// deltaX: trackpad horizontal swipe; deltaMode 1 = lines
			const rawDelta =
				e.deltaMode === 1
					? e.deltaX * 20
					: Math.abs(e.deltaX) > Math.abs(e.deltaY)
						? e.deltaX
						: e.deltaY;
			if (rawDelta === 0) return;
			e.preventDefault();
			// Scroll right (positive delta) = see older data (decrease start)
			const shift = rawDelta > 0 ? -1 : 1;
			const newStart = Math.max(0, Math.min(maxStart, effectiveStart + shift));
			setUserStart(newStart);
		},
		[pannable, maxStart, effectiveStart],
	);

	if (data.length === 0) {
		return (
			<div className="ui-card p-6">
				<div className="flex items-center justify-between mb-4">
					<h3 className="text-lg font-semibold text-(--text-primary) flex items-center gap-2">
						<Icon size={18} style={{ color }} />
						{metric} /{" "}
						{range === "1h"
							? t("dashboard.chart.hour")
							: t("dashboard.chart.day")}
						{loading && <Spinner className="ml-1" />}
					</h3>
					{showToggle && <RangeToggle value={range} onChange={onRangeChange} />}
				</div>
				<p className="text-sm text-(--text-muted) text-center py-12">
					{t("dashboard.chart.emptyState", { metric })}
				</p>
			</div>
		);
	}

	const gradientId = `${dataKey}Area`;

	return (
		<div className="ui-card p-6">
			<div className="flex items-center justify-between mb-4">
				<h3 className="text-lg font-semibold text-(--text-primary) flex items-center gap-2">
					<Icon size={18} style={{ color }} />
					{metric} /{" "}
					{range === "1h"
						? t("dashboard.chart.hour")
						: t("dashboard.chart.day")}
					{loading && <Spinner className="ml-1" />}
				</h3>
				{showToggle && <RangeToggle value={range} onChange={onRangeChange} />}
			</div>
			<div
				style={{
					height,
					cursor: pannable ? (isDragging ? "grabbing" : "grab") : undefined,
					position: "relative",
					overflow: "hidden",
					borderRadius: "8px",
					userSelect: isDragging ? "none" : undefined,
					WebkitUserSelect: isDragging ? "none" : undefined,
				}}
				onPointerDown={pannable ? onPointerDown : undefined}
				onPointerMove={pannable ? onPointerMove : undefined}
				onPointerUp={pannable ? onPointerUp : undefined}
				onPointerCancel={pannable ? onPointerUp : undefined}
				onWheel={pannable ? onWheel : undefined}
			>
				{isDragging && (
					<div
						style={{
							position: "absolute",
							inset: 0,
							background: `linear-gradient(135deg, ${color}15, ${color}08)`,
							border: `2px solid ${color}40`,
							borderRadius: "8px",
							zIndex: 10,
							pointerEvents: "none",
						}}
					/>
				)}
				<ResponsiveContainer width="100%" height="100%">
					<AreaChart
						data={visibleData}
						margin={{ top: 5, right: 5, left: 0, bottom: 0 }}
					>
						<defs>
							<linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
								<stop offset="0%" stopColor={color} stopOpacity={0.3} />
								<stop offset="100%" stopColor={color} stopOpacity={0.02} />
							</linearGradient>
						</defs>
						<CartesianGrid
							strokeDasharray="3 3"
							stroke={grid}
							vertical={false}
						/>
						<XAxis
							dataKey="hour"
							tick={{ fontSize: 10, fill: text }}
							tickLine={false}
							axisLine={false}
							interval={4}
						/>
						<YAxis
							tick={{ fontSize: 10, fill: text }}
							tickLine={false}
							axisLine={false}
							allowDecimals={allowDecimals}
							tickFormatter={(v: number) => {
								const raw = Number(v) * scale;
								const val = allowDecimals ? raw : Math.round(raw);
								return val.toLocaleString(undefined, {
									maximumFractionDigits: allowDecimals ? 2 : 0,
								});
							}}
						/>
						<Tooltip
							contentStyle={{
								backgroundColor: "var(--surface-elevated)",
								border: "1px solid var(--border-default)",
								borderRadius: "10px",
								fontSize: "12px",
							}}
							labelStyle={{
								color: "var(--text-muted)",
								fontSize: "10px",
								textTransform: "uppercase",
								letterSpacing: "0.05em",
							}}
							itemStyle={{
								color: "var(--text-primary)",
								fontSize: "13px",
							}}
							formatter={(value: number | string | unknown) => {
								const raw = Number(value) * scale;
								const val = allowDecimals ? raw : Math.round(raw);
								return [
									val.toLocaleString(undefined, {
										maximumFractionDigits: allowDecimals ? 2 : 0,
									}),
									label,
								];
							}}
						/>
						<Area
							type="monotone"
							dataKey={dataKey}
							stroke={color}
							strokeWidth={2}
							fill={`url(#${gradientId})`}
							dot={false}
							activeDot={{ r: 4, fill: color, strokeWidth: 0 }}
							isAnimationActive={!isDragging}
							animationDuration={0}
						/>
					</AreaChart>
				</ResponsiveContainer>
			</div>
			{pannable && (canPanLeft || canPanRight) && (
				<div className="flex items-center justify-center gap-2 mt-2 text-xs text-(--text-muted) select-none">
					{canPanLeft && <span>→</span>}
					<span>{t("dashboard.chart.dragToPan")}</span>
					{canPanRight && <span>←</span>}
				</div>
			)}
		</div>
	);
}
