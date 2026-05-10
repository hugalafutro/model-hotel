import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Eye, EyeOff } from "lucide-react";
import { type FormEvent, useState } from "react";
import { api } from "../../api/client";
import type { Provider } from "../../api/types";
import { Modal } from "../../components/Modal";
import {
	baseUrls,
	getProviderType,
	providerTypeAllowsEmptyKey,
	providerTypeDisplayNames,
} from "./constants";

interface AddProviderModalProps {
	onClose: () => void;
	onToast: (
		msg: string,
		type: "success" | "error" | "info" | "warning",
	) => void;
	settings: Record<string, string> | undefined;
	providers: Provider[] | undefined;
}

function generateProviderName(
	type: string,
	providers: Provider[] | undefined,
): string {
	const baseName = providerTypeDisplayNames[type] || "Provider";
	if (!providers) return baseName;
	const existingNames = new Set(providers.map((p) => p.name));
	if (!existingNames.has(baseName)) return baseName;
	let n = 2;
	while (existingNames.has(`${baseName} ${n}`)) n++;
	return `${baseName} ${n}`;
}

export function AddProviderModal({
	onClose,
	onToast,
	settings,
	providers,
}: AddProviderModalProps) {
	const queryClient = useQueryClient();
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
	const [error, setError] = useState<string | null>(null);

	const createMutation = useMutation({
		mutationFn: (data: { name: string; base_url: string; api_key: string }) =>
			api.providers.create(data),
		onSuccess: async (newProvider) => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			onClose();
			setFormData({
				name: "",
				base_url: "",
				api_key: "",
				provider_type: "custom",
			});
			setError(null);
			onToast(`Provider "${newProvider.name}" added`, "success");
			const shouldDiscover = settings?.discovery_on_provider_create !== "false";
			const providerType = getProviderType(newProvider.base_url);
			if (shouldDiscover) {
				try {
					const result = await api.providers.discover(newProvider.id);
					queryClient.invalidateQueries({ queryKey: ["models"] });
					queryClient.invalidateQueries({ queryKey: ["providers"] });
					onToast(
						`Discovered ${result.discovered} model${result.discovered === 1 ? "" : "s"} from ${newProvider.name}`,
						"success",
					);
				} catch (e) {
					onToast(
						`Auto-discovery failed: ${e instanceof Error ? e.message : "Unknown error"}`,
						"warning",
					);
				}
			}

			// Try to detect quota/balance for providers that support it
			try {
				switch (providerType) {
					case "nanogpt":
						await api.providers.getUsage(newProvider.id);
						onToast("NanoGPT quota detected", "info");
						queryClient.invalidateQueries({ queryKey: ["nanogpt-usage"] });
						break;
					case "zai-coding":
						await api.providers.getUsage(newProvider.id);
						onToast("Z.ai Coding quota detected", "info");
						queryClient.invalidateQueries({ queryKey: ["zai-coding-usage"] });
						break;
					case "deepseek": {
						const balance = await api.providers.getBalance(newProvider.id);
						const usd = balance.balance_infos.find((b) => b.currency === "USD");
						if (usd) {
							onToast(
								`DeepSeek balance detected: $${usd.total_balance}`,
								"info",
							);
						} else {
							onToast("DeepSeek balance detected", "info");
						}
						queryClient.invalidateQueries({ queryKey: ["deepseek-balance"] });
						break;
					}
					case "openrouter": {
						const orBalance = await api.providers.getOpenRouterBalance(
							newProvider.id,
						);
						onToast(
							`OpenRouter balance detected: $${orBalance.credits_remaining?.toFixed(2) ?? "-"}`,
							"info",
						);
						queryClient.invalidateQueries({ queryKey: ["openrouter-balance"] });
						break;
					}
					case "ollama-cloud": {
						const account = await api.providers.getOllamaCloudAccount(
							newProvider.id,
						);
						onToast(`Ollama Cloud ${account.plan} plan detected`, "info");
						queryClient.invalidateQueries({
							queryKey: ["ollama-cloud-account"],
						});
						break;
					}
				}
			} catch {
				// Quota/balance detection is non-critical; silently skip on failure
			}
		},
		onError: (err: Error) => {
			setError(err.message);
			onToast(`Failed to add provider: ${err.message}`, "error");
		},
	});

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
		const newName = generateProviderName(type, providers);
		setFormData((prev) => ({
			...prev,
			provider_type: type,
			base_url: baseUrls[type] || prev.base_url,
			name: newName,
		}));
	};

	const handleSubmit = (e: FormEvent) => {
		e.preventDefault();
		setError(null);
		createMutation.mutate({
			name: formData.name.trim(),
			base_url: formData.base_url,
			api_key: formData.api_key,
		});
	};

	const closeAndReset = () => {
		onClose();
		setFormData({
			name: "",
			base_url: "",
			api_key: "",
			provider_type: "custom",
		});
		setShowApiKey(false);
		setError(null);
	};

	return (
		<Modal title="Add Provider" onClose={closeAndReset}>
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
						<option value="ollama-cloud">Ollama Cloud</option>
						<option value="ollama">Ollama</option>
						<option value="koboldcpp">KoboldCPP</option>
						<option value="lmstudio">LM Studio</option>
						<option value="openai">OpenAI</option>
						<option value="opencode-go">OpenCode Go</option>
						<option value="opencode-zen">OpenCode Zen</option>
						<option value="openrouter">OpenRouter</option>
						<option value="xai">xAI (Grok)</option>
						<option value="zai-coding">Z.ai Coding Plan</option>
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
						Dots, spaces, and special characters are replaced with &quot;-&quot;
						when routing.
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
									? "Optional - free models work without a key"
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
						onClick={closeAndReset}
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
	);
}
