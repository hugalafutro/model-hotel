import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";

/**
 * The settings row, from the one query key every screen shares. Going through
 * this hook is what lets a mutation's invalidation reach every reader instead
 * of leaving a second copy of the same GET behind.
 */
export function useSettingsQuery() {
	return useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});
}
