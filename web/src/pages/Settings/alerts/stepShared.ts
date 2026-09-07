import type { TFunction } from "i18next";
import type { Dispatch } from "react";
import type { Action, WizardState } from "./wizardState";

export const K = "settings.alerts.wizard";

export interface StepProps {
	state: WizardState;
	dispatch: Dispatch<Action>;
	t: TFunction;
}
