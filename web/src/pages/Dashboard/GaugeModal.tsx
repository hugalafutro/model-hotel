import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../../api/client";
import { Modal } from "../../components/Modal";
import { TimeSeriesChart } from "./TimeSeriesChart";
import type { GaugeDataKey, Range } from "./types";

export function GaugeModal({
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
	dataKey: GaugeDataKey;
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
		<Modal
			header={
				<div className="flex justify-between items-center mb-4">
					<h3
						className="text-lg font-semibold flex items-center gap-2"
						style={{ color }}
					>
						{title}
					</h3>
				</div>
			}
			onClose={onClose}
			maxWidth="max-w-2xl"
			scrollable
		>
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
		</Modal>
	);
}
