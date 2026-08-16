import { type Dispatch, useCallback, useEffect, useReducer } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../../../api/client";
import type { AlertEventDef, AlertStatus } from "../../../api/types";
import { Modal } from "../../../components/Modal";
import {
	compose,
	type DestinationFields,
	type DestinationKind,
	FIELDS,
	ntfyServerOf,
} from "./composers";
import { StepApprise, StepDetails, StepKind, StepTest } from "./steps";

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

export type Step = 1 | 2 | 3 | 4 | 5 | 6 | 7;
export const TOTAL_STEPS = 7;

// The address apprise-api answers on when it runs as `apprise` in the Model
// Hotel compose stack, which is how the documented setup names it.
export const DEFAULT_APPRISE_URL = "http://apprise:8000";

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

/** The destination being built on steps 2 to 4, before it joins `added`. */
export interface Draft {
	kind: DestinationKind | null;
	fields: DestinationFields;
	/** compose(kind, fields); "" while the fields are incomplete or invalid. */
	url: string;
	/** True once a test to exactly this URL came back successful. */
	tested: boolean;
	// The URL this draft was last accepted into `added` as, or null when it has
	// not been accepted yet. Re-accepting an edited draft replaces that entry
	// instead of leaving the superseded URL in the list beside it.
	acceptedUrl: string | null;
}

export interface WizardState {
	step: Step;
	/** The first step this run can reach; Back is hidden there. */
	minStep: Step;
	apiUrl: string;
	/** The URL `apiStatus` describes. Editing apiUrl past it re-locks step 1. */
	probedUrl: string;
	apiStatus: AlertStatus | null;
	apiChecking: boolean;
	draft: Draft;
	/** Destinations tested and accepted during this run, in the order added. */
	added: string[];
	/** The destinations already stored; they survive the wizard untouched. */
	saved: string[];
	events: Set<string>;
	testing: boolean;
	/** Reason code of the last failed test; "" when there is no failure to show. */
	testError: string;
	testOk: boolean;
	/** True while the single settings write is in flight. */
	finishing: boolean;
	/** Message from a rejected write; "" when there is nothing to show. */
	finishError: string;
	/** True once the write succeeded: the run is over and only Close remains. */
	done: boolean;
	/** Probe result read straight after the write, for the closing pill. */
	finalStatus: AlertStatus | null;
	sendingAll: boolean;
	/** Outcome of "send test to everything"; "none" until it is used. */
	sentAll: "none" | "ok" | "failed";
}

export type Action =
	| { type: "setApiUrl"; value: string }
	| { type: "checking" }
	| { type: "probed"; url: string; status: AlertStatus; demote: boolean }
	| { type: "setKind"; kind: DestinationKind; ntfyServer: string }
	| { type: "setField"; key: string; value: string }
	| { type: "testing" }
	| { type: "tested" }
	| { type: "testFailed"; code: string }
	| { type: "go"; step: Step }
	| { type: "acceptDraft" }
	| { type: "newDraft" }
	| { type: "dropAdded"; url: string }
	| { type: "savedRefreshed"; targets: string[] }
	| { type: "toggleEvent"; eventType: string; on: boolean }
	| { type: "resetEvents"; types: string[] }
	| { type: "finishing" }
	| { type: "finished"; status: AlertStatus | null }
	| { type: "finishFailed"; message: string }
	| { type: "sendingAll" }
	| { type: "sentAll"; ok: boolean };

/** A draft with nothing chosen yet, which is where steps 2 to 4 start. */
const EMPTY_DRAFT: Draft = {
	kind: null,
	fields: {},
	url: "",
	tested: false,
	acceptedUrl: null,
};

// isDuplicate answers "is the draft a second copy of a destination the run
// already has", counting both the stored list and what this run accepted. A
// draft being edited back into its own accepted row is not a duplicate of
// itself, so re-accepting an edit still works. Step 3 is gated on this: the
// same URL twice is never what the operator meant, and apprise would just be
// told to deliver to one address twice.
// eslint-disable-next-line react-refresh/only-export-components
export function isDuplicate(s: WizardState): boolean {
	const url =
		s.draft.kind === null ? "" : compose(s.draft.kind, s.draft.fields);
	return (
		url !== "" &&
		url !== s.draft.acceptedUrl &&
		(s.saved.includes(url) || s.added.includes(url))
	);
}

