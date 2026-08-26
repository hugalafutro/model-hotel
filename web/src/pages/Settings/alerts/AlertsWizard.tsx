import { ntfyServerOf } from "@web-shared/alerts/composers";
import type { TFunction } from "i18next";
import { type Dispatch, useCallback, useEffect, useReducer } from "react";
import { useTranslation } from "react-i18next";
import { Check } from "@/lib/icons";
import { ApiError, api } from "../../../api/client";
import type { AlertEventDef } from "../../../api/types";
import { Modal } from "../../../components/Modal";
import { stripApiHead } from "./apiText";
import {
	StepApprise,
	StepDestinations,
	StepDetails,
	StepEvents,
	StepFinish,
	StepKind,
	StepTest,
} from "./steps";

// AlertsWizard walks an operator from "no alerting at all" to a working setup in
// one gated flow: point at apprise-api and prove it answers, pick a destination
// kind, fill in the parts that kind needs, deliver one real test to it, review
// the destination list, choose the events, and only then write settings once.
//
// Nothing is persisted before the last step: every earlier step either reads
// (probe) or sends a throwaway notification (test) with an explicit URL, so
// cancelling at any point leaves the stored configuration untouched. Each gate
// is a fact the wizard verified, never a checkbox the operator ticked, which is
// what makes "Finish" mean "this works" rather than "this was typed in".

import {
	type Action,
	canNext,
	initialState,
	reducer,
	type Step,
	TOTAL_STEPS,
} from "./wizardState";

