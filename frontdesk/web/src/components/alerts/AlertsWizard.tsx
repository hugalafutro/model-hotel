import { type Dispatch, useCallback, useEffect, useReducer } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../../api/client";
import type { AlertEventDef, AlertStatus } from "../../api/types";
import { Modal } from "../Modal";
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

// The address apprise-api answers on when it runs as `apprise` in the Front Desk
// compose stack, which is how the documented setup names it.
export const DEFAULT_APPRISE_URL = "http://apprise:8000";

const K = "settings.alerts.wizard";

export interface AlertsWizardProps {
	/** Saved Apprise API URL ("" when none is configured yet). */
	initialApiUrl: string;
	/** Plaintext saved destinations. The wizard appends to these, never drops one. */
	savedTargets: string[];
	/** Saved alert_events CSV; "" means "use the recommended preset". */
	savedEvents: string;
	catalog: AlertEventDef[];
	/** 1 = Set up alerts / Re-run setup, 2 = Add destination to a working setup. */
	startAt: 1 | 2;
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
	/** How many destinations are already stored; they survive the wizard. */
	savedCount: number;
	events: Set<string>;
	testing: boolean;
	/** Reason code of the last failed test; "" when there is no failure to show. */
	testError: string;
	testOk: boolean;
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
	| { type: "acceptDraft" };

// canNext answers "may this step advance", from state alone. Every gate is a
// verified fact: a healthy probe of the URL currently in the field, a chosen
// kind, a URL that composes, a test that was delivered. Editing anything a gate
// depends on clears the fact, so the gate closes again by construction.
// eslint-disable-next-line react-refresh/only-export-components
export function canNext(s: WizardState): boolean {
	switch (s.step) {
		case 1:
			return s.apiStatus?.healthy === true && s.apiUrl === s.probedUrl;
		case 2:
			return s.draft.kind !== null;
		case 3:
			return (
				s.draft.kind !== null && compose(s.draft.kind, s.draft.fields) !== ""
			);
		case 4:
			return s.draft.tested;
		case 5:
			return s.savedCount + s.added.length > 0;
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
	return { kind, fields, url: compose(kind, fields), tested: false };
}

// eslint-disable-next-line react-refresh/only-export-components
export function initialState(p: AlertsWizardProps): WizardState {
	// "Add destination" only makes sense against a configured apprise-api; without
	// one the run starts at step 1 whatever the caller asked for.
	const start: Step = p.startAt === 2 && p.initialApiUrl !== "" ? 2 : 1;
	const events =
		p.savedEvents.trim() === ""
			? new Set(p.catalog.filter((e) => e.defaultOn).map((e) => e.type))
			: new Set(
					p.savedEvents
						.split(",")
						.map((s) => s.trim())
						.filter(Boolean),
				);
	return {
		step: start,
		minStep: start,
		apiUrl: p.initialApiUrl || DEFAULT_APPRISE_URL,
		probedUrl: "",
		apiStatus: null,
		apiChecking: false,
		draft: { kind: null, fields: {}, url: "", tested: false },
		added: [],
		savedCount: p.savedTargets.length,
		events,
		testing: false,
		testError: "",
		testOk: false,
	};
}

// eslint-disable-next-line react-refresh/only-export-components
export function reducer(s: WizardState, a: Action): WizardState {
	switch (a.type) {
		case "setApiUrl":
			return { ...s, apiUrl: a.value };
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
		case "acceptDraft":
			return {
				...s,
				step: 5,
				added: s.added.includes(s.draft.url)
					? s.added
					: [...s.added, s.draft.url],
			};
	}
}

export function AlertsWizard(props: AlertsWizardProps) {
	const { savedTargets, startAt, initialApiUrl, onClose } = props;
	const { t } = useTranslation();
	const [state, dispatch] = useReducer(reducer, props, initialState);

	const runProbe = useCallback((url: string, demote: boolean) => {
		dispatch({ type: "checking" });
		api
			.probeAlert(url)
			.then((status) => dispatch({ type: "probed", url, status, demote }))
			.catch((err) =>
				dispatch({
					type: "probed",
					url,
					// A transport failure is indistinguishable from an unreachable
					// apprise as far as the gate is concerned: both mean "not proven".
					status: {
						configured: true,
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
			.testAlert({ api_url: state.apiUrl, targets: [state.draft.url] })
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
					<StepKind
						{...stepProps}
						ntfyServer={ntfyServerOf([...savedTargets, ...state.added])}
					/>
				);
			case 3:
				return <StepDetails {...stepProps} />;
			case 4:
				return <StepTest {...stepProps} onSendTest={sendTest} />;
			default:
				// Steps 5 to 7 (destinations, events, finish) are not built yet.
				return null;
		}
	};

	const busy = state.apiChecking || state.testing;
	return (
		<Modal
			title={t(`${K}.title`)}
			subtitle={t(`${K}.stepOf`, { step: state.step, total: TOTAL_STEPS })}
			onClose={onClose}
			dismissible={!busy}
			actions={
				<>
					<button
						type="button"
						className="ui-btn"
						data-testid="wiz-cancel"
						onClick={onClose}
					>
						{t(`${K}.cancel`)}
					</button>
					{state.step > state.minStep && (
						<button
							type="button"
							className="ui-btn"
							data-testid="wiz-back"
							onClick={() =>
								dispatch({ type: "go", step: (state.step - 1) as Step })
							}
						>
							{t(`${K}.back`)}
						</button>
					)}
					{state.step < TOTAL_STEPS && (
						<button
							type="button"
							className="ui-btn ui-btn-primary"
							data-testid="wiz-next"
							disabled={busy || !canNext(state)}
							onClick={goNext}
						>
							{t(`${K}.next`)}
						</button>
					)}
				</>
			}
		>
			<div className="fd-stack" data-testid={`wiz-step-${state.step}`}>
				{body()}
			</div>
		</Modal>
	);
}
