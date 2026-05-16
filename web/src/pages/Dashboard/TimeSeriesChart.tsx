import { useMemo } from "react";
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
			<div style={{ height }}>
				<ResponsiveContainer width="100%" height="100%">
					<AreaChart
						data={data}
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
		</div>
	);
}
