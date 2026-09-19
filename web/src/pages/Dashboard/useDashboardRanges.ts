import { useEffect, useRef } from "react";
import { METRIC_TYPES, type MetricType } from "../../api/types";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import { DASHBOARD_RANGES, type Range } from "./types";

const VALID_RANGES: ReadonlySet<Range> = new Set(DASHBOARD_RANGES);
const VALID_METRICS: ReadonlySet<MetricType> = new Set(METRIC_TYPES);

const deserializeRange = (stored: string, fallback: Range): Range =>
	VALID_RANGES.has(stored as Range) ? (stored as Range) : fallback;
const deserializeMetric = (stored: string, fallback: MetricType): MetricType =>
	VALID_METRICS.has(stored as MetricType) ? (stored as MetricType) : fallback;

/**
 * The metric the second time-series chart shows next to a given first one:
 * requests beside anything else, tokens beside requests. The two charts never
 * show the same metric.
 */
export const companionMetric = (m: MetricType): MetricType =>
	m === "requests" ? "tokens" : "requests";

/**
 * The dashboard's time-range and metric selections: a global pair driven by
 * the page header plus a persisted range per section, and a metric for the
 * sections that have one. Sections follow the header whenever it changes and
 * otherwise keep whatever the user picked.
 */
export function useDashboardRanges() {
	// Global header toggles, persisted.
	const [globalRange, setGlobalRange] = useLocalStorage<Range>(
		"dashboardRange",
		"24h",
		{ deserialize: deserializeRange },
	);
	const [globalMetric, setGlobalMetric] = useLocalStorage<MetricType>(
		"dashboardMetric",
		"tokens",
		{ deserialize: deserializeMetric },
	);
	// Per-section local states: persisted in localStorage, synced from global
	// header toggles when those change.
	const [requestsChartRange, setRequestsChartRange] = useLocalStorage<Range>(
		"dashboard.requestsChartRange",
		globalRange,
		{ deserialize: deserializeRange },
	);
	const [tokensChartRange, setTokensChartRange] = useLocalStorage<Range>(
		"dashboard.tokensChartRange",
		globalRange,
		{ deserialize: deserializeRange },
	);
	// The two time-series charts each pick a metric; the header metric lands in
	// the left chart and its companion in the right one. A stored pair that
	// collides (edited storage) resolves at read time, so the charts never agree.
	const [leftChartMetric, setLeftChartMetric] = useLocalStorage<MetricType>(
		"dashboard.leftChartMetric",
		globalMetric,
		{ deserialize: deserializeMetric },
	);
	const [storedRightChartMetric, setRightChartMetric] =
		useLocalStorage<MetricType>(
			"dashboard.rightChartMetric",
			companionMetric(globalMetric),
			{ deserialize: deserializeMetric },
		);
	const rightChartMetric =
		storedRightChartMetric === leftChartMetric
			? companionMetric(leftChartMetric)
			: storedRightChartMetric;
	// Write the resolution back, or the stale stored value would resurface as
	// soon as the left chart moved off it.
	useEffect(() => {
		if (storedRightChartMetric === leftChartMetric) {
			setRightChartMetric(companionMetric(leftChartMetric));
		}
	}, [storedRightChartMetric, leftChartMetric, setRightChartMetric]);
	const [doughnutRange, setDoughnutRange] = useLocalStorage<Range>(
		"dashboard.doughnutRange",
		globalRange,
		{ deserialize: deserializeRange },
	);
	const [doughnutMetric, setDoughnutMetric] = useLocalStorage<MetricType>(
		"dashboard.doughnutMetric",
		globalMetric,
		{ deserialize: deserializeMetric },
	);
	const [tokenRange, setTokenRange] = useLocalStorage<Range>(
		"dashboard.tokenRange",
		globalRange,
		{ deserialize: deserializeRange },
	);
	const [modelsRange, setModelsRange] = useLocalStorage<Range>(
		"dashboard.modelsRange",
		globalRange,
		{ deserialize: deserializeRange },
	);
	const [modelsMetric, setModelsMetric] = useLocalStorage<MetricType>(
		"dashboard.modelsMetric",
		globalMetric,
		{ deserialize: deserializeMetric },
	);
	const [latencyRange, setLatencyRange] = useLocalStorage<Range>(
		"dashboard.latencyRange",
		globalRange,
		{ deserialize: deserializeRange },
	);
	const [virtualKeysRange, setVirtualKeysRange] = useLocalStorage<Range>(
		"dashboard.virtualKeysRange",
		globalRange,
		{ deserialize: deserializeRange },
	);
	const [virtualKeysMetric, setVirtualKeysMetric] = useLocalStorage<MetricType>(
		"dashboard.virtualKeysMetric",
		globalMetric,
		{ deserialize: deserializeMetric },
	);

	// Sync locals when global header toggles change
	const prevGlobalRangeRef = useRef(globalRange);
	const prevGlobalMetricRef = useRef(globalMetric);
	useEffect(() => {
		if (prevGlobalRangeRef.current !== globalRange) {
			prevGlobalRangeRef.current = globalRange;
			setRequestsChartRange(globalRange);
			setTokensChartRange(globalRange);
			setDoughnutRange(globalRange);
			setTokenRange(globalRange);
			setModelsRange(globalRange);
			setLatencyRange(globalRange);
			setVirtualKeysRange(globalRange);
		}
	}, [
		globalRange,
		setRequestsChartRange,
		setTokensChartRange,
		setDoughnutRange,
		setTokenRange,
		setModelsRange,
		setLatencyRange,
		setVirtualKeysRange,
	]);
	useEffect(() => {
		if (prevGlobalMetricRef.current !== globalMetric) {
			prevGlobalMetricRef.current = globalMetric;
			setLeftChartMetric(globalMetric);
			setRightChartMetric(companionMetric(globalMetric));
			setDoughnutMetric(globalMetric);
			setModelsMetric(globalMetric);
			setVirtualKeysMetric(globalMetric);
		}
	}, [
		globalMetric,
		setLeftChartMetric,
		setRightChartMetric,
		setDoughnutMetric,
		setModelsMetric,
		setVirtualKeysMetric,
	]);

	return {
		globalRange,
		setGlobalRange,
		globalMetric,
		setGlobalMetric,
		requestsChartRange,
		setRequestsChartRange,
		tokensChartRange,
		setTokensChartRange,
		leftChartMetric,
		setLeftChartMetric,
		rightChartMetric,
		setRightChartMetric,
		doughnutRange,
		setDoughnutRange,
		doughnutMetric,
		setDoughnutMetric,
		tokenRange,
		setTokenRange,
		modelsRange,
		setModelsRange,
		modelsMetric,
		setModelsMetric,
		latencyRange,
		setLatencyRange,
		virtualKeysRange,
		setVirtualKeysRange,
		virtualKeysMetric,
		setVirtualKeysMetric,
	};
}
