import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type SubmitEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { Eye, EyeOff } from "@/lib/icons";
import { api } from "../../api/client";
import type { Provider } from "../../api/types";
import { FilterDropdown } from "../../components/FilterDropdown";
import { Modal } from "../../components/Modal";
import { useRefreshDiscoveryBadge } from "../../hooks/useRefreshDiscoveryBadge";
import {
	baseUrls,
	isLocalProviderType,
	localProviderPlaceholders,
	providerTypeAllowsEmptyKey,
	providerTypeHasFreeModels,
	providerTypeTranslationKeys,
} from "./constants";
import { findProviderAtAddress } from "./duplicateAddress";
import { providerTypeGateMessage } from "./typeGateError";

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
	const refreshBadge = useRefreshDiscoveryBadge();
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
		mutationFn: (data: {
			name: string;
			base_url: string;
			provider_type: string;
			api_key: string;
		}) => api.providers.create(data),
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
			const providerType = newProvider.provider_type;
			if (shouldDiscover) {
				try {
					const result = await api.providers.discover(newProvider.id);
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
				} finally {
					// The new provider owns no claims of its own, but the scan re-syncs
					// failover, and an auto-disabled group IS a counted claim: fresh
					// members can lift one back over the routable floor and retire its
					// claim. Re-read here rather than only on success, since a scan that
					// errors partway has still upserted whatever it reached.
					queryClient.invalidateQueries({ queryKey: ["models"] });
					queryClient.invalidateQueries({ queryKey: ["providers"] });
					refreshBadge();
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
					case "kimi-code":
						await api.providers.getUsage(newProvider.id);
						onToast(t("providers.toast_quota_detected_kimi"), "info");
						queryClient.invalidateQueries({ queryKey: ["kimi-code-usage"] });
						break;
					case "minimax":
						await api.providers.getUsage(newProvider.id);
						onToast(t("providers.toast_quota_detected_minimax"), "info");
						queryClient.invalidateQueries({ queryKey: ["minimax-usage"] });
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
			// A failed type check or a refused address is the operator's mistake
			// to fix in this form, so both are phrased for them rather than
			// shown as a raw HTTP error.
			const message = providerTypeGateMessage(err, t) ?? err.message;
			setError(message);
			onToast(t("providers.toast_add_failed", { message }), "error");
		},
	});

	// Two providers may share a hosted API's address (each row carries its own
	// key, so each gets its own quota), which is worth a warning but not a
	// refusal. A self-hosted server has no such split and the backend refuses
	// it outright, so the warning says which of the two this is.
	const duplicateOf = findProviderAtAddress(providers, formData.base_url);
	const duplicateIsBlocked =
		duplicateOf !== null && isLocalProviderType(formData.provider_type);

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
			// Self-hosted servers get no pre-filled address: their host is the
			// operator's to know, and a wrong guess is worse than an empty field.
			base_url: isLocalProviderType(type)
				? ""
				: baseUrls[type] || prev.base_url,
			name: newName,
		}));
	};

	const handleSubmit = (e: SubmitEvent) => {
		e.preventDefault();
		setError(null);
		createMutation.mutate({
			name: formData.name.trim(),
			base_url: formData.base_url,
			provider_type: formData.provider_type,
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
				<div
					data-testid="add-provider-error"
					className="mb-4 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm"
				>
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
						placeholder={
							localProviderPlaceholders[formData.provider_type] ??
							t("providers.form_base_url_placeholder")
						}
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
					{duplicateOf && (
						<p
							data-testid="duplicate-address-warning"
							className="text-amber-400 text-xs mt-1"
						>
							{duplicateIsBlocked
								? t("providers.add.duplicateAddressBlocked", {
										name: duplicateOf.name,
									})
								: t("providers.add.duplicateAddress", {
										name: duplicateOf.name,
									})}
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
						className="ui-btn ui-btn-primary"
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
