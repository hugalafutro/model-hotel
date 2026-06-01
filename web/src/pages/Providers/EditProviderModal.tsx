import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Eye, EyeOff } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import type { Provider } from "../../api/types";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Modal } from "../../components/Modal";
import { Toggle } from "../../components/Toggle";
import { isKnownProviderUrl } from "./constants";

export function EditProviderModal({
	provider,
	onClose,
	onToast,
}: {
	provider: Provider;
	onClose: () => void;
	onToast: (msg: string, type: "success" | "error" | "info") => void;
}) {
	const queryClient = useQueryClient();
	const { t } = useTranslation();
	const [formData, setFormData] = useState({
		name: provider.name,
		base_url: provider.base_url,
		api_key: "",
		enabled: provider.enabled,
		autodiscovery_enabled: provider.autodiscovery_enabled,
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
			autodiscovery_enabled?: boolean;
		}) => api.providers.update(provider.id, data),
		onSuccess: (updated: Provider) => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			onToast(
				t("providers.toast_provider_updated", { name: updated.name }),
				"success",
			);
			onClose();
		},
		onError: (err: Error) => {
			setError(err.message);
			onToast(t("update_failed", { message: err.message }), "error");
		},
	});

	const getChangedFields = (): string[] => {
		const fields: string[] = [];
		if (formData.name !== provider.name) fields.push("name");
		if (formData.base_url !== provider.base_url) fields.push("base_url");
		if (formData.api_key !== "") fields.push("api_key");
		if (formData.enabled !== provider.enabled) fields.push("enabled");
		if (formData.autodiscovery_enabled !== provider.autodiscovery_enabled)
			fields.push("autodiscovery_enabled");
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
			autodiscovery_enabled?: boolean;
		} = {};
		if (formData.name !== provider.name) payload.name = formData.name.trim();
		if (formData.base_url !== provider.base_url)
			payload.base_url = formData.base_url;
		if (formData.api_key !== "") payload.api_key = formData.api_key;
		if (formData.enabled !== provider.enabled)
			payload.enabled = formData.enabled;
		if (formData.autodiscovery_enabled !== provider.autodiscovery_enabled)
			payload.autodiscovery_enabled = formData.autodiscovery_enabled;
		updateMutation.mutate(payload);
	};

	return (
		<>
			<Modal title={t("providers.edit_modal_title")} onClose={handleClose}>
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
							{t("providers.form_name_label")}
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
							placeholder={t("providers.form_name_placeholder")}
						/>
					</div>

					<div>
						<label
							htmlFor="edit-provider-base-url"
							className="block text-sm font-medium text-gray-300 mb-1"
						>
							{t("providers.form_base_url_label")}
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
								{t("providers.form_base_url_hint_preset")}
							</p>
						)}
					</div>

					<div>
						<label
							htmlFor="edit-provider-api-key"
							className="block text-sm font-medium text-gray-300 mb-1"
						>
							{t("providers.form_api_key_label")}
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
								placeholder={t("providers.edit_api_key_placeholder")}
							/>
							<button
								type="button"
								onClick={() => setShowApiKey(!showApiKey)}
								className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 transition-colors"
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
						<p className="text-gray-500 text-xs mt-1">
							{t("providers.edit_api_key_current", {
								key: provider.masked_key,
							})}
						</p>
					</div>

					<div className="space-y-1">
						<div className="flex items-center gap-3">
							<Toggle
								checked={formData.enabled}
								onChange={(v) =>
									setFormData({
										...formData,
										enabled: v,
									})
								}
								showFocusRing
								ariaLabel={t("providers.edit.enabledToggle")}
							/>
							<label
								htmlFor="edit-provider-enabled"
								className="text-sm font-medium text-gray-300"
							>
								{t("providers.edit_enabled_label")}
							</label>
						</div>
						<p className="text-gray-500 text-xs ml-0">
							{t("providers.edit.enabledHelper")}
						</p>
					</div>

					<div
						className={`space-y-3 ${!formData.enabled ? "opacity-40 pointer-events-none" : ""}`}
					>
						<div className="flex items-center gap-3">
							<Toggle
								checked={formData.autodiscovery_enabled}
								onChange={(v) =>
									setFormData({
										...formData,
										autodiscovery_enabled: v,
									})
								}
								showFocusRing
								ariaLabel={t("providers.edit.autodiscoveryToggle")}
								disabled={!formData.enabled}
							/>
							<label
								htmlFor="edit-provider-autodiscovery"
								className="text-sm font-medium text-gray-300"
							>
								{t("providers.edit_autodiscovery_label")}
							</label>
						</div>
						<p className="text-gray-500 text-xs ml-0">
							{t("providers.edit.autodiscoveryHelper")}
						</p>
					</div>

					<div className="flex space-x-3 justify-end pt-4">
						<button
							type="button"
							onClick={handleClose}
							className="ui-btn ui-btn-secondary"
						>
							{t("common.cancel")}
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
							{updateMutation.isPending
								? t("common.saving")
								: t("providers.form_btn_save")}
						</button>
					</div>
				</form>
			</Modal>
			{confirmFields && (
				<ConfirmDialog
					title={t("delete_confirm.unsaved_changes")}
					fields={confirmFields}
					onConfirm={onClose}
					onCancel={() => setConfirmFields(null)}
				/>
			)}
		</>
	);
}
