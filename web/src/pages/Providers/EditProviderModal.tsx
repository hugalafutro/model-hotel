import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { CalendarDays, Eye, EyeOff } from "@/lib/icons";
import { api } from "../../api/client";
import type { Provider } from "../../api/types";
import { toISODate } from "../../components/AccentCalendar.utils";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { DatePickerPopover } from "../../components/DatePickerPopover";
import { FilterDropdown } from "../../components/FilterDropdown";
import { Modal } from "../../components/Modal";
import { Toggle } from "../../components/Toggle";
import { useRefreshDiscoveryBadge } from "../../hooks/useRefreshDiscoveryBadge";
import { formatDate } from "../../utils/format";
import { isKnownProviderUrl, providerTypeTranslationKeys } from "./constants";
import { findProviderAtAddress } from "./duplicateAddress";
import { providerTypeGateMessage } from "./typeGateError";

// parsedMaxInFlight turns the ceiling input's text into the API's three-state
// value: a number sets it, an empty box means "no ceiling" (null).
function parsedMaxInFlight(text: string): number | null {
	const n = Number.parseInt(text, 10);
	return Number.isNaN(n) ? null : n;
}

// Earliest schedulable day. Today is excluded because a same-day schedule is
// indistinguishable from disabling the provider outright.
function tomorrowISO(): string {
	const d = new Date();
	d.setDate(d.getDate() + 1);
	return toISODate(d);
}

