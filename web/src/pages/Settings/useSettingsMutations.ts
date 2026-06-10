import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { useToast } from "../../context/ToastContext";

/**
 * useSettingsMutations provides the shared query + mutation + toast pattern
 * used by all Settings pages. Each page was previously duplicating the same
 * ~40-line block (useQuery, updateMutation, resetSettingMutation, toast calls).
 *
 * Returns:
 * - settings: the current settings object (or undefined while loading)
 * - updateMutation: mutation to save settings changes
 * - resetSettingMutation: mutation to reset settings to defaults
 * - isResetting: convenience alias for resetSettingMutation.isPending
 */
export function useSettingsMutations() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();

	const { data: settings } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	const updateMutation = useMutation({
		mutationFn: (updates: Record<string, string>) =>
			api.settings.update(updates),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["settings"] });
			toast(t("settings.common.settingsSaved"), "success");
		},
		onError: (err: Error) => {
			toast(
				t("settings.common.failedToSave", { message: err.message }),
				"error",
			);
		},
	});

	const resetSettingMutation = useMutation({
		mutationFn: (keys: string[]) => api.settings.reset(keys),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["settings"] });
			toast(t("settings.common.resetSettingDone"), "success");
		},
		onError: (err: Error) => {
			toast(
				t("settings.common.resetFailed", { message: err.message }),
				"error",
			);
		},
	});

	const isResetting = resetSettingMutation.isPending;

	return { settings, updateMutation, resetSettingMutation, isResetting };
}