const K = "settings.alerts.wizard";
export interface AlertsWizardProps {
	/** Saved Apprise API URL ("" when none is configured yet). */
	initialApiUrl: string;
	/** Plaintext saved destinations. The wizard appends to these, never drops one. */
	savedTargets: string[];
	/**
	 * Saved alert_events CSV, or null when the setting has never been written.
	 * Null on a setup run seeds the recommended preset; "" is a real selection
	 * (everything deselected) and stays empty.
	 */
	savedEvents: string | null;
	catalog: AlertEventDef[];
	/** 1 = Set up alerts / Re-run setup, 2 = Add destination to a working setup. */
	startAt: 1 | 2;
	/** True on a member whose events are owned fleet-wide by config sync. */
	managed?: boolean;
	/** Cancel: nothing has been written. */
	onClose: () => void;
	/** After the single PUT succeeded; the parent reloads its own state. */
	onFinished: () => void;
}
export function AlertsWizard(props: AlertsWizardProps) {
	// savedTargets is seeded into state (and re-read at Finish), so the stored
	// half is read off state from here on rather than off the prop.
	const { startAt, initialApiUrl, catalog, managed, onClose, onFinished } =
		props;
	const { t } = useTranslation();
	const [state, dispatch] = useReducer(reducer, props, initialState);

	// The probe is taken against the trimmed address (which is what a save would
	// store), while the result is filed under the raw field value so the step 1
	// gate keeps comparing like for like.
	const runProbe = useCallback((url: string, demote: boolean) => {
		dispatch({ type: "checking" });
		api.alert
			.probe(url.trim())
			.then((status) => dispatch({ type: "probed", url, status, demote }))
			.catch((err) =>
				dispatch({
					type: "probed",
					url,
					// A transport failure is indistinguishable from an unreachable
					// apprise as far as the gate is concerned: both mean "not proven".
					status: {
						configured: url.trim() !== "",
						reachable: false,
						healthy: false,
						reason:
							err instanceof ApiError
								? (err.code ?? "unreachable")
								: "unreachable",
					},
					demote,
				}),
			);
	}, []);

	// Entering at "Add destination" trusts the saved URL enough to show step 2
	// straight away, then confirms it in the background. The confirmation is what
	// keeps the trust honest: a dead apprise sends the run back to step 1.
	useEffect(() => {
		if (startAt === 2 && initialApiUrl !== "") runProbe(initialApiUrl, true);
	}, [startAt, initialApiUrl, runProbe]);

	const sendTest = () => {
		dispatch({ type: "testing" });
		api.alert
			.test({ api_url: state.apiUrl.trim(), targets: [state.draft.url] })
			.then(() => dispatch({ type: "tested" }))
			.catch((err) =>
				dispatch({
					type: "testFailed",
					code: err instanceof ApiError ? (err.code ?? "") : "",
				}),
			);
	};

	// The gate is re-checked here as well as on the button: a disabled button is a
	// hint, this is the rule.
	const goNext = () => {
		if (!canNext(state)) return;
		if (state.step === 4) dispatch({ type: "acceptDraft" });
		else dispatch({ type: "go", step: (state.step + 1) as Step });
	};

	// The destination list the run finishes with: the stored half plus what this
	// run proved. Nothing is ever taken away from the stored half, and `finish`
	// re-reads it into state before the write, so from step 7 onwards the summary
	// and the done screen show exactly what was written.
	const finalTargets = [...state.saved, ...state.added];

	// The one and only write. Everything before this step was a read or a
	// throwaway notification, so this is the moment the wizard's work becomes
	// configuration; the status read after it is what the closing pill reports.
	const finish = async () => {
		dispatch({ type: "finishing" });

		// The write replaces the whole destination list, and the copy this dialog
		// opened with is as old as the dialog: anything saved elsewhere since then
		// (another tab, another operator) would be written away. The stored list is
		// re-read here so the write is "what is stored now, plus this run's work".
		let stored: string[];
		try {
			stored = (await api.alert.targets()).targets;
		} catch (err) {
			// Without a trustworthy stored list the only write available is one that
			// loses destinations, so nothing is written at all and the run stays on
			// step 7 where Finish can be pressed again. The card reads this failure
			// the same way: a rotated master key is the one cause worth naming,
			// because it tells the operator what to do about it.
			dispatch({
				type: "finishFailed",
				message:
					err instanceof ApiError && err.code === "undecryptable"
						? t("settings.alerts.destinations.error")
						: t("settings.alerts.destinations.readFailed"),
			});
			return;
		}
		// The summary and the done screen read off state, so the fresh list lands
		// there before the write rather than after it: what step 7 shows while the
		// write is in flight is then already what the write carries.
		dispatch({ type: "savedRefreshed", targets: stored });
		const merged = [...stored, ...state.added].filter(
			(u, i, all) => all.indexOf(u) === i,
		);

		try {
			await api.settings.update({
				alert_apprise_api_url: state.apiUrl.trim(),
				alert_apprise_targets: merged.join("; "),
				// Config sync owns both of these on a managed member, so the wizard
				// writes only what is local to it: the address and the destinations.
				...(managed
					? {}
					: {
							alert_enabled: "true",
							// The selection starts as the catalog's recommended set, so
							// without a catalog it starts empty and writing it would store
							// "alert me about nothing" as though it had been chosen. The key
							// is left out of the write instead, which leaves whatever is
							// stored (and the server's defaults when nothing is) in force.
							// The card keeps the run from starting in this state; this is
							// the belt to that braces.
							...(catalog.length === 0
								? {}
								: { alert_events: [...state.events].join(",") }),
						}),
			});
		} catch (err) {
			dispatch({
				type: "finishFailed",
				// A 400 carries a safe, actionable sentence; anything else could
				// leak internals, so it is reported generically. The dialog already
				// says which step failed, so fetchOK's "what failed: <status>" head
				// comes off and only the sentence is shown.
				message:
					err instanceof ApiError && err.status === 400
						? stripApiHead(err.message, "Failed to update settings")
						: t("common.unknownError"),
			});
			return;
		}

		try {
			dispatch({ type: "finished", status: await api.alert.status() });
		} catch {
			// The settings landed; a failed probe read only costs the pill.
			dispatch({ type: "finished", status: null });
		}
	};

	// The stored configuration is now the live one, so this test carries no
	// body: it exercises exactly what was written, to every destination at once.
	const sendAll = () => {
		dispatch({ type: "sendingAll" });
		api.alert
			.test()
			.then(() => dispatch({ type: "sentAll", ok: true }))
			.catch(() => dispatch({ type: "sentAll", ok: false }));
	};

	// One row of the step 5 list, through the address this run proved rather
	// than the stored one, which may still be a different apprise.
	const testRow = async (url: string) => {
		await api.alert.test({ api_url: state.apiUrl.trim(), targets: [url] });
	};

	const stepProps = { state, dispatch: dispatch as Dispatch<Action>, t };
	const body = () => {
		switch (state.step) {
			case 1:
				return (
					<StepApprise
						{...stepProps}
						onCheck={() => runProbe(state.apiUrl, false)}
					/>
				);
			case 2:
				return (
					<StepKind {...stepProps} ntfyServer={ntfyServerOf(finalTargets)} />
				);
			case 3:
				return <StepDetails {...stepProps} />;
			case 4:
				return <StepTest {...stepProps} onSendTest={sendTest} />;
			case 5:
				return (
					<StepDestinations
						{...stepProps}
						savedTargets={state.saved}
						onTestRow={testRow}
					/>
				);
			case 6:
				return (
					<StepEvents {...stepProps} catalog={catalog} managed={managed} />
				);
			default:
				return (
					<StepFinish
						{...stepProps}
						targets={finalTargets}
						managed={managed}
						onSendAll={sendAll}
					/>
				);
		}
	};

	// Work that a step is waiting on, so moving off it would strand the result.
	// Sending the closing test is deliberately not part of it: the run is over,
	// its outcome is a note, and nothing downstream depends on it.
	const busy = state.apiChecking || state.testing || state.finishing;

	// After "Add another" was abandoned back to the list, the draft is empty and
	// step 4 has nothing to test: Back goes to where a destination is started.
	const backStep: Step =
		state.step === 5 && state.draft.kind === null
			? 2
			: ((state.step - 1) as Step);

	return (
		<Modal
			title={t(`${K}.title`)}
			// Once the write has landed there is nothing left to cancel, and the
			// parent's copy of the settings is stale: every way out of the dialog
			// tells it to reload.
			onClose={state.done ? onFinished : onClose}
			// Escape is Cancel by another name, so it is allowed wherever Cancel is:
			// a probe or a test in flight changes nothing that is stored, and only
			// the write itself is worth waiting for.
			dismissible={!state.finishing}
			closeOnBackdrop={false}
			maxWidth="max-w-2xl"
			scrollable
		>
			<div className="space-y-5">
				<StepAnnouncer step={state.step} done={state.done} t={t} />
				<StepRail step={state.step} t={t} />
				{/* The step change is announced by StepAnnouncer above and nothing
				    else: a live region over the body would read every keystroke in a
				    destination field back at the operator. The role="alert" nodes
				    inside the body (a failed test) sit outside it and keep announcing
				    themselves. */}
				<div className="space-y-3" data-testid={`wiz-step-${state.step}`}>
					{body()}
				</div>
				<div className="flex flex-wrap justify-end gap-2 border-t border-(--border-subtle) pt-4">
					{state.done ? (
						<button
							type="button"
							className="ui-btn ui-btn-primary"
							data-testid="wiz-close"
							onClick={onFinished}
						>
							{t(`${K}.close`)}
						</button>
					) : (
						<>
							<button
								type="button"
								className="ui-btn ui-btn-secondary"
								data-testid="wiz-cancel"
								// A probe or a test in flight changes nothing that is stored,
								// so walking out mid-request is always allowed; only the write
								// itself is worth waiting for.
								disabled={state.finishing}
								onClick={onClose}
							>
								{t(`${K}.cancel`)}
							</button>
							{state.step > state.minStep && (
								<button
									type="button"
									className="ui-btn ui-btn-secondary"
									data-testid="wiz-back"
									disabled={busy}
									onClick={() => dispatch({ type: "go", step: backStep })}
								>
									{t(`${K}.back`)}
								</button>
							)}
							{state.step < TOTAL_STEPS ? (
								<button
									type="button"
									className="ui-btn ui-btn-primary"
									data-testid="wiz-next"
									disabled={busy || !canNext(state)}
									onClick={goNext}
								>
									{t(`${K}.next`)}
								</button>
							) : (
								<button
									type="button"
									className="ui-btn ui-btn-primary"
									data-testid="wiz-finish"
									disabled={busy}
									onClick={finish}
								>
									{state.finishing ? t(`${K}.finishing`) : t(`${K}.finish`)}
								</button>
							)}
						</>
					)}
				</div>
			</div>
		</Modal>
	);
}
// StepAnnouncer is the wizard's single live region, mounted for the whole run
// so that moving to another step mutates its text. A region that arrives with
// its text already in it is the case screen readers routinely miss, which is
// what a per-step region (one live node per step body) would be. It carries
// only the step's name, so nothing typed into the step is read back.
function StepAnnouncer({
	step,
	done,
	t,
}: {
	step: Step;
	done: boolean;
	t: TFunction;
}) {
	return (
		<p role="status" className="sr-only" data-testid="wiz-announce">
			{done ? t(`${K}.done`) : t(`${K}.step${step}Title`)}
		</p>
	);
}
// StepRail is the run's progress, drawn as one node per step joined by
// hairlines: done nodes carry a check, the current one is lit in the accent,
// the ones ahead wait in the input surface.
//
// It is decoration, and marked as such. Everything it says is already in the
// "Step n of N" caption beside it, in words, which is what a screen reader
// reads; exposing the rail as well would spend seven list items on the same
// fact and still leave done-versus-ahead carried by colour and a glyph. Each
// node names its step in a tooltip for the pointer; the titles are too long,
// in most locales, to sit under seven nodes at this width.
function StepRail({ step, t }: { step: Step; t: TFunction }) {
	const steps = Array.from({ length: TOTAL_STEPS }, (_, i) => (i + 1) as Step);
	return (
		<div className="flex flex-wrap items-center gap-x-4 gap-y-2">
			<ol className="ui-wizard-rail min-w-0 flex-1" aria-hidden="true">
				{steps.map((n) => {
					const state = n < step ? "done" : n === step ? "current" : "ahead";
					return (
						<li key={n} className="contents">
							{n > 1 && (
								<span
									aria-hidden="true"
									className="ui-wizard-link"
									data-done={n <= step ? "true" : "false"}
								/>
							)}
							<span
								className="ui-wizard-node"
								data-state={state}
								data-testid={`wiz-rail-${n}`}
								title={t(`${K}.step${n}Title`)}
							>
								{state === "done" ? <Check size={14} weight="bold" /> : n}
							</span>
						</li>
					);
				})}
			</ol>
			<p
				className="shrink-0 text-xs text-(--text-muted) tabular-nums"
				data-testid="wiz-step-of"
			>
				{t(`${K}.stepOf`, { step, total: TOTAL_STEPS })}
			</p>
		</div>
	);
}
