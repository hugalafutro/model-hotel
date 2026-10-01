import { useMemo } from "react";
import type { GenerationParams } from "../api/types";
import { recommendedSettings } from "../utils/recommendedSettings";

/**
 * Recommended generation settings for a model: its curated family defaults
 * plus a max_tokens default from the model's stored max output.
 *
 * @param modelId - The proxy model ID (e.g. "OpenAI/gpt-4o")
 * @param maxOutputTokens - The model's stored max output, if known.
 * @returns The recommended params, or null when there is no recommendation.
 */
export function useRecommendedSettings(
	modelId: string,
	maxOutputTokens?: number | null,
): GenerationParams | null {
	return useMemo(
		() => recommendedSettings(modelId, maxOutputTokens),
		[modelId, maxOutputTokens],
	);
}
