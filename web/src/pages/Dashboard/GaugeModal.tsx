import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../../api/client";
import { Modal } from "../../components/Modal";
import { toChartPoints } from "./chartPoints";
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
	overlayDataKey,
	overlayColor,
	overlayLabel,
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
	overlayDataKey?: GaugeDataKey;
	overlayColor?: string;
	overlayLabel?: string;
}) {
	const [range, setRange] = useState<Range>("24h");
	const { data: tsData } = useQuery({
		queryKey: ["stats-timeseries-modal", range],
		queryFn: () => api.stats.getTimeSeries({ period: range }),
		placeholderData: keepPreviousData,
		enabled: open,
	});

	const chartData = toChartPoints(tsData, range);

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
				overlayDataKey={overlayDataKey}
				overlayColor={overlayColor}
				overlayLabel={overlayLabel}
			/>
		</Modal>
	);
}
