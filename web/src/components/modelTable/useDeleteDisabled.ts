import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Model } from "../../api/types";
import { useToast } from "../../context/ToastContext";
import { loadAllDisabledModels } from "./modelCursor";

/**
 * The "Delete disabled" flow: pressing the button fetches every disabled row
 * of the current filters; the confirm dialog opens once that lands and lists
 * exactly those rows. `pendingDisabled` is null while nothing is pending.
 */
export function useDeleteDisabled(filters: Record<string, string | undefined>) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const [pendingDisabled, setPendingDisabled] = useState<Model[] | null>(null);
	const [loadingDisabled, setLoadingDisabled] = useState(false);

	const openDeleteDisabled = useCallback(async () => {
		setLoadingDisabled(true);
		try {
			setPendingDisabled(await loadAllDisabledModels(filters));
		} catch (err) {
			toast(
				t("components.virtualModelTable.deleteDisabledLoadFailed", {
					message: err instanceof Error ? err.message : String(err),
				}),
				"error",
			);
		} finally {
			setLoadingDisabled(false);
		}
	}, [filters, toast, t]);

	return {
		pendingDisabled,
		clearPendingDisabled: () => setPendingDisabled(null),
		loadingDisabled,
		openDeleteDisabled,
	};
}
