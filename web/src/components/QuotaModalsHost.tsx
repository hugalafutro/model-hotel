import { useQuotaModal } from "../context/QuotaModalContext";
import { useToast } from "../context/ToastContext";
import { useProviders } from "../hooks/useModels";
import { useQuotaData } from "../hooks/useQuotaData";
import {
	KimiCodeQuotaModal,
	MiniMaxQuotaModal,
	NanoGPTQuotaModal,
	NeuralWattQuotaModal,
	OpenRouterQuotaModal,
	ZAICodingQuotaModal,
} from "./ProviderModals";

/**
 * The one mount point for the provider quota modals.
 *
 * Which modal is showing is context state that both the sidebar quota panel and
 * the Providers page set, and both used to render the modals themselves, so an
 * open modal on /providers mounted twice: two portals, two Escape handlers, two
 * refresh buttons. Rendering them here, once, from Layout keeps a modal
 * reachable whether or not the sidebar panel is on screen.
 *
 * The quota queries are the same keys the badges read, so this shares their
 * cached data rather than fetching a second time.
 */
export function QuotaModalsHost() {
	const { open, setOpen } = useQuotaModal();
	const { toast } = useToast();

	const { data: providers } = useProviders();
	// Read-only: the panel and the Providers page own the polling interval, this
	// host only renders whatever their queries have already put in the cache.
	const q = useQuotaData(providers, { refetchInterval: false });

	const onClose = () => setOpen(null);

	switch (open) {
		case "nanogpt":
			return (
				q.nanogptUsage && (
					<NanoGPTQuotaModal
						usage={q.nanogptUsage}
						onClose={onClose}
						onRefresh={q.refetchNano}
						isRefreshing={q.isNanoRefetching}
						onToast={toast}
						lastRefreshed={q.nanogptDataUpdatedAt}
					/>
				)
			);
		case "zai-coding":
			return (
				q.zaiCodingUsage && (
					<ZAICodingQuotaModal
						usage={q.zaiCodingUsage}
						onClose={onClose}
						onRefresh={q.refetchZaiCoding}
						isRefreshing={q.isZaiCodingRefetching}
						onToast={toast}
						lastRefreshed={q.zaiCodingDataUpdatedAt}
					/>
				)
			);
		case "kimi-code":
			return (
				q.kimiCodeUsage && (
					<KimiCodeQuotaModal
						usage={q.kimiCodeUsage}
						onClose={onClose}
						onRefresh={q.refetchKimiCode}
						isRefreshing={q.isKimiCodeRefetching}
						onToast={toast}
						lastRefreshed={q.kimiCodeDataUpdatedAt}
					/>
				)
			);
		case "minimax":
			return (
				q.minimaxUsage && (
					<MiniMaxQuotaModal
						usage={q.minimaxUsage}
						onClose={onClose}
						onRefresh={q.refetchMiniMax}
						isRefreshing={q.isMiniMaxRefetching}
						onToast={toast}
						lastRefreshed={q.minimaxDataUpdatedAt}
					/>
				)
			);
		case "openrouter":
			return (
				q.openrouterBalance && (
					<OpenRouterQuotaModal
						balance={q.openrouterBalance}
						onClose={onClose}
						onRefresh={q.refetchOpenRouter}
						isRefreshing={q.isOrRefetching}
						onToast={toast}
						lastRefreshed={q.openrouterDataUpdatedAt}
					/>
				)
			);
		case "neuralwatt":
			return (
				q.neuralwattQuota && (
					<NeuralWattQuotaModal
						quota={q.neuralwattQuota}
						onClose={onClose}
						onRefresh={q.refetchNeuralwatt}
						isRefreshing={q.isNeuralwattRefetching}
						onToast={toast}
						lastRefreshed={q.neuralwattDataUpdatedAt}
					/>
				)
			);
		default:
			// deepseek and ollama-cloud have no modal: their badges refresh in place.
			return null;
	}
}
