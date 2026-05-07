import { useQuery } from "@tanstack/react-query";
import type { GenerationParams } from "../api/types";
import { fetchRecommendedSettings } from "../utils/recommendedSettings";

/**
 * Hook to fetch recommended generation settings for a model.
 * Uses TanStack Query for caching and deduplication.
 *
 * @param modelId - The proxy model ID (e.g. "openai/gpt-4o")
 * @param providerName - The display provider name (e.g. "OpenAI")
 * @returns Object with recommended params, loading state, and an apply helper.
 */
export function useRecommendedSettings(
	modelId: string,
	providerName: string,
): {
	recommended: GenerationParams | null;
	loading: boolean;
	error: string | null;
	matchedModel: string | null;
} {
	const { data, isLoading, error } = useQuery({
		queryKey: ["recommendedSettings", modelId, providerName],
		queryFn: () => fetchRecommendedSettings(modelId, providerName),
		staleTime: 30 * 60 * 1000, // 30 min - same as the underlying cache
		gcTime: 60 * 60 * 1000, // keep in cache for 1 hour
		retry: 1,
		// Don't refetch on window focus - this data changes rarely
		refetchOnWindowFocus: false,
	});

	const queryError = error
		? error instanceof Error
			? error.message
			: "Failed to fetch"
		: null;

	return {
		recommended: data?.params ?? null,
		loading: isLoading,
		error: queryError,
		matchedModel: data?.matchedModelId ?? null,
	};
}
