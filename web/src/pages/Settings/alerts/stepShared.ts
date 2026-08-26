import type { TFunction } from "i18next";
import type { Dispatch } from "react";
import type { Action, WizardState } from "./wizardState";

export const K = "settings.alerts.wizard";

export interface StepProps {
	state: WizardState;
	dispatch: Dispatch<Action>;
	t: TFunction;
}

// reasonText renders a server reason code. Codes the catalog does not cover
// (and a failure that carried none) fall back to the caller's own wording
// rather than leaking a raw key, or a sentence about the wrong thing, into the
// dialog: a probe that could not be made is not a test that failed to deliver.
export function reasonText(
	code: string,
	t: TFunction,
	fallback: string,
): string {
	return t(`settings.alerts.reason.${code}`, { defaultValue: fallback });
}
