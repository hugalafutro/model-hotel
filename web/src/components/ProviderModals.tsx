import * as Body from "@web-shared/quota/modals";
import type { ComponentType } from "react";
import { useTranslation } from "react-i18next";
import { formatTimestamp, formatTimeUntil } from "../utils/format";
import { Modal } from "./Modal";
import {
	LastRefreshedRow,
	type OnToast,
	QuotaModalHeader,
	useQuotaBarMode,
	useQuotaRefreshToast,
} from "./modals/shared";

// The dashboard's bindings of the shared quota modal bodies (web-shared/quota/
// modals): each body gets this app's Modal, header actions and last-refreshed
// row, its `components.providerModals` strings and its date formatting.

/** Formats an ISO instant as epoch ms, or "-" when it is missing or unparseable. */
function fromIso(iso: string | undefined, format: (ms: number) => string) {
	const ms = iso ? Date.parse(iso) : Number.NaN;
	return Number.isFinite(ms) ? format(ms) : "-";
}

const TIME: Body.TimeFormat = {
	absolute: (iso) => fromIso(iso, formatTimestamp),
	relative: (iso) => fromIso(iso, formatTimeUntil),
};

export interface QuotaModalProps<P> {
	payload: P;
	onClose: () => void;
	onRefresh: () => Promise<unknown>;
	isRefreshing: boolean;
	onToast: OnToast;
	/** When the payload was fetched (the query's dataUpdatedAt), epoch ms. */
	lastRefreshed?: number;
	/** When the server's stored snapshot was fetched upstream (RFC3339). */
	fetchedAt?: string | null;
}

function bind<P>(QuotaBody: ComponentType<Body.QuotaBodyProps<P>>) {
	return function WebQuotaModal({
		payload,
		onClose,
		onRefresh,
		isRefreshing,
		onToast,
		lastRefreshed,
		fetchedAt,
	}: QuotaModalProps<P>) {
		const { t } = useTranslation(undefined, {
			keyPrefix: "components.providerModals",
		});
		const [barMode, toggleBarMode] = useQuotaBarMode();
		const handleRefresh = useQuotaRefreshToast(onRefresh, onToast);
		return (
			<QuotaBody
				payload={payload}
				barMode={barMode}
				// The dashboard's titles name the provider family, not the account.
				providerName=""
				// Durations in a payload (MiniMax's remains_time) were measured when
				// the server's snapshot was fetched upstream, so they anchor there
				// rather than at this query's or the render's time. The query time
				// stands in only when the server did not say.
				fetchedAt={
					fetchedAt ?? new Date(lastRefreshed ?? Date.now()).toISOString()
				}
				kit={{
					t,
					time: TIME,
					shell: ({ title, subtitle, children }) => (
						<Modal
							header={
								<QuotaModalHeader
									title={title}
									subtitle={subtitle}
									barMode={barMode}
									onToggleBarMode={toggleBarMode}
									onRefresh={handleRefresh}
									isRefreshing={isRefreshing}
								/>
							}
							onClose={onClose}
							scrollable
						>
							<div className="space-y-6">
								{children}
								<LastRefreshedRow at={lastRefreshed} />
							</div>
						</Modal>
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
