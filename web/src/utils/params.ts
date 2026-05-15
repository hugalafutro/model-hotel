import type { GenerationParams } from "../api/types";

export function hasAnyParam(p: GenerationParams): boolean {
	return (
		p.temperature !== undefined ||
		p.max_tokens !== undefined ||
		p.top_p !== undefined ||
		p.min_p !== undefined ||
		p.top_k !== undefined ||
		p.frequency_penalty !== undefined ||
		p.presence_penalty !== undefined ||
		p.reasoning_effort !== undefined
	);
}
