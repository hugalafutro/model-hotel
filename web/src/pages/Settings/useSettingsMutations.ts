import {
	type QueryClient,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { useToast } from "../../context/ToastContext";
import { useSettingsQuery } from "../../hooks/useSettingsQuery";

// The alert card's two reads describe stored settings from the server's side:
// the decrypted destination list and the apprise-api reachability probe. Any
// settings write can change what they would return (a reset clears the Apprise
// address and targets as readily as an edit does), so both are dropped after
// every one of them. Nothing refetches unless the alert card is on screen.
export function invalidateAlertReads(queryClient: QueryClient) {
	queryClient.invalidateQueries({ queryKey: ["alert-targets"] });
	queryClient.invalidateQueries({ queryKey: ["alert-status"] });
}

/** Drops the settings read and everything derived from it, after a write. */
export function invalidateSettings(queryClient: QueryClient) {
	queryClient.invalidateQueries({ queryKey: ["settings"] });
	invalidateAlertReads(queryClient);
}

/**
 * useSettingsMutations is the shared settings read, mutation and toast bundle
 * every Settings page builds on: the settings query through useSettingsQuery,
 * the update and reset mutations, and the success and error toasts.
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

	const { data: settings } = useSettingsQuery();

	const updateMutation = useMutation({
		mutationFn: (updates: Record<string, string>) =>
			api.settings.update(updates),
		onSuccess: () => {
			invalidateSettings(queryClient);
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
			invalidateSettings(queryClient);
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
