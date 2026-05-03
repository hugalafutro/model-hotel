import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDownAZ, ArrowUpZA, Eye, EyeOff, PlugZap } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api/client";
import type {
	DeepSeekBalance,
	DeepSeekBalanceInfo,
	NanoGPTUsage,
	Provider,
	ZAICodingQuotaResponse,
} from "../api/types";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { CopyablePill } from "../components/CopyablePill";
import { FilterDropdown } from "../components/FilterDropdown";
import { Modal } from "../components/Modal";
import {
	NanoGPTQuotaModal,
	ZAICodingQuotaModal,
} from "../components/ProviderModals";
import { ProviderModelsModal } from "../components/ProviderModelsModal";
import { Spinner } from "../components/Spinner";
import { useToast } from "../context/ToastContext";
import { formatTimestamp, formatTokens } from "../utils/format";

const CACHE_PREFIX = "model-hotel";

function getCachedData<T>(key: string): T | undefined {
	try {
		const raw = localStorage.getItem(`${CACHE_PREFIX}:${key}`);
		if (raw) return JSON.parse(raw) as T;
	} catch {
		/* ignore */
	}
	return undefined;
}

function setCachedData<T>(key: string, data: T) {
	try {
		localStorage.setItem(`${CACHE_PREFIX}:${key}`, JSON.stringify(data));
	} catch {
		/* ignore */
	}
}

const baseUrls: Record<string, string> = {
	nanogpt: "https://nano-gpt.com/api/subscription/v1",
	"z-ai-coding": "https://api.z.ai/api/paas/v4",
	openai: "https://api.openai.com/v1",
	anthropic: "https://api.anthropic.com",
	deepseek: "https://api.deepseek.com/v1",
	ollama: "http://localhost:11434",
	"opencode-zen": "https://opencode.ai/zen/v1",
	"opencode-go": "https://opencode.ai/zen/go/v1",
	xai: "https://api.x.ai/v1",
	google: "https://generativelanguage.googleapis.com/v1beta/openai",
	cohere: "https://api.cohere.ai/compatibility/v1",
	openrouter: "https://openrouter.ai/api/v1",
};

function isKnownProviderUrl(url: string): boolean {
	return Object.values(baseUrls).includes(url);
}

function getProviderType(baseUrl: string): string {
	for (const [type, url] of Object.entries(baseUrls)) {
		if (baseUrl === url) return type;
	}
	return "custom";
}

const providerTypeDisplayNames: Record<string, string> = {
	custom: "Custom",
	nanogpt: "NanoGPT",
	"z-ai-coding": "Z.ai Coding Plan",
	openai: "OpenAI",
	anthropic: "Anthropic",
	deepseek: "DeepSeek",
	ollama: "Ollama",
	"opencode-zen": "OpenCode Zen",
	"opencode-go": "OpenCode Go",
	xai: "xAI (Grok)",
	google: "Google AI Studio (Gemini)",
	cohere: "Cohere",
	openrouter: "OpenRouter",
};

function providerTypeAllowsEmptyKey(type: string): boolean {
	return type === "opencode-zen" || type === "ollama" || type === "custom";
}

