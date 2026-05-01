import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
	Activity,
	AlertTriangle,
	ArrowUpRight,
	Bot,
	Clock,
	Gauge as GaugeIcon,
	LayoutDashboard,
	PlugZap,
	RefreshCw,
	ShieldAlert,
	Target,
	Timer,
	TrendingUp,
	X,
	Zap,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
	Area,
	AreaChart,
	CartesianGrid,
	Cell,
	Pie,
	PieChart,
	ResponsiveContainer,
	Tooltip,
	XAxis,
	YAxis,
} from "recharts";
import { api } from "../api/client";
import type { MetricType, Model } from "../api/types";
import { ModelDetailModal } from "../components/ModelDetailPanel";
import { Spinner } from "../components/Spinner";
import { useToast } from "../context/ToastContext";
import { proxyModelID } from "../utils/model";

type Range = "1h" | "24h" | "7d";

function RangeToggle({
	value,
	onChange,
}: {
	value: Range;
	onChange: (v: Range) => void;
}) {
	const labels: Record<Range, string> = { "1h": "1H", "24h": "1D", "7d": "7D" };
	return (
		<div className="flex items-center gap-0.5">
			{(["1h", "24h", "7d"] as Range[]).map((r) => {
				const active = value === r;
				return (
					<button
						type="button"
						key={r}
						onClick={() => onChange(r)}
						className={`px-2 py-0.5 text-[10px] font-semibold tracking-wide rounded-md transition-colors ${
							active
								? "text-white"
								: "text-(--text-muted) hover:text-(--text-secondary)"
						}`}
						style={active ? { backgroundColor: "var(--accent)" } : {}}
					>
						{labels[r]}
					</button>
				);
			})}
		</div>
	);
}

function MetricToggle({
	value,
	onChange,
}: {
	value: MetricType;
	onChange: (v: MetricType) => void;
}) {
	return (
		<div className="flex items-center gap-0.5">
			{(["tokens", "requests"] as MetricType[]).map((m) => {
				const active = value === m;
				const label = m === "tokens" ? "Tok" : "Req";
				return (
					<button
						type="button"
						key={m}
						onClick={() => onChange(m)}
						className={`px-2 py-0.5 text-[10px] font-semibold tracking-wide rounded-md transition-colors ${
							active
								? "text-white"
								: "text-(--text-muted) hover:text-(--text-secondary)"
						}`}
						style={active ? { backgroundColor: "var(--accent)" } : {}}
					>
						{label}
					</button>
				);
			})}
		</div>
	);
}

/* =====================================================
   NUMBER FORMATTERS
   ===================================================== */
function formatCompact(n: number): string {
	if (n === 0) return "0";
	const abs = Math.abs(n);
	const fmt = (v: number) => {
		const s = v.toFixed(1);
		return s.endsWith(".0") ? s.slice(0, -2) : s;
	};
	if (abs >= 1_000_000) return `${fmt(n / 1_000_000)}M`;
	if (abs >= 1_000) return `${fmt(n / 1_000)}K`;
	return fmt(n);
}

function dropTrailingZero(v: number, decimals: number): string {
	const s = v.toFixed(decimals);
	if (decimals > 0 && s.includes(".")) {
		return s.replace(/\.?0+$/, "");
	}
	return s;
}

/* =====================================================
   ANIMATED COUNTER
   ===================================================== */
function AnimatedValue({
	value,
	decimals = 0,
	suffix = "",
	duration = 1200,
	formatter,
}: {
	value: number;
	decimals?: number;
	suffix?: string;
	duration?: number;
	formatter?: (val: number) => string;
}) {
	const [display, setDisplay] = useState(0);
	const startRef = useRef<number | null>(null);
	const fromRef = useRef(0);
	const toRef = useRef(value);

	useEffect(() => {
		fromRef.current = display;
		toRef.current = value;
		startRef.current = null;

		let raf: number;
		const ease = (t: number) => 1 - (1 - t) ** 3;

		const tick = (ts: number) => {
			if (startRef.current === null) startRef.current = ts;
			const elapsed = ts - startRef.current;
			const p = Math.min(elapsed / duration, 1);
			const eased = ease(p);
			const current =
				fromRef.current + (toRef.current - fromRef.current) * eased;
			setDisplay(current);
			if (p < 1) raf = requestAnimationFrame(tick);
		};

		raf = requestAnimationFrame(tick);
		return () => cancelAnimationFrame(raf);
	}, [value, duration, display]);

	const formatted = formatter
		? formatter(display)
		: dropTrailingZero(display, decimals);
	return (
		<span style={{ textTransform: "none" }}>
			{formatted}
			{suffix && (
				<span
					className="text-sm font-normal text-(--text-muted) ml-1"
					style={{ textTransform: "none" }}
				>
					{suffix}
				</span>
			)}
		</span>
	);
}

/* =====================================================
   STAT CARD
   ===================================================== */