export function EditProviderModal({
	provider,
	providers,
	onClose,
	onToast,
}: {
	provider: Provider;
	/** Every provider, so a URL edit can warn when it collides with another. */
	providers?: Provider[];
	onClose: () => void;
	onToast: (msg: string, type: "success" | "error" | "info") => void;
}) {
	const queryClient = useQueryClient();
	// Claims are read per enabled provider, so switching one off takes every
	// claim it owns out of the Models nav badge, and back on restores them.
	const refreshBadge = useRefreshDiscoveryBadge();
	const { t } = useTranslation();
	const [formData, setFormData] = useState({
		name: provider.name,
		provider_type: provider.provider_type,
		base_url: provider.base_url,
		api_key: "",
		enabled: provider.enabled,
		autodiscovery_enabled: provider.autodiscovery_enabled,
		scheduled_disable_on: provider.scheduled_disable_on,
		// Held as the input's text; "" means no ceiling.
		max_in_flight:
			provider.max_in_flight == null ? "" : String(provider.max_in_flight),
	});
	const [error, setError] = useState<string | null>(null);
	const [confirmFields, setConfirmFields] = useState<string[] | null>(null);
	const [showApiKey, setShowApiKey] = useState(false);
	const [pickerOpen, setPickerOpen] = useState(false);
	// The picked day is held aside until Apply, so dismissing the popover leaves
	// the form untouched.
	const [pendingDate, setPendingDate] = useState<string | null>(null);
	const scheduleRowRef = useRef<HTMLDivElement>(null);

	const updateMutation = useMutation({
		mutationFn: (data: {
			name?: string;
			provider_type?: string;
			base_url?: string;
			api_key?: string;
			enabled?: boolean;
			autodiscovery_enabled?: boolean;
			scheduled_disable_on?: string | null;
			max_in_flight?: number | null;
		}) => api.providers.update(provider.id, data),
		onSuccess: (updated: Provider) => {
			onToast(
				t("providers.toast_provider_updated", { name: updated.name }),
				"success",
			);
			onClose();
		},
		onError: (err: Error) => {
			// A new address that does not answer as the stored type is rejected;
			// say which server answered instead of surfacing the HTTP error.
			const message = providerTypeGateMessage(err, t) ?? err.message;
			setError(message);
			onToast(t("providers.toast_update_failed", { message }), "error");
		},
		onSettled: () => {
			// A rejected write can still have landed, so the provider list and the
			// badge are both re-read rather than inferred from a response that
			// never arrived.
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			refreshBadge();
		},
	});

	// Warns, never blocks: two providers on one address are legitimate when they
	// carry different API keys.
	const duplicateOf = findProviderAtAddress(
		providers,
		formData.base_url,
		provider.id,
	);

	const getChangedFields = (): string[] => {
		const fields: string[] = [];
		if (formData.name !== provider.name) fields.push("name");
		if (formData.provider_type !== provider.provider_type)
			fields.push("provider_type");
		if (formData.base_url !== provider.base_url) fields.push("base_url");
		if (formData.api_key !== "") fields.push("api_key");
		if (formData.enabled !== provider.enabled) fields.push("enabled");
		if (formData.autodiscovery_enabled !== provider.autodiscovery_enabled)
			fields.push("autodiscovery_enabled");
		if (
			(formData.scheduled_disable_on ?? null) !==
			(provider.scheduled_disable_on ?? null)
		)
			fields.push("scheduled_disable_on");
		if (parsedMaxInFlight(formData.max_in_flight) !== provider.max_in_flight)
			fields.push("max_in_flight");
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

	const handleSubmit = (e: React.SubmitEvent) => {
		e.preventDefault();
		setError(null);
		const payload: {
			name?: string;
			provider_type?: string;
			base_url?: string;
			api_key?: string;
			enabled?: boolean;
			autodiscovery_enabled?: boolean;
			scheduled_disable_on?: string | null;
			max_in_flight?: number | null;
		} = {};
		if (formData.name !== provider.name) payload.name = formData.name.trim();
		if (formData.provider_type !== provider.provider_type)
			payload.provider_type = formData.provider_type;
		if (formData.base_url !== provider.base_url)
			payload.base_url = formData.base_url;
		if (formData.api_key !== "") payload.api_key = formData.api_key;
		if (formData.enabled !== provider.enabled)
			payload.enabled = formData.enabled;
		if (formData.autodiscovery_enabled !== provider.autodiscovery_enabled)
			payload.autodiscovery_enabled = formData.autodiscovery_enabled;
		if (
			(formData.scheduled_disable_on ?? null) !==
			(provider.scheduled_disable_on ?? null)
		)
			payload.scheduled_disable_on = formData.scheduled_disable_on ?? null;
		if (parsedMaxInFlight(formData.max_in_flight) !== provider.max_in_flight)
			payload.max_in_flight = parsedMaxInFlight(formData.max_in_flight);
		updateMutation.mutate(payload);
	};

	return (
		<>
			<Modal title={t("providers.edit_modal_title")} onClose={handleClose}>
				{error && (
					<div
						data-testid="edit-provider-error"
						className="mb-4 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm"
					>
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
						<span className="block text-sm font-medium text-gray-300 mb-1">
							{t("providers.form_type_label")}
						</span>
						<FilterDropdown
							allowClear={false}
							className="w-full"
							placeholder={t("providers.form_type_label")}
							value={formData.provider_type}
							onChange={(type) =>
								setFormData({ ...formData, provider_type: type ?? "" })
							}
							options={Object.keys(providerTypeTranslationKeys)
								.sort((a, b) => {
									if (a === "custom") return -1;
									if (b === "custom") return 1;
									return t(providerTypeTranslationKeys[a]).localeCompare(
										t(providerTypeTranslationKeys[b]),
									);
								})
								.map((type) => ({
									value: type,
									label: t(providerTypeTranslationKeys[type]),
								}))}
						/>
						<p className="text-gray-500 text-xs mt-1">
							{t("providers.edit.typeHelper")}
						</p>
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
						{duplicateOf && (
							<p
								data-testid="duplicate-address-warning"
								className="text-amber-400 text-xs mt-1"
							>
								{t("providers.add.duplicateAddress", {
									name: duplicateOf.name,
								})}
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
						<p className="text-gray-500 text-xs mt-1">
							{t("providers.edit_api_key_current", {
								key: provider.masked_key,
							})}
						</p>
					</div>

					<div className="space-y-1" ref={scheduleRowRef}>
						<div className="flex items-center gap-3">
							<Toggle
								checked={formData.enabled}
								onChange={(v) => {
									// Switching off hides the schedule row and disables its
									// trigger, so an open picker closes with it. The keyboard
									// path fires no mousedown and so never reaches the
									// popover's own click-outside handler.
									if (!v) setPickerOpen(false);
									setFormData({
										...formData,
										enabled: v,
									});
								}}
								showFocusRing
								ariaLabel={t("providers.edit.enabledToggle")}
							/>
							<label
								htmlFor="edit-provider-enabled"
								className="text-sm font-medium text-gray-300"
							>
								{t("providers.edit_enabled_label")}
							</label>
							<button
								type="button"
								data-testid="schedule-disable-btn"
								data-popover-trigger="schedule-disable"
								disabled={!formData.enabled}
								onClick={() => {
									setPendingDate(formData.scheduled_disable_on);
									setPickerOpen((o) => !o);
								}}
								className="ui-icon-btn p-1 disabled:cursor-not-allowed"
								title={t("providers.schedule_disable_tooltip")}
								aria-label={t("providers.schedule_disable_tooltip")}
							>
								<CalendarDays size={16} />
							</button>
							{formData.enabled && formData.scheduled_disable_on && (
								<>
									<span
										data-testid="scheduled-disable-date"
										className="text-xs text-orange-400"
									>
										{formatDate(`${formData.scheduled_disable_on}T00:00:00`)}
									</span>
									<button
										type="button"
										data-testid="schedule-disable-cancel"
										onClick={() =>
											setFormData({ ...formData, scheduled_disable_on: null })
										}
										className="text-xs text-(--text-secondary) hover:text-(--text-primary) underline"
									>
										{t("providers.schedule_disable_cancel")}
									</button>
								</>
							)}
						</div>
						<p className="text-gray-500 text-xs ml-0">
							{t("providers.edit.enabledHelper")}
						</p>
					</div>
					{pickerOpen && (
						<DatePickerPopover
							value={pendingDate}
							minDate={tomorrowISO()}
							onSelect={(d) => setPendingDate(d)}
							onApply={() => {
								setFormData({ ...formData, scheduled_disable_on: pendingDate });
								setPickerOpen(false);
							}}
							onCancel={() => {
								setPendingDate(null);
								setPickerOpen(false);
							}}
							onClose={() => setPickerOpen(false)}
							triggerRef={scheduleRowRef}
						/>
					)}

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

					<div>
						<label
							htmlFor="edit-provider-max-in-flight"
							className="block text-sm font-medium text-gray-300 mb-1"
						>
							{t("providers.edit.maxInFlightLabel")}
						</label>
						<input
							id="edit-provider-max-in-flight"
							type="number"
							min={1}
							max={10000}
							value={formData.max_in_flight}
							onChange={(e) =>
								setFormData({ ...formData, max_in_flight: e.target.value })
							}
							className="ui-input"
							placeholder={t("providers.edit.maxInFlightPlaceholder")}
						/>
						<p className="text-gray-500 text-xs mt-1">
							{t("providers.edit.maxInFlightHelper")}
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
							className="ui-btn ui-btn-primary"
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