// canNext answers "may this step advance", from state alone. Every gate is a
// verified fact: a healthy probe of the URL currently in the field, a chosen
// kind, a URL that composes and is not already on the list, a test that was
// delivered. Editing anything a gate depends on clears the fact, so the gate
// closes again by construction.
// eslint-disable-next-line react-refresh/only-export-components
export function canNext(s: WizardState): boolean {
	switch (s.step) {
		case 1:
			return s.apiStatus?.healthy === true && s.apiUrl === s.probedUrl;
		case 2:
			return s.draft.kind !== null;
		case 3:
			return (
				s.draft.kind !== null &&
				compose(s.draft.kind, s.draft.fields) !== "" &&
				!isDuplicate(s)
			);
		case 4:
			return s.draft.tested;
		case 5:
			return s.saved.length + s.added.length > 0;
		case 6:
			return true;
		default:
			return false;
	}
}

// A fresh draft for `kind`, seeded with the field defaults. The ntfy server is
// carried over from the destinations already in play so a second phone on the
// same server does not have to be told the address again; there is deliberately
// no ntfy.sh default, because guessing the server wrong is worse than asking.
function newDraft(kind: DestinationKind, ntfyServer: string): Draft {
	const fields: DestinationFields = {};
	for (const f of FIELDS[kind]) fields[f.key] = f.defaultValue ?? "";
	if (kind === "ntfy") fields.server = ntfyServer;
	return {
		kind,
		fields,
		url: compose(kind, fields),
		tested: false,
		acceptedUrl: null,
	};
}

/** parseCsv turns a stored alert_events CSV into a membership Set. */
function parseCsv(csv: string): Set<string> {
	return new Set(
		csv
			.split(",")
			.map((s) => s.trim())
			.filter(Boolean),
	);
}

