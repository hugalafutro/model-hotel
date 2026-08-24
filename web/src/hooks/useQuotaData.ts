import type { QuotaProviderType } from "@quota-shared";
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
} from "@quota-shared";
import { useQuery, useQueryClient } from "@tanstack/react-query";
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
export type { QuotaProviderType } from "@quota-shared";
export {
	getKimiCodeFiveHourLimit,
	getKimiCodeWeeklyLimit,
	getMiniMaxFiveHourLimit,
	getMiniMaxGeneralEntry,
	getMiniMaxWeeklyLimit,
	getZaiCodingFiveHourLimit,
	getZaiCodingWeeklyLimit,
} from "@quota-shared";

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

// ── Hook ─────────────────────────────────────────────────────────────────

export function useQuotaData(
	providers: Provider[] | undefined,
	options: UseQuotaDataOptions = {},
): QuotaDataResult {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const { refetchInterval, collapsed, toastErrors } = options;

	// ── Provider detection ──
	const nanogptProviderId = useMemo(
		() => findProviderId(providers, "nanogpt"),
		[providers],
	);
	const zaiCodingProviderId = useMemo(
		() => findProviderId(providers, "zai-coding"),
		[providers],
	);
	const kimiCodeProviderId = useMemo(
		() => findProviderId(providers, "kimi-code"),
		[providers],
	);
	const minimaxProviderId = useMemo(
		() => findProviderId(providers, "minimax"),
		[providers],
	);
	const deepseekProviderId = useMemo(
		() => findProviderId(providers, "deepseek"),
		[providers],
	);
	const openrouterProviderId = useMemo(
		() => findProviderId(providers, "openrouter"),
		[providers],
	);
	const ollamaCloudProviderId = useMemo(
		() => findProviderId(providers, "ollama-cloud"),
		[providers],
	);
	const neuralwattProviderId = useMemo(
		() => findProviderId(providers, "neuralwatt"),
		[providers],
	);

	// Derive effective refetch interval: disabled when collapsed or explicit false
	const effectiveRefetchInterval =
		collapsed === true
			? false
			: refetchInterval === false
				? false
				: refetchInterval;

	// ── NanoGPT query ──
	const {
		data: nanogptUsage,
		dataUpdatedAt: nanogptDataUpdatedAt,
		isRefetching: isNanoRefetching,
		isError: isNanoGPTError,
		refetch: refetchNanoRaw,
	} = useQuery({
		queryKey: ["nanogpt-usage", nanogptProviderId],
		queryFn: () =>
			api.providers.getUsage(
				nanogptProviderId as string,
			) as Promise<NanoGPTUsage>,
		enabled: Boolean(nanogptProviderId),
		refetchInterval: effectiveRefetchInterval,
		// Reflect the server's stored snapshot on every mount (reload after a
		// rebuild shows correct quotas within ~1s), while initialData still paints
		// the cached value instantly.
		staleTime: 0,
		refetchOnMount: "always",
		initialData: () => getCachedData<NanoGPTUsage>("nanogpt-usage"),
	});

	// Cache writes
	useEffect(() => {
		if (nanogptUsage) setCachedData("nanogpt-usage", nanogptUsage);
	}, [nanogptUsage]);

	// ── Z.ai Coding query ──
	const {
		data: zaiCodingUsage,
		dataUpdatedAt: zaiCodingDataUpdatedAt,
		isRefetching: isZaiCodingRefetching,
		isError: isZAICodingError,
		refetch: refetchZaiRaw,
	} = useQuery({
		queryKey: ["zai-coding-usage", zaiCodingProviderId],
		queryFn: () =>
			api.providers.getUsage(
				zaiCodingProviderId as string,
			) as Promise<ZAICodingQuotaResponse>,
		enabled: Boolean(zaiCodingProviderId),
		refetchInterval: effectiveRefetchInterval,
		staleTime: 0,
		refetchOnMount: "always",
		initialData: () =>
			getCachedData<ZAICodingQuotaResponse>("zai-coding-usage"),
	});

	useEffect(() => {
		if (zaiCodingUsage) setCachedData("zai-coding-usage", zaiCodingUsage);
	}, [zaiCodingUsage]);

	// ── Kimi Code query ──
	const {
		data: kimiCodeUsage,
		dataUpdatedAt: kimiCodeDataUpdatedAt,
		isRefetching: isKimiCodeRefetching,
		isError: isKimiCodeError,
		refetch: refetchKimiRaw,
	} = useQuery({
		queryKey: ["kimi-code-usage", kimiCodeProviderId],
		queryFn: () =>
			api.providers.getUsage(
				kimiCodeProviderId as string,
			) as Promise<KimiCodeQuotaResponse>,
		enabled: Boolean(kimiCodeProviderId),
		refetchInterval: effectiveRefetchInterval,
		staleTime: 0,
		refetchOnMount: "always",
		initialData: () => getCachedData<KimiCodeQuotaResponse>("kimi-code-usage"),
	});

	useEffect(() => {
		if (kimiCodeUsage) setCachedData("kimi-code-usage", kimiCodeUsage);
	}, [kimiCodeUsage]);

	// ── MiniMax query ──
	const {
		data: minimaxUsage,
		dataUpdatedAt: minimaxDataUpdatedAt,
		isRefetching: isMiniMaxRefetching,
		isError: isMiniMaxError,
		refetch: refetchMiniMaxRaw,
	} = useQuery({
		queryKey: ["minimax-usage", minimaxProviderId],
		queryFn: () =>
			api.providers.getUsage(
				minimaxProviderId as string,
			) as Promise<MiniMaxQuotaResponse>,
		enabled: Boolean(minimaxProviderId),
		refetchInterval: effectiveRefetchInterval,
		staleTime: 0,
		refetchOnMount: "always",
		initialData: () => getCachedData<MiniMaxQuotaResponse>("minimax-usage"),
	});

	useEffect(() => {
		if (minimaxUsage) setCachedData("minimax-usage", minimaxUsage);
	}, [minimaxUsage]);

	// ── DeepSeek query ──
	const {
		data: deepseekBalance,
		dataUpdatedAt: deepseekDataUpdatedAt,
		isRefetching: isDsRefetching,
		isError: isDeepseekError,
		refetch: refetchDsRaw,
	} = useQuery({
		queryKey: ["deepseek-balance", deepseekProviderId],
		queryFn: () => api.providers.getBalance(deepseekProviderId as string),
		enabled: Boolean(deepseekProviderId),
		refetchInterval: effectiveRefetchInterval,
		staleTime: 0,
		refetchOnMount: "always",
		initialData: () => getCachedData<DeepSeekBalance>("deepseek-balance"),
	});

	useEffect(() => {
		if (deepseekBalance) setCachedData("deepseek-balance", deepseekBalance);
	}, [deepseekBalance]);

	// ── OpenRouter query ──
	const {
		data: openrouterBalance,
		dataUpdatedAt: openrouterDataUpdatedAt,
		isRefetching: isOrRefetching,
		isError: isOpenRouterError,
		refetch: refetchOrRaw,
	} = useQuery<OpenRouterBalance>({
		queryKey: ["openrouter-balance", openrouterProviderId],
		queryFn: () =>
			api.providers.getOpenRouterBalance(openrouterProviderId as string),
		enabled: Boolean(openrouterProviderId),
		refetchInterval: effectiveRefetchInterval,
		staleTime: 0,
		refetchOnMount: "always",
		initialData: () => getCachedData<OpenRouterBalance>("openrouter-balance"),
	});

	useEffect(() => {
		if (openrouterBalance !== undefined)
			setCachedData("openrouter-balance", openrouterBalance);
	}, [openrouterBalance]);

	// ── Ollama Cloud query ──
	const {
		data: ollamaCloudAccount,
		dataUpdatedAt: ollamaCloudDataUpdatedAt,
		isRefetching: isOllamaCloudRefetching,
		isError: isOllamaCloudError,
		refetch: refetchOcRaw,
	} = useQuery<OllamaCloudAccount>({
		queryKey: ["ollama-cloud-account", ollamaCloudProviderId],
		queryFn: () =>
			api.providers.getOllamaCloudAccount(ollamaCloudProviderId as string),
		enabled: Boolean(ollamaCloudProviderId),
		refetchInterval: effectiveRefetchInterval,
		staleTime: 0,
		refetchOnMount: "always",
		initialData: () =>
			getCachedData<OllamaCloudAccount>("ollama-cloud-account"),
	});

	useEffect(() => {
		if (ollamaCloudAccount)
			setCachedData("ollama-cloud-account", ollamaCloudAccount);
	}, [ollamaCloudAccount]);

	// ── NeuralWatt query ──
	const {
		data: neuralwattQuota,
		dataUpdatedAt: neuralwattDataUpdatedAt,
		isRefetching: isNeuralwattRefetching,
		isError: isNeuralwattError,
		refetch: refetchNwRaw,
	} = useQuery<NeuralWattQuotaResponse | null>({
		queryKey: ["neuralwatt-quota", neuralwattProviderId],
		queryFn: () =>
			api.providers.getNeuralWattQuota(neuralwattProviderId as string),
		enabled: Boolean(neuralwattProviderId),
		refetchInterval: effectiveRefetchInterval,
		staleTime: 0,
		refetchOnMount: "always",
		initialData: () =>
			getCachedData<NeuralWattQuotaResponse>("neuralwatt-quota"),
	});

	useEffect(() => {
		if (neuralwattQuota) setCachedData("neuralwatt-quota", neuralwattQuota);
	}, [neuralwattQuota]);

	// ── Error toasting ──
	const nanoErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isNanoGPTError && !nanoErrorToasted.current) {
			toastErrors(t("hooks.useQuotaData.nanoGPTError"), "warning");
			nanoErrorToasted.current = true;
		}
		if (!isNanoGPTError) nanoErrorToasted.current = false;
	}, [isNanoGPTError, toastErrors, t]);

	const zaiErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isZAICodingError && !zaiErrorToasted.current) {
			toastErrors(t("hooks.useQuotaData.zaiError"), "warning");
			zaiErrorToasted.current = true;
		}
		if (!isZAICodingError) zaiErrorToasted.current = false;
	}, [isZAICodingError, toastErrors, t]);

	const kimiErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isKimiCodeError && !kimiErrorToasted.current) {
			toastErrors(t("hooks.useQuotaData.kimiError"), "warning");
			kimiErrorToasted.current = true;
		}
		if (!isKimiCodeError) kimiErrorToasted.current = false;
	}, [isKimiCodeError, toastErrors, t]);

	const minimaxErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isMiniMaxError && !minimaxErrorToasted.current) {
			toastErrors(t("hooks.useQuotaData.miniMaxError"), "warning");
			minimaxErrorToasted.current = true;
		}
		if (!isMiniMaxError) minimaxErrorToasted.current = false;
	}, [isMiniMaxError, toastErrors, t]);

	const dsErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isDeepseekError && !dsErrorToasted.current) {
			toastErrors(t("hooks.useQuotaData.deepSeekError"), "warning");
			dsErrorToasted.current = true;
		}
		if (!isDeepseekError) dsErrorToasted.current = false;
	}, [isDeepseekError, toastErrors, t]);

	const orErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isOpenRouterError && !orErrorToasted.current) {
			toastErrors(t("hooks.useQuotaData.openRouterError"), "warning");
			orErrorToasted.current = true;
		}
		if (!isOpenRouterError) orErrorToasted.current = false;
	}, [isOpenRouterError, toastErrors, t]);

	const ocErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isOllamaCloudError && !ocErrorToasted.current) {
			toastErrors(t("hooks.useQuotaData.ollamaCloudError"), "warning");
			ocErrorToasted.current = true;
		}
		if (!isOllamaCloudError) ocErrorToasted.current = false;
	}, [isOllamaCloudError, toastErrors, t]);

	const nwErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isNeuralwattError && !nwErrorToasted.current) {
			toastErrors(t("hooks.useQuotaData.neuralwattError"), "warning");
			nwErrorToasted.current = true;
		}
		if (!isNeuralwattError) nwErrorToasted.current = false;
	}, [isNeuralwattError, toastErrors, t]);

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

	// ── Refetch helpers ──
	const refetchNano = useCallback(async () => {
		await refetchNanoRaw();
	}, [refetchNanoRaw]);

	const refetchZaiCoding = useCallback(async () => {
		await refetchZaiRaw();
	}, [refetchZaiRaw]);

	const refetchKimiCode = useCallback(async () => {
		await refetchKimiRaw();
	}, [refetchKimiRaw]);

	const refetchMiniMax = useCallback(async () => {
		await refetchMiniMaxRaw();
	}, [refetchMiniMaxRaw]);

	const refetchDeepseek = useCallback(async () => {
		await refetchDsRaw();
	}, [refetchDsRaw]);

	const refetchOpenRouter = useCallback(async () => {
		await refetchOrRaw();
	}, [refetchOrRaw]);

	const refetchOllamaCloud = useCallback(async () => {
		await refetchOcRaw();
	}, [refetchOcRaw]);

	const refetchNeuralwatt = useCallback(async () => {
		await refetchNwRaw();
	}, [refetchNwRaw]);

	const invalidateAll = useCallback(() => {
		queryClient.invalidateQueries({ queryKey: ["nanogpt-usage"] });
		queryClient.invalidateQueries({ queryKey: ["zai-coding-usage"] });
		queryClient.invalidateQueries({ queryKey: ["kimi-code-usage"] });
		queryClient.invalidateQueries({ queryKey: ["minimax-usage"] });
		queryClient.invalidateQueries({ queryKey: ["deepseek-balance"] });
		queryClient.invalidateQueries({ queryKey: ["openrouter-balance"] });
		queryClient.invalidateQueries({ queryKey: ["ollama-cloud-account"] });
		queryClient.invalidateQueries({ queryKey: ["neuralwatt-quota"] });
	}, [queryClient]);

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
		refetchNano,
		refetchZaiCoding,
		refetchKimiCode,
		refetchMiniMax,
		refetchDeepseek,
		refetchOpenRouter,
		refetchOllamaCloud,
		refetchNeuralwatt,
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
