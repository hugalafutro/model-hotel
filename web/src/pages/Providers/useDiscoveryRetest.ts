import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import type { DiscoveryDiff } from "../../api/types";
import { useToast } from "../../context/ToastContext";
import type { DiscoverySummaryEntry } from "./DiscoverySummaryModal";

/** Stable key for a summary entry, matching the modal's entryKeyOf. */
const keyOf = (entry: DiscoverySummaryEntry): string =>
	entry.entryKey ?? entry.providerName;

/**
 * Shared "Retest" behaviour for the discovery summary modal: re-runs discovery
 * for one provider (re-probing models disabled during the original run) and
 * hands the fresh diff back to `patchEntry` so the caller can update just that
 * provider's row in place. Used by both the Providers page (foreground discover)
 * and the global Layout (background changes modal).
 */
export function useDiscoveryRetest(
	patchEntry: (key: string, diff: DiscoveryDiff) => void,
) {
	const queryClient = useQueryClient();
	const { toast } = useToast();
	const { t } = useTranslation();
	const [retestingKey, setRetestingKey] = useState<string | undefined>(
		undefined,
	);

	const mutation = useMutation({
		mutationFn: (entry: DiscoverySummaryEntry) =>
			api.providers.discover(entry.providerId as string),
		onMutate: (entry) => {
			setRetestingKey(keyOf(entry));
		},
		onSuccess: (data, entry) => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			queryClient.invalidateQueries({ queryKey: ["models"] });
			patchEntry(keyOf(entry), data.diff);
			toast(
				t("providers.discoverySummary.retestDone", {
					provider: entry.providerName,
				}),
				"success",
			);
		},
		onError: (err: Error) => {
			toast(
				t("providers.toast_discover_failed", { message: err.message }),
				"error",
			);
		},
		onSettled: () => {
			setRetestingKey(undefined);
		},
	});

	return {
		onRetest: (entry: DiscoverySummaryEntry) => mutation.mutate(entry),
		retestingKey,
	};
}