// eslint-disable-next-line react-refresh/only-export-components
export function initialState(p: AlertsWizardProps): WizardState {
	// "Add destination" only makes sense against a configured apprise-api; without
	// one the run starts at step 1 whatever the caller asked for.
	const start: Step = p.startAt === 2 && p.initialApiUrl !== "" ? 2 : 1;
	// A missing alert_events row is "nothing has been decided yet", which a setup
	// run answers with the recommended preset. A stored blank is the opposite: the
	// operator turned every event off, and re-ticking the preset behind their back
	// would silently undo that.
	const events =
		p.startAt === 1 && p.savedEvents === null
			? new Set(p.catalog.filter((e) => e.defaultOn).map((e) => e.type))
			: parseCsv(p.savedEvents ?? "");
	return {
		step: start,
		minStep: start,
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

// eslint-disable-next-line react-refresh/only-export-components
export function reducer(s: WizardState, a: Action): WizardState {
	switch (a.type) {
		case "setApiUrl":
			// A successful test proves one destination through one apprise. Pointing
			// at a different apprise makes that proof worthless, so step 4 re-locks
			// alongside step 1, and every destination accepted in this run goes with
			// it: each was proven through the old address only, and carrying an
			// unproven URL to Finish is exactly what the gates exist to prevent. The
			// stored destinations are untouched; they are not this run's to drop.
			return {
				...s,
				apiUrl: a.value,
				added: [],
				draft: { ...s.draft, tested: false, acceptedUrl: null },
				testOk: false,
				testError: "",
			};
		case "checking":
			return { ...s, apiChecking: true };
		case "probed": {
			// A failed re-probe of a saved URL means the shortcut into step 2 was
			// wrong: apprise cannot deliver, so the run drops back to step 1 where
			// the address can be fixed.
			const fellBack = a.demote && !a.status.healthy;
			return {
				...s,
				apiChecking: false,
				probedUrl: a.url,
				apiStatus: a.status,
				step: fellBack ? 1 : s.step,
				minStep: fellBack ? 1 : s.minStep,
			};
		}
		case "setKind":
			return {
				...s,
				draft: newDraft(a.kind, a.ntfyServer),
				testError: "",
				testOk: false,
			};
		case "setField": {
			if (s.draft.kind === null) return s;
			const fields = { ...s.draft.fields, [a.key]: a.value };
			// Any edit invalidates the test: the successful delivery described the
			// old URL, not this one.
			return {
				...s,
				draft: {
					...s.draft,
					fields,
					url: compose(s.draft.kind, fields),
					tested: false,
				},
				testError: "",
				testOk: false,
			};
		}
		case "testing":
			return { ...s, testing: true, testError: "", testOk: false };
		case "tested":
			return {
				...s,
				testing: false,
				testOk: true,
				testError: "",
				draft: { ...s.draft, tested: true },
			};
		case "testFailed":
			return {
				...s,
				testing: false,
				testOk: false,
				testError: a.code || "generic",
				draft: { ...s.draft, tested: false },
			};
		case "go":
			return { ...s, step: a.step };
		case "acceptDraft": {
			// Editing and re-testing an already accepted destination replaces it; a
			// draft that has never been accepted (Add another) joins the list.
			const previous = s.draft.acceptedUrl;
			const next =
				previous !== null && s.added.includes(previous)
					? s.added.map((u) => (u === previous ? s.draft.url : u))
					: [...s.added, s.draft.url];
			return {
				...s,
				step: 5,
				// Step 3 refuses a duplicate, so this filter is the belt to that
				// braces: whatever route a repeat took to get here, the list the run
				// finishes with holds each destination once.
				added: next.filter(
					(u, i) => next.indexOf(u) === i && !s.saved.includes(u),
				),
				draft: { ...s.draft, acceptedUrl: s.draft.url },
			};
		}
		case "newDraft":
			// "Add another" walks the same three steps again from nothing, so the
			// next acceptance appends instead of replacing what it started from.
			return {
				...s,
				step: 2,
				draft: EMPTY_DRAFT,
				testOk: false,
				testError: "",
			};
		case "dropAdded":
			return {
				...s,
				added: s.added.filter((u) => u !== a.url),
				// The draft no longer has a place in the list, so re-accepting it
				// appends rather than replacing a row that is gone.
				draft:
					s.draft.acceptedUrl === a.url
						? { ...s.draft, acceptedUrl: null }
						: s.draft,
			};
		case "savedRefreshed":
			// The stored half re-read at Finish. Anything this run added that turns
			// out to be stored already (the same destination set up elsewhere while
			// the dialog was open) moves to the stored side, so the list the summary
			// shows carries each destination exactly once.
			return {
				...s,
				saved: a.targets,
				added: s.added.filter((u) => !a.targets.includes(u)),
			};
		case "toggleEvent": {
			const events = new Set(s.events);
			if (a.on) events.add(a.eventType);
			else events.delete(a.eventType);
			return { ...s, events };
		}
		case "resetEvents":
			return { ...s, events: new Set(a.types) };
		case "finishing":
			return { ...s, finishing: true, finishError: "" };
		case "finished":
			return {
				...s,
				finishing: false,
				finishError: "",
				done: true,
				finalStatus: a.status,
			};
		case "finishFailed":
			// Nothing was written, so the run stays exactly where it was: the
			// address or a destination can be fixed and Finish pressed again.
			return { ...s, finishing: false, finishError: a.message };
		case "sendingAll":
			return { ...s, sendingAll: true, sentAll: "none" };
		case "sentAll":
			return { ...s, sendingAll: false, sentAll: a.ok ? "ok" : "failed" };
	}
}

export function AlertsWizard(props: AlertsWizardProps) {
	// savedTargets is seeded into state (and re-read at Finish), so the stored
	// half is read off state from here on rather than off the prop.
	const { startAt, initialApiUrl, managed, onClose, onFinished } = props;
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
						: t("common.unknownError"),
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
							alert_events: [...state.events].join(","),
						}),
			});
		} catch (err) {
			dispatch({
				type: "finishFailed",
				// A 400 carries a safe, actionable sentence; anything else could
				// leak internals, so it is reported generically.
				message:
					err instanceof ApiError && err.status === 400
						? err.message
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

	const stepProps = { state, dispatch: dispatch as Dispatch<Action>, t };
	// Steps 5 to 7 have no body yet; the flow through them is already live.
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
			default:
				return null;
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
			closeOnBackdrop={false}
			maxWidth="max-w-2xl"
			scrollable
		>
			<div className="space-y-4">
				<p className="text-xs text-(--text-muted)" data-testid="wiz-step-of">
					{t(`${K}.stepOf`, { step: state.step, total: TOTAL_STEPS })}
				</p>
				{/* The step change is announced by the step title alone (StepTitle in
				    steps.tsx); the body is not a live region, or every keystroke in a
				    destination field would be read back. */}
				<div className="space-y-3" data-testid={`wiz-step-${state.step}`}>
					{body()}
				</div>
				<div className="flex flex-wrap justify-end gap-2">
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
