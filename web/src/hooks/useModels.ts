import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { api } from "../api/client";
import type { Model } from "../api/types";

/**
 * Fetch all models. React Query deduplicates by queryKey — multiple
 * components sharing the key get cached data without extra requests.
 */
export function useModels() {
	return useQuery({
		queryKey: ["models"],
		queryFn: () => api.models.list(),
		staleTime: 60_000,
	});
}

/**
 * Fetch all providers. Same caching behaviour as useModels.
 */
export function useProviders() {
	return useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
		staleTime: 60_000,
	});
}

/**
 * Enabled models only — filters to models that are both enabled and
 * have a provider assigned. Used by Chat and Arena for model pickers.
 */
export function useEnabledModels() {
	const { data: models, ...rest } = useModels();
	const enabledModels = useMemo(
		() => models?.filter((m: Model) => m.enabled && m.provider_name) || [],
		[models],
	);
	return { ...rest, data: enabledModels };
}

/**
 * Simplified provider data — just name + base_url.
 * Used by Chat and Arena for ModelPicker grouping.
 */
export function useProviderData() {
	const { data: providers, ...rest } = useProviders();
	const providerData = useMemo(
		() =>
			providers?.map((p: { name: string; base_url: string }) => ({
				name: p.name,
				base_url: p.base_url,
			})) ?? [],
		[providers],
	);
	return { ...rest, data: providerData };
}