function StatCard({
	label,
	value,
	decimals,
	suffix,
	icon: Icon,
	accent,
	sparkline,
	sparklineTooltip,
	formatter,
	onClick,
	tooltip,
	loading,
}: {
	label: string;
	value: number;
	decimals?: number;
	suffix?: string;
	icon: React.ElementType;
	accent: string;
	sparkline?: number; // 0-1 ratio for tiny horizontal fill
	sparklineTooltip?: string;
	formatter?: (val: number) => string;
	onClick?: () => void;
	tooltip?: string;
	loading?: boolean;
}) {
	return (
		// biome-ignore lint/a11y/noStaticElementInteractions: interactive only when onClick is provided, role/tabIndex/onKeyDown are set conditionally
		<div
			onClick={onClick}
			title={tooltip}
			className={`ui-card p-5 group text-left w-full ${onClick ? "cursor-pointer hover:brightness-110 transition-all" : ""}`}
			role={onClick ? "button" : undefined}
			tabIndex={onClick ? 0 : undefined}
			onKeyDown={
				onClick
					? (e) => {
							if (e.key === "Enter" || e.key === " ") {
								e.preventDefault();
								onClick();
							}
						}
					: undefined
			}
		>
			<div className="flex items-center justify-between mb-2">
				<div
					className="w-9 h-9 flex items-center justify-center rounded-lg"
					style={{ backgroundColor: `${accent}18` }}
				>
					<Icon size={18} style={{ color: accent }} />
				</div>
				<span className="text-[10px] font-semibold uppercase tracking-wider text-(--text-muted) text-right">
					{label}
				</span>
			</div>
			<p
				className="text-xl font-bold text-(--text-primary)"
				style={{ textTransform: "none" }}
			>
				<AnimatedValue
					value={value}
					decimals={decimals}
					suffix={suffix}
					formatter={formatter}
				/>
				{loading && <Spinner className="ml-1" />}
			</p>
			{sparkline != null && (
				<div
					className="mt-3 h-[4px] rounded-full overflow-hidden bg-(--border-subtle)"
					title={sparklineTooltip}
				>
					<div
						className="h-full rounded-full transition-all duration-1000 [transform:translateZ(0)]"
						style={{
							width: `${Math.max(0, Math.min(1, sparkline)) * 100}%`,
							backgroundColor: accent,
						}}
					/>
				</div>
			)}
		</div>
	);
}

/* =====================================================
   TIME-SERIES AREA CHART
   ===================================================== */
