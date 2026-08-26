import { api } from "../../api/client";
import type { Model, ModelsCursorResponse } from "../../api/types";
import { encodeCursor } from "../../utils/format";

export type ModelSortField =
	| "name"
	| "discovered"
	| "context"
	| "output"
	| "provider"
	| "status";

export interface ModelSortState {
	field: ModelSortField;
	dir: "asc" | "desc";
}

/** The keyset cursor for `entry` under `field`, matching the server's ORDER BY. */
export function modelCursor(field: ModelSortField, entry: Model): string {
	let cursorObj: Record<string, unknown>;
	switch (field) {
		case "name":
			cursorObj = {
				sort_by: "name",
				name: entry.name || entry.model_id,
				model_id: entry.model_id,
				id: entry.id,
			};
			break;
		case "discovered":
			cursorObj = {
				sort_by: "discovered",
				last_seen_at: entry.last_seen_at,
				id: entry.id,
			};
			break;
		case "context":
			cursorObj = {
				sort_by: "context",
				context_length: entry.context_length ?? 0,
				id: entry.id,
			};
			break;
		case "output":
			cursorObj = {
				sort_by: "output",
				max_output_tokens: entry.max_output_tokens ?? 0,
				id: entry.id,
			};
			break;
		case "provider":
			cursorObj = {
				sort_by: "provider",
				provider_name: entry.provider_name,
				id: entry.id,
			};
			break;
		case "status":
			cursorObj = {
				sort_by: "status",
				status_sort: entry.enabled ? (entry.disabled_manually ? 1 : 0) : 2,
				id: entry.id,
			};
			break;
		default:
			cursorObj = { sort_by: "name", name: entry.name, id: entry.id };
	}
	return encodeCursor(cursorObj);
}

/** The string-keyed filter bag the cursor hook carries, mapped back to the client's typed call. */
export function fetchModelsPage(params: {
	cursor?: string;
	direction: "after" | "before";
	limit: number;
	sort_dir: string;
	[key: string]: string | number | undefined;
}): Promise<ModelsCursorResponse> {
	return api.models.cursor({
		cursor: params.cursor,
		direction: params.direction as "after" | "before",
		limit: params.limit,
		sort_by: params.sort_by as string | undefined,
		sort_dir: params.sort_dir,
		provider_id: params.provider_id as string | undefined,
		search: params.search as string | undefined,
		capabilities: params.capabilities as string | undefined,
		outputs: params.outputs as string | undefined,
		provider_enabled:
			params.provider_enabled === undefined
				? undefined
				: params.provider_enabled === "true",
	});
}

/**
 * Walk every disabled row of the current filters, page by page, so a delete
 * covers exactly what the server's disabled count promised. Sorted by name
 * with its own cursor chain: the table's sort and cursor are irrelevant here.
 */
export async function loadAllDisabledModels(
	filters: Record<string, string | undefined>,
): Promise<Model[]> {
	const { provider_id, search, capabilities, outputs, provider_enabled } =
		filters;
	const all: Model[] = [];
	let cursor: string | undefined;
	for (;;) {
		const page = await api.models.cursor({
			cursor,
			direction: "after",
			limit: 200,
			sort_by: "name",
			sort_dir: "asc",
			provider_id,
			search,
			capabilities,
			outputs,
			provider_enabled:
				provider_enabled === undefined
					? undefined
					: provider_enabled === "true",
			enabled: false,
		});
		all.push(...page.entries);
		const last = page.entries.at(-1);
		if (!page.has_after || !last) break;
		cursor = modelCursor("name", last);
	}
	return all;
}
