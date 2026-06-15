import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";

// useReadOnly reports whether the server runs in read-only (demo) mode, in which
// case the admin API refuses every mutation with 403. The frontend uses it to
// hide create/edit/delete affordances and show a banner. The value is fetched
// once and cached app-wide (config does not change at runtime), so calling this
// hook from multiple components costs a single request.
export function useReadOnly(): boolean {
	const { data } = useQuery({
		queryKey: ["public-config"],
		queryFn: () => api.publicConfig.get(),
		staleTime: Number.POSITIVE_INFINITY,
		retry: 1,
	});
	return data?.read_only ?? false;
}
