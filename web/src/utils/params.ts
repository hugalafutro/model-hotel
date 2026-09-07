import type { GenerationParams } from "../api/types";

/** True when any generation parameter carries a value. */
export function hasAnyParam(p: GenerationParams): boolean {
	return Object.values(p).some((v) => v !== undefined);
}

/**
 * The seven numeric generation parameters as the sliders render them: the
 * order, the range, and the label key. `integer` marks the two whose values are
 * whole numbers, which the slider rounds before it hands them over.
 */
export interface ParamSpec {
	key:
		| "temperature"
		| "max_tokens"
		| "top_p"
		| "min_p"
		| "top_k"
		| "frequency_penalty"
		| "presence_penalty";
	labelKey: string;
	min: number;
	max: number;
	step: number;
	integer?: boolean;
}

export const PARAM_SPECS: readonly ParamSpec[] = [
	{
		key: "temperature",
		labelKey: "components.modelDetailPanel.temperature",
		min: 0,
		max: 2,
		step: 0.01,
	},
	{
		key: "max_tokens",
		labelKey: "components.modelDetailPanel.maxTokens",
		min: 1,
		max: 32768,
		step: 1,
		integer: true,
	},
	{
		key: "top_p",
		labelKey: "components.modelDetailPanel.topP",
		min: 0,
		max: 1,
		step: 0.01,
	},
	{
		key: "min_p",
		labelKey: "components.modelDetailPanel.minP",
		min: 0,
		max: 1,
		step: 0.01,
	},
	{
		key: "top_k",
		labelKey: "components.modelDetailPanel.topK",
		min: 1,
		max: 100,
		step: 1,
		integer: true,
	},
	{
		key: "frequency_penalty",
		labelKey: "components.modelDetailPanel.freqPenalty",
		min: -2,
		max: 2,
		step: 0.01,
	},
	{
		key: "presence_penalty",
		labelKey: "components.modelDetailPanel.presPenalty",
		min: -2,
		max: 2,
		step: 0.01,
	},
];
