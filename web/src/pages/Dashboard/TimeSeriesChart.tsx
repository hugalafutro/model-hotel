import { useCallback, useMemo, useRef, useState } from "react";
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
	const { grid, text } = useMemo(() => {
		const style = getComputedStyle(document.documentElement);
		return {
			grid:
				style.getPropertyValue("--border-subtle").trim() ||
				"rgba(255,255,255,0.04)",
			text: style.getPropertyValue("--text-muted").trim() || "#7a7e8c",
		};
	}, []);

	// Compute viewport size based on range
	const viewportSize = range === "1h" ? 12 : range === "24h" ? 24 : 7;

	// Find last index with real data to prevent scrolling into empty "future"
	const lastRealIndex = (() => {
		for (let i = data.length - 1; i >= 0; i--) {
			if (((data[i] as Record<string, unknown>)[dataKey] as number) !== 0)
				return i;
		}
		return data.length - 1;
	})();

	// Drag-to-pan state: enabled when data exceeds viewport
	const pannable = data.length > viewportSize;
	const maxStart = Math.max(0, lastRealIndex - viewportSize + 1);
	// Snap to latest data; reset when range changes (keyed state)
	const [viewportStart, setViewportStart] = useState(maxStart);
	const [viewportRange, setViewportRange] = useState(range);
	if (viewportRange !== range) {
		setViewportRange(range);
		setViewportStart(maxStart);
	}
	const [isDragging, setIsDragging] = useState(false);
	const [dragOffset, setDragOffset] = useState(0);
	const dragRef = useRef<{
		startX: number;
		startOffset: number;
		containerWidth: number;
	} | null>(null);

	// Clamp viewport to valid range
	const effectiveStart = Math.max(0, Math.min(viewportStart, maxStart));

	const visibleData = pannable
		? data.slice(effectiveStart, effectiveStart + viewportSize)
		: data;

	const canPanLeft = pannable && effectiveStart > 0;
	const canPanRight = pannable && effectiveStart < maxStart;

	const onPointerDown = useCallback(
		(e: React.PointerEvent<HTMLDivElement>) => {
			if (!pannable) return;
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
			// Smooth sub-pixel drag via CSS transform
			const pxPerBucket = containerWidth / viewportSize;
			// Clamp drag so we don't overshoot edges
			const maxDx = startOffset * pxPerBucket;
			const minDx = (startOffset - maxStart) * pxPerBucket;
			const clampedDx = Math.max(minDx, Math.min(maxDx, dx));
			setDragOffset(-clampedDx);
		},
		[maxStart, viewportSize],
	);

	const onPointerUp = useCallback(() => {
		if (!dragRef.current) return;
		const { startOffset, containerWidth } = dragRef.current;
		// Snap to nearest bucket on release
		const pxPerBucket = containerWidth / viewportSize;
		const bucketShift = Math.round(-dragOffset / pxPerBucket);
		const newStart = Math.max(0, Math.min(maxStart, startOffset + bucketShift));
		setViewportStart(newStart);
		setDragOffset(0);
		dragRef.current = null;
		setIsDragging(false);
	}, [maxStart, viewportSize, dragOffset]);

	if (data.length === 0) {
		return (
			<div className="ui-card p-6">
				<div className="flex items-center justify-between mb-4">
					<h3 className="text-lg font-semibold text-(--text-primary) flex items-center gap-2">
						<Icon size={18} style={{ color }} />
						{metric} / {range === "7d" ? "Day" : "Hour"}
						{loading && <Spinner className="ml-1" />}
					</h3>
					{showToggle && <RangeToggle value={range} onChange={onRangeChange} />}
				</div>
				<p className="text-sm text-(--text-muted) text-center py-12">
					No time-series data yet. {metric} will appear here once traffic flows.
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
					{metric} / {range === "7d" ? "Day" : "Hour"}
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
				}}
				onPointerDown={pannable ? onPointerDown : undefined}
				onPointerMove={pannable ? onPointerMove : undefined}
				onPointerUp={pannable ? onPointerUp : undefined}
				onPointerCancel={pannable ? onPointerUp : undefined}
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
				<div
					style={{
						transform: isDragging ? `translateX(${dragOffset}px)` : undefined,
						transition: isDragging ? "none" : "transform 0.15s ease-out",
					}}
				>
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
			</div>
			{pannable && (canPanLeft || canPanRight) && (
				<div className="flex items-center justify-center gap-2 mt-2 text-xs text-(--text-muted) select-none">
					{canPanLeft && <span>←</span>}
					<span>drag to pan</span>
					{canPanRight && <span>→</span>}
				</div>
			)}
		</div>
	);
}
