import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { api } from "../api/client";
import type { Model } from "../api/types";
import { useIdentity } from "../context/IdentityContext";
import { isChatModel } from "../utils/model";

/**
 * Fetch all models. React Query deduplicates by queryKey - multiple
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
 * Models the proxy can serve: the model AND its provider are enabled, the
 * same rule /v1/models applies. Base list for the chat surfaces, which layer
 * useChatModels on top to also drop non-chat modalities.
 */
export function useEnabledModels() {
	const { data: models, ...rest } = useModels();
	const enabledModels = useMemo(
		() => models?.filter((m: Model) => m.enabled && m.provider_enabled) || [],
		[models],
	);
	return { ...rest, data: enabledModels };
}

/**
 * Enabled, chat-capable models the caller can actually reach. Two constraints,
 * both of which make a model unusable rather than merely unattractive:
 *
 *   - embedding/rerank (and other non-chat) modalities, which the Chat and
 *     Arena pickers could never send to;
 *   - providers outside the caller's account cap (users.allowed_providers).
 *     Both pickers post to /api/chat/*, where the admin-chat middleware
 *     publishes that same cap and the proxy's candidate filter refuses anything
 *     outside it with a 403. Listing them only moves the refusal from selection
 *     time to send time.
 *
 * The cap is read by PRESENCE: null or undefined is no cap, and a non-null list
 * admits exactly its members, so an empty one (reachable when the last provider
 * a capped account named is deleted) leaves no chat model selectable at all.
 *
 * The failover group editor and the Models page keep listing every model: they
 * are configuration surfaces, not send surfaces.
 */
export function useChatModels() {
	const { data: models, ...rest } = useEnabledModels();
	const { me } = useIdentity();
	const cap = me?.allowed_providers ?? null;
	const chatModels = useMemo(
		() =>
			models.filter(
				(m: Model) =>
					isChatModel(m) && (cap === null || cap.includes(m.provider_id)),
			),
		[models, cap],
	);
	return { ...rest, data: chatModels };
}

/**
 * Simplified provider data - just name + base_url.
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
