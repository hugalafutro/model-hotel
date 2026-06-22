import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { Bell, ChevronDown, ChevronRight, RefreshCw } from "@/lib/icons";
import { api } from "../../api/client";
import { ResetButton } from "../../components/ResetButton";
import { SettingsSection } from "../../components/SettingsSection";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../context/ToastContext";
import { AlertEventPicker } from "./AlertEventPicker";
import { AlertSnippets } from "./AlertSnippets";
import { useSettingsMutations } from "./useSettingsMutations";

interface AlertsSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
	onResetSection?: () => void;
}

export function AlertsSettings({
	collapsed,
	onToggle,
	onResetSection,
}: AlertsSettingsProps) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const { settings, updateMutation, resetSettingMutation, isResetting } =
		useSettingsMutations();

	const enabled = settings?.alert_enabled === "true";
	const apiUrl = settings?.alert_apprise_api_url ?? "";
	const targetConfigured = Boolean(settings?.alert_apprise_targets);

	const [apiUrlDraft, setApiUrlDraft] = useState<string | null>(null);
	const [targetDraft, setTargetDraft] = useState("");
	const [pickerOpen, setPickerOpen] = useState(false);

	const testMutation = useMutation({
		mutationFn: () => api.alert.test(),
		onSuccess: () => toast(t("settings.alerts.testSent"), "success"),
		onError: (err: Error) =>
			toast(t("settings.alerts.testFailed", { message: err.message }), "error"),
	});

	// Probe apprise-api reachability. Keyed on the saved URL so it re-runs when
	// the operator changes it; disabled until alerting is on and a URL is set.
	const statusQuery = useQuery({
		queryKey: ["alert-status", apiUrl],
		queryFn: () => api.alert.status(),
		enabled: enabled && apiUrl !== "",
		refetchOnWindowFocus: false,
	});

	const commitApiUrl = () => {
		if (apiUrlDraft !== null && apiUrlDraft !== apiUrl) {
			updateMutation.mutate({ alert_apprise_api_url: apiUrlDraft });
		}
		setApiUrlDraft(null);
	};

	const commitTarget = () => {
		const v = targetDraft.trim();
		if (v !== "") {
			updateMutation.mutate({ alert_apprise_targets: v });
			setTargetDraft("");
		}
	};

	const clearTarget = () => {
		updateMutation.mutate({ alert_apprise_targets: "" });
		setTargetDraft("");
	};

	const canTest = enabled && apiUrl !== "" && targetConfigured;

	const status = statusQuery.data;
	const statusDot =
		status?.reachable && status.healthy
			? "bg-green-500"
			: status?.reachable
				? "bg-amber-500"
				: "bg-red-500";
	const statusText =
		status?.reachable && status.healthy
			? t("settings.alerts.status.reachable")
			: status?.reachable
				? t("settings.alerts.status.issues")
				: t("settings.alerts.status.unreachable");

	return (
		<SettingsSection
			icon={Bell}
			title={t("settings.alerts.title")}
			collapsed={collapsed}
			onToggle={onToggle}
			onResetSection={onResetSection}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					{t("settings.alerts.description")}
				</p>

				<div className="grid grid-cols-2 gap-x-6 gap-y-5 [align-items:start]">
					{/* Enable toggle */}
					<div className="flex items-center justify-between gap-3 ui-settings-group">
						<div className="min-w-0">
							<div className="flex items-center gap-1">
								<p className="text-sm font-medium text-gray-300">
									{t("settings.alerts.enable")}
								</p>
								<ResetButton
									tooltip={t("settings.common.resetSetting")}
									onClick={() => resetSettingMutation.mutate(["alert_enabled"])}
									size={12}
									disabled={isResetting}
								/>
							</div>
							<p className="text-gray-500 text-xs mt-0.5">
								{t("settings.alerts.enableDescription")}
							</p>
						</div>
						<Toggle
							checked={enabled}
							size="sm"
							onChange={(v) =>
								updateMutation.mutate({ alert_enabled: v ? "true" : "false" })
							}
							ariaLabel={t("settings.alerts.enable")}
						/>
					</div>

					{/* Events to notify on (right column). Kept visible but dimmed and
					    uninteractible until alerting is enabled, so the column is not
					    empty when off. */}
					<div
						className={`space-y-2${enabled ? "" : " opacity-50"}`}
						aria-disabled={!enabled}
					>
						<div className="flex items-center gap-1.5">
							<button
								type="button"
								className="flex items-center gap-1.5 text-sm font-medium text-gray-300"
								onClick={() => setPickerOpen((o) => !o)}
								aria-expanded={pickerOpen}
								disabled={!enabled}
								data-testid="alert-picker-toggle"
							>
								{pickerOpen ? (
									<ChevronDown size={14} />
								) : (
									<ChevronRight size={14} />
								)}
								{t("settings.alerts.events.title")}
							</button>
							<ResetButton
								tooltip={t("settings.alerts.events.reset")}
								onClick={() => resetSettingMutation.mutate(["alert_events"])}
								size={12}
								disabled={isResetting || !enabled}
							/>
						</div>
						{pickerOpen && enabled && (
							<div className="pl-5">
								<AlertEventPicker
									value={settings?.alert_events}
									onChange={(csv) =>
										updateMutation.mutate({ alert_events: csv })
									}
								/>
							</div>
						)}
					</div>
				</div>

				{enabled && (
					<>
						{/* apprise-api base URL */}
						<div className="space-y-1.5">
							<label
								htmlFor="alert-api-url"
								className="text-sm font-medium text-gray-300"
							>
								{t("settings.alerts.apiUrl")}
							</label>
							<input
								id="alert-api-url"
								type="text"
								value={apiUrlDraft ?? apiUrl}
								placeholder="http://apprise:8000"
								spellCheck={false}
								autoComplete="off"
								onChange={(e) => setApiUrlDraft(e.target.value)}
								onBlur={commitApiUrl}
								onKeyDown={(e) => {
									if (e.key === "Enter") e.currentTarget.blur();
								}}
								className="ui-input text-sm w-full"
								data-testid="alert-api-url-input"
							/>
							<p className="text-gray-500 text-xs">
								{t("settings.alerts.apiUrlDescription")}
							</p>
							{apiUrl !== "" && (
								<div
									className="flex items-center gap-2 text-xs"
									data-testid="alert-status"
								>
									{statusQuery.isFetching ? (
										<span className="inline-flex items-center gap-1.5 text-gray-400">
											<RefreshCw size={12} className="animate-spin" />
											{t("settings.alerts.status.checking")}
										</span>
									) : statusQuery.isError ? (
										<span className="inline-flex items-center gap-1.5 text-gray-300">
											<span
												className="inline-block w-2 h-2 rounded-full bg-red-500"
												aria-hidden="true"
											/>
											{t("settings.alerts.status.checkFailed")}
										</span>
									) : status ? (
										<span className="inline-flex items-center gap-1.5 text-gray-300">
											<span
												className={`inline-block w-2 h-2 rounded-full ${statusDot}`}
												aria-hidden="true"
											/>
											{statusText}
										</span>
									) : null}
									<button
										type="button"
										className="ui-link-accent inline-flex items-center gap-1"
										onClick={() => statusQuery.refetch()}
										data-testid="alert-status-recheck"
									>
										<RefreshCw size={11} />
										{t("settings.alerts.status.recheck")}
									</button>
								</div>
							)}
						</div>

						{/* Apprise target (encrypted secret) */}
						<div className="space-y-1.5">
							<label
								htmlFor="alert-target"
								className="text-sm font-medium text-gray-300"
							>
								{t("settings.alerts.target")}
							</label>
							<div className="flex items-center gap-2">
								<input
									id="alert-target"
									type="text"
									value={targetDraft}
									placeholder={
										targetConfigured
											? t("settings.alerts.targetConfigured")
											: "tgram://{bot_token}/{chat_id}"
									}
									spellCheck={false}
									autoComplete="off"
									onChange={(e) => setTargetDraft(e.target.value)}
									onBlur={commitTarget}
									onKeyDown={(e) => {
										if (e.key === "Enter") e.currentTarget.blur();
									}}
									className="ui-input text-sm w-full font-mono"
									data-testid="alert-target-input"
								/>
								{targetConfigured && (
									<button
										type="button"
										className="ui-link-accent text-xs whitespace-nowrap"
										onClick={clearTarget}
										data-testid="alert-target-clear"
									>
										{t("settings.alerts.clear")}
									</button>
								)}
							</div>
							<p className="text-gray-500 text-xs">
								{/* The ';' separator is rendered as a code token (same effect as
								    pg_dump in DB settings) so it doesn't read as ' ; ' literal. */}
								<Trans
									i18nKey="settings.alerts.targetDescription"
									components={{
										code: <code className="font-mono text-(--text-primary)" />,
									}}
								/>
							</p>
						</div>

						{/* Test button + inline hint (beside, not below, to save a row) */}
						<div className="flex items-center gap-3">
							<button
								type="button"
								className="ui-btn ui-btn-secondary text-sm shrink-0 disabled:opacity-50"
								disabled={!canTest || testMutation.isPending}
								onClick={() => testMutation.mutate()}
								data-testid="alert-test-button"
							>
								{testMutation.isPending
									? t("settings.alerts.testSending")
									: t("settings.alerts.testButton")}
							</button>
							{!canTest && (
								<p className="text-gray-500 text-xs">
									{t("settings.alerts.testHint")}
								</p>
							)}
						</div>

						{/* Service example snippets */}
						<AlertSnippets />
					</>
				)}
			</div>
		</SettingsSection>
	);
}
