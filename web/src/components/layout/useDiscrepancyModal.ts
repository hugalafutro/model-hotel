import { useQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { useToast } from "../../context/ToastContext";
import {
	type MergedProvider,
	providerHasNoPending,
	retestProvesNothing,
	useDiscrepancies,
} from "../../hooks/useDiscrepancies";
import { useRefreshDiscoveryBadge } from "../../hooks/useRefreshDiscoveryBadge";
import { useDiscoveryRetest } from "../../pages/Providers/useDiscoveryRetest";
import type { ModelDiscrepancyModalProps } from "../ModelDiscrepancyModal";

/** What the Models nav badge shows, derived from the polled discovery status. */
export interface DiscoveryBadge {
	claimCount: number;
	informationalUnseen: number;
	hasPinned: boolean;
	/** One string for the accessible name AND the tooltip; see below. */
	label: string;
}

export type DiscrepancyModalProps = Omit<
	ModelDiscrepancyModalProps,
	"readOnly" | "managed"
>;

/**
 * Everything behind the Models nav badge and the discrepancy modal: the
 * polled discovery status the badge reads, the modal's open state, and the
 * retest / dismiss / unpin actions the modal drives. Called unconditionally
 * from Layout, which never unmounts while the dashboard is up. That is
 * load-bearing, not incidental: useDiscrepancies keys its fetch on a session
 * counter that only advances on an open transition, so unmounting it on
 * close would reset the counter, replay the first query from cache on
 * reopen, and stop the per-open ?review=1 stamp.
 */
export function useDiscrepancyModal() {
	const { t } = useTranslation();
	const { toast } = useToast();

	// Live discrepancy state → Models nav badge. `claim_count` is what the number
	// shows; `informational_unseen` only ever produces a dot.
	//
	// `status(false)`, never `status(true)`: the review variant stamps the
	// server-side last-reviewed marker, and a 60s poll doing that would collapse
	// every "since your last visit" flap count to zero, permanently. Only the
	// modal-open fetch inside useDiscrepancies is allowed to stamp.
	const { data: discoveryStatus } = useQuery({
		queryKey: ["discovery-status"],
		queryFn: () => api.discovery.status(false),
		refetchInterval: 60_000,
		placeholderData: (prev) => prev,
	});
	const claimCount = discoveryStatus?.claim_count ?? 0;
	const informationalUnseen = discoveryStatus?.informational_unseen ?? 0;
	// A pinned model moves neither counter: it is never counted (it is a decision,
	// not a problem) and it is not informational news. The badge is the only way
	// into the modal, so without this a status whose sole content is a pin renders
	// no badge at all and the pinned bucket, with its Unpin control, cannot be
	// reached. `?? []` because a server that predates the pin omits the bucket.
	const hasPinned =
		discoveryStatus?.claims.some((p) => (p.pinned ?? []).length > 0) ?? false;
	// One string for the badge's accessible name AND its tooltip, deliberately.
	// The dot carries no visible text, so this IS its accessible name; splitting
	// them would mean a sighted user reads one sentence and a screen-reader user
	// hears another. The counted branches name the count the badge is triggered by
	// (for the news dot the UNSEEN count, not the zone's total, which is what makes
	// it legible next to the modal's "Recent changes" header). Counted keys, so the
	// number agrees in every language.
	//
	// The pin branch is last and carries no count: it is what a dot lit by a pin
	// alone says, and borrowing the news key there would announce "0 unreviewed
	// changes" for a badge that is not about news at all.
	const label =
		claimCount > 0
			? t("layout.nav.discoveryClaimsBadge", { count: claimCount })
			: informationalUnseen > 0
				? t("layout.nav.discoveryNewsBadge", { count: informationalUnseen })
				: t("layout.nav.discoveryPinnedBadge");
	const badge: DiscoveryBadge = {
		claimCount,
		informationalUnseen,
		hasPinned,
		label,
	};

	const [open, setOpen] = useState(false);
	const {
		snapshot,
		groupClaims,
		informational,
		refresh,
		loading,
		isError: failed,
		error,
		refreshError,
		dismissClaim,
	} = useDiscrepancies(open);
	const [retestErrors, setRetestErrors] = useState<Record<string, string>>({});
	const [retestAllProgress, setRetestAllProgress] = useState<
		{ done: number; total: number } | undefined
	>(undefined);

	// Requirement 5 keeps the ?review=1 stamp off the 60s timer; the `exact: true`
	// inside this hook is what keeps it off a click. See useRefreshDiscoveryBadge.
	//
	// Retests do NOT call it from here: useDiscoveryRetest owns that, so a retest
	// run from the Providers page refreshes the badge too.
	const refreshBadge = useRefreshDiscoveryBadge();

	// The retest response's own diff is deliberately discarded. It describes what
	// THAT run changed, which is empty when the model is still missing, and
	// reading that emptiness as "fixed" is the original defect. Truth comes from
	// re-reading /api/discovery/status instead.
	const { retestAsync, retestingKey, isAnyRetesting } = useDiscoveryRetest(
		() => {},
	);

	// Serialises retests inside this hook. A ref, not `isAnyRetesting`: a running
	// walk holds a closure from one render, so any state read inside it is frozen
	// at that render's value and cannot act as a lock.
	const retestInFlight = useRef(false);

	const runRetest = useCallback(
		async (
			providerId: string,
			providerName: string,
			// The walk silences the shared hook's per-provider toast and reports once
			// at the end: eight providers would otherwise stack eight toasts, none of
			// which ToastContext can dedupe because each names a different provider.
			silent = false,
		): Promise<boolean> => {
			if (retestInFlight.current) return false;
			retestInFlight.current = true;
			try {
				await retestAsync(
					{
						providerName,
						providerId,
						// keyOf() prefers entryKey, so this is what `retestingKey` becomes
						// and is what the modal matches against `provider_id`.
						entryKey: providerId,
					},
					silent,
				);
				setRetestErrors((prev) => {
					if (!(providerId in prev)) return prev;
					const next = { ...prev };
					delete next[providerId];
					return next;
				});
				await refresh();
				return true;
			} catch (err) {
				// Recorded per provider, not only toasted: a toast fades before it is
				// read, and the section must keep saying why it did not clear.
				setRetestErrors((prev) => ({
					...prev,
					[providerId]: err instanceof Error ? err.message : String(err),
				}));
				return false;
			} finally {
				retestInFlight.current = false;
			}
		},
		[retestAsync, refresh],
	);

	// Set by Cancel, read at the top of each walk iteration.
	const cancelRetestAll = useRef(false);
	const onCancelRetestAll = useCallback(() => {
		cancelRetestAll.current = true;
	}, []);

	const onRetestAll = useCallback(async () => {
		// Same predicate the modal's controls use, and applied HERE because this is
		// the walk. Gating only the button meant a mixed fleet still retested the
		// retired-only providers: the button rendered for the one that needed it,
		// and the walk then visited every provider with anything pending, each
		// pointless one costing a slow upstream call while its own pill sat
		// disabled saying so. The progress readout counted them too.
		const targets = snapshot.filter(
			(p: MergedProvider) =>
				!providerHasNoPending(p) && !retestProvesNothing(p),
		);
		if (targets.length === 0 || retestInFlight.current) return;
		cancelRetestAll.current = false;
		setRetestAllProgress({ done: 0, total: targets.length });
		let failed = 0;
		let done = 0;
		for (const p of targets) {
			// Checked here and nowhere else: Cancel stops the walk BEFORE the next
			// provider starts, so the run already in flight finishes. Aborting a
			// discovery request mid-call would leave that provider half-applied.
			if (cancelRetestAll.current) break;
			if (!(await runRetest(p.provider_id, p.provider_name, true))) failed++;
			done++;
			setRetestAllProgress({ done, total: targets.length });
		}
		// Read before the reset: the summary below must know whether the walk ran
		// out of providers or was stopped.
		const cancelled = cancelRetestAll.current;
		cancelRetestAll.current = false;
		setRetestAllProgress(undefined);
		// One report for the whole walk, since the per-provider toasts are silenced.
		// Failures also stay bannered in their own provider section, so this is a
		// summary and not the only record.
		//
		// Cancellation is reported ahead of failures precisely because it is the one
		// outcome with no other record: a failed provider keeps its banner, but a
		// walk that stopped early leaves untouched providers looking retested.
		if (cancelled) {
			toast(
				t("providers.discrepancies.retestAllCancelled", { count: done }),
				"info",
			);
		} else if (failed > 0) {
			toast(
				t("providers.discrepancies.retestAllFailed", { count: failed }),
				"error",
			);
		} else {
			toast(
				t("providers.discrepancies.retestAllDone", { count: done }),
				"success",
			);
		}
	}, [snapshot, runRetest, toast, t]);

	const onDismissAll = useCallback(
		async (providerId: string, modelIds: string[]) => {
			if (modelIds.length === 0) return;
			// NOT optimistic, unlike the per-row path, and that is the whole point.
			//
			// A batch can come back short, and `updated` does not say WHICH ids it
			// missed. Marking every requested row dismissed up front and correcting
			// afterwards cannot be made to work: only a successful refresh knows which
			// took, `refresh` absorbs its own failure, and the rollback that was tried
			// here could not survive a concurrent refresh.
			//
			// The cost of getting it wrong is not cosmetic. With every row dismissed,
			// providerHasNoPending goes true, so the pill swaps Retest and Dismiss all
			// for Clean: the operator loses the controls for models the server never
			// dismissed. A refresh-error banner does not give those back.
			//
			// So nothing is claimed that the server did not confirm. A full-count
			// response is confirmation for every id; a short one is confirmation for
			// none of them in particular, so the rows stay actionable and the refresh
			// reconciles. Costs the strike-through for one round trip, behind a
			// confirmation dialog, which is a fair price for never showing a provider
			// as clean when it is not.
			try {
				const res = await api.discovery.dismiss(providerId, modelIds);
				// The response NAMES what it dismissed, so a partial result needs no
				// guessing: mark exactly those and leave the rest pending for the
				// refresh. Marking all of them would strike through models the server
				// skipped; marking none would let the merge read the ones that did land
				// as "listed again", and could swap the pill to Clean on a partial.
				dismissClaim(providerId, new Set(res.dismissed));
				await refresh();
				if (res.updated < modelIds.length) {
					toast(
						t("providers.discrepancies.dismissAllPartial", {
							count: res.updated,
							total: modelIds.length,
						}),
						"warning",
					);
					return;
				}
				toast(
					t("providers.discrepancies.dismissAllDone", { count: res.updated }),
					"success",
				);
			} catch (err) {
				// No rollback needed: nothing was claimed before the response.
				toast(
					t("providers.discrepancies.dismissFailed", {
						message: err instanceof Error ? err.message : String(err),
					}),
					"error",
				);
			} finally {
				// `finally`, not the success path: a request that rejects can still
				// have landed — the response is what was lost — and the rows correctly
				// stay actionable either way. Re-reading the badge is how it learns
				// which of the two happened.
				refreshBadge();
			}
		},
		[dismissClaim, refresh, refreshBadge, toast, t],
	);

	const onDismissEverything = useCallback(
		async (batches: { providerID: string; modelIDs: string[] }[]) => {
			if (batches.length === 0) return;
			const total = batches.reduce((n, b) => n + b.modelIDs.length, 0);
			const results = await Promise.allSettled(
				batches.map((b) => api.discovery.dismiss(b.providerID, b.modelIDs)),
			);
			// Per batch, claim only what that provider's own response confirmed; see
			// onDismissAll for why nothing is claimed up front. A rejected batch and a
			// short batch are both "not confirmed" and both stay actionable, so no
			// rollback is needed for either.
			let dismissed = 0;
			batches.forEach((b, i) => {
				const r = results[i];
				if (r.status !== "fulfilled") return;
				dismissed += r.value.updated;
				dismissClaim(b.providerID, new Set(r.value.dismissed));
			});
			await refresh();
			// After the batches, not per batch: one invalidation covers every
			// provider's worth of dismissals. Unconditional even when nothing was
			// confirmed, because a batch that rejects can still have landed — the
			// response is what was lost — and the badge is better re-read than
			// inferred from a count the client only partly knows.
			refreshBadge();
			if (dismissed === 0) {
				toast(t("providers.discrepancies.dismissEverythingFailed"), "error");
				return;
			}
			toast(
				dismissed < total
					? t("providers.discrepancies.dismissAllPartial", {
							count: dismissed,
							total,
						})
					: t("providers.discrepancies.dismissAllDone", { count: dismissed }),
				dismissed < total ? "warning" : "success",
			);
		},
		[dismissClaim, refresh, refreshBadge, toast, t],
	);

	const onDismiss = useCallback(
		async (providerId: string, modelId: string) => {
			try {
				// Exactly one model per request. The endpoint 200s with a short
				// `updated` for a mixed list and only 404s when NOTHING matched, so a
				// batch cannot say which models it missed; one at a time makes
				// `updated: 0` an unambiguous failure for this model.
				const res = await api.discovery.dismiss(providerId, [modelId]);
				// Unreachable today: the server 404s (and fetchJSON throws before `res`
				// exists) whenever `affected == 0`, so a 0-updated response cannot reach
				// this branch. Kept as the guard for the one-model-per-call contract
				// described above, in case the server ever starts 200ing on a partial or
				// zero-count match.
				if (res.updated === 0) {
					throw new Error(t("providers.discrepancies.dismissNoMatch"));
				}
				// Marked AFTER the server confirms, never before, and from the ids the
				// response names, which is the same rule the provider-wide and
				// modal-wide paths follow.
				//
				// Marking it up front raced any refresh that landed while the request was
				// out: that refresh still saw the model reported, so it rebuilt the row as
				// `pending` and dropped the dismissed status. The next refresh then found
				// the model absent, and an absent row that is not marked dismissed reads
				// as `resolved` - "is listed again" - for a model the operator had just
				// dismissed by hand. Exactly the false relist this rework exists to
				// remove. Setting the status only once the write is confirmed leaves
				// nothing for a refresh to strip.
				dismissClaim(providerId, new Set(res.dismissed));
				await refresh();
				toast(
					t("providers.discrepancies.dismissed", { model: modelId }),
					"success",
				);
			} catch (err) {
				// No rollback: nothing was claimed before the response.
				toast(
					t("providers.discrepancies.dismissFailed", {
						message: err instanceof Error ? err.message : String(err),
					}),
					"error",
				);
			} finally {
				// See onDismissAll: a rejected request can still have landed.
				refreshBadge();
			}
		},
		[dismissClaim, refresh, refreshBadge, toast, t],
	);

	// Hands one pinned model back to automatic management.
	//
	// One model per request, like onDismiss and for the same reason: the response
	// names the ids it cleared, and asking about one makes a short answer
	// unambiguous. There is no `updated` count to read here; the server 404s (and
	// fetchJSON throws) when nothing matched.
	//
	// The row is marked with `dismissClaim` rather than being removed, once the
	// server confirms. An unpinned model leaves /api/discovery/status outright -
	// the pin is gone and the miss streak is reset, so there is no claim left to
	// report - and that absence is caused by the operator, exactly like a
	// dismissal. Left unmarked, the next refresh would read it as `resolved` and
	// the cleared summary would announce "is listed again" for a model whose
	// listing has not changed at all.
	const onUnpin = useCallback(
		async (providerId: string, modelId: string) => {
			try {
				const res = await api.discovery.unpin(providerId, [modelId]);
				dismissClaim(providerId, new Set(res.unpinned));
				toast(
					t("providers.discrepancies.unpinned", { model: modelId }),
					"success",
				);
			} catch (err) {
				// No rollback: nothing was claimed before the response.
				toast(
					t("providers.discrepancies.unpinFailed", {
						message: err instanceof Error ? err.message : String(err),
					}),
					"error",
				);
			} finally {
				// See onDismiss: a rejected request can still have landed, so both
				// reads happen on either path. The badge is the half unique to a pin:
				// a pinned row is what keeps it lit when nothing is counted, so the
				// last unpin has to be able to put it out.
				await refresh();
				refreshBadge();
			}
		},
		[dismissClaim, refresh, refreshBadge, toast, t],
	);

	// Expanding the journal is what marks it read; the destructive ack-on-open is
	// gone, so nothing clears the dot until the operator actually looks.
	const onExpandInformational = useCallback(() => {
		api.discovery
			.ackChanges()
			.catch(() => {
				// Badge dot simply stays lit for a later attempt.
			})
			.finally(refreshBadge);
	}, [refreshBadge]);

	useEffect(() => {
		const handler = (e: Event) => {
			const detail = (e as CustomEvent).detail;
			if (detail?.type === "discovery.changes_pending") {
				refreshBadge();
			}
		};
		window.addEventListener("server-event", handler);
		return () => window.removeEventListener("server-event", handler);
	}, [refreshBadge]);

	// A failed fetch must reach the modal: it renders a failure banner and, more
	// importantly, suppresses the "nothing is wrong" empty state.
	const loadError = failed
		? error instanceof Error
			? error.message
			: String(error)
		: refreshError?.message;

	const modalProps: DiscrepancyModalProps = {
		providers: snapshot,
		groupClaims,
		informational,
		onClose: () => {
			setOpen(false);
			// Retest failures are per visit. Carrying them into the next open
			// would banner a stale reason next to freshly fetched claims.
			setRetestErrors({});
		},
		onRetest: (providerId, providerName) => {
			void runRetest(providerId, providerName);
		},
		onRetestAll: () => {
			void onRetestAll();
		},
		onCancelRetestAll,
		onDismiss: (providerId, modelId) => {
			void onDismiss(providerId, modelId);
		},
		onDismissAll: (providerId, modelIds) => {
			void onDismissAll(providerId, modelIds);
		},
		onDismissEverything: (batches) => {
			void onDismissEverything(batches);
		},
		onUnpin: (providerId, modelId) => {
			void onUnpin(providerId, modelId);
		},
		retestingProviderId: retestingKey,
		isRetesting: isAnyRetesting,
		retestAllProgress,
		errors: retestErrors,
		onExpandInformational,
		loadError,
		loading,
	};

	return { badge, open, setOpen, modalProps };
}