function EditProviderModal({
	provider,
	onClose,
	onToast,
}: {
	provider: Provider;
	onClose: () => void;
	onToast: (msg: string, type: "success" | "error" | "info") => void;
}) {
	const queryClient = useQueryClient();
	const [formData, setFormData] = useState({
		name: provider.name,
		base_url: provider.base_url,
		api_key: "",
		enabled: provider.enabled,
	});
	const [error, setError] = useState<string | null>(null);
	const [confirmFields, setConfirmFields] = useState<string[] | null>(null);
	const [showApiKey, setShowApiKey] = useState(false);

	const updateMutation = useMutation({
		mutationFn: (data: {
			name?: string;
			base_url?: string;
			api_key?: string;
			enabled?: boolean;
		}) => api.providers.update(provider.id, data),
		onSuccess: (updated: Provider) => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			onToast(`Provider "${updated.name}" updated`, "success");
			onClose();
		},
		onError: (err: Error) => {
			setError(err.message);
			onToast(`Failed to update provider: ${err.message}`, "error");
		},
	});

	const getChangedFields = (): string[] => {
		const fields: string[] = [];
		if (formData.name !== provider.name) fields.push("name");
		if (formData.base_url !== provider.base_url) fields.push("base_url");
		if (formData.api_key !== "") fields.push("api_key");
		if (formData.enabled !== provider.enabled) fields.push("enabled");
		return fields;
	};

	const handleClose = () => {
		const changed = getChangedFields();
		if (changed.length > 0) {
			setConfirmFields(changed);
		} else {
			onClose();
		}
	};

	const handleSubmit = (e: React.FormEvent) => {
		e.preventDefault();
		setError(null);
		const payload: {
			name?: string;
			base_url?: string;
			api_key?: string;
			enabled?: boolean;
		} = {};
		if (formData.name !== provider.name) payload.name = formData.name.trim();
		if (formData.base_url !== provider.base_url)
			payload.base_url = formData.base_url;
		if (formData.api_key !== "") payload.api_key = formData.api_key;
		if (formData.enabled !== provider.enabled)
			payload.enabled = formData.enabled;
		updateMutation.mutate(payload);
	};

	return (
		<>
			<Modal title="Edit Provider" onClose={handleClose}>
				{error && (
					<div className="mb-4 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
						{error}
					</div>
				)}

				<form onSubmit={handleSubmit} className="space-y-4">
					<div>
						<label
							htmlFor="edit-provider-name"
							className="block text-sm font-medium text-gray-300 mb-1"
						>
							Name
						</label>
						<input
							id="edit-provider-name"
							type="text"
							maxLength={100}
							required
							value={formData.name}
							onChange={(e) =>
								setFormData({
									...formData,
									name: e.target.value,
								})
							}
							className="ui-input"
							placeholder="e.g., OpenAI"
						/>
					</div>

					<div>
						<label
							htmlFor="edit-provider-base-url"
							className="block text-sm font-medium text-gray-300 mb-1"
						>
							Base URL
						</label>
						<input
							id="edit-provider-base-url"
							type="url"
							required
							readOnly={isKnownProviderUrl(provider.base_url)}
							value={formData.base_url}
							onChange={(e) =>
								setFormData({
									...formData,
									base_url: e.target.value,
								})
							}
							className={
								isKnownProviderUrl(provider.base_url)
									? "ui-input opacity-60 cursor-not-allowed"
									: "ui-input"
							}
							placeholder="https://api.openai.com/v1"
						/>
						{isKnownProviderUrl(provider.base_url) && (
							<p className="text-gray-500 text-xs mt-1">
								Base URL is preset for this provider type
							</p>
						)}
					</div>

					<div>
						<label
							htmlFor="edit-provider-api-key"
							className="block text-sm font-medium text-gray-300 mb-1"
						>
							API Key
						</label>
						<div className="relative">
							<input
								id="edit-provider-api-key"
								type={showApiKey ? "text" : "password"}
								maxLength={500}
								value={formData.api_key}
								onChange={(e) =>
									setFormData({
										...formData,
										api_key: e.target.value,
									})
								}
								className="ui-input pr-10! overflow-hidden"
								placeholder="Leave blank to keep current key"
							/>
							<button
								type="button"
								onClick={() => setShowApiKey(!showApiKey)}
								className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 transition-colors"
								tabIndex={-1}
								aria-label={showApiKey ? "Hide API key" : "Show API key"}
							>
								{showApiKey ? <EyeOff size={18} /> : <Eye size={18} />}
							</button>
						</div>
						<p className="text-gray-500 text-xs mt-1">
							Current: {provider.masked_key}
						</p>
					</div>

					<div className="flex items-center gap-3">
						<label
							htmlFor="edit-provider-enabled"
							className="text-sm font-medium text-gray-300"
						>
							Enabled
						</label>
						<button
							type="button"
							role="switch"
							aria-checked={formData.enabled}
							onClick={() =>
								setFormData({
									...formData,
									enabled: !formData.enabled,
								})
							}
							className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors focus:ring-2 focus:ring-(--accent) focus:ring-offset-2 focus:ring-offset-gray-800 ${
								formData.enabled ? "bg-(--accent)" : "bg-gray-600"
							}`}
						>
							<span
								className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
									formData.enabled ? "translate-x-6" : "translate-x-1"
								}`}
							/>
						</button>
					</div>

					<div className="flex space-x-3 justify-end pt-4">
						<button
							type="button"
							onClick={handleClose}
							className="ui-btn ui-btn-secondary"
						>
							Cancel
						</button>
						<button
							type="submit"
							disabled={updateMutation.isPending}
							className={`px-3 py-1.5 text-xs rounded-full border transition-all ${
								updateMutation.isPending
									? "bg-(--accent-lighter) text-(--accent)/50 border-(--accent-light) cursor-not-allowed"
									: "bg-(--accent-light) text-(--accent) border-(--accent-lighter) cursor-pointer hover:brightness-125"
							}`}
						>
							{updateMutation.isPending ? "Saving…" : "Save Changes"}
						</button>
					</div>
				</form>
			</Modal>
			{confirmFields && (
				<ConfirmDialog
					title="Unsaved Changes"
					fields={confirmFields}
					onConfirm={onClose}
					onCancel={() => setConfirmFields(null)}
				/>
			)}
		</>
	);
}

