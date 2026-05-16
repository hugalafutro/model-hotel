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

/** Number of 5-min buckets visible in the 1h viewport. */
const VIEWPORT_SIZE = 12;

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

	// Drag-to-pan state for 1h range
	const pannable = range === "1h" && data.length > VIEWPORT_SIZE;
	const maxStart = Math.max(0, data.length - VIEWPORT_SIZE);
	// Snap to latest data; reset when range changes (keyed state)
	const [viewportStart, setViewportStart] = useState(maxStart);
	const [viewportRange, setViewportRange] = useState(range);
	if (viewportRange !== range) {
		setViewportRange(range);
		setViewportStart(maxStart);
	}
	const [isDragging, setIsDragging] = useState(false);
	const dragRef = useRef<{
		startX: number;
		startOffset: number;
		containerWidth: number;
	} | null>(null);

	// Clamp viewport to valid range
	const effectiveStart = Math.max(0, Math.min(viewportStart, maxStart));

	const visibleData = pannable
		? data.slice(effectiveStart, effectiveStart + VIEWPORT_SIZE)
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
			// Pixels per bucket: container width / visible points
			const pxPerBucket = containerWidth / VIEWPORT_SIZE;
			const bucketShift = Math.round(dx / pxPerBucket);
			const newStart = Math.max(
				0,
				Math.min(maxStart, startOffset - bucketShift),
			);
			setViewportStart(newStart);
		},
		[maxStart],
	);

	const onPointerUp = useCallback(() => {
		dragRef.current = null;
		setIsDragging(false);
	}, []);

	if (data.length === 0) {
		return (
			<div className="ui-card p-6">
				<div className="flex items-center justify-between mb-4">
					<h3 className="text-lg font-semibold text-(--text-primary) flex items-center gap-2">
						<Icon size={18} style={{ color }} />
						{metric} /{" "}
						{range === "7d" ? "Day" : range === "1h" ? "5 min" : "Hour"}
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
					{metric} /{" "}
					{range === "7d" ? "Day" : range === "1h" ? "5 min" : "Hour"}
					{loading && <Spinner className="ml-1" />}
				</h3>
				{showToggle && <RangeToggle value={range} onChange={onRangeChange} />}
			</div>
			<div
				style={{
					height,
					cursor: pannable ? (isDragging ? "grabbing" : "grab") : undefined,
				}}
				onPointerDown={pannable ? onPointerDown : undefined}
				onPointerMove={pannable ? onPointerMove : undefined}
				onPointerUp={pannable ? onPointerUp : undefined}
				onPointerCancel={pannable ? onPointerUp : undefined}
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
						/>
					</AreaChart>
				</ResponsiveContainer>
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
