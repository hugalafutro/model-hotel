import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { api } from "../api/client";
import type {
	DeepSeekBalance,
	NanoGPTUsage,
	OllamaCloudAccount,
	OpenRouterBalance,
	Provider,
	ZAICodingQuotaLimit,
	ZAICodingQuotaResponse,
} from "../api/types";

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

export type QuotaProviderType =
	| "nanogpt"
	| "zai-coding"
	| "deepseek"
	| "openrouter"
	| "ollama-cloud";

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
	if (hostnameMatches(baseUrl, "deepseek.com")) return "deepseek";
	if (hostnameMatches(baseUrl, "openrouter.ai")) return "openrouter";
	if (hostnameMatches(baseUrl, "ollama.com")) return "ollama-cloud";
	return null;
}

/** Find the first provider ID matching a quota provider type. */
function findProviderId(
	providers: Provider[] | undefined,
	type: QuotaProviderType,
): string | undefined {
	return providers?.find((p) => detectQuotaProviderType(p.base_url) === type)
		?.id;
}

// ── Z.ai Coding limit helpers ───────────────────────────────────────────

export function getZaiCodingFiveHourLimit(
	data: ZAICodingQuotaResponse | undefined | null,
): ZAICodingQuotaLimit | undefined {
	return data?.data?.limits?.find(
		(l) => l.type === "TOKENS_LIMIT" && l.unit === 3,
	);
}

export function getZaiCodingWeeklyLimit(
	data: ZAICodingQuotaResponse | undefined | null,
): ZAICodingQuotaLimit | undefined {
	return data?.data?.limits?.find(
		(l) => l.type === "TOKENS_LIMIT" && l.unit === 6,
	);
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
	deepseekProviderId: string | undefined;
	openrouterProviderId: string | undefined;
	ollamaCloudProviderId: string | undefined;

	/** Raw query data. */
	nanogptUsage: NanoGPTUsage | undefined;
	zaiCodingUsage: ZAICodingQuotaResponse | undefined;
	deepseekBalance: DeepSeekBalance | undefined;
	openrouterBalance: OpenRouterBalance | undefined;
	ollamaCloudAccount: OllamaCloudAccount | undefined;

	/** Derived Z.ai limits. */
	zaiCodingFiveHour: ZAICodingQuotaLimit | undefined;
	zaiCodingWeekly: ZAICodingQuotaLimit | undefined;

	/** NanoGPT weekly helpers. */
	nanoWeeklyUsed: number | null | undefined;
	nanoWeeklyLimit: number | null | undefined;

	/** Badge visibility booleans (already account for providerId + data). */
	showNanoBadge: boolean;
	showZaiCodingBadge: boolean;
	showDsBadge: boolean;
	showOrBadge: boolean;
	showOllamaCloudBadge: boolean;

	/** Whether any quota-supporting provider exists. */
	hasAnyProvider: boolean;

	/** Individual refetch fns. */
	refetchNano: () => Promise<void>;
	refetchZaiCoding: () => Promise<void>;
	refetchDeepseek: () => Promise<void>;
	refetchOpenRouter: () => Promise<void>;
	refetchOllamaCloud: () => Promise<void>;

	/** Individual isRefetching flags. */
	isNanoRefetching: boolean;
	isZaiCodingRefetching: boolean;
	isDsRefetching: boolean;
	isOrRefetching: boolean;
	isOllamaCloudRefetching: boolean;

	/** dataUpdatedAt for modals. */
	openrouterDataUpdatedAt: number;
	nanogptDataUpdatedAt: number;
	zaiCodingDataUpdatedAt: number;
	deepseekDataUpdatedAt: number;
	ollamaCloudDataUpdatedAt: number;

	/** Invalidate all quota query keys. */
	invalidateAll: () => void;
}

// ── Hook ─────────────────────────────────────────────────────────────────

