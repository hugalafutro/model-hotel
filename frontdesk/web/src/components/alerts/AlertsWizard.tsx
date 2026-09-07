import { ntfyServerOf } from "@web-shared/alerts/composers";
import { parseCsv } from "@web-shared/alerts/events";
import {
	type Action,
	canNext,
	DEFAULT_APPRISE_URL,
	EMPTY_DRAFT,
	reducer,
	type Step,
	TOTAL_STEPS,
	type WizardState,
} from "@web-shared/alerts/wizardState";
import { type Dispatch, useCallback, useEffect, useReducer } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../../api/client";
import type { AlertEventDef } from "../../api/types";
import { Modal } from "../Modal";
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

const K = "settings.alerts.wizard";

export interface AlertsWizardProps {
	/** Saved Apprise API URL ("" when none is configured yet). */
	initialApiUrl: string;
	/** Plaintext saved destinations. The wizard appends to these, never drops one. */
	savedTargets: string[];
	/**
	 * Saved alert_events CSV. Blank is a real selection (everything deselected),
	 * not "unset": only a run that starts at step 1 reads it as "nothing has been
	 * chosen yet" and seeds the recommended preset.
	 */
	savedEvents: string;
	catalog: AlertEventDef[];
	/** 1 = Set up alerts / Re-run setup, 2 = Add destination to a working setup. */
	startAt: 1 | 2;
	/** Cancel: nothing has been written. */
	onClose: () => void;
	/** After the single PUT succeeded; the parent reloads its own state. */
	onFinished: () => void;
}

// eslint-disable-next-line react-refresh/only-export-components -- wizard state logic exported for its tests beside the component
export function initialState(p: AlertsWizardProps): WizardState {
	// "Add destination" only makes sense against a configured apprise-api; without
	// one the run starts at step 1 whatever the caller asked for.
	const start: Step = p.startAt === 2 && p.initialApiUrl !== "" ? 2 : 1;
	// Front Desk stores a non-empty CSV for every selection it has ever been
	// given, so a blank one on an "Add destination" run is the operator having
	// deliberately turned every event off. Re-ticking the preset behind their back
	// would silently undo that, so the preset is only the seed for a setup run.
	const events =
		p.startAt === 1 && p.savedEvents.trim() === ""
			? new Set(p.catalog.filter((e) => e.defaultOn).map((e) => e.type))
			: parseCsv(p.savedEvents);
	return {
		step: start,
		minStep: start,
		listSeen: false,
		apiUrl: p.initialApiUrl || DEFAULT_APPRISE_URL,
		probedUrl: "",
		apiStatus: null,
		apiChecking: false,
		draft: EMPTY_DRAFT,
		added: [],
		saved: p.savedTargets,
		events,
		testing: false,
		testError: "",
		testOk: false,
		finishing: false,
		finishError: "",
		done: false,
		finalStatus: null,
		sendingAll: false,
		sentAll: "none",
	};
}

export function AlertsWizard(props: AlertsWizardProps) {
	// savedTargets is seeded into state (and re-read at Finish), so the stored
	// half is read off state from here on rather than off the prop.
	const { catalog, startAt, initialApiUrl, onClose, onFinished } = props;
	const { t } = useTranslation();
	const [state, dispatch] = useReducer(reducer, props, initialState);

	// The probe is taken against the trimmed address (which is what a save would
	// store), while the result is filed under the raw field value so the step 1
	// gate keeps comparing like for like.
	const runProbe = useCallback((url: string, demote: boolean) => {
		dispatch({ type: "checking" });
		api
			.probeAlert(url.trim())
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
		api
			.testAlert({ api_url: state.apiUrl.trim(), targets: [state.draft.url] })
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
			stored = (await api.getAlertTargets()).targets;
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
						? t("settings.alerts.destinationsError")
						: t("errors.generic"),
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
			await api.putSettings({
				alert_enabled: true,
				alert_apprise_api_url: state.apiUrl.trim(),
				alert_apprise_targets: merged.join("; "),
				alert_events: [...state.events].join(","),
			});
		} catch (err) {
			dispatch({
				type: "finishFailed",
				// A 400 carries a safe, actionable sentence; anything else could
				// leak internals, so it is reported generically.
				message:
					err instanceof ApiError && err.status === 400
						? err.message
						: t("errors.generic"),
			});
			return;
		}

		try {
			dispatch({ type: "finished", status: await api.getAlertStatus() });
		} catch {
			// The settings landed; a failed probe read only costs the pill.
			dispatch({ type: "finished", status: null });
		}
	};

	// The stored configuration is now the live one, so this test carries no body:
	// it exercises exactly what was written, to every destination at once.
	const sendAll = () => {
		dispatch({ type: "sendingAll" });
		api
			.testAlert()
			.then(() => dispatch({ type: "sentAll", ok: true }))
			.catch(() => dispatch({ type: "sentAll", ok: false }));
	};

	const testRow = (url: string) =>
		api.testAlert({ api_url: state.apiUrl.trim(), targets: [url] });

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
				return <StepEvents {...stepProps} catalog={catalog} />;
			default:
				return (
					<StepFinish
						{...stepProps}
						targets={finalTargets}
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
			subtitle={t(`${K}.stepOf`, { step: state.step, total: TOTAL_STEPS })}
			// Once the write has landed there is nothing left to cancel, and the
			// parent's copy of the settings is stale: every way out of the dialog
			// tells it to reload.
			onClose={state.done ? onFinished : onClose}
			// Escape is Cancel by another name, so it is allowed wherever Cancel is:
			// a probe or a test in flight changes nothing that is stored, and only
			// the write itself is worth waiting for.
			dismissible={!state.finishing}
			closeOnBackdrop={false}
			actions={
				state.done ? (
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
							className="ui-btn"
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
								className="ui-btn"
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
				)
			}
		>
			{/* The step change is announced by the step title alone (StepTitle in
			    steps.tsx); the body is not a live region, or every keystroke in a
			    destination field would be read back. */}
			<div className="fd-stack" data-testid={`wiz-step-${state.step}`}>
				{body()}
			</div>
		</Modal>
	);
}
