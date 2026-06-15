import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { Eye, EyeOff } from "@/lib/icons";
import { api } from "../../api/client";
import type { Provider } from "../../api/types";
import { FilterDropdown } from "../../components/FilterDropdown";
import { Modal } from "../../components/Modal";
import {
	baseUrls,
	getProviderType,
	isLocalProviderType,
	localProviderDefaults,
	providerTypeAllowsEmptyKey,
	providerTypeHasFreeModels,
	providerTypeTranslationKeys,
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
	t: (key: string) => string,
): string {
	const baseName = t(
		providerTypeTranslationKeys[type] || "providers.add.providerFallback",
	);
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
	const { t } = useTranslation();
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
			onToast(
				t("providers.toast_provider_added", { name: newProvider.name }),
				"success",
			);
			const shouldDiscover = settings?.discovery_on_provider_create !== "false";
			const providerType = getProviderType(newProvider.base_url);
			if (shouldDiscover) {
				try {
					const result = await api.providers.discover(newProvider.id);
					queryClient.invalidateQueries({ queryKey: ["models"] });
					queryClient.invalidateQueries({ queryKey: ["providers"] });
					onToast(
						t("providers.add.discoveredModels", { count: result.discovered }),
						"success",
					);
				} catch (e) {
					onToast(
						t("providers.toast_discover_failed", {
							message:
								e instanceof Error ? e.message : t("common.unknownError"),
						}),
						"warning",
					);
				}
			}

			// Try to detect quota/balance for providers that support it
			try {
				switch (providerType) {
					case "nanogpt":
						await api.providers.getUsage(newProvider.id);
						onToast(t("providers.toast_quota_detected_nanogpt"), "info");
						queryClient.invalidateQueries({ queryKey: ["nanogpt-usage"] });
						break;
					case "zai-coding":
						await api.providers.getUsage(newProvider.id);
						onToast(t("providers.toast_quota_detected_zai"), "info");
						queryClient.invalidateQueries({ queryKey: ["zai-coding-usage"] });
						break;
					case "deepseek": {
						const balance = await api.providers.getBalance(newProvider.id);
						const usd = balance.balance_infos.find((b) => b.currency === "USD");
						if (usd) {
							onToast(
								t("providers.add.deepseekBalance", {
									balance: usd.total_balance,
								}),
								"info",
							);
						} else {
							onToast(t("providers.add.deepseekBalanceDetected"), "info");
						}
						queryClient.invalidateQueries({ queryKey: ["deepseek-balance"] });
						break;
					}
					case "openrouter": {
						const orBalance = await api.providers.getOpenRouterBalance(
							newProvider.id,
						);
						onToast(
							t("providers.add.openrouterBalance", {
								balance: orBalance.credits_remaining?.toFixed(2) ?? "-",
							}),
							"info",
						);
						queryClient.invalidateQueries({ queryKey: ["openrouter-balance"] });
						break;
					}
					case "ollama-cloud": {
						const account = await api.providers.getOllamaCloudAccount(
							newProvider.id,
						);
						onToast(
							t("providers.toast_ollama_cloud_plan_detected", {
								plan: account.plan,
							}),
							"info",
						);
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
			onToast(
				t("providers.toast_add_failed", { message: err.message }),
				"error",
			);
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
		const newName = generateProviderName(type, providers, t);
		setFormData((prev) => ({
			...prev,
			provider_type: type,
			base_url: localProviderDefaults[type] || baseUrls[type] || prev.base_url,
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
		<Modal title={t("providers.form_modal_title")} onClose={closeAndReset}>
			{error && (
				<div className="mb-4 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
					{error}
				</div>
			)}

			<form onSubmit={handleSubmit} className="space-y-4">
				<div>
					<span className="block text-sm font-medium text-gray-300 mb-1">
						{t("providers.form_type_label")}
					</span>
					<FilterDropdown
						allowClear={false}
						className="w-full"
						placeholder={t("providers.form_type_label")}
						value={formData.provider_type}
						onChange={handleProviderTypeChange}
						options={Object.keys(providerTypeTranslationKeys)
							.sort((aKey, bKey) => {
								if (aKey === "custom") return -1;
								if (bKey === "custom") return 1;
								return t(
									providerTypeTranslationKeys[aKey] || aKey,
								).localeCompare(t(providerTypeTranslationKeys[bKey] || bKey));
							})
							.map((key) => ({
								value: key,
								label: t(providerTypeTranslationKeys[key] || key),
							}))}
					/>
				</div>

				<div>
					<label
						htmlFor="provider-name"
						className="block text-sm font-medium text-gray-300 mb-1"
					>
						{t("providers.form_name_label")}
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
						placeholder={t("providers.form_name_placeholder")}
					/>
					<p className="text-gray-500 text-xs mt-1">
						{t("providers.form_name_hint")}
					</p>
				</div>

				<div>
					<label
						htmlFor="provider-base-url"
						className="block text-sm font-medium text-gray-300 mb-1"
					>
						{t("providers.add.baseUrl")}
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
						readOnly={
							formData.provider_type !== "custom" &&
							!isLocalProviderType(formData.provider_type)
						}
						className={
							formData.provider_type !== "custom" &&
							!isLocalProviderType(formData.provider_type)
								? "ui-input opacity-60 cursor-not-allowed"
								: "ui-input"
						}
						placeholder={t("providers.form_base_url_placeholder")}
					/>
					{formData.provider_type !== "custom" &&
						!isLocalProviderType(formData.provider_type) && (
							<p className="text-gray-500 text-xs mt-1">
								{t("providers.form_base_url_hint_preset")}
							</p>
						)}
					{isLocalProviderType(formData.provider_type) && (
						<p className="text-gray-500 text-xs mt-1">
							{t("providers.add.baseUrlHelperDefault")}
						</p>
					)}
					{formData.provider_type === "custom" && (
						<p className="text-gray-500 text-xs mt-1">
							{t("providers.add.baseUrlHelperFull")}
						</p>
					)}
				</div>

				<div>
					<label
						htmlFor="provider-api-key"
						className="block text-sm font-medium text-gray-300 mb-1"
					>
						{t("providers.add.apiKey")}
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
								providerTypeHasFreeModels(formData.provider_type)
									? t("providers.form_api_key_placeholder_optional")
									: t("providers.form_api_key_placeholder_required")
							}
						/>
						<button
							type="button"
							onClick={() => setShowApiKey(!showApiKey)}
							className="ui-icon-btn absolute right-3 top-1/2 -translate-y-1/2"
							tabIndex={-1}
							aria-label={
								showApiKey
									? t("providers.form_api_key_hide")
									: t("providers.form_api_key_show")
							}
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
						{t("common.cancel")}
					</button>
					<button
						type="submit"
						disabled={createMutation.isPending}
						className="ui-btn ui-btn-primary disabled:opacity-50"
					>
						{createMutation.isPending
							? t("providers.form_btn_adding")
							: t("providers.form_btn_add")}
					</button>
				</div>
			</form>
		</Modal>
	);
}
