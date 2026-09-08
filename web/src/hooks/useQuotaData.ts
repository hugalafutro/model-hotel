import {
	type QueryClient,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import type { QuotaProviderType } from "@web-shared/quota";
import {
	getKimiCodeFiveHourLimit,
	getKimiCodeWeeklyLimit,
	getMiniMaxFiveHourLimit,
	getMiniMaxWeeklyLimit,
	getZaiCodingFiveHourLimit,
	getZaiCodingWeeklyLimit,
	isDeepSeekQuotaVisible,
	isKimiCodeQuotaVisible,
	isMiniMaxQuotaVisible,
	isNanoGptQuotaVisible,
	isNeuralWattQuotaVisible,
	isOllamaCloudQuotaVisible,
	isOpenRouterQuotaVisible,
	isZaiCodingQuotaVisible,
} from "@web-shared/quota";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import type {
	DeepSeekBalance,
	KimiCodeQuotaResponse,
	KimiCodeQuotaWindow,
	MiniMaxQuotaResponse,
	MiniMaxQuotaWindow,
	NanoGPTUsage,
	NeuralWattQuotaResponse,
	OllamaCloudAccount,
	OpenRouterBalance,
	Provider,
	ZAICodingQuotaLimit,
	ZAICodingQuotaResponse,
} from "../api/types";

// The payload parsing behind these helpers lives in web-shared/quota so Front
// Desk derives identical numbers from identical payloads. Re-exported here
// because app code has always reached them through this hook module.
export type { QuotaProviderType } from "@web-shared/quota";
export {
	getKimiCodeFiveHourLimit,
	getKimiCodeWeeklyLimit,
	getMiniMaxFiveHourLimit,
	getMiniMaxGeneralEntry,
	getMiniMaxWeeklyLimit,
	getZaiCodingFiveHourLimit,
	getZaiCodingWeeklyLimit,
} from "@web-shared/quota";

// ── Cache helpers (shared across consumers) ──────────────────────────────

const CACHE_PREFIX = "model-hotel";

export function getCachedData<T>(key: string): T | undefined {
	try {
		const raw = localStorage.getItem(`${CACHE_PREFIX}:${key}`);
		if (raw) return JSON.parse(raw) as T;
	} catch {
		/* ignore */
	}
	return undefined;
}

export function setCachedData<T>(key: string, data: T) {
	try {
		localStorage.setItem(`${CACHE_PREFIX}:${key}`, JSON.stringify(data));
	} catch {
		/* ignore */
	}
}

// ── Provider type detection ──────────────────────────────────────────────
// The type union is the shared one; sniffing a base URL for it is the
// dashboard's own job, because only it holds provider records. Front Desk reads
// the type the fleet primary stamps on each snapshot instead.

function hostnameMatches(url: string, suffix: string, exact?: string): boolean {
	try {
		const h = new URL(url).hostname;
		return exact ? h === exact || h.endsWith(suffix) : h.endsWith(suffix);
	} catch {
		return false;
	}
}

/** Detect which quota-supporting provider type a base URL belongs to. */
export function detectQuotaProviderType(
	baseUrl: string,
): QuotaProviderType | null {
	if (hostnameMatches(baseUrl, "nano-gpt.com")) return "nanogpt";
	if (hostnameMatches(baseUrl, ".z.ai", "z.ai")) return "zai-coding";
	if (hostnameMatches(baseUrl, ".kimi.com", "kimi.com")) return "kimi-code";
	if (hostnameMatches(baseUrl, ".minimax.io", "minimax.io")) return "minimax";
	if (hostnameMatches(baseUrl, "deepseek.com")) return "deepseek";
	if (hostnameMatches(baseUrl, "openrouter.ai")) return "openrouter";
	if (hostnameMatches(baseUrl, "ollama.com")) return "ollama-cloud";
	if (hostnameMatches(baseUrl, "neuralwatt.com")) return "neuralwatt";
	return null;
}

/** Find the first enabled provider ID matching a quota provider type. Disabled
 * providers are invisible to the quota surface entirely: no provider ID means
 * every downstream query stays disabled and every badge stays hidden. */
function findProviderId(
	providers: Provider[] | undefined,
	type: QuotaProviderType,
): string | undefined {
	return providers?.find(
		(p) => p.enabled && detectQuotaProviderType(p.base_url) === type,
	)?.id;
}

// ── Hook options ─────────────────────────────────────────────────────────

export interface UseQuotaDataOptions {
	/** Optional auto-refresh interval in ms. false = disabled. */
	refetchInterval?: number | false;
	/** Whether the hook is conceptually "collapsed" (disables auto-refresh). */
	collapsed?: boolean;
	/** Toast errors to user (requires a toast fn). If omitted, errors are silent. */
	toastErrors?: (msg: string, severity: "warning") => void;
}

// ── Return type ──────────────────────────────────────────────────────────

export interface QuotaDataResult {
	/** Per-provider IDs (undefined if no such provider exists). */
	nanogptProviderId: string | undefined;
	zaiCodingProviderId: string | undefined;
	kimiCodeProviderId: string | undefined;
	minimaxProviderId: string | undefined;
	deepseekProviderId: string | undefined;
	openrouterProviderId: string | undefined;
	ollamaCloudProviderId: string | undefined;
	neuralwattProviderId: string | undefined;

	/** Raw query data. */
	nanogptUsage: NanoGPTUsage | undefined;
	zaiCodingUsage: ZAICodingQuotaResponse | undefined;
	kimiCodeUsage: KimiCodeQuotaResponse | undefined;
	minimaxUsage: MiniMaxQuotaResponse | undefined;
	deepseekBalance: DeepSeekBalance | undefined;
	openrouterBalance: OpenRouterBalance | undefined;
	ollamaCloudAccount: OllamaCloudAccount | undefined;
	neuralwattQuota: NeuralWattQuotaResponse | null | undefined;

	/** Derived Z.ai limits. */
	zaiCodingFiveHour: ZAICodingQuotaLimit | undefined;
	zaiCodingWeekly: ZAICodingQuotaLimit | undefined;

	/** Derived Kimi Code limits. */
	kimiCodeFiveHour: KimiCodeQuotaWindow | undefined;
	kimiCodeWeekly: KimiCodeQuotaWindow | undefined;

	/** Derived MiniMax limits (from the active "general" model class). */
	minimaxFiveHour: MiniMaxQuotaWindow | undefined;
	minimaxWeekly: MiniMaxQuotaWindow | undefined;

	/** NanoGPT weekly helpers. */
	nanoWeeklyUsed: number | null | undefined;
	nanoWeeklyLimit: number | null | undefined;

	/** Badge visibility booleans (already account for providerId + data). */
	showNanoBadge: boolean;
	showZaiCodingBadge: boolean;
	showKimiCodeBadge: boolean;
	showMiniMaxBadge: boolean;
	showDsBadge: boolean;
	showOrBadge: boolean;
	showOllamaCloudBadge: boolean;
	showNeuralwattBadge: boolean;

	/** Whether any quota-supporting provider exists. */
	hasAnyProvider: boolean;

	/** Individual refetch fns. */
	refetchNano: () => Promise<void>;
	refetchZaiCoding: () => Promise<void>;
	refetchKimiCode: () => Promise<void>;
	refetchMiniMax: () => Promise<void>;
	refetchDeepseek: () => Promise<void>;
	refetchOpenRouter: () => Promise<void>;
	refetchOllamaCloud: () => Promise<void>;
	refetchNeuralwatt: () => Promise<void>;

	/** Individual isRefetching flags. */
	isNanoRefetching: boolean;
	isZaiCodingRefetching: boolean;
	isKimiCodeRefetching: boolean;
	isMiniMaxRefetching: boolean;
	isDsRefetching: boolean;
	isOrRefetching: boolean;
	isOllamaCloudRefetching: boolean;
	isNeuralwattRefetching: boolean;

	/** dataUpdatedAt for modals. */
	openrouterDataUpdatedAt: number;
	nanogptDataUpdatedAt: number;
	zaiCodingDataUpdatedAt: number;
	kimiCodeDataUpdatedAt: number;
	minimaxDataUpdatedAt: number;
	deepseekDataUpdatedAt: number;
	ollamaCloudDataUpdatedAt: number;
	neuralwattDataUpdatedAt: number;

	/** Invalidate all quota query keys. */
	invalidateAll: () => void;
}

// ── Per-provider query ───────────────────────────────────────────────────

/**
 * One provider's quota query: find the enabled provider of that type, read its
 * payload, mirror it into the local cache so a reload paints instantly, and
 * toast once per error transition. Every provider below is this same block with
 * a different payload type and endpoint.
 */
function useProviderQuota<T>(
	providers: Provider[] | undefined,
	type: QuotaProviderType,
	cacheKey: string,
	fetchUsage: (providerId: string) => Promise<T>,
	errorKey: string,
	toastErrors: ((msg: string, severity: "warning") => void) | undefined,
	refetchInterval: number | false | undefined,
) {
	const { t } = useTranslation();
	const providerId = useMemo(
		() => findProviderId(providers, type),
		[providers, type],
	);

	const { data, dataUpdatedAt, isRefetching, isError, refetch } = useQuery<T>({
		queryKey: [cacheKey, providerId],
		queryFn: () => fetchUsage(providerId as string),
		enabled: Boolean(providerId),
		refetchInterval,
		// Reflect the server's stored snapshot on every mount (reload after a
		// rebuild shows correct quotas within ~1s), while initialData still paints
		// the cached value instantly.
		staleTime: 0,
		refetchOnMount: "always",
		initialData: () => getCachedData<T>(cacheKey),
	});

	useEffect(() => {
		if (data != null) setCachedData(cacheKey, data);
	}, [cacheKey, data]);

	// One toast per healthy-to-failing transition, not one per refetch.
	const toasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isError && !toasted.current) {
			toastErrors(t(errorKey), "warning");
			toasted.current = true;
		}
		if (!isError) toasted.current = false;
	}, [isError, toastErrors, t, errorKey]);

	// Narrowed to Promise<void>: every consumer awaits the refresh for its
	// spinner and none reads the query result the raw refetch resolves with.
	const refresh = useCallback(async () => {
		await refetch();
	}, [refetch]);

	return { providerId, data, dataUpdatedAt, isRefetching, refetch: refresh };
}

