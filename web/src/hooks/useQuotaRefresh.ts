import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useCallback, useRef } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import { useToast } from "../context/ToastContext";

// quotaRefreshCooldownMs is the click cooldown on a quota refresh: the sweep
// forwards to every quota-bearing provider and each call lands in the audit
// trail, so a second click inside the window is answered with a toast.
export const quotaRefreshCooldownMs = 10_000;

// useQuotaRefresh is the Providers page's "refresh quotas" action: the
// server-side sweep, its result toasts, and the click cooldown.
export function useQuotaRefresh() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const lastRefresh = useRef(0);
	const refreshQuotasMutation = useMutation({
		mutationFn: async () => {
			return api.providers.refreshQuotas();
		},
		onSuccess: (data) => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			if (data.failed > 0) {
				toast(
					t("providers.toast.refreshedQuotas", {
						refreshed: data.refreshed,
						failed: data.failed,
						skipped: data.skipped,
					}),
					"warning",
				);
			} else if (data.refreshed === 0) {
				toast(t("providers.toast_refresh_none"), "info");
			} else {
				toast(
					t("providers.toast_refresh_success", { count: data.refreshed }),
					"success",
				);
			}
		},
		onError: (err: Error) => {
			toast(
				t("providers.toast_refresh_failed", { message: err.message }),
				"error",
			);
		},
	});
	const refreshQuotas = useCallback(() => {
		const now = Date.now();
		if (now - lastRefresh.current < quotaRefreshCooldownMs) {
			toast(
				t("components.providerQuotaPanel.pleaseWaitBeforeRefreshing"),
				"info",
			);
			return;
		}
		lastRefresh.current = now;
		refreshQuotasMutation.mutate();
	}, [toast, t, refreshQuotasMutation]);
	return { refreshQuotas, isRefreshingQuotas: refreshQuotasMutation.isPending };
}