export function useQuotaData(
	providers: Provider[] | undefined,
	options: UseQuotaDataOptions = {},
): QuotaDataResult {
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
		initialData: () =>
			getCachedData<ZAICodingQuotaResponse>("zai-coding-usage"),
	});

	useEffect(() => {
		if (zaiCodingUsage) setCachedData("zai-coding-usage", zaiCodingUsage);
	}, [zaiCodingUsage]);

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
		initialData: () =>
			getCachedData<OllamaCloudAccount>("ollama-cloud-account"),
	});

	useEffect(() => {
		if (ollamaCloudAccount)
			setCachedData("ollama-cloud-account", ollamaCloudAccount);
	}, [ollamaCloudAccount]);

	// ── Error toasting ──
	const nanoErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isNanoGPTError && !nanoErrorToasted.current) {
			toastErrors("Failed to fetch NanoGPT usage quota", "warning");
			nanoErrorToasted.current = true;
		}
		if (!isNanoGPTError) nanoErrorToasted.current = false;
	}, [isNanoGPTError, toastErrors]);

	const zaiErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isZAICodingError && !zaiErrorToasted.current) {
			toastErrors("Failed to fetch ZAI usage quota", "warning");
			zaiErrorToasted.current = true;
		}
		if (!isZAICodingError) zaiErrorToasted.current = false;
	}, [isZAICodingError, toastErrors]);

	const dsErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isDeepseekError && !dsErrorToasted.current) {
			toastErrors("Failed to fetch DeepSeek balance", "warning");
			dsErrorToasted.current = true;
		}
		if (!isDeepseekError) dsErrorToasted.current = false;
	}, [isDeepseekError, toastErrors]);

	const orErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isOpenRouterError && !orErrorToasted.current) {
			toastErrors("Failed to fetch OpenRouter key balance", "warning");
			orErrorToasted.current = true;
		}
		if (!isOpenRouterError) orErrorToasted.current = false;
	}, [isOpenRouterError, toastErrors]);

	const ocErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isOllamaCloudError && !ocErrorToasted.current) {
			toastErrors("Failed to fetch Ollama Cloud account", "warning");
			ocErrorToasted.current = true;
		}
		if (!isOllamaCloudError) ocErrorToasted.current = false;
	}, [isOllamaCloudError, toastErrors]);

	// ── Derived values ──
	const zaiCodingFiveHour = getZaiCodingFiveHourLimit(zaiCodingUsage);
	const zaiCodingWeekly = getZaiCodingWeeklyLimit(zaiCodingUsage);

	const nanoWeeklyUsed = nanogptUsage?.weeklyInputTokens?.used;
	const nanoWeeklyLimit = nanogptUsage?.limits?.weeklyInputTokens;

	const showNanoBadge =
		Boolean(nanogptProviderId) &&
		Boolean(nanogptUsage) &&
		nanoWeeklyUsed != null &&
		Boolean(nanoWeeklyLimit);

	const showZaiCodingBadge =
		Boolean(zaiCodingProviderId) &&
		Boolean(zaiCodingUsage?.success) &&
		Boolean(zaiCodingFiveHour || zaiCodingWeekly);

	const showDsBadge = Boolean(deepseekProviderId) && Boolean(deepseekBalance);

	const showOrBadge =
		Boolean(openrouterProviderId) &&
		Boolean(openrouterBalance) &&
		openrouterBalance?.credits_remaining != null;

	const showOllamaCloudBadge =
		Boolean(ollamaCloudProviderId) && Boolean(ollamaCloudAccount);

	const hasAnyProvider = Boolean(
		nanogptProviderId ||
			zaiCodingProviderId ||
			deepseekProviderId ||
			openrouterProviderId ||
			ollamaCloudProviderId,
	);

	// ── Refetch helpers ──
	const refetchNano = useCallback(async () => {
		await refetchNanoRaw();
	}, [refetchNanoRaw]);

	const refetchZaiCoding = useCallback(async () => {
		await refetchZaiRaw();
	}, [refetchZaiRaw]);

	const refetchDeepseek = useCallback(async () => {
		await refetchDsRaw();
	}, [refetchDsRaw]);

	const refetchOpenRouter = useCallback(async () => {
		await refetchOrRaw();
	}, [refetchOrRaw]);

	const refetchOllamaCloud = useCallback(async () => {
		await refetchOcRaw();
	}, [refetchOcRaw]);

	const invalidateAll = useCallback(() => {
		queryClient.invalidateQueries({ queryKey: ["nanogpt-usage"] });
		queryClient.invalidateQueries({ queryKey: ["zai-coding-usage"] });
		queryClient.invalidateQueries({ queryKey: ["deepseek-balance"] });
		queryClient.invalidateQueries({ queryKey: ["openrouter-balance"] });
		queryClient.invalidateQueries({ queryKey: ["ollama-cloud-account"] });
	}, [queryClient]);

	return {
		nanogptProviderId,
		zaiCodingProviderId,
		deepseekProviderId,
		openrouterProviderId,
		ollamaCloudProviderId,
		nanogptUsage,
		zaiCodingUsage,
		deepseekBalance,
		openrouterBalance,
		ollamaCloudAccount,
		zaiCodingFiveHour,
		zaiCodingWeekly,
		nanoWeeklyUsed,
		nanoWeeklyLimit,
		showNanoBadge,
		showZaiCodingBadge,
		showDsBadge,
		showOrBadge,
		showOllamaCloudBadge,
		hasAnyProvider,
		refetchNano,
		refetchZaiCoding,
		refetchDeepseek,
		refetchOpenRouter,
		refetchOllamaCloud,
		isNanoRefetching,
		isZaiCodingRefetching,
		isDsRefetching,
		isOrRefetching,
		isOllamaCloudRefetching,
		nanogptDataUpdatedAt,
		zaiCodingDataUpdatedAt,
		deepseekDataUpdatedAt,
		openrouterDataUpdatedAt,
		ollamaCloudDataUpdatedAt,
		invalidateAll,
	};
}