/** The query keys every quota reader shares, so an invalidation misses none. */
export const QUOTA_QUERY_KEYS = [
	"nanogpt-usage",
	"zai-coding-usage",
	"kimi-code-usage",
	"minimax-usage",
	"deepseek-balance",
	"openrouter-balance",
	"ollama-cloud-account",
	"neuralwatt-quota",
] as const;

/** Marks every quota query stale, e.g. after a provider is added or removed. */
export function invalidateQuotaQueries(queryClient: QueryClient): void {
	for (const key of QUOTA_QUERY_KEYS) {
		queryClient.invalidateQueries({ queryKey: [key] });
	}
}

// ── Hook ─────────────────────────────────────────────────────────────────

export function useQuotaData(
	providers: Provider[] | undefined,
	options: UseQuotaDataOptions = {},
): QuotaDataResult {
	const queryClient = useQueryClient();
	const { refetchInterval, collapsed, toastErrors } = options;

	// Auto-refresh is off while the panel is collapsed: nothing is on screen to
	// keep current.
	const interval = collapsed === true ? false : refetchInterval;

	const nano = useProviderQuota<NanoGPTUsage>(
		providers,
		"nanogpt",
		"nanogpt-usage",
		(id) => api.providers.getUsage(id) as Promise<NanoGPTUsage>,
		"hooks.useQuotaData.nanoGPTError",
		toastErrors,
		interval,
	);
	const zai = useProviderQuota<ZAICodingQuotaResponse>(
		providers,
		"zai-coding",
		"zai-coding-usage",
		(id) => api.providers.getUsage(id) as Promise<ZAICodingQuotaResponse>,
		"hooks.useQuotaData.zaiError",
		toastErrors,
		interval,
	);
	const kimi = useProviderQuota<KimiCodeQuotaResponse>(
		providers,
		"kimi-code",
		"kimi-code-usage",
		(id) => api.providers.getUsage(id) as Promise<KimiCodeQuotaResponse>,
		"hooks.useQuotaData.kimiError",
		toastErrors,
		interval,
	);
	const minimax = useProviderQuota<MiniMaxQuotaResponse>(
		providers,
		"minimax",
		"minimax-usage",
		(id) => api.providers.getUsage(id) as Promise<MiniMaxQuotaResponse>,
		"hooks.useQuotaData.miniMaxError",
		toastErrors,
		interval,
	);
	const deepseek = useProviderQuota<DeepSeekBalance>(
		providers,
		"deepseek",
		"deepseek-balance",
		(id) => api.providers.getBalance(id),
		"hooks.useQuotaData.deepSeekError",
		toastErrors,
		interval,
	);
	const openrouter = useProviderQuota<OpenRouterBalance>(
		providers,
		"openrouter",
		"openrouter-balance",
		(id) => api.providers.getOpenRouterBalance(id),
		"hooks.useQuotaData.openRouterError",
		toastErrors,
		interval,
	);
	const ollamaCloud = useProviderQuota<OllamaCloudAccount>(
		providers,
		"ollama-cloud",
		"ollama-cloud-account",
		(id) => api.providers.getOllamaCloudAccount(id),
		"hooks.useQuotaData.ollamaCloudError",
		toastErrors,
		interval,
	);
	const neuralwatt = useProviderQuota<NeuralWattQuotaResponse | null>(
		providers,
		"neuralwatt",
		"neuralwatt-quota",
		(id) => api.providers.getNeuralWattQuota(id),
		"hooks.useQuotaData.neuralwattError",
		toastErrors,
		interval,
	);

	const {
		providerId: nanogptProviderId,
		data: nanogptUsage,
		dataUpdatedAt: nanogptDataUpdatedAt,
		isRefetching: isNanoRefetching,
	} = nano;
	const {
		providerId: zaiCodingProviderId,
		data: zaiCodingUsage,
		dataUpdatedAt: zaiCodingDataUpdatedAt,
		isRefetching: isZaiCodingRefetching,
	} = zai;
	const {
		providerId: kimiCodeProviderId,
		data: kimiCodeUsage,
		dataUpdatedAt: kimiCodeDataUpdatedAt,
		isRefetching: isKimiCodeRefetching,
	} = kimi;
	const {
		providerId: minimaxProviderId,
		data: minimaxUsage,
		dataUpdatedAt: minimaxDataUpdatedAt,
		isRefetching: isMiniMaxRefetching,
	} = minimax;
	const {
		providerId: deepseekProviderId,
		data: deepseekBalance,
		dataUpdatedAt: deepseekDataUpdatedAt,
		isRefetching: isDsRefetching,
	} = deepseek;
	const {
		providerId: openrouterProviderId,
		data: openrouterBalance,
		dataUpdatedAt: openrouterDataUpdatedAt,
		isRefetching: isOrRefetching,
	} = openrouter;
	const {
		providerId: ollamaCloudProviderId,
		data: ollamaCloudAccount,
		dataUpdatedAt: ollamaCloudDataUpdatedAt,
		isRefetching: isOllamaCloudRefetching,
	} = ollamaCloud;
	const {
		providerId: neuralwattProviderId,
		data: neuralwattQuota,
		dataUpdatedAt: neuralwattDataUpdatedAt,
		isRefetching: isNeuralwattRefetching,
	} = neuralwatt;

	// ── Derived values ──
	const zaiCodingFiveHour = getZaiCodingFiveHourLimit(zaiCodingUsage);
	const zaiCodingWeekly = getZaiCodingWeeklyLimit(zaiCodingUsage);

	const kimiCodeFiveHour = getKimiCodeFiveHourLimit(kimiCodeUsage);
	const kimiCodeWeekly = getKimiCodeWeeklyLimit(kimiCodeUsage);

	const minimaxFiveHour = getMiniMaxFiveHourLimit(minimaxUsage);
	const minimaxWeekly = getMiniMaxWeeklyLimit(minimaxUsage);

	const nanoWeeklyUsed = nanogptUsage?.weeklyInputTokens?.used;
	const nanoWeeklyLimit = nanogptUsage?.limits?.weeklyInputTokens;

	// Badge visibility: the provider has to exist and its payload has to have
	// arrived here, then the shared rule decides whether the payload is worth a
	// badge. Front Desk gates on the same rule.
	const showNanoBadge =
		Boolean(nanogptProviderId) &&
		nanogptUsage != null &&
		isNanoGptQuotaVisible(nanogptUsage);

	const showZaiCodingBadge =
		Boolean(zaiCodingProviderId) &&
		zaiCodingUsage != null &&
		isZaiCodingQuotaVisible(zaiCodingUsage);

	const showKimiCodeBadge =
		Boolean(kimiCodeProviderId) &&
		kimiCodeUsage != null &&
		isKimiCodeQuotaVisible(kimiCodeUsage);

	const showMiniMaxBadge =
		Boolean(minimaxProviderId) &&
		minimaxUsage != null &&
		isMiniMaxQuotaVisible(minimaxUsage);

	const showDsBadge =
		Boolean(deepseekProviderId) &&
		deepseekBalance != null &&
		isDeepSeekQuotaVisible(deepseekBalance);

	const showOrBadge =
		Boolean(openrouterProviderId) &&
		openrouterBalance != null &&
		isOpenRouterQuotaVisible(openrouterBalance);

	const showOllamaCloudBadge =
		Boolean(ollamaCloudProviderId) &&
		ollamaCloudAccount != null &&
		isOllamaCloudQuotaVisible(ollamaCloudAccount);

	const showNeuralwattBadge =
		Boolean(neuralwattProviderId) &&
		neuralwattQuota != null &&
		isNeuralWattQuotaVisible(neuralwattQuota);

	const hasAnyProvider = Boolean(
		nanogptProviderId ||
			zaiCodingProviderId ||
			kimiCodeProviderId ||
			minimaxProviderId ||
			deepseekProviderId ||
			openrouterProviderId ||
			ollamaCloudProviderId ||
			neuralwattProviderId,
	);

	const invalidateAll = useCallback(
		() => invalidateQuotaQueries(queryClient),
		[queryClient],
	);

	return {
		nanogptProviderId,
		zaiCodingProviderId,
		kimiCodeProviderId,
		minimaxProviderId,
		deepseekProviderId,
		openrouterProviderId,
		ollamaCloudProviderId,
		neuralwattProviderId,
		nanogptUsage,
		zaiCodingUsage,
		kimiCodeUsage,
		minimaxUsage,
		deepseekBalance,
		openrouterBalance,
		ollamaCloudAccount,
		neuralwattQuota,
		zaiCodingFiveHour,
		zaiCodingWeekly,
		kimiCodeFiveHour,
		kimiCodeWeekly,
		minimaxFiveHour,
		minimaxWeekly,
		nanoWeeklyUsed,
		nanoWeeklyLimit,
		showNanoBadge,
		showZaiCodingBadge,
		showKimiCodeBadge,
		showMiniMaxBadge,
		showDsBadge,
		showOrBadge,
		showOllamaCloudBadge,
		showNeuralwattBadge,
		hasAnyProvider,
		refetchNano: nano.refetch,
		refetchZaiCoding: zai.refetch,
		refetchKimiCode: kimi.refetch,
		refetchMiniMax: minimax.refetch,
		refetchDeepseek: deepseek.refetch,
		refetchOpenRouter: openrouter.refetch,
		refetchOllamaCloud: ollamaCloud.refetch,
		refetchNeuralwatt: neuralwatt.refetch,
		isNanoRefetching,
		isZaiCodingRefetching,
		isKimiCodeRefetching,
		isMiniMaxRefetching,
		isDsRefetching,
		isOrRefetching,
		isOllamaCloudRefetching,
		isNeuralwattRefetching,
		nanogptDataUpdatedAt,
		zaiCodingDataUpdatedAt,
		kimiCodeDataUpdatedAt,
		minimaxDataUpdatedAt,
		deepseekDataUpdatedAt,
		openrouterDataUpdatedAt,
		ollamaCloudDataUpdatedAt,
		neuralwattDataUpdatedAt,
		invalidateAll,
	};
}
