import { useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import { useToast } from "../context/ToastContext";
import { errorMessage } from "../utils/errors";
import { useRefreshDiscoveryBadge } from "./useRefreshDiscoveryBadge";

/**
 * Deletes disabled models in one atomic request instead of one DELETE per
 * model: a concurrent burst trips the admin IP rate limiter and reports
 * spurious failures. A failure does not prove nothing was deleted, so the
 * re-read runs whichever way the request went; `onDone` is where a page hangs
 * its own extra refresh.
 */
export function useBulkDeleteModels({
	successKey,
	errorKey,
	onDone,
}: {
	successKey: string;
	errorKey: string;
	onDone?: () => void;
}) {
	const queryClient = useQueryClient();
	const refreshBadge = useRefreshDiscoveryBadge();
	const { toast } = useToast();
	const { t } = useTranslation();

	return useCallback(
		async (ids: string[]) => {
			try {
				const { deleted } = await api.models.bulkDelete(ids);
				toast(t(successKey, { count: deleted }), "success");
			} catch (err) {
				toast(t(errorKey, { message: errorMessage(err) }), "error");
			} finally {
				queryClient.invalidateQueries({ queryKey: ["models"] });
				refreshBadge();
				onDone?.();
			}
		},
		[queryClient, refreshBadge, toast, t, successKey, errorKey, onDone],
	);
}