export function Providers() {
	const queryClient = useQueryClient();
	const { toast } = useToast();
	const [showModal, setShowModal] = useState(false);
	const [editProvider, setEditProvider] = useState<Provider | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [discoveringId, setDiscoveringId] = useState<string | null>(null);
	const [discoverAllCurrentId, setDiscoverAllCurrentId] = useState<
		string | null
	>(null);
	const [quotaUsage, setQuotaUsage] = useState<NanoGPTUsage | null>(null);
	const [zaiCodingQuotaUsage, setZaiCodingQuotaUsage] =
		useState<ZAICodingQuotaResponse | null>(null);
	const [formData, setFormData] = useState<{
		name: string;
		base_url: string;
		api_key: string;
		provider_type: string;
	}>({
		name: "",
		base_url: "",
		api_key: "",
		provider_type: "custom",
	});
	const [showApiKey, setShowApiKey] = useState(false);
	const [modelsProvider, setModelsProvider] = useState<Provider | null>(null);
	const [typeFilter, setTypeFilter] = useState("");
	const [sortAsc, setSortAsc] = useState(true);

	const { data: providers, isLoading } = useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
	});

	const { data: models } = useQuery({
		queryKey: ["models"],
		queryFn: () => api.models.list(),
		staleTime: 60_000,
	});

	const modelCounts = useMemo(() => {
		const map = new Map<string, number>();
		if (models) {
			for (const m of models) {
				if (m.provider_name && m.enabled) {
					map.set(m.provider_name, (map.get(m.provider_name) || 0) + 1);
				}
			}
		}
		return map;
	}, [models]);

	const { data: settings } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	const nanogptProviderId = useMemo(() => {
		return providers?.find((p) => {
			try {
				return new URL(p.base_url).hostname.endsWith("nano-gpt.com");
			} catch {
				return false;
			}
		})?.id;
	}, [providers]);

	const zaiCodingProviderId = useMemo(() => {
		return providers?.find((p) => {
			try {
				const h = new URL(p.base_url).hostname;
				return h === "z.ai" || h.endsWith(".z.ai");
			} catch {
				return false;
			}
		})?.id;
	}, [providers]);

	const deepseekProviderId = useMemo(() => {
		return providers?.find((p) => {
			try {
				return new URL(p.base_url).hostname.endsWith("deepseek.com");
			} catch {
				return false;
			}
		})?.id;
	}, [providers]);

	const {
		data: nanogptUsage,
		dataUpdatedAt: nanogptDataUpdatedAt,
		refetch,
		isRefetching,
		isError: isNanoGPTError,
	} = useQuery({
		queryKey: ["nanogpt-usage", nanogptProviderId],
		queryFn: () =>
			api.providers.getUsage(
				nanogptProviderId as string,
			) as Promise<NanoGPTUsage>,
		enabled: Boolean(nanogptProviderId),
		initialData: () => getCachedData<NanoGPTUsage>("nanogpt-usage"),
	});

	useEffect(() => {
		if (nanogptUsage) setCachedData("nanogpt-usage", nanogptUsage);
	}, [nanogptUsage]);

	const {
		data: zaiCodingUsage,
		dataUpdatedAt: zaiCodingDataUpdatedAt,
		refetch: refetchZai,
		isRefetching: isZaiCodingRefetching,
		isError: isZAICodingError,
	} = useQuery({
		queryKey: ["zai-coding-usage", zaiCodingProviderId],
		queryFn: () =>
			api.providers.getUsage(
				zaiCodingProviderId as string,
			) as Promise<ZAICodingQuotaResponse>,
		enabled: Boolean(zaiCodingProviderId),
		initialData: () =>
			getCachedData<ZAICodingQuotaResponse>("zai-coding-usage"),
	});

	useEffect(() => {
		if (zaiCodingUsage) setCachedData("zai-coding-usage", zaiCodingUsage);
	}, [zaiCodingUsage]);

	const {
		data: deepseekBalanceData,
		refetch: refetchDeepseekBalance,
		isError: isDeepseekError,
	} = useQuery({
		queryKey: ["deepseek-balance", deepseekProviderId],
		queryFn: () => api.providers.getBalance(deepseekProviderId as string),
		enabled: Boolean(deepseekProviderId),
		initialData: () => getCachedData<DeepSeekBalance>("deepseek-balance"),
	});

	useEffect(() => {
		if (deepseekBalanceData)
			setCachedData("deepseek-balance", deepseekBalanceData);
	}, [deepseekBalanceData]);

	const nanoGPTErrorToasted = useRef(false);
	useEffect(() => {
		if (isNanoGPTError && !nanoGPTErrorToasted.current) {
			toast("Failed to fetch NanoGPT usage quota", "warning");
			nanoGPTErrorToasted.current = true;
		}
		if (!isNanoGPTError) nanoGPTErrorToasted.current = false;
	}, [isNanoGPTError, toast]);

	const zaiCodingErrorToasted = useRef(false);
	useEffect(() => {
		if (isZAICodingError && !zaiCodingErrorToasted.current) {
			toast("Failed to fetch ZAI usage quota", "warning");
			zaiCodingErrorToasted.current = true;
		}
		if (!isZAICodingError) zaiCodingErrorToasted.current = false;
	}, [isZAICodingError, toast]);

	const deepseekErrorToasted = useRef(false);
	useEffect(() => {
		if (isDeepseekError && !deepseekErrorToasted.current) {
			toast("Failed to fetch DeepSeek balance", "warning");
			deepseekErrorToasted.current = true;
		}
		if (!isDeepseekError) deepseekErrorToasted.current = false;
	}, [isDeepseekError, toast]);

	// Track which provider is currently being scanned during Discover All
	useEffect(() => {
		const handler = (e: Event) => {
			const event = (e as CustomEvent).detail;
			if (
				event?.type === "request.discovery.provider_starting" &&
				event?.metadata?.provider_id
			) {
				setDiscoverAllCurrentId(event.metadata.provider_id as string);
			}
		};
		window.addEventListener("server-event", handler);
		return () => window.removeEventListener("server-event", handler);
	}, []);

	const discoverAllMutation = useMutation({
		mutationFn: async () => {
			return api.providers.discoverAll();
		},
		onSuccess: (data) => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			queryClient.invalidateQueries({ queryKey: ["models"] });
			setDiscoverAllCurrentId(null);
			if (data.failed > 0 && data.succeeded === 0) {
				toast(`Discovery failed for all ${data.failed} providers`, "error");
			}
		},
		onError: (err: Error) => {
			toast(`Discover all failed: ${err.message}`, "error");
			setDiscoverAllCurrentId(null);
		},
	});

	const refreshQuotasMutation = useMutation({
		mutationFn: async () => {
			return api.providers.refreshQuotas();
		},
		onSuccess: (data) => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			if (data.failed > 0) {
				toast(
					`Refreshed ${data.refreshed} quotas (${data.failed} failed, ${data.skipped} unsupported)`,
					"warning",
				);
			} else if (data.refreshed === 0) {
				toast("No providers with quota/balance support found", "info");
			} else {
				toast(`Refreshed ${data.refreshed} quotas/balances`, "success");
			}
		},
		onError: (err: Error) => {
			toast(`Refresh quotas failed: ${err.message}`, "error");
		},
	});

	const discoverMutation = useMutation({
		mutationFn: async (id: string) => {
			setDiscoveringId(id);
			return api.providers.discover(id);
		},
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			queryClient.invalidateQueries({ queryKey: ["models"] });
		},
		onError: (err: Error) => {
			toast(`Discovery failed: ${err.message}`, "error");
		},
		onSettled: () => {
			setDiscoveringId(null);
		},
	});

	const createMutation = useMutation({
		mutationFn: (data: { name: string; base_url: string; api_key: string }) =>
			api.providers.create(data),
		onSuccess: async (newProvider) => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			setShowModal(false);
			setFormData({
				name: "",
				base_url: "",
				api_key: "",
				provider_type: "custom",
			});
			setError(null);
			toast(`Provider "${newProvider.name}" added`, "success");
			const shouldDiscover = settings?.discovery_on_provider_create !== "false";
			if (shouldDiscover) {
				try {
					await api.providers.discover(newProvider.id);
					queryClient.invalidateQueries({ queryKey: ["models"] });
					queryClient.invalidateQueries({ queryKey: ["providers"] });
				} catch (e) {
					// SSE events show the error to the user; log for diagnostics.
					console.warn("Auto-discovery failed after provider creation:", e);
				}
			}
		},
		onError: (err: Error) => {
			setError(err.message);
			toast(`Failed to add provider: ${err.message}`, "error");
		},
	});

	const deleteMutation = useMutation({
		mutationFn: (id: string) => api.providers.delete(id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			queryClient.invalidateQueries({ queryKey: ["models"] });
			toast("Provider deleted", "success");
		},
		onError: (err: Error) => {
			toast(`Failed to delete: ${err.message}`, "error");
		},
	});

	const handleSubmit = (e: React.FormEvent) => {
		e.preventDefault();
		setError(null);
		createMutation.mutate({
			name: formData.name.trim(),
			base_url: formData.base_url,
			api_key: formData.api_key,
		});
	};

	const generateProviderName = (type: string): string => {
		const baseName = providerTypeDisplayNames[type] || "Provider";
		if (!providers) return baseName;
		const existingNames = new Set(providers.map((p) => p.name));
		if (!existingNames.has(baseName)) return baseName;
		let n = 2;
		while (existingNames.has(`${baseName} ${n}`)) n++;
		return `${baseName} ${n}`;
	};

	const handleProviderTypeChange = (type: string) => {
		if (type === "custom") {
			setFormData((prev) => ({
				...prev,
				provider_type: type,
				base_url: prev.base_url,
				name: prev.name,
			}));
			return;
		}
		const newName = generateProviderName(type);
		setFormData((prev) => ({
			...prev,
			provider_type: type,
			base_url: baseUrls[type] || prev.base_url,
			name: newName,
		}));
	};

	const typeOptions = useMemo(() => {
		if (!providers) return [];
		const typeCounts = new Map<string, number>();
		for (const p of providers) {
			const type = getProviderType(p.base_url);
			typeCounts.set(type, (typeCounts.get(type) || 0) + 1);
		}
		const entries = Array.from(typeCounts.entries());
		entries.sort((a, b) => {
			if (a[0] === "custom") return -1;
			if (b[0] === "custom") return 1;
			const labelA = providerTypeDisplayNames[a[0]] || a[0];
			const labelB = providerTypeDisplayNames[b[0]] || b[0];
			return labelA.localeCompare(labelB);
		});
		return entries.map(([type, count]) => ({
			value: type,
			label: providerTypeDisplayNames[type] || type,
			count,
		}));
	}, [providers]);

	const filteredProviders = useMemo(() => {
		if (!providers) return providers;
		const list = typeFilter
			? providers.filter((p) => getProviderType(p.base_url) === typeFilter)
			: providers;
		return [...list].sort((a, b) =>
			sortAsc ? a.name.localeCompare(b.name) : b.name.localeCompare(a.name),
		);
	}, [providers, typeFilter, sortAsc]);

	const allProvidersCount = providers?.length ?? 0;

	if (isLoading) {
		return (
			<div className="flex items-center justify-center h-64">
				<div className="animate-spin rounded-full h-12 w-12 border-b-2 border-(--accent)"></div>
			</div>
		);
	}

	return (
		<div className="space-y-6">
			<div className="flex justify-between items-center">
				<div>
					<div className="flex items-center gap-3">
						<PlugZap size={28} strokeWidth={2} className="text-(--accent)" />
						<h1 className="text-2xl font-bold text-(--text-primary)">
							Providers
						</h1>
					</div>
					<p className="text-gray-400">Manage your provider configurations</p>
				</div>
				<div className="flex items-center gap-3">
					<button
						type="button"
						onClick={() => discoverAllMutation.mutate()}
						disabled={discoverAllMutation.isPending || discoveringId !== null}
						className="ui-btn ui-btn-secondary"
					>
						{discoverAllMutation.isPending ? (
							<>
								<Spinner /> Discovering...
							</>
						) : (
							"Discover All Models"
						)}
					</button>
					<button
						type="button"
						onClick={() => refreshQuotasMutation.mutate()}
						disabled={refreshQuotasMutation.isPending}
						className="ui-btn ui-btn-secondary"
					>
						{refreshQuotasMutation.isPending ? (
							<>
								<Spinner /> Refreshing...
							</>
						) : (
							"Refresh Quotas/Balances"
						)}
					</button>
					<button
						type="button"
						onClick={() => setShowModal(true)}
						className="ui-btn ui-btn-primary"
					>
						+ Add Provider
					</button>
				</div>
			</div>

			<div className="flex items-center justify-end gap-2">
				<button
					type="button"
					onClick={() => setSortAsc((prev) => !prev)}
					title={
						sortAsc
							? "Sorted A-Z (click to reverse)"
							: "Sorted Z-A (click to reverse)"
					}
					className="p-1.5 rounded-md transition-all cursor-pointer text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[0_0_6px_var(--accent)]"
				>
					{sortAsc ? <ArrowDownAZ size={16} /> : <ArrowUpZA size={16} />}
				</button>
				<FilterDropdown
					value={typeFilter}
					onChange={setTypeFilter}
					placeholder="Provider type"
					allLabel={`All (${allProvidersCount})`}
					options={typeOptions}
					className="w-44"
				/>
			</div>

			<div
				className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6"
			>
				{filteredProviders?.map((provider) => {
					const isNanoGPT = provider.base_url.includes("nano-gpt.com");
					const isZAI = provider.base_url.includes("z.ai");
					const weeklyUsed = isNanoGPT
						? nanogptUsage?.weeklyInputTokens?.used
						: null;
					const weeklyLimit = isNanoGPT
						? nanogptUsage?.limits?.weeklyInputTokens
						: null;
					const showQuotaBadge =
						isNanoGPT && weeklyUsed != null && weeklyLimit && nanogptUsage;

					const zaiCodingFiveHour = isZAI
						? zaiCodingUsage?.data?.limits?.find(
								(l) => l.type === "TOKENS_LIMIT" && l.unit === 3,
							)
						: null;
					const zaiCodingWeekly = isZAI
						? zaiCodingUsage?.data?.limits?.find(
								(l) => l.type === "TOKENS_LIMIT" && l.unit === 6,
							)
						: null;
					const showZaiCodingBadge =
						isZAI &&
						zaiCodingUsage?.success &&
						(zaiCodingFiveHour || zaiCodingWeekly);

					return (
						<div
							key={provider.id}
							className={`ui-card p-6 flex flex-col ${!provider.enabled ? "opacity-50" : ""}`}
						>
							<div className="mb-4">
								<div className="flex items-center justify-between">
									<CopyablePill
										text={provider.name}
										displayText={provider.name}
										textClassName="text-lg font-semibold text-white"
										tooltip="Click to copy provider name"
									/>
									<div className="flex items-center gap-2">
										{!provider.enabled && (
											<span className="px-2 py-0.5 rounded-full bg-gray-600/40 text-gray-400 text-xs font-medium border border-gray-600/50">
												Disabled
											</span>
										)}
										{provider.total_tokens > 0 && (
											<span className="px-2 py-0.5 rounded-full bg-purple-500/20 text-purple-400 text-xs font-medium border border-purple-500/30">
												{formatTokens(provider.total_tokens)} tokens
											</span>
										)}
										{(() => {
											const count = modelCounts.get(provider.name) ?? 0;
											return (
												count > 0 && (
													<button
														type="button"
														onClick={() => setModelsProvider(provider)}
														className="px-2 py-0.5 rounded-full bg-cyan-500/20 text-cyan-400 text-xs font-medium border border-cyan-500/30 cursor-pointer hover:bg-cyan-500/30 hover:border-cyan-400/50 transition-colors"
													>
														{count} {count === 1 ? "model" : "models"}
													</button>
												)
											);
										})()}
									</div>
								</div>
								<CopyablePill
									text={provider.base_url}
									textClassName="text-sm text-gray-400 font-mono"
									tooltip="Click to copy API base URL"
								/>
							</div>

							<div className="space-y-2 text-sm">
								<div className="flex justify-between">
									<span className="text-gray-500">Created</span>
									<span className="text-gray-300">
										{formatTimestamp(provider.created_at)}
									</span>
								</div>
								<div className="flex justify-between">
									<span className="text-gray-500">API Key</span>
									<span className="font-mono text-gray-300">
										{provider.masked_key}
									</span>
								</div>
								<div className="flex justify-between">
									<span className="text-gray-500">Last Used</span>
									<span className="text-gray-300">
										{provider.last_used_at
											? formatTimestamp(provider.last_used_at)
											: "N/A"}
									</span>
								</div>
								{provider.last_discovered_at && (
									<div className="flex justify-between">
										<span className="text-gray-500">Last Discovery</span>
										<span className="text-gray-300">
											{formatTimestamp(provider.last_discovered_at)}
										</span>
									</div>
								)}
							</div>

							<div className="mt-auto pt-4 flex items-center justify-between gap-2">
								<div className="flex items-center gap-2 min-h-7">
									{showQuotaBadge && (
										<button
											type="button"
											onClick={() =>
												nanogptUsage && setQuotaUsage(nanogptUsage)
											}
											className="px-2 py-1.5 rounded-full bg-[#0690a8]/20 text-[#0690a8] border border-[#0690a8]/50 text-xs font-medium cursor-pointer hover:bg-[#0690a8]/30 transition-colors"
											title="View quota details"
										>
											{formatTokens(weeklyUsed)}/{formatTokens(weeklyLimit)}
										</button>
									)}
									{showZaiCodingBadge && (
										<button
											type="button"
											onClick={() =>
												zaiCodingUsage && setZaiCodingQuotaUsage(zaiCodingUsage)
											}
											className="px-2 py-1.5 rounded-full bg-[#36aaff]/20 text-[#36aaff] border border-[#36aaff]/50 text-xs font-medium cursor-pointer hover:bg-[#36aaff]/30 transition-colors"
											title="View quota details"
										>
											{zaiCodingFiveHour
												? `${(100 - zaiCodingFiveHour.percentage).toFixed(0)}%`
												: "-"}
											/
											{zaiCodingWeekly
												? `${(100 - zaiCodingWeekly.percentage).toFixed(0)}%`
												: "-"}
										</button>
									)}
									{provider.base_url.includes("deepseek.com") &&
										deepseekBalanceData && (
											<button
												type="button"
												onClick={async () => {
													try {
														await refetchDeepseekBalance();
														toast("Balance refreshed", "success");
													} catch {
														toast("Failed to refresh balance", "error");
													}
												}}
												className="px-2 py-1.5 rounded-full bg-[#36aaff]/20 text-[#36aaff] border border-[#36aaff]/50 text-xs font-medium cursor-pointer hover:bg-[#36aaff]/30 transition-colors"
												title="Refresh balance"
											>
												{deepseekBalanceData.balance_infos.find(
													(b: DeepSeekBalanceInfo) => b.currency === "USD",
												)?.total_balance ?? "-"}{" "}
												USD
											</button>
										)}
								</div>
								<div className="flex gap-2">
									<button
										type="button"
										onClick={() => setEditProvider(provider)}
										className="ui-btn ui-btn-secondary"
									>
										Edit
									</button>
									<button
										type="button"
										onClick={() => discoverMutation.mutate(provider.id)}
										disabled={
											discoveringId !== null || discoverAllMutation.isPending
										}
										className={`px-3 py-1.5 text-xs rounded-full border transition-all ${
											discoveringId === provider.id ||
											discoverAllCurrentId === provider.id
												? "bg-(--accent-lighter) text-(--accent)/50 border-(--accent-light) cursor-not-allowed"
												: discoveringId !== null ||
														discoverAllMutation.isPending
													? "bg-gray-800/50 text-gray-600 border-gray-700/30 cursor-not-allowed"
													: "bg-(--accent-light) text-(--accent) border-(--accent-lighter) cursor-pointer hover:brightness-125"
										}`}
									>
										{discoveringId === provider.id ||
										discoverAllCurrentId === provider.id ? (
											<>
												<Spinner /> Discovering...
											</>
										) : (
											"Discover Models"
										)}
									</button>
									<button
										type="button"
										onClick={() => {
											if (
												window.confirm(
													"Delete this provider? This cannot be undone.",
												)
											)
												deleteMutation.mutate(provider.id);
										}}
										className="ui-btn ui-btn-danger"
									>
										Delete
									</button>
								</div>
							</div>
						</div>
					);
				})}

				{filteredProviders?.length === 0 &&
					providers &&
					providers.length > 0 && (
						<div className="col-span-full text-center py-12 ui-card">
							<p className="text-gray-500">
								No providers match the selected filter.
							</p>
						</div>
					)}
				{providers?.length === 0 && (
					<div className="col-span-full text-center py-12 ui-card">
						<p className="text-gray-500">
							No providers configured. Add your first provider to get started.
						</p>
					</div>
				)}
			</div>

			{showModal && (
				<Modal
					title="Add Provider"
					onClose={() => {
						setShowModal(false);
						setFormData({
							name: "",
							base_url: "",
							api_key: "",
							provider_type: "custom",
						});
						setShowApiKey(false);
						setError(null);
					}}
				>
					{error && (
						<div className="mb-4 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
							{error}
						</div>
					)}

					<form onSubmit={handleSubmit} className="space-y-4">
						<div>
							<label
								htmlFor="provider-type"
								className="block text-sm font-medium text-gray-300 mb-1"
							>
								Type
							</label>
							<select
								id="provider-type"
								value={formData.provider_type}
								onChange={(e) => handleProviderTypeChange(e.target.value)}
								className="ui-input"
							>
								<option value="custom">Custom</option>
								<option value="anthropic">Anthropic</option>
								<option value="cohere">Cohere</option>
								<option value="deepseek">DeepSeek</option>
								<option value="google">Google AI Studio (Gemini)</option>
								<option value="nanogpt">NanoGPT</option>
								<option value="ollama">Ollama</option>
								<option value="openai">OpenAI</option>
								<option value="opencode-go">OpenCode Go</option>
								<option value="opencode-zen">OpenCode Zen</option>
								<option value="openrouter">OpenRouter</option>
								<option value="xai">xAI (Grok)</option>
								<option value="z-ai-coding">Z.ai Coding Plan</option>
							</select>
						</div>

						<div>
							<label
								htmlFor="provider-name"
								className="block text-sm font-medium text-gray-300 mb-1"
							>
								Name
							</label>
							<input
								id="provider-name"
								type="text"
								maxLength={100}
								required
								value={formData.name}
								onChange={(e) =>
									setFormData({
										...formData,
										name: e.target.value,
									})
								}
								onFocus={(e) => e.target.select()}
								className="ui-input"
								placeholder="e.g., OpenAI"
							/>
							<p className="text-gray-500 text-xs mt-1">
								Dots, spaces, and special characters are replaced with
								&quot;-&quot; when routing.
							</p>
						</div>

						<div>
							<label
								htmlFor="provider-base-url"
								className="block text-sm font-medium text-gray-300 mb-1"
							>
								Base URL
							</label>
							<input
								id="provider-base-url"
								type="url"
								required
								value={formData.base_url}
								onChange={(e) =>
									setFormData({
										...formData,
										base_url: e.target.value,
									})
								}
								readOnly={formData.provider_type !== "custom"}
								className={
									formData.provider_type !== "custom"
										? "ui-input opacity-60 cursor-not-allowed"
										: "ui-input"
								}
								placeholder="https://api.openai.com/v1"
							/>
							{formData.provider_type !== "custom" && (
								<p className="text-gray-500 text-xs mt-1">
									Base URL is preset for this provider type
								</p>
							)}
							{formData.provider_type === "custom" && (
								<p className="text-gray-500 text-xs mt-1">
									Full API base URL including any path prefix. Models will be
									discovered from {"<base_url>"}/models
								</p>
							)}
						</div>

						<div>
							<label
								htmlFor="provider-api-key"
								className="block text-sm font-medium text-gray-300 mb-1"
							>
								API Key
							</label>
							<div className="relative">
								<input
									id="provider-api-key"
									type={showApiKey ? "text" : "password"}
									maxLength={500}
									required={!providerTypeAllowsEmptyKey(formData.provider_type)}
									value={formData.api_key}
									onChange={(e) =>
										setFormData({
											...formData,
											api_key: e.target.value,
										})
									}
									className="ui-input pr-10! overflow-hidden"
									placeholder={
										providerTypeAllowsEmptyKey(formData.provider_type)
											? "Optional — free models work without a key"
											: "API key"
									}
								/>
								<button
									type="button"
									onClick={() => setShowApiKey(!showApiKey)}
									className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 transition-colors"
									tabIndex={-1}
									aria-label={showApiKey ? "Hide API key" : "Show API key"}
								>
									{showApiKey ? <EyeOff size={18} /> : <Eye size={18} />}
								</button>
							</div>
						</div>

						<div className="flex space-x-3 justify-end pt-4">
							<button
								type="button"
								onClick={() => {
									setShowModal(false);
									setFormData({
										name: "",
										base_url: "",
										api_key: "",
										provider_type: "custom",
									});
									setShowApiKey(false);
									setError(null);
								}}
								className="ui-btn ui-btn-secondary"
							>
								Cancel
							</button>
							<button
								type="submit"
								disabled={createMutation.isPending}
								className="ui-btn ui-btn-primary disabled:opacity-50"
							>
								{createMutation.isPending ? "Adding…" : "Add Provider"}
							</button>
						</div>
					</form>
				</Modal>
			)}

			{quotaUsage && (
				<NanoGPTQuotaModal
					usage={quotaUsage}
					onClose={() => setQuotaUsage(null)}
					onRefresh={refetch}
					isRefreshing={isRefetching}
					onToast={toast}
					lastRefreshed={nanogptDataUpdatedAt}
				/>
			)}

			{zaiCodingQuotaUsage && (
				<ZAICodingQuotaModal
					usage={zaiCodingQuotaUsage}
					onClose={() => setZaiCodingQuotaUsage(null)}
					onRefresh={refetchZai}
					isRefreshing={isZaiCodingRefetching}
					onToast={toast}
					lastRefreshed={zaiCodingDataUpdatedAt}
				/>
			)}

			{editProvider && (
				<EditProviderModal
					provider={editProvider}
					onClose={() => setEditProvider(null)}
					onToast={toast}
				/>
			)}

			{modelsProvider && models && (
				<ProviderModelsModal
					provider={modelsProvider}
					models={models}
					onClose={() => setModelsProvider(null)}
				/>
			)}
		</div>
	);
}
