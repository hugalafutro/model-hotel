import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Model } from "../api/types";
import { useToast } from "../context/ToastContext";
import { proxyModelID } from "../utils/model";

/**
 * Hook that returns a callback to disable a model by its proxyModelID (e.g. "OpenAI/gpt-4o").
 * Finds the matching model in enabledModels, calls the PATCH endpoint, invalidates cache,
 * and shows a toast. Does not navigate away so users stay in their current context.
 */
export function useDisableModel(enabledModels: Model[]) {
	const queryClient = useQueryClient();
	const { toast } = useToast();

	return useMutation({
		mutationFn: async (modelIdentifier: string) => {
			const modelObj = enabledModels.find(
				(m) => proxyModelID(m.provider_name, m.model_id) === modelIdentifier,
			);
			if (!modelObj) {
				throw new Error(
					`Model "${modelIdentifier}" not found in enabled models`,
				);
			}
			return api.models.update(modelObj.id, { enabled: false });
		},
		onSuccess: (_data, modelIdentifier) => {
			toast(`Model "${modelIdentifier}" disabled`, "success");
			queryClient.invalidateQueries({ queryKey: ["models"] });
			queryClient.invalidateQueries({ queryKey: ["providers"] });
		},
		onError: (err: Error) => {
			toast(`Failed to disable model: ${err.message}`, "error");
		},
	});
}
