// The alerts wizard's state machine: the step count, the draft shape, the
// action union, the reducer and the per-step gate. Kept apart from the
// component so a step can be reasoned about (and tested) without rendering,
// and so the step bodies can import the types without importing the wizard.

import {
	compose,
	type DestinationFields,
	type DestinationKind,
	FIELDS,
} from "@web-shared/alerts/composers";
import { parseCsv } from "@web-shared/alerts/events";
import type { AlertStatus } from "../../../api/types";
// Type-only, so the pair does not form a runtime cycle: the component owns its
// props, and initialState seeds the machine from them.
import type { AlertsWizardProps } from "./AlertsWizard";

export type Step = 1 | 2 | 3 | 4 | 5 | 6 | 7;
export const TOTAL_STEPS = 7;
// The address apprise-api answers on when it runs as `apprise` in the Model
// Hotel compose stack, which is how the documented setup names it.
export const DEFAULT_APPRISE_URL = "http://apprise:8000";
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
	/**
	 * True once the run has reached the destination list. It is what makes
	 * "Back to destinations" a real escape from step 2: the list can be empty
	 * again (every row this run added was removed) and still be the place the
	 * operator came from and wants back.
	 */
	listSeen: boolean;
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
export const EMPTY_DRAFT: Draft = {
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
export function newDraft(kind: DestinationKind, ntfyServer: string): Draft {
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
export function initialState(p: AlertsWizardProps): WizardState {
	// "Add destination" only makes sense against a configured apprise-api; without
	// one the run starts at step 1 whatever the caller asked for.
	const start: Step = p.startAt === 2 && p.initialApiUrl !== "" ? 2 : 1;
	// A missing alert_events row is "nothing has been decided yet": Model Hotel
	// runs on the recommended defaults until the setting is written, so the wizard
	// shows the same set it is already behaving as. A stored blank is the
	// opposite, and the only value that means it: the operator turned every event
	// off, and re-ticking the preset behind their back would silently undo that.
	const events =
		p.savedEvents === null
			? new Set(p.catalog.filter((e) => e.defaultOn).map((e) => e.type))
			: parseCsv(p.savedEvents);
	return {
		step: start,
		minStep: start,
		apiUrl: p.initialApiUrl || DEFAULT_APPRISE_URL,
		probedUrl: "",
		apiStatus: null,
		apiChecking: false,
		draft: EMPTY_DRAFT,
		added: [],
		listSeen: false,
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
export function reducer(s: WizardState, a: Action): WizardState {
	switch (a.type) {
		case "setApiUrl":
			// Editing the field costs this run nothing on its own. The step 1 gate
			// closes the moment the text stops matching what was probed, so an
			// unverified address can never be carried forward, and an address typed
			// back (a stray keystroke, undone) leaves the run exactly as it was.
			// The proofs this run holds are only really invalidated when a
			// different apprise is verified, which is where they are dropped.
			return { ...s, apiUrl: a.value };
		case "checking":
			return { ...s, apiChecking: true };
		case "probed": {
			// A failed re-probe of a saved URL means the shortcut into step 2 was
			// wrong: apprise cannot deliver, so the run drops back to step 1 where
			// the address can be fixed.
			const fellBack = a.demote && !a.status.healthy;
			// A different apprise than the one this run last verified. Every proof
			// the run holds describes that old address: a successful test proves
			// one destination through one apprise, so step 4 re-locks and the
			// destinations accepted here go with it, rather than reaching Finish as
			// addresses nothing has delivered to. The stored destinations are
			// untouched; they are not this run's to drop. The first probe of a run
			// takes this branch too and has nothing to clear, which is the same
			// statement with an empty list.
			const switched = a.url.trim() !== s.probedUrl.trim();
			return {
				...s,
				apiChecking: false,
				probedUrl: a.url,
				apiStatus: a.status,
				step: fellBack ? 1 : s.step,
				minStep: fellBack ? 1 : s.minStep,
				...(switched
					? {
							added: [],
							draft: { ...s.draft, tested: false, acceptedUrl: null },
							testOk: false,
							testError: "",
						}
					: {}),
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
			return { ...s, step: a.step, listSeen: s.listSeen || a.step === 5 };
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
				listSeen: true,
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