function TimeSeriesChart({
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
	data: {
		hour: string;
		total: number;
		errors: number;
		tokens: number;
		latency: number;
		overhead_ms: number;
		provider_latency_ms: number;
		rate_limit_hits: number;
		avg_ttft_ms: number;
	}[];
	range: Range;
	onRangeChange: (r: Range) => void;
	metric: string;
	icon: React.ElementType;
	color: string;
	label: string;
	dataKey:
		| "total"
		| "tokens"
		| "errors"
		| "latency"
		| "overhead_ms"
		| "provider_latency_ms"
		| "rate_limit_hits"
		| "avg_ttft_ms";
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
				<h3 className="text-lg font-semibold text-(--text-primary) mb-4 flex items-center gap-2">
					<Icon size={18} style={{ color }} />
					{metric} / Hour
					{loading && <Spinner className="ml-1" />}
				</h3>
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
							tickFormatter={(v: number) =>
								(Number(v) * scale).toLocaleString(undefined, {
									maximumFractionDigits: allowDecimals ? 2 : 0,
								})
							}
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
							formatter={(value: number | string | unknown) => [
								(Number(value) * scale).toLocaleString(undefined, {
									minimumFractionDigits: allowDecimals ? 1 : 0,
									maximumFractionDigits: allowDecimals ? 2 : 0,
								}),
								label,
							]}
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

/* =====================================================
   PROVIDER DOUGHNUT
   ===================================================== */
function ProviderDoughnut({
	items,
	range,
	onRangeChange,
	metric,
	onMetricChange,
	loading,
}: {
	items: { name: string; count: number; tokens: number; share: number }[];
	range: Range;
	onRangeChange: (r: Range) => void;
	metric: MetricType;
	onMetricChange: (m: MetricType) => void;
	loading?: boolean;
}) {
	const colors = ["#818cf8", "#059669", "#fbbf24", "#f87171", "#a78bfa"];

	return (
		<div className="ui-card p-6">
			<div className="flex items-center justify-between mb-4">
				<h3 className="text-lg font-semibold text-(--text-primary) flex items-center gap-2">
					<TrendingUp size={18} className="text-(--accent)" />
					Provider Breakdown
					{loading && <Spinner className="ml-1" />}
				</h3>
				<div className="flex items-center gap-1.5">
					<MetricToggle value={metric} onChange={onMetricChange} />
					<RangeToggle value={range} onChange={onRangeChange} />
				</div>
			</div>
			{items.length === 0 ? (
				<p className="text-sm text-(--text-muted) text-center py-12">
					No provider data
				</p>
			) : (
				<div className="flex items-center gap-6">
					<div className="w-35 h-35">
						<ResponsiveContainer width="100%" height="100%">
							<PieChart>
								<Pie
									data={items}
									cx="50%"
									cy="50%"
									innerRadius={50}
									outerRadius={65}
									paddingAngle={2}
									dataKey="share"
									stroke="none"
								>
									{items.map((item, i) => (
										<Cell key={item.name} fill={colors[i % colors.length]} />
									))}
								</Pie>
							</PieChart>
						</ResponsiveContainer>
					</div>
					<div className="flex-1 space-y-2">
						{items.map((it, i) => (
							<div
								key={it.name}
								className="flex items-center justify-between gap-3"
							>
								<div className="flex items-center gap-2 min-w-0">
									<span
										className="w-2.5 h-2.5 rounded-full shrink-0"
										style={{
											backgroundColor: colors[i % colors.length],
										}}
									/>
									<span className="text-sm text-(--text-secondary) truncate">
										{it.name}
									</span>
								</div>
								<div className="text-right shrink-0 flex items-baseline justify-end tabular-nums">
									<span className="text-sm font-medium text-(--text-primary) w-14 text-right">
										{it.share.toFixed(1)}%
									</span>
									<span className="text-xs text-(--text-muted) ml-1 min-w-20 text-left">
										(
										{metric === "tokens"
											? `${formatCompact(it.tokens)} Token${it.tokens !== 1 ? "s" : ""}`
											: `${it.count} Request${it.count !== 1 ? "s" : ""}`}
										)
									</span>
								</div>
							</div>
						))}
					</div>
				</div>
			)}
		</div>
	);
}

/* =====================================================
   TOKEN SPLIT BAR
   ===================================================== */
function TokenSplitBar({
	prompt,
	completion,
	total,
	range,
	onRangeChange,
}: {
	prompt: number;
	completion: number;
	total: number;
	range: Range;
	onRangeChange: (r: Range) => void;
}) {
	const totalPC = prompt + completion;
	if (totalPC === 0) {
		return (
			<div className="ui-card p-6">
				<div className="flex items-center justify-between mb-1">
					<h3 className="text-lg font-semibold text-(--text-primary) flex items-center gap-2">
						<Target size={18} className="text-(--accent)" />
						Token Mix
					</h3>
					<RangeToggle value={range} onChange={onRangeChange} />
				</div>
				<p className="text-sm text-(--text-muted) text-center py-12">
					No token data yet. Token mix will appear here once traffic flows.
				</p>
			</div>
		);
	}
	const promptPct = (prompt / totalPC) * 100;
	const completionPct = (completion / totalPC) * 100;

	return (
		<div className="ui-card p-6">
			<div className="flex items-center justify-between mb-1">
				<h3 className="text-lg font-semibold text-(--text-primary) flex items-center gap-2">
					<Target size={18} className="text-(--accent)" />
					Token Mix
				</h3>
				<RangeToggle value={range} onChange={onRangeChange} />
			</div>
			<p
				className="text-2xl font-bold text-(--text-primary) mb-4"
				style={{ textTransform: "none" }}
			>
				{total.toLocaleString()}{" "}
				<span className="text-sm font-normal text-(--text-muted)">Tokens</span>
			</p>
			<div className="flex rounded-lg overflow-hidden h-6">
				<div
					className="flex items-center justify-center text-[10px] font-semibold text-white tracking-wider overflow-hidden whitespace-nowrap shrink-0"
					style={{
						width: `${promptPct}%`,
						backgroundColor: "#818cf8",
					}}
				>
					{promptPct > 12 ? `${promptPct.toFixed(0)}%` : ""}
				</div>
				<div
					className="flex items-center justify-center text-[10px] font-semibold text-white tracking-wider overflow-hidden whitespace-nowrap shrink-0"
					style={{
						width: `${completionPct}%`,
						backgroundColor: "#059669",
					}}
				>
					{completionPct > 6 ? `${completionPct.toFixed(0)}%` : ""}
				</div>
			</div>
			<div className="flex justify-between mt-3 text-sm">
				<div className="flex items-center gap-1.5">
					<span
						className="w-2 h-2 rounded-full"
						style={{ backgroundColor: "#818cf8" }}
					/>
					<span className="text-(--text-tertiary)">Prompt</span>
					<span className="font-medium text-(--text-primary) ml-1">
						{prompt.toLocaleString()}
					</span>
				</div>
				<div className="flex items-center gap-1.5">
					<span
						className="w-2 h-2 rounded-full"
						style={{ backgroundColor: "#059669" }}
					/>
					<span className="text-(--text-tertiary)">Completion</span>
					<span className="font-medium text-(--text-primary) ml-1">
						{completion.toLocaleString()}
					</span>
				</div>
			</div>
		</div>
	);
}

/* =====================================================
   USAGE BAR PANEL
   ===================================================== */
function UsageBarPanel({
	title,
	icon: Icon,
	entries,
	range,
	onRangeChange,
	metric,
	onMetricChange,
	loading,
	onEntryClick,
}: {
	title: string;
	icon: React.ElementType;
	entries: {
		label: string;
		value: number;
		suffix?: string;
		deleted?: boolean;
	}[];
	range: Range;
	onRangeChange: (r: Range) => void;
	metric?: MetricType;
	onMetricChange?: (m: MetricType) => void;
	loading?: boolean;
	/** When provided, entry labels become clickable buttons that invoke this callback */
	onEntryClick?: (label: string) => void;
}) {
	const max = entries.length > 0 ? Math.max(...entries.map((e) => e.value)) : 0;

	return (
		<div className="ui-card p-6">
			<div className="flex items-center justify-between mb-5">
				<div className="flex items-center gap-2">
					<Icon size={18} className="text-(--accent)" />
					<h3 className="text-lg font-semibold text-(--text-primary)">
						{title}
					</h3>
					{loading && <Spinner className="ml-1" />}
				</div>
				<div className="flex items-center gap-1.5">
					{metric !== undefined && onMetricChange !== undefined && (
						<MetricToggle value={metric} onChange={onMetricChange} />
					)}
					<RangeToggle value={range} onChange={onRangeChange} />
				</div>
			</div>
			{entries.length === 0 ? (
				<p className="text-sm text-(--text-muted) text-center py-8">
					No usage data available
				</p>
			) : (
				<div className="space-y-3.5">
					{entries.map((entry) => {
						const pct = max > 0 ? (entry.value / max) * 100 : 0;
						return (
							<div key={entry.label} className="space-y-1.5">
								<div className="flex justify-between items-center text-sm">
									{onEntryClick ? (
										<button
											type="button"
											onClick={() => onEntryClick(entry.label)}
											className={`truncate max-w-[70%] text-left cursor-pointer transition-colors hover:text-(--accent) hover:drop-shadow-[0_0_6px_var(--accent)] ${entry.deleted ? "text-red-400 italic pr-1" : "text-(--text-secondary)"}`}
											title={`View details for ${entry.label}`}
										>
											{entry.label}
										</button>
									) : (
										<span
											className={`truncate max-w-[70%] ${entry.deleted ? "text-red-400 italic pr-1" : "text-(--text-secondary)"}`}
											title={entry.label}
										>
											{entry.label}
										</span>
									)}
									<span className="font-semibold text-(--text-primary) ml-2 shrink-0">
										{entry.value.toLocaleString()}
										{entry.suffix
											? entry.value === 1
												? entry.suffix.replace(/s$/, "")
												: entry.suffix
											: ""}
									</span>
								</div>
								<div className="h-[4px] rounded-full overflow-hidden bg-(--border-subtle)">
									<div
										className="h-full rounded-full transition-all duration-700 [transform:translateZ(0)]"
										style={{
											width: `${pct}%`,
											backgroundColor: "var(--accent)",
										}}
									/>
								</div>
							</div>
						);
					})}
				</div>
			)}
		</div>
	);
}

/* =====================================================
   GAUGE MODAL
   ===================================================== */
function GaugeModal({
	open,
	onClose,
	title,
	metric,
	icon,
	color,
	dataKey,
	label,
	allowDecimals = true,
	scale,
}: {
	open: boolean;
	onClose: () => void;
	title: string;
	metric: string;
	icon: React.ElementType;
	color: string;
	dataKey:
		| "overhead_ms"
		| "provider_latency_ms"
		| "latency"
		| "errors"
		| "rate_limit_hits"
		| "avg_ttft_ms"
		| "total"
		| "tokens";
	label: string;
	allowDecimals?: boolean;
	scale?: number;
}) {
	const [range, setRange] = useState<Range>("24h");
	const { data: tsData } = useQuery({
		queryKey: ["stats-timeseries-modal", range],
		queryFn: () => api.stats.getTimeSeries({ period: range }),
		placeholderData: (prev) => prev,
		enabled: open,
	});

	const chartData = (() => {
		if (!tsData?.points) return [];
		return tsData.points.map((p) => {
			const d = new Date(p.bucket);
			const label =
				range === "7d"
					? d.toLocaleDateString("en-US", {
							month: "short",
							day: "numeric",
						})
					: `${d.getHours().toString().padStart(2, "0")}:00`;
			return {
				hour: label,
				total: p.count,
				errors: p.errors,
				tokens: p.tokens,
				latency: p.latency_ms,
				overhead_ms: p.overhead_ms,
				provider_latency_ms: p.provider_latency_ms,
				rate_limit_hits: p.rate_limit_hits,
				avg_ttft_ms: p.avg_ttft_ms,
			};
		});
	})();

	if (!open) return null;

	return (
		<div
			role="dialog"
			aria-modal="true"
			className="fixed inset-0 flex items-center justify-center z-50"
			onKeyDown={(e) => {
				if (e.key === "Escape") onClose();
			}}
		>
			<button
				type="button"
				className="absolute inset-0 bg-black/60 cursor-default"
				onClick={onClose}
				aria-label="Close dialog"
			/>
			<div className="relative ui-card p-6 w-full max-w-2xl mx-4">
				<div className="flex justify-between items-center mb-4">
					<h3
						className="text-lg font-semibold flex items-center gap-2"
						style={{ color }}
					>
						{title}
					</h3>
					<button
						type="button"
						onClick={onClose}
						className="text-(--text-secondary) hover:text-(--text-primary) transition-all cursor-default hover:drop-shadow-[0_0_8px_var(--accent)]"
						aria-label="Close"
					>
						<X size={18} />
					</button>
				</div>
				<TimeSeriesChart
					data={chartData}
					range={range}
					onRangeChange={setRange}
					metric={metric}
					icon={icon}
					color={color}
					label={label}
					dataKey={dataKey}
					allowDecimals={allowDecimals}
					height={280}
					scale={scale ?? (dataKey === "latency" ? 0.001 : 1)}
				/>
			</div>
		</div>
	);
}

/* =====================================================
   GAUGE
   ===================================================== */
function Gauge({
	label,
	value,
	decimals,
	suffix,
	color,
	onClick,
	tooltip,
	maxScale,
}: {
	label: string;
	value: number;
	decimals: number;
	suffix: string;
	color: string;
	onClick?: () => void;
	tooltip?: string;
	maxScale?: number;
}) {
	const radius = 40;
	const circumference = 2 * Math.PI * radius;
	const pathArc = circumference / 2;
	// For percentage metrics (error rate), cap at 100. For absolute metrics
	// (requests, ms), scale relative to maxScale so the arc is meaningful.
	const scaleMax = maxScale ?? 100;
	const pct = Math.min(Math.max((value / scaleMax) * 100, 0), 100);
	const dashOffset = pathArc - (pathArc * pct) / 100;

	return (
		<button
			type="button"
			onClick={onClick}
			title={tooltip}
			className={`flex flex-col items-center ${onClick ? "cursor-pointer hover:opacity-80 transition-opacity" : "cursor-default"}`}
		>
			<div className="relative w-28 h-14">
				<svg className="w-full h-full" viewBox="0 0 100 60">
					<title>Gauge</title>
					<path
						d="M 10 50 A 40 40 0 0 1 90 50"
						fill="none"
						stroke="var(--border-subtle)"
						strokeWidth="8"
						strokeLinecap="round"
					/>
					<path
						d="M 10 50 A 40 40 0 0 1 90 50"
						fill="none"
						stroke={color}
						strokeWidth="8"
						strokeLinecap="round"
						strokeDasharray={pathArc}
						strokeDashoffset={dashOffset}
						style={{ transition: "stroke-dashoffset 1s ease-out" }}
					/>
				</svg>
				<div className="absolute inset-x-0 bottom-0 text-center">
					<p className="text-sm font-bold text-(--text-primary)">
						{dropTrailingZero(value, decimals)}
						{suffix}
					</p>
				</div>
			</div>
			<p className="text-[10px] uppercase tracking-wider text-(--text-muted) mt-2">
				{label}
			</p>
		</button>
	);
}

/* =====================================================
   DASHBOARD
   ===================================================== */
export function Dashboard() {
	const [globalRange, setGlobalRange] = useState<Range>("24h");
	const [globalMetric, setGlobalMetric] = useState<MetricType>("tokens");
	const [excludeDeleted, setExcludeDeleted] = useState(false);
	const [overheadModalOpen, setOverheadModalOpen] = useState(false);
	const [errorModalOpen, setErrorModalOpen] = useState(false);
	const [latencyModalOpen, setLatencyModalOpen] = useState(false);
	const [ttftModalOpen, setTtftModalOpen] = useState(false);
	const [rateLimitModalOpen, setRateLimitModalOpen] = useState(false);
	const [requestsModalOpen, setRequestsModalOpen] = useState(false);
	const [tokensModalOpen, setTokensModalOpen] = useState(false);
	const [detailModel, setDetailModel] = useState<Model | null>(null);

	// Dashboard auto-refresh
	const queryClient = useQueryClient();
	const { toast } = useToast();
	const lastManualRefresh = useRef(0);
	const refreshCooldownMs = 5000;
	const [isRefreshing, setIsRefreshing] = useState(false);

	const [dashboardRefreshMs, setDashboardRefreshMs] = useState(() => {
		try {
			const sec = Number(localStorage.getItem("dashboardRefreshSec"));
			if (sec > 0) return sec * 1000;
		} catch {
			/* ignore */
		}
		return 30000; // default 30s
	});

	// React to interval changes from Settings page mid-session
	useEffect(() => {
		const handler = () => {
			try {
				const sec = Number(localStorage.getItem("dashboardRefreshSec"));
				setDashboardRefreshMs(sec > 0 ? sec * 1000 : 30000);
			} catch {
				setDashboardRefreshMs(30000);
			}
		};
		window.addEventListener("dashboardRefreshChange", handler);
		return () => window.removeEventListener("dashboardRefreshChange", handler);
	}, []);

	const handleRefresh = useCallback(() => {
		const now = Date.now();
		if (now - lastManualRefresh.current < refreshCooldownMs) {
			toast("Please wait before refreshing again", "info");
			return;
		}
		lastManualRefresh.current = now;
		setIsRefreshing(true);
		queryClient.invalidateQueries({ queryKey: ["stats"] });
		queryClient.invalidateQueries({ queryKey: ["models"] });
		queryClient.invalidateQueries({ queryKey: ["providers"] });
		queryClient.invalidateQueries({ queryKey: ["stats-timeseries"] });
		queryClient.invalidateQueries({ queryKey: ["stats-timeseries-tokens"] });
		queryClient.invalidateQueries({
			queryKey: ["stats-provider-distribution"],
		});
		queryClient.invalidateQueries({ queryKey: ["stats-top-models"] });
		queryClient.invalidateQueries({ queryKey: ["stats-top-providers"] });
		queryClient.invalidateQueries({ queryKey: ["stats-top-virtual-keys"] });
		toast("Refreshing dashboard…", "info");
		setTimeout(() => setIsRefreshing(false), refreshCooldownMs);
	}, [queryClient, toast]);

	// Hide manual refresh when auto-refresh is 10s or faster
	const hideManualRefresh =
		dashboardRefreshMs > 0 && dashboardRefreshMs <= 10000;

	const {
		data: stats,
		isLoading: statsLoading,
		error: statsError,
	} = useQuery({
		queryKey: ["stats", globalRange, excludeDeleted],
		queryFn: () => api.stats.get({ period: globalRange, excludeDeleted }),
		placeholderData: (prev) => prev,
		refetchInterval: dashboardRefreshMs,
		retry: 1,
	});

	const {
		data: gaugeStats,
		isLoading: gaugeStatsLoading,
		error: gaugeStatsError,
	} = useQuery({
		queryKey: [
			"stats",
			globalRange === "1h" ? "1h" : globalRange,
			excludeDeleted,
		],
		queryFn: () => api.stats.get({ period: globalRange, excludeDeleted }),
		placeholderData: (prev) => prev,
		refetchInterval: dashboardRefreshMs,
		retry: 1,
	});

	const { data: models, isLoading: modelsLoading } = useQuery({
		queryKey: ["models", excludeDeleted],
		queryFn: () =>
			api.models
				.list()
				.then((d) =>
					excludeDeleted ? d.filter((m) => !m.disabled_manually) : d,
				),
		refetchInterval: dashboardRefreshMs,
	});

	const { data: providers, isLoading: providersLoading } = useQuery({
		queryKey: ["providers", excludeDeleted],
		queryFn: () =>
			api.providers
				.list()
				.then((d) => (excludeDeleted ? d.filter((p) => p.enabled) : d)),
		refetchInterval: dashboardRefreshMs,
	});

	const { data: tsData, isLoading: tsDataLoading } = useQuery({
		queryKey: ["stats-timeseries", globalRange, excludeDeleted],
		queryFn: () =>
			api.stats.getTimeSeries({ period: globalRange, excludeDeleted }),
		placeholderData: (prev) => prev,
		refetchInterval: dashboardRefreshMs,
	});

	const { data: tokenTsData, isLoading: tokenTsDataLoading } = useQuery({
		queryKey: ["stats-timeseries-tokens", globalRange, excludeDeleted],
		queryFn: () =>
			api.stats.getTimeSeries({ period: globalRange, excludeDeleted }),
		placeholderData: (prev) => prev,
		refetchInterval: dashboardRefreshMs,
	});

	const { data: provDist, isLoading: provDistLoading } = useQuery({
		queryKey: [
			"stats-provider-distribution",
			globalRange,
			globalMetric,
			excludeDeleted,
		],
		queryFn: () =>
			api.stats.getProviderDistribution({
				period: globalRange,
				metric: globalMetric,
				excludeDeleted,
			}),
		placeholderData: (prev) => prev,
		refetchInterval: dashboardRefreshMs,
	});

	const { data: modelStats, isLoading: modelStatsLoading } = useQuery({
		queryKey: ["stats-top-models", globalRange, globalMetric, excludeDeleted],
		queryFn: () =>
			api.stats.get({
				period: globalRange,
				metric: globalMetric,
				excludeDeleted,
			}),
		placeholderData: (prev) => prev,
		refetchInterval: dashboardRefreshMs,
	});

	const { data: providerStats, isLoading: providerStatsLoading } = useQuery({
		queryKey: [
			"stats-top-providers",
			globalRange,
			globalMetric,
			excludeDeleted,
		],
		queryFn: () =>
			api.stats.get({
				period: globalRange,
				metric: globalMetric,
				excludeDeleted,
			}),
		placeholderData: (prev) => prev,
		refetchInterval: dashboardRefreshMs,
	});

	const { data: vkStats, isLoading: vkStatsLoading } = useQuery({
		queryKey: [
			"stats-top-virtual-keys",
			globalRange,
			globalMetric,
			excludeDeleted,
		],
		queryFn: () =>
			api.stats.get({
				period: globalRange,
				metric: globalMetric,
				excludeDeleted,
			}),
		placeholderData: (prev) => prev,
		refetchInterval: dashboardRefreshMs,
	});

	// Auth check: on stats error, test if token is still valid.
	// Wrapped in useEffect so window.location.reload() never runs during render.
	useEffect(() => {
		if (!statsError) return;
		const errMsg = statsError.message || "";
		if (
			errMsg.includes("401") ||
			errMsg.includes("Unauthorized") ||
			errMsg.includes("Admin token")
		) {
			localStorage.removeItem("adminToken");
			window.location.reload();
		}
	}, [statsError]);

	const handleModelClick = useCallback(
		(label: string) => {
			const found = models?.find(
				(m) => proxyModelID(m.provider_name, m.model_id) === label,
			);
			if (found) setDetailModel(found);
		},
		[models],
	);

	if (!stats && statsLoading) {
		return (
			<div className="flex items-center justify-center h-64">
				<div
					className="animate-spin rounded-full h-12 w-12 border-b-2"
					style={{ borderColor: "var(--accent)" }}
				></div>
			</div>
		);
	}

	if (statsError) {
		return (
			<div className="space-y-6">
				<div>
					<h1 className="text-2xl font-bold text-white">Dashboard</h1>
					<p className="text-gray-400">Overview of your Model Hotel usage</p>
				</div>
				<div className="bg-red-900/50 border border-red-700 rounded-lg p-6 text-red-300">
					Failed to load stats: {statsError.message}
				</div>
			</div>
		);
	}

	// Derived values
	const totalRequests7d = stats?.total_requests_last_7d || 1;
	const req24h = stats?.total_requests_last_24h || 0;
	const sparkReq = totalRequests7d > 0 ? req24h / totalRequests7d : 0;

	const totalTokens =
		(stats?.total_tokens_prompt || 0) + (stats?.total_tokens_completion || 0);

	const rangeLabel =
		globalRange === "1h" ? "1h" : globalRange === "24h" ? "1d" : "7d";
	const gaugeRequestCount =
		globalRange === "1h"
			? gaugeStats?.requests_last_1h || 0
			: globalRange === "24h"
				? gaugeStats?.total_requests_last_24h || 0
				: gaugeStats?.total_requests_last_7d || 0;

	const acData = (() => {
		if (!tsData?.points) return [];
		return tsData.points.map((p) => {
			const d = new Date(p.bucket);
			const label =
				globalRange === "7d"
					? d.toLocaleDateString("en-US", {
							month: "short",
							day: "numeric",
						})
					: `${d.getHours().toString().padStart(2, "0")}:00`;
			return {
				hour: label,
				total: p.count,
				errors: p.errors,
				tokens: p.tokens,
				latency: Math.round(p.latency_ms),
				overhead_ms: p.overhead_ms,
				provider_latency_ms: p.provider_latency_ms,
				rate_limit_hits: p.rate_limit_hits,
				avg_ttft_ms: p.avg_ttft_ms,
			};
		});
	})();

	const tokenAcData = (() => {
		if (!tokenTsData?.points) return [];
		return tokenTsData.points.map((p) => {
			const d = new Date(p.bucket);
			const label =
				globalRange === "7d"
					? d.toLocaleDateString("en-US", {
							month: "short",
							day: "numeric",
						})
					: `${d.getHours().toString().padStart(2, "0")}:00`;
			return {
				hour: label,
				total: p.count,
				errors: p.errors,
				tokens: p.tokens,
				latency: Math.round(p.latency_ms),
				overhead_ms: p.overhead_ms,
				provider_latency_ms: p.provider_latency_ms,
				rate_limit_hits: p.rate_limit_hits,
				avg_ttft_ms: p.avg_ttft_ms,
			};
		});
	})();

	// Format usage panels from their respective range queries.
	// Filter out zero-value entries so NULL/empty aggregates don't clutter the UI.
	const byModel = modelStats
		? Object.entries(modelStats.by_model)
				.filter(([, v]) => Number(v) > 0)
				.sort(([, a], [, b]) => Number(b) - Number(a))
				.slice(0, 5)
				.map(([k, v]) => ({
					label: k,
					value: Number(v),
					suffix: globalMetric === "tokens" ? " tokens" : " requests",
				}))
		: [];
	const byProvider = providerStats
		? Object.entries(providerStats.by_provider)
				.filter(([, v]) => Number(v) > 0)
				.sort(([, a], [, b]) => Number(b) - Number(a))
				.slice(0, 5)
				.map(([k, v]) => ({
					label: k,
					value: Number(v),
					suffix: globalMetric === "tokens" ? " tokens" : " requests",
				}))
		: [];
	const byVK = vkStats
		? Object.entries(vkStats.by_virtual_key)
				.filter(([, v]) => Number(v) > 0)
				.sort(([, a], [, b]) => Number(b) - Number(a))
				.slice(0, 5)
				.map(([k, v]) => ({
					label: k,
					value: Number(v),
					suffix: globalMetric === "tokens" ? " tokens" : " requests",
					deleted: k === "Deleted",
				}))
		: [];

	// Card accent colors (subtle tints in light, slightly brighter in dark)
	const accents = {
		providers: "#14b8a6",
		models: "#818cf8",
		requests: "#0ea5e9",
		latency: "#f59e0b",
		overhead: "#f472b6",
		errors: "#ef4444",
		tokens: "#22c55e",
		rateLimit: "#a855f7",
	};

	return (
		<div className="space-y-6">
			{/* Page header */}
			<div className="flex items-end justify-between">
				<div>
					<div className="flex items-center gap-3">
						<LayoutDashboard
							size={28}
							strokeWidth={2}
							className="text-(--accent)"
						/>
						<h1 className="text-2xl font-bold text-(--text-primary)">
							Dashboard
						</h1>
						<button
							type="button"
							onClick={() => setExcludeDeleted(!excludeDeleted)}
							title={
								excludeDeleted
									? "Showing only active (non-deleted) virtual keys. Click to include deleted keys in stats."
									: "Showing all virtual keys including deleted ones. Click to filter to active keys only."
							}
							className={`flex items-center gap-2 px-3 py-1.5 rounded-full text-sm transition-colors ${
								excludeDeleted
									? "bg-amber-500/20 text-amber-400 hover:bg-amber-500/30"
									: "bg-gray-700 text-gray-400 hover:bg-gray-600"
							}`}
						>
							<span
								className={`w-2 h-2 rounded-full transition-colors ${
									excludeDeleted ? "bg-amber-400" : "bg-gray-500"
								}`}
							/>
							{excludeDeleted ? "Active Keys Only" : "All Keys"}
						</button>
						<div className="flex items-center gap-1.5 ml-2 px-2 py-1 rounded-lg bg-(--surface-elevated) border border-(--border-default)">
							<RangeToggle value={globalRange} onChange={setGlobalRange} />
							<div className="w-px h-4 bg-(--border-subtle)" />
							<MetricToggle value={globalMetric} onChange={setGlobalMetric} />
						</div>
						{!hideManualRefresh && (
							<button
								type="button"
								onClick={handleRefresh}
								disabled={isRefreshing}
								title={isRefreshing ? "Refreshing…" : "Refresh dashboard"}
								className={`flex items-center justify-center w-7 h-7 rounded-full transition-all ${
									isRefreshing
										? "cursor-not-allowed opacity-60"
										: "cursor-pointer hover:bg-(--accent)/20 hover:drop-shadow-[0_0_8px_var(--accent)]"
								}`}
							>
								<RefreshCw
									size={14}
									className={`text-(--text-muted) ${isRefreshing ? "animate-spin" : ""}`}
								/>
							</button>
						)}
					</div>
					<p className="text-gray-400">Overview of your Model Hotel usage</p>
				</div>
				<div className="flex gap-4">
					{gaugeStatsLoading ? (
						<div className="flex items-center justify-center gap-2 py-4 text-sm text-(--text-muted)">
							<Spinner /> Loading gauges…
						</div>
					) : gaugeStatsError ? (
						<div className="flex items-center justify-center gap-2 py-4 text-sm text-red-400">
							<AlertTriangle size={14} /> Failed to load gauge stats
						</div>
					) : (
						<>
							<Gauge
								label={`Requests/${rangeLabel}`}
								value={gaugeRequestCount}
								decimals={0}
								suffix=""
								color={accents.requests}
								onClick={() => setRequestsModalOpen(true)}
								tooltip="Click to view request history"
								maxScale={Math.max(100, gaugeRequestCount * 1.2)}
							/>
							<Gauge
								label={`Avg TTFT/${rangeLabel}`}
								value={(gaugeStats?.avg_ttft_ms || 0) / 1000}
								decimals={1}
								suffix="s"
								color={accents.latency}
								onClick={() => setTtftModalOpen(true)}
								tooltip="Click to view TTFT history"
								maxScale={Math.max(
									1,
									((gaugeStats?.avg_ttft_ms || 0) / 1000) * 1.5,
								)}
							/>
							<Gauge
								label={`Avg Overhead/${rangeLabel}`}
								value={gaugeStats?.avg_overhead_ms || 0}
								decimals={1}
								suffix="ms"
								color={accents.overhead}
								onClick={() => setOverheadModalOpen(true)}
								tooltip="Click to view overhead history"
								maxScale={Math.max(
									100,
									(gaugeStats?.avg_overhead_ms || 0) * 1.5,
								)}
							/>
							<Gauge
								label={`Rate Limit Hits/${rangeLabel}`}
								value={gaugeStats?.rate_limit_hits || 0}
								decimals={0}
								suffix=""
								color="#a855f7"
								onClick={() => setRateLimitModalOpen(true)}
								tooltip="Click to view rate limit hit history"
								maxScale={Math.max(
									10,
									(gaugeStats?.rate_limit_hits || 0) * 1.5,
								)}
							/>
							<Gauge
								label={`Error Rate/${rangeLabel}`}
								value={(gaugeStats?.error_rate || 0) * 100}
								decimals={1}
								suffix="%"
								color={accents.errors}
								onClick={() => setErrorModalOpen(true)}
								tooltip="Click to view error rate history"
							/>
						</>
					)}
				</div>
			</div>

			{/* Stat cards */}
			<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-6 gap-4">
				<StatCard
					label="Total Providers"
					value={providers?.length || 0}
					icon={PlugZap}
					accent={accents.providers}
					loading={providersLoading}
				/>
				<StatCard
					label="Total Models"
					value={models?.length || 0}
					icon={Bot}
					accent={accents.models}
					loading={modelsLoading}
				/>
				<StatCard
					label={`Requests/${rangeLabel}`}
					value={
						globalRange === "1h"
							? gaugeStats?.requests_last_1h || 0
							: globalRange === "24h"
								? stats?.total_requests_last_24h || 0
								: stats?.total_requests_last_7d || 0
					}
					icon={Activity}
					accent={accents.requests}
					sparkline={globalRange === "24h" ? sparkReq : undefined}
					sparklineTooltip={
						globalRange === "24h"
							? "Share of last 7 days traffic that was today"
							: undefined
					}
					onClick={() => setRequestsModalOpen(true)}
					tooltip="Click to view request history"
				/>
				<StatCard
					label={`Error Rate/${rangeLabel}`}
					value={(stats?.error_rate || 0) * 100}
					decimals={1}
					suffix="%"
					icon={AlertTriangle}
					accent={accents.errors}
					onClick={() => setErrorModalOpen(true)}
					tooltip="Click to view error rate history"
				/>
				<StatCard
					label={`Avg Duration/${rangeLabel}`}
					value={(stats?.avg_latency_ms || 0) / 1000}
					decimals={1}
					suffix="s"
					icon={Clock}
					accent={accents.latency}
					onClick={() => setLatencyModalOpen(true)}
					tooltip="Click to view duration history"
				/>
				<StatCard
					label={
						globalMetric === "tokens"
							? `Total Tokens/${rangeLabel}`
							: "Avg Tokens/Req"
					}
					value={
						globalMetric === "tokens"
							? totalTokens
							: stats?.avg_tokens_per_request || 0
					}
					decimals={0}
					suffix={globalMetric === "tokens" ? "" : "T/Rq"}
					icon={globalMetric === "tokens" ? Zap : Target}
					accent={accents.tokens}
					formatter={globalMetric === "tokens" ? formatCompact : undefined}
					onClick={() => setTokensModalOpen(true)}
					tooltip="Click to view token history"
				/>
			</div>

			{/* Time-series charts row */}
			<div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
				<TimeSeriesChart
					data={acData}
					range={globalRange}
					onRangeChange={setGlobalRange}
					metric="Requests"
					icon={Activity}
					color={accents.requests}
					label="Requests"
					dataKey="total"
					loading={tsDataLoading}
				/>
				<TimeSeriesChart
					data={tokenAcData}
					range={globalRange}
					onRangeChange={setGlobalRange}
					metric="Tokens"
					icon={Zap}
					color={accents.tokens}
					label="Tokens"
					dataKey="tokens"
					allowDecimals
					loading={tokenTsDataLoading}
				/>
			</div>

			{/* Charts row: doughnut + token split */}
			<div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
				<ProviderDoughnut
					items={provDist?.items || []}
					range={globalRange}
					onRangeChange={setGlobalRange}
					metric={globalMetric}
					onMetricChange={setGlobalMetric}
					loading={provDistLoading}
				/>
				<TokenSplitBar
					prompt={stats?.total_tokens_prompt || 0}
					completion={stats?.total_tokens_completion || 0}
					total={totalTokens}
					range={globalRange}
					onRangeChange={setGlobalRange}
				/>
			</div>

			{/* Bottom row: three usage panels with horizontal bars */}
			<div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
				<UsageBarPanel
					title="Top Models"
					icon={ArrowUpRight}
					entries={byModel}
					range={globalRange}
					onRangeChange={setGlobalRange}
					metric={globalMetric}
					onMetricChange={setGlobalMetric}
					loading={modelStatsLoading}
					onEntryClick={handleModelClick}
				/>
				<UsageBarPanel
					title="Top Providers"
					icon={ArrowUpRight}
					entries={byProvider}
					range={globalRange}
					onRangeChange={setGlobalRange}
					metric={globalMetric}
					onMetricChange={setGlobalMetric}
					loading={providerStatsLoading}
				/>
				<UsageBarPanel
					title="Top Virtual Keys"
					icon={ArrowUpRight}
					entries={byVK}
					range={globalRange}
					onRangeChange={setGlobalRange}
					metric={globalMetric}
					onMetricChange={setGlobalMetric}
					loading={vkStatsLoading}
				/>
			</div>

			<GaugeModal
				open={overheadModalOpen}
				onClose={() => setOverheadModalOpen(false)}
				title="Avg Overhead"
				metric="Overhead"
				icon={Clock}
				color={accents.overhead}
				dataKey="overhead_ms"
				label="ms"
			/>
			<GaugeModal
				open={errorModalOpen}
				onClose={() => setErrorModalOpen(false)}
				title="Error Rate"
				metric="Errors"
				icon={AlertTriangle}
				color={accents.errors}
				dataKey="errors"
				label="errors"
			/>
			<GaugeModal
				open={latencyModalOpen}
				onClose={() => setLatencyModalOpen(false)}
				title="Avg Duration"
				metric="Duration"
				icon={Timer}
				color={accents.latency}
				dataKey="latency"
				label="s"
			/>
			<GaugeModal
				open={requestsModalOpen}
				onClose={() => setRequestsModalOpen(false)}
				title="Requests"
				metric="Requests"
				icon={Activity}
				color={accents.requests}
				dataKey="total"
				label="requests"
				allowDecimals={false}
			/>
			<GaugeModal
				open={ttftModalOpen}
				onClose={() => setTtftModalOpen(false)}
				title="Avg TTFT"
				metric="TTFT"
				icon={GaugeIcon}
				color={accents.latency}
				dataKey="avg_ttft_ms"
				label="s"
				scale={0.001}
			/>
			<GaugeModal
				open={rateLimitModalOpen}
				onClose={() => setRateLimitModalOpen(false)}
				title="Rate Limit Hits"
				metric="Rate Limit Hits"
				icon={ShieldAlert}
				color="#a855f7"
				dataKey="rate_limit_hits"
				label="hits"
				allowDecimals={false}
			/>
			<GaugeModal
				open={tokensModalOpen}
				onClose={() => setTokensModalOpen(false)}
				title="Avg Tokens"
				metric="Tokens"
				icon={Zap}
				color={accents.tokens}
				dataKey="tokens"
				label="tokens"
				allowDecimals
			/>

			{detailModel && (
				<ModelDetailModal
					model={detailModel}
					onClose={() => setDetailModel(null)}
				/>
			)}
		</div>
	);
}
