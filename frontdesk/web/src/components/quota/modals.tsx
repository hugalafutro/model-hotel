import * as Body from "@web-shared/quota/modals";
import type { ComponentType } from "react";
import { useTranslation } from "react-i18next";
import { formatAbsolute, formatRelative } from "../../utils/time";
import { type QuotaModalProps, QuotaModalShell } from "./shared";

// Front Desk's bindings of the shared quota modal bodies (web-shared/quota/
// modals): each body gets Front Desk's shell, its `quota.modal` strings and its
// date formatting.

const TIME: Body.TimeFormat = {
	absolute: formatAbsolute,
	relative: formatRelative,
};

function bind<P>(QuotaBody: ComponentType<Body.QuotaBodyProps<P>>) {
	return function FdQuotaModal({
		providerName,
		payload,
		barMode,
		fetchedAt,
		...controls
	}: QuotaModalProps<P>) {
		const { t } = useTranslation(undefined, { keyPrefix: "quota.modal" });
		return (
			<QuotaBody
				providerName={providerName}
				payload={payload}
				barMode={barMode}
				fetchedAt={fetchedAt}
				kit={{
					t,
					time: TIME,
					shell: (head) => (
						<QuotaModalShell
							{...head}
							{...controls}
							barMode={barMode}
							fetchedAt={fetchedAt}
						/>
					),
				}}
			/>
		);
	};
}

export const KimiCodeQuotaModal = bind(Body.KimiCodeQuotaModal);
export const MiniMaxQuotaModal = bind(Body.MiniMaxQuotaModal);
export const NanoGPTQuotaModal = bind(Body.NanoGPTQuotaModal);
export const NeuralWattQuotaModal = bind(Body.NeuralWattQuotaModal);
export const OpenCodeGoQuotaModal = bind(Body.OpenCodeGoQuotaModal);
export const OpenRouterQuotaModal = bind(Body.OpenRouterQuotaModal);
export const ZAICodingQuotaModal = bind(Body.ZAICodingQuotaModal);
