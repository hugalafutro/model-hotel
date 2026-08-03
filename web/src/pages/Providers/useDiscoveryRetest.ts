import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import type { DiscoveryDiff } from "../../api/types";
import { useToast } from "../../context/ToastContext";
import { useRefreshDiscoveryBadge } from "../../hooks/useRefreshDiscoveryBadge";
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
	const refreshBadge = useRefreshDiscoveryBadge();
	const { toast } = useToast();
	const { t } = useTranslation();
	const [retestingKey, setRetestingKey] = useState<string | undefined>(
		undefined,
	);

	// `silent` suppresses this hook's own per-provider toasts. A caller retesting
	// one provider on one click wants the confirmation; a caller walking eight
	// providers does not want eight stacked toasts (ToastContext dedupes by
	// message, and the provider name differs in every one, so nothing collapses
	// them). Carried on the mutation variables rather than a ref so it is
	// explicit at the call site and typed.
	interface RetestVars {
		entry: DiscoverySummaryEntry;
		silent?: boolean;
	}

	const mutation = useMutation({
		mutationFn: ({ entry }: RetestVars) => {
			// Retest is only ever wired up for entries with a providerId (the modal
			// hides the button otherwise), but guard here instead of casting so a
			// missing id fails loudly rather than hitting /providers/undefined/discover.
			if (!entry.providerId) {
				return Promise.reject(new Error("retest requires a providerId"));
			}
			return api.providers.discover(entry.providerId);
		},
		onMutate: ({ entry }) => {
			setRetestingKey(keyOf(entry));
		},
		onSuccess: (data, { entry, silent }) => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			queryClient.invalidateQueries({ queryKey: ["models"] });
			patchEntry(keyOf(entry), data.diff);
			if (silent) return;
			toast(
				t("providers.discoverySummary.retestDone", {
					provider: entry.providerName,
				}),
				"success",
			);
		},
		onError: (err: Error, { silent }) => {
			if (silent) return;
			toast(
				t("providers.toast_discover_failed", { message: err.message }),
				"error",
			);
		},
		onSettled: () => {
			setRetestingKey(undefined);
			// Here rather than in onSuccess, unlike the two invalidations above: a
			// discovery run that errors partway has still upserted whatever it got
			// to before failing, so the claim set can move without a success.
			//
			// And here rather than in the callers, so a retest refreshes the badge
			// from the Providers page as well as from the discrepancy modal.
			refreshBadge();
		},
	});

	return {
		onRetest: (entry: DiscoverySummaryEntry) => {
			// Discovery is a heavy upstream call, so callers disable every Retest
			// while one runs: three rapid clicks otherwise each overwrite
			// retestingKey and stop the previous row's spinner while its request
			// is still out. Guarding here (not just in the UI) keeps the lock
			// correct even if a caller doesn't wire isAnyRetesting into its buttons.
			if (mutation.isPending) return;
			mutation.mutate({ entry });
		},
		/**
		 * Awaitable single retest, for callers that walk providers sequentially and
		 * must know when each run has settled. Rejects on failure so the caller can
		 * record a per-provider error. `onRetest`'s fire-and-forget lock is not
		 * applied here: it reads `mutation.isPending` from the render that produced
		 * the closure, which a running walk holds fixed, so it would be a stale
		 * value rather than a lock. Callers of this must serialize themselves.
		 *
		 * Pass `silent` to take over the messaging: the walk reports once at the
		 * end instead of once per provider.
		 */
		retestAsync: (entry: DiscoverySummaryEntry, silent = false) =>
			mutation.mutateAsync({ entry, silent }),
		retestingKey,
		/** True while any retest is in flight; callers disable every Retest button off this. */
		isAnyRetesting: mutation.isPending,
	};
}
