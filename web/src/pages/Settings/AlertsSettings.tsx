import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { Bell, ChevronDown, ChevronRight, RefreshCw } from "@/lib/icons";
import { ApiError, api } from "../../api/client";
import { ResetButton } from "../../components/ResetButton";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSlider } from "../../components/SettingsSlider";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../context/ToastContext";
import { AlertEventPicker } from "./AlertEventPicker";
import { AlertSnippets } from "./AlertSnippets";
import { AlertsWizard } from "./alerts/AlertsWizard";
import { stripApiHead } from "./alerts/apiText";
import { DestinationList } from "./alerts/DestinationList";
import { SETTING_DEFAULTS } from "./defaults";
import {
	invalidateAlertReads,
	useSettingsMutations,
} from "./useSettingsMutations";

// The stable failure codes the alert endpoints report: /api/alert/test with a
// 502 and the reachability probe in AlertStatus.reason. Anything outside this
// set falls back to a generic error, so no server internals reach the screen.
const REASON_CODES = new Set([
	"not_configured",
	"invalid_url",
	"unreachable",
	"unhealthy",
	"apprise_reject",
	"deliver_failed",
	"undecryptable",
]);

interface AlertsSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
	onResetSection?: () => void;
	managed?: boolean;
}

export function AlertsSettings({
	collapsed,
	onToggle,
	onResetSection,
	managed,
}: AlertsSettingsProps) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const { settings, updateMutation, resetSettingMutation, isResetting } =
		useSettingsMutations();

	const enabled = settings?.alert_enabled === "true";
	const apiUrl = settings?.alert_apprise_api_url ?? "";
	const targetConfigured = Boolean(settings?.alert_apprise_targets);

	const [apiUrlDraft, setApiUrlDraft] = useState<string | null>(null);
	// The typed target list while the operator is editing it, or null when the
	// field just shows what is stored. The settings row only ever carries the
	// "********" mask, so the readable value comes from the decrypted read below.
	const [targetDraft, setTargetDraft] = useState<string | null>(null);
	const [pickerOpen, setPickerOpen] = useState(false);
	// Which guided run is open, as the step it starts on; null when the dialog is
	// closed. Nothing is written until its own Finish, so closing it changes
	// nothing on this card.
	const [wizardStart, setWizardStart] = useState<1 | 2 | null>(null);
	// The picker is only "expanded" while alerting is on: when disabled the
	// panel is not rendered, so aria-expanded and the chevron must follow suit
	// (otherwise the toggle announces "expanded" with no region in the DOM).
	const pickerExpanded = pickerOpen && enabled;

	// The saved destinations, decrypted. Read whether or not alerting is on: the
	// readable list, the reason a stored value cannot be read, and the guided
	// entry point are all offered in both states.
	const targetsQuery = useQuery({
		queryKey: ["alert-targets"],
		queryFn: () => api.alert.targets(),
		refetchOnWindowFocus: false,
		// A stored value that cannot be decrypted fails the same way every time,
		// so a retry only delays the message that says so.
		retry: false,
	});
	// The event catalog, read with the card rather than on the click that opens
	// the wizard: the guided run seeds its recommended selection from it the
	// moment it mounts. It shares its cache key with the picker below, so the
	// two never ask for it twice.
	const eventsQuery = useQuery({
		queryKey: ["alert-events"],
		queryFn: () => api.alert.getEvents(),
		refetchOnWindowFocus: false,
	});
	const targets = targetsQuery.data?.targets ?? [];
	const storedTargets = targets.join("; ");
	// An unreadable stored value (master key rotated) is a message beside an
	// empty list, not a card that refuses to render.
	const targetsError: "" | "undecryptable" | "generic" = !targetsQuery.error
		? ""
		: targetsQuery.error instanceof ApiError &&
				targetsQuery.error.code === "undecryptable"
			? "undecryptable"
			: "generic";
	const targetsErrorText =
		targetsError === "undecryptable"
			? t("settings.alerts.destinations.error")
			: targetsError === "generic"
				? t("settings.alerts.destinations.readFailed")
				: "";

	// A validation error (400) carries a safe, user-facing message and a 502 from
	// the test endpoint carries a machine-readable reason code; anything else
	// (network, other 5xx, auth) is shown as a generic string so internals do not
	// leak. The apprise response body is never surfaced: it can echo target URLs.
	const describeError = (err: unknown) => {
		if (!(err instanceof ApiError)) return t("common.unknownError");
		// The toast supplies the "test failed" head itself from the testFailed
		// string, so the one fetchOK built is stripped off the sentence.
		if (err.status === 400) {
			return stripApiHead(err.message, "Test notification failed");
		}
		if (err.status === 502 && err.code && REASON_CODES.has(err.code)) {
			return t(`settings.alerts.reason.${err.code}`);
		}
		return t("common.unknownError");
	};

	const testFailedToast = (err: Error) =>
		toast(
			t("settings.alerts.testFailed", { message: describeError(err) }),
			"error",
		);

	const testMutation = useMutation({
		mutationFn: () => api.alert.test(),
		onSuccess: () => toast(t("settings.alerts.testSent"), "success"),
		onError: testFailedToast,
	});

	// Delivers to one saved destination only, so several phones can be told
	// apart. It tests what is stored, so nothing on screen is saved first.
	const rowTestMutation = useMutation({
		mutationFn: (url: string) => api.alert.test({ targets: [url] }),
		onSuccess: () => toast(t("settings.alerts.testSent"), "success"),
		onError: testFailedToast,
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

	// The field is the stored list in plain text, so a blur that changed nothing
	// writes nothing and an emptied field clears the destinations.
	const commitTarget = () => {
		if (targetDraft !== null && targetDraft.trim() !== storedTargets) {
			updateMutation.mutate({ alert_apprise_targets: targetDraft.trim() });
		}
		setTargetDraft(null);
	};

	const clearTarget = () => {
		updateMutation.mutate({ alert_apprise_targets: "" });
		setTargetDraft(null);
	};

	// Removing one destination persists the rest; an empty remaining list sends
	// "", which clears the setting.
	const removeDestination = (url: string) =>
		updateMutation.mutate({
			alert_apprise_targets: targets.filter((x) => x !== url).join("; "),
		});

	const busy =
		updateMutation.isPending ||
		testMutation.isPending ||
		rowTestMutation.isPending;

	// The manual field holds an unsaved edit, so the rows no longer describe what
	// is stored: testing or removing one would act on the stored list while the
	// operator is looking at a different one. Both row actions wait for a save.
	const targetsDirty =
		targetDraft !== null && targetDraft.trim() !== storedTargets;

	// Why the guided setup cannot be started right now. An unreadable list means
	// it cannot show what is already configured, and a pending manual edit means
	// it would show the list from before that edit; both are sorted out on the
	// card first, which is what the message points at. The event catalog is the
	// third: the run seeds its recommended selection from it once, when it
	// mounts, so starting without it would offer an empty preset and a "reset to
	// recommended" with nothing to reset to. Waiting is a moment; a failed read
	// is worth saying out loud, because reloading is what fixes it.
	const wizardBlocked =
		targetsError !== ""
			? targetsErrorText
			: targetsDirty
				? t("settings.alerts.destinations.dirty")
				: eventsQuery.isPending
					? t("settings.alerts.wizard.catalogLoading")
					: eventsQuery.isError
						? t("settings.alerts.wizard.catalogUnavailable")
						: undefined;

	const canTest = enabled && apiUrl !== "" && targetConfigured;

	// The ceiling for the outstanding-discrepancy threshold is served by the
	// backend (ClaimWindow in days, read-only) and is never a literal here. A
	// discrepancy stops counting once it is older than the claim window, so a
	// threshold at or above the window could never fire; the maximum is
	// therefore one day below it. Until settings load there is no honest
	// ceiling to draw, so the control waits rather than guessing one.
	const claimWindowDays = Number(settings?.discovery_claim_window_days);
	const maxClaimAlertDays =
		Number.isFinite(claimWindowDays) && claimWindowDays > 1
			? claimWindowDays - 1
			: null;
	// Clamped for DISPLAY as well as for saving. A value stored above the
	// ceiling (written through the API, restored from a backup, or carried in
	// by a config-sync import) must render as the number that is actually in
	// effect: a slider showing 45 while the backend acts on 29 is exactly the
	// kind of quiet disagreement this whole change exists to remove.
	// `||`, not `??`: a restored backup or hand-edited row can store an empty
	// string, which `??` only substitutes for null/undefined. `Number("")` is
	// 0, a value the Number.isFinite check below treats as a legitimate
	// (if out-of-range) stored number and clamps to the slider's minimum of 1
	// instead of falling back to the backend's actual default.
	const storedClaimAlertDays = Number(
		settings?.discovery_claim_alert_days ||
			SETTING_DEFAULTS.discovery_claim_alert_days,
	);
	const claimAlertDays =
		maxClaimAlertDays === null
			? 0
			: Math.min(
					maxClaimAlertDays,
					Math.max(
						1,
						Number.isFinite(storedClaimAlertDays)
							? storedClaimAlertDays
							: Number(SETTING_DEFAULTS.discovery_claim_alert_days),
					),
				);

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
	// The reason code is the translated, actionable half of the probe result; the
	// detail is raw server text (English, sometimes an HTTP status). The note
	// therefore carries the reason and keeps the detail as the tooltip, where an
	// operator who wants the literal answer can still find it.
	const statusReason =
		status &&
		(!status.reachable || !status.healthy) &&
		status.reason &&
		REASON_CODES.has(status.reason)
			? t(`settings.alerts.reason.${status.reason}`)
			: "";

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

				{managed && (
					<p data-testid="managed-note" className="text-xs text-(--text-muted)">
						{t("settings.managed.alertsNote")}
					</p>
				)}
				{/* Only alerting on/off and event routing are syncable; the Apprise
				    delivery settings below stay instance-local, so the disabled
				    fieldset wraps just this grid. */}
				<fieldset disabled={managed} className="m-0 min-w-0 border-0 p-0">
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
										onClick={() =>
											resetSettingMutation.mutate(["alert_enabled"])
										}
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
									aria-expanded={pickerExpanded}
									disabled={!enabled}
									data-testid="alert-picker-toggle"
								>
									{pickerExpanded ? (
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
							{pickerExpanded && (
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

					{/* Age threshold for discovery.claims_outstanding. Inside the
					    managed fieldset because config sync replicates this key, so a
					    managed member must not edit it locally. */}
					{maxClaimAlertDays !== null && (
						<div className="mt-5" data-testid="alert-claim-age">
							<SettingsSlider
								id="alert-claim-age-days"
								disabled={!enabled}
								label={t("settings.alerts.claimAge")}
								value={claimAlertDays}
								min={1}
								max={maxClaimAlertDays}
								step={1}
								unit="d"
								hideUnit
								onChange={(v) =>
									updateMutation.mutate({
										discovery_claim_alert_days: String(v),
									})
								}
								description={t("settings.alerts.claimAge.description", {
									count: maxClaimAlertDays,
								})}
								onReset={() =>
									resetSettingMutation.mutate(["discovery_claim_alert_days"])
								}
								resetTooltip={t("settings.common.resetSetting")}
							/>
						</div>
					)}
				</fieldset>

				{/* An unreadable destination list is what greys out the guided
				    button, and that button is offered whether or not alerting is
				    switched on. The reason therefore sits outside the toggle's block
				    too: a disabled button with no visible explanation is the one
				    state to avoid. */}
				{targetsErrorText !== "" && (
					<p
						className="ui-callout ui-callout-warning"
						data-testid="alert-destinations-error"
						role="alert"
					>
						{targetsErrorText}
					</p>
				)}

				{enabled && (
					<div className="space-y-1.5" data-testid="alert-destinations">
						{/* Whether apprise-api can be reached decides whether any of these
						    destinations can be delivered to, so the probe sits with the
						    list it qualifies and stays out of the collapsed manual block
						    that holds the address itself. */}
						<div className="flex flex-wrap items-center justify-between gap-2">
							<p className="text-sm font-medium text-(--text-secondary)">
								{t("settings.alerts.destinations.title")}
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
										<>
											<span
												className="inline-flex items-center gap-1.5 text-gray-300"
												title={status.detail}
											>
												<span
													className={`inline-block w-2 h-2 rounded-full ${statusDot}`}
													aria-hidden="true"
												/>
												{statusText}
											</span>
											{statusReason !== "" && (
												<span
													className="text-(--text-secondary)"
													data-testid="alert-status-note"
												>
													{statusReason}
												</span>
											)}
										</>
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
						<p className="text-xs text-(--text-muted)">
							{t("settings.alerts.destinations.note")}
						</p>
						<DestinationList
							targets={targets}
							onRemove={removeDestination}
							onTest={(url) => rowTestMutation.mutate(url)}
							busy={busy}
							disabledReason={
								targetsDirty
									? t("settings.alerts.destinations.dirty")
									: undefined
							}
						/>
					</div>
				)}

				{/* Exactly one guided entry point, chosen by whether anything is
				    stored. It is offered whether or not alerting is switched on,
				    because "Set up alerts" is what an operator looking at a
				    switched-off card is after, and the run switches it on itself. */}
				<div className="flex flex-wrap items-center gap-3">
					{targets.length === 0 ? (
						<button
							type="button"
							className="ui-btn ui-btn-primary"
							data-testid="alert-wizard-open"
							title={wizardBlocked}
							disabled={busy || wizardBlocked !== undefined}
							onClick={() => setWizardStart(1)}
						>
							{t("settings.alerts.wizard.open")}
						</button>
					) : (
						<button
							type="button"
							className="ui-btn ui-btn-primary"
							data-testid="alert-wizard-add"
							title={wizardBlocked}
							disabled={busy || wizardBlocked !== undefined}
							onClick={() => setWizardStart(2)}
						>
							{t("settings.alerts.wizard.addDestination")}
						</button>
					)}
					{/* Nothing to probe yet, so the status line above the list has
					    nothing to say. The hint takes its place and names both ways in:
					    it points at the manual block, so it waits until that block is
					    on screen. */}
					{enabled && apiUrl === "" && (
						<p
							className="text-xs text-(--text-muted)"
							data-testid="alert-status-hint"
						>
							{t("settings.alerts.statusNotConfiguredHint")}
						</p>
					)}
				</div>

				{/* Everything the guided run writes for you, kept reachable for the
				    operator who would rather type the Apprise URL themselves. */}
				{enabled && (
					<details data-testid="alert-manual">
						<summary className="text-sm font-medium text-(--text-secondary)">
							{t("settings.alerts.manualTitle")}
						</summary>
						<div className="space-y-5 mt-3">
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
										value={targetDraft ?? storedTargets}
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
											code: (
												<code className="font-mono text-(--text-primary)" />
											),
										}}
									/>
								</p>
							</div>

							{/* Test button + inline hint (beside, not below, to save a row) */}
							<div className="flex items-center gap-3">
								<button
									type="button"
									className="ui-btn ui-btn-secondary shrink-0"
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
						</div>
					</details>
				)}

				{wizardStart !== null && (
					<AlertsWizard
						initialApiUrl={apiUrl}
						savedTargets={targets}
						// An absent alert_events row is "nothing has been decided yet",
						// which the wizard answers with the recommended preset; a stored
						// blank is every event deliberately switched off.
						savedEvents={
							settings?.alert_events === undefined
								? null
								: settings.alert_events
						}
						catalog={eventsQuery.data ?? []}
						startAt={wizardStart}
						managed={managed}
						onClose={() => setWizardStart(null)}
						onFinished={() => {
							setWizardStart(null);
							// The run wrote settings behind this card's back, so both its
							// own copy and the reads derived from it are stale.
							queryClient.invalidateQueries({ queryKey: ["settings"] });
							invalidateAlertReads(queryClient);
							toast(t("settings.common.settingsSaved"), "success");
						}}
					/>
				)}
			</div>
		</SettingsSection>
	);
}
