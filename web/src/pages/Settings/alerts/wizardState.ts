// The dashboard's seam onto the shared alerts wizard machine. The machine
// itself (step count, draft shape, action union, reducer, per-step gate and
// initial state) lives in web-shared so this dashboard and Front Desk run the
// same one; what stays here is this app's rule for seeding the events.

import {
	initialState as seedState,
	type WizardState,
} from "@web-shared/alerts/wizardState";
// Type-only, so the pair does not form a runtime cycle: the component owns its
// props, and initialState seeds the machine from them.
import type { AlertsWizardProps } from "./AlertsWizard";

export * from "@web-shared/alerts/wizardState";

export function initialState(p: AlertsWizardProps): WizardState {
	// A missing alert_events row is "nothing has been decided yet": Model Hotel
	// runs on the recommended defaults until the setting is written, so the wizard
	// shows the same set it is already behaving as. A stored blank is the
	// opposite, and the only value that means it: the operator turned every event
	// off, and re-ticking the preset behind their back would silently undo that.
	return seedState(p, p.savedEvents === null);
}
