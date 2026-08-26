import type { TFunction } from "i18next";

export type LogSortField =
	| "time"
	| "model"
	| "provider"
	| "status"
	| "tokens"
	| "tps"
	| "response_header_ms"
	| "ttft"
	| "duration"
	| "overhead"
	| "key"
	| "ip";

export interface RequestLogColumn {
	field: LogSortField;
	label: string;
	tooltip: string;
}

/**
 * The request table's sortable columns, in display order. Built from `t`
 * rather than declared with key strings so every label stays a literal
 * t() call the i18n source-key check can follow.
 */
export function requestLogColumns(t: TFunction): RequestLogColumn[] {
	return [
		{
			field: "time",
			label: t("logs.table.timeDate"),
			tooltip: t("logs.tooltip.timeDate"),
		},
		{
			field: "model",
			label: t("logs.table.model"),
			tooltip: t("logs.tooltip.model"),
		},
		{
			field: "provider",
			label: t("logs.table.provider"),
			tooltip: t("logs.tooltip.provider"),
		},
		{
			field: "status",
			label: t("logs.table.status"),
			tooltip: t("logs.tooltip.status"),
		},
		{
			field: "tokens",
			label: t("logs.table.tokens"),
			tooltip: t("logs.tooltip.tokens"),
		},
		{
			field: "tps",
			label: t("logs.table.tps"),
			tooltip: t("logs.tooltip.tps"),
		},
		{
			field: "response_header_ms",
			label: t("logs.table.headers"),
			tooltip: t("logs.tooltip.headers"),
		},
		{
			field: "ttft",
			label: t("logs.table.ttft"),
			tooltip: t("logs.tooltip.ttft"),
		},
		{
			field: "duration",
			label: t("logs.table.duration"),
			tooltip: t("logs.tooltip.duration"),
		},
		{
			field: "overhead",
			label: t("logs.table.overhead"),
			tooltip: t("logs.tooltip.overhead"),
		},
		{
			field: "key",
			label: t("logs.table.key"),
			tooltip: t("logs.tooltip.key"),
		},
		{ field: "ip", label: t("logs.table.ip"), tooltip: t("logs.tooltip.ip") },
	];
}
