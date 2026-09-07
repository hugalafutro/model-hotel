// The dashboard's seam onto the shared alerts wizard machine. The machine
// itself (step count, draft shape, action union, reducer and per-step gate)
// lives in web-shared so this dashboard and Front Desk run the same one; what
// stays here is the seeding, which reads this app's wizard props.

import { parseCsv } from "@web-shared/alerts/events";
import {
	DEFAULT_APPRISE_URL,
	EMPTY_DRAFT,
	type Step,
	type WizardState,
} from "@web-shared/alerts/wizardState";
// Type-only, so the pair does not form a runtime cycle: the component owns its
// props, and initialState seeds the machine from them.
import type { AlertsWizardProps } from "./AlertsWizard";

export * from "@web-shared/alerts/wizardState";

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
