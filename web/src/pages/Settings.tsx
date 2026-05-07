import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	Bell,
	Database,
	Gauge,
	LayoutDashboard,
	Monitor,
	Palette,
	ScrollText,
	Search,
	Settings as SettingsIcon,
	Sparkles,
	Terminal,
	Timer,
} from "lucide-react";
import type React from "react";
import { useCallback, useState } from "react";
import { HexColorPicker } from "react-colorful";
import { api } from "../api/client";
import { useCollapsible } from "../components/CollapsibleToggle";
import { LoadingSpinner } from "../components/LoadingSpinner";
import { Modal } from "../components/Modal";
import { PageHeader } from "../components/PageHeader";
import { SettingsSection } from "../components/SettingsSection";
import { Toggle } from "../components/Toggle";
import { useStorage } from "../context/StorageContext";
import { useTheme } from "../context/ThemeContext";
import { useToast } from "../context/ToastContext";
import { clearArenaHistory, getArenaHistoryCount } from "../utils/arenaHistory";

const DISCOVERY_INTERVALS = [
	{ value: "30m", label: "30 minutes" },
	{ value: "1h", label: "1 hour" },
	{ value: "6h", label: "6 hours" },
	{ value: "12h", label: "12 hours" },
	{ value: "24h", label: "24 hours" },
	{ value: "0", label: "Disabled" },
];

const UI_STYLES = [
	{
		id: "clean-saas" as const,
		label: "Clean SaaS",
		description: "Refined, professional, minimal",
		icon: Monitor,
	},
	{
		id: "cyber-terminal" as const,
		label: "Cyber Terminal",
		description: "Developer-centric, high-contrast",
		icon: Terminal,
	},
	{
		id: "glassmorphism-lite" as const,
		label: "Glassmorphism",
		description: "Slick, translucent surfaces",
		icon: Sparkles,
	},
];

function ColorPickerModal({
	color,
	onChange,
	onClose,
	onApply,
}: {
	color: string;
	onChange: (color: string) => void;
	onClose: () => void;
	onApply: () => void;
}) {
	return (
		<Modal title="Pick a Color" onClose={onClose} maxWidth="max-w-sm">
			<div className="flex flex-col items-center gap-4">
				<HexColorPicker
					color={color}
					onChange={onChange}
					style={{ width: "100%", height: 200 }}
				/>
				<div className="flex items-center gap-2 w-full">
					<span className="text-gray-400 text-sm font-mono">#</span>
					<input
						type="text"
						value={color.replace("#", "")}
						onChange={(e) => {
							const val = e.target.value.replace(/[^0-9a-fA-F]/g, "");
							if (val.length <= 6) {
								onChange(`#${val}`);
							}
						}}
						className="ui-input font-mono text-sm flex-1"
						maxLength={6}
					/>
					<div
						className="w-8 h-8 rounded-full border-2 border-gray-600 shrink-0"
						style={{ backgroundColor: color }}
					/>
				</div>
				<div className="flex gap-3 w-full">
					<button
						type="button"
						onClick={onClose}
						className="flex-1 px-3 py-2 rounded-lg text-sm font-medium bg-gray-700 text-gray-300 hover:bg-gray-600 transition-colors"
					>
						Cancel
					</button>
					<button
						type="button"
						onClick={onApply}
						className="flex-1 px-3 py-2 rounded-lg text-sm font-medium ui-btn ui-btn-primary"
					>
						Apply
					</button>
				</div>
			</div>
		</Modal>
	);
}

const PROVIDER_CACHE_KEYS = [
	"model-hotel:nanogpt-usage",
	"model-hotel:zai-coding-usage",
	"model-hotel:deepseek-balance",
];

function getProviderCacheCount(): number {
	let count = 0;
	for (const key of PROVIDER_CACHE_KEYS) {
		try {
			if (localStorage.getItem(key) !== null) count++;
		} catch {
			/* ignore */
		}
	}
	return count;
}

function clearProviderCache() {
	for (const key of PROVIDER_CACHE_KEYS) {
		try {
			localStorage.removeItem(key);
		} catch {
			/* ignore */
		}
	}
}

export function Settings() {
	const {
		theme,
		setTheme,
		uiStyle,
		setUIStyle,
		accentColor,
		setAccentColor,
		accentPresets,
	} = useTheme();
	const {
		toast,
		position: toastPosition,
		setPosition,
		timeout,
		setTimeout: setToastTimeout,
	} = useToast();
	const {
		persistChat,
		setPersistChat,
		persistArena,
		setPersistArena,
		persistConversation,
		setPersistConversation,
		arenaHistoryEnabled,
		setArenaHistoryEnabled,
		arenaHistoryLimit,
		setArenaHistoryLimit,
	} = useStorage();

	const queryClient = useQueryClient();
	const [pickerOpen, setPickerOpen] = useState(false);
	const [pickerColor, setPickerColor] = useState(accentColor);
	const [quotaDisabled, setQuotaDisabled] = useState(() => {
		try {
			return localStorage.getItem("sidebarQuotaDisabled") === "true";
		} catch {
			return false;
		}
	});
	const { collapsed: modelDiscoveryCollapsed, toggle: toggleModelDiscovery } =
		useCollapsible("settings_modelDiscoveryCollapsed");
	const { collapsed: appearanceCollapsed, toggle: toggleAppearance } =
		useCollapsible("settings_appearanceCollapsed");
	const { collapsed: toastCollapsed, toggle: toggleToast } = useCollapsible(
		"settings_toastCollapsed",
	);
	const { collapsed: sidebarQuotaCollapsed, toggle: toggleSidebarQuota } =
		useCollapsible("settings_sidebarQuotaCollapsed");
	const { collapsed: dashboardCollapsed, toggle: toggleDashboard } =
		useCollapsible("settings_dashboardCollapsed");
	const { collapsed: dataStorageCollapsed, toggle: toggleDataStorage } =
		useCollapsible("settings_dataStorageCollapsed");
	const { collapsed: loggingCollapsed, toggle: toggleLogging } = useCollapsible(
		"settings_loggingCollapsed",
	);
	const { collapsed: rateLimitCollapsed, toggle: toggleRateLimit } =
		useCollapsible("settings_rateLimitCollapsed");
	const { collapsed: proxyCollapsed, toggle: toggleProxy } = useCollapsible(
		"settings_proxyCollapsed",
	);

	const openPicker = useCallback(() => {
		setPickerColor(accentColor);
		setPickerOpen(true);
	}, [accentColor]);

	const applyPickerColor = useCallback(() => {
		setAccentColor(pickerColor);
		setPickerOpen(false);
	}, [pickerColor, setAccentColor]);

	const { data: settings, isLoading } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	const updateMutation = useMutation({
		mutationFn: (updates: Record<string, string>) =>
			api.settings.update(updates),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["settings"] });
			toast("Settings saved", "success");
		},
		onError: (err: Error) => {
			toast(`Failed to save: ${err.message}`, "error");
		},
	});

	const isUpdating = updateMutation.isPending;

	if (isLoading) {
		return <LoadingSpinner />;
	}

	const discoveryInterval = settings?.discovery_interval || "6h";
	const discoveryOnStartup = settings?.discovery_on_startup !== "false";
	const discoveryOnCreate = settings?.discovery_on_provider_create !== "false";

	return (
		<div className="space-y-8 max-w-5xl">
			<PageHeader
				icon={SettingsIcon}
				title="Settings"
				description="Configure your Model Hotel instance"
			/>

			<div className="space-y-6">
				{/* Model Discovery */}
				<SettingsSection
					icon={Search}
					title="Model Discovery"
					collapsed={modelDiscoveryCollapsed}
					onToggle={toggleModelDiscovery}
				>
					<div className="space-y-5">
						<p className="text-gray-400 text-sm">
							Configure how and when models are auto-discovered from your
							providers.
						</p>
						<div>
							<label
								htmlFor="discovery-interval"
								className="block text-sm font-medium text-gray-300 mb-2"
							>
								Discovery Interval
							</label>
							<select
								id="discovery-interval"
								value={discoveryInterval}
								onChange={(e) =>
									updateMutation.mutate({
										discovery_interval: e.target.value,
									})
								}
								className="ui-input"
								disabled={isUpdating}
							>
								{DISCOVERY_INTERVALS.map((opt) => (
									<option key={opt.value} value={opt.value}>
										{opt.label}
									</option>
								))}
							</select>
							{discoveryInterval === "0" ? (
								<p className="text-amber-400 text-xs mt-1">
									Periodic discovery is disabled. Models will only be discovered
									when you click "Discover Now" or "Discover All", or when a new
									provider is created.
								</p>
							) : (
								<p className="text-gray-500 text-xs mt-1">
									How often to automatically re-discover models from all enabled
									providers
								</p>
							)}
						</div>

						<div className="flex items-center justify-between">
							<div>
								<p className="text-sm font-medium text-gray-300">
									Discover on Startup
								</p>
								<p className="text-gray-500 text-xs mt-0.5">
									Run discovery for all providers when the server starts
								</p>
							</div>
							<Toggle
								checked={discoveryOnStartup}
								onChange={(v) =>
									updateMutation.mutate({
										discovery_on_startup: v ? "true" : "false",
									})
								}
								disabled={isUpdating}
							/>
						</div>

						<div className="flex items-center justify-between">
							<div>
								<p className="text-sm font-medium text-gray-300">
									Discover on Provider Creation
								</p>
								<p className="text-gray-500 text-xs mt-0.5">
									Automatically discover models when a new provider is added
								</p>
							</div>
							<Toggle
								checked={discoveryOnCreate}
								onChange={(v) =>
									updateMutation.mutate({
										discovery_on_provider_create: v ? "true" : "false",
									})
								}
								disabled={isUpdating}
							/>
						</div>
					</div>
				</SettingsSection>

				{/* Appearance */}
				<SettingsSection
					icon={Palette}
					title="Appearance"
					collapsed={appearanceCollapsed}
					onToggle={toggleAppearance}
				>
					<div className="space-y-6">
						{/* UI Style */}
						<div>
							<p className="text-sm font-medium text-gray-300 mb-3">UI Style</p>
							<div className="grid grid-cols-3 gap-3">
								{UI_STYLES.map((style) => {
									const Icon = style.icon;
									const active = uiStyle === style.id;
									return (
										<button
											key={style.id}
											type="button"
											onClick={() => setUIStyle(style.id)}
											className={`flex flex-col items-center gap-2 p-3 rounded-xl border transition-all ${
												active
													? "border-(--accent) bg-(--accent-lighter)"
													: "border-gray-700 hover:border-gray-600 bg-gray-800/50"
											}`}
										>
											<Icon
												size={20}
												className={active ? "text-(--accent)" : "text-gray-400"}
											/>
											<div className="text-center">
												<p
													className={`text-xs font-medium ${active ? "text-(--accent)" : "text-gray-300"}`}
												>
													{style.label}
												</p>
												<p className="text-[10px] text-gray-500 mt-0.5">
													{style.description}
												</p>
											</div>
										</button>
									);
								})}
							</div>
						</div>

						{/* Theme */}
						<div className="flex items-center justify-between">
							<div>
								<p className="text-sm font-medium text-gray-300">Theme</p>
								<p className="text-gray-500 text-xs mt-0.5">
									Switch between dark and light mode
								</p>
							</div>
							<div className="flex rounded-lg overflow-hidden border border-gray-600">
								<button
									type="button"
									onClick={() => setTheme("dark")}
									className={`px-4 py-2 text-sm font-medium transition-colors ${
										theme === "dark"
											? "bg-(--accent) text-white"
											: "bg-gray-700 text-gray-400 hover:bg-gray-600"
									}`}
								>
									Dark
								</button>
								<button
									type="button"
									onClick={() => setTheme("light")}
									className={`px-4 py-2 text-sm font-medium transition-colors ${
										theme === "light"
											? "bg-(--accent) text-white"
											: "bg-gray-700 text-gray-400 hover:bg-gray-600"
									}`}
								>
									Light
								</button>
							</div>
						</div>

						{/* Accent Color */}
						<div>
							<p className="text-sm font-medium text-gray-300 mb-2">
								Accent Color
							</p>
							<div className="flex flex-wrap gap-2">
								{accentPresets.map((preset) => (
									<button
										key={preset.name}
										type="button"
										onClick={() => setAccentColor(preset.color)}
										className={`w-8 h-8 rounded-full border-2 transition-transform hover:scale-110 ${
											accentColor === preset.color
												? "border-white scale-110"
												: "border-transparent"
										}`}
										style={{
											backgroundColor: preset.color,
										}}
										title={preset.name}
									/>
								))}
								<button
									type="button"
									onClick={openPicker}
									className={`w-8 h-8 rounded-full border-2 border-dashed border-gray-500 flex items-center justify-center hover:border-gray-400 transition-colors ${
										accentColor &&
										!accentPresets.some((p) => p.color === accentColor)
											? "bg-gray-800"
											: ""
									}`}
									title="Custom color"
								>
									{accentColor &&
									!accentPresets.some((p) => p.color === accentColor) ? (
										<div
											className="w-5 h-5 rounded-full"
											style={{
												backgroundColor: accentColor,
											}}
										/>
									) : (
										<svg
											className="w-4 h-4 text-gray-400"
											fill="none"
											stroke="currentColor"
											viewBox="0 0 24 24"
										>
											<title>Add</title>
											<path
												strokeLinecap="round"
												strokeLinejoin="round"
												strokeWidth={2}
												d="M12 4v16m8-8H4"
											/>
										</svg>
									)}
								</button>
							</div>
						</div>
					</div>
				</SettingsSection>

				{/* Toast Notifications */}
				<SettingsSection
					icon={Bell}
					title="Toast Notifications"
					collapsed={toastCollapsed}
					onToggle={toggleToast}
				>
					<p className="text-gray-400 text-sm mb-4">
						Choose where notification toasts appear and how long they stay
						visible.
					</p>
					<div className="flex justify-center">
						<div className="relative w-40 h-26 rounded-lg border-2 border-gray-600 bg-gray-800/50 overflow-visible">
							{/* top-left */}
							<button
								type="button"
								onClick={() => {
									setPosition("top-left");
									toast("Test notification - you'll see toasts here", "info");
								}}
								className={`absolute top-2 left-2 w-3 h-3 rounded-full transition-all ${
									toastPosition === "top-left"
										? "bg-(--accent) scale-125 ring-2 ring-white/40"
										: "bg-(--accent) opacity-30 hover:opacity-70"
								}`}
								title="Top Left"
							/>
							{/* top-center */}
							<button
								type="button"
								onClick={() => {
									setPosition("top-center");
									toast("Test notification - you'll see toasts here", "info");
								}}
								className={`absolute top-2 left-1/2 -translate-x-1/2 w-3 h-3 rounded-full transition-all ${
									toastPosition === "top-center"
										? "bg-(--accent) scale-125 ring-2 ring-white/40"
										: "bg-(--accent) opacity-30 hover:opacity-70"
								}`}
								title="Top Center"
							/>
							{/* top-right */}
							<button
								type="button"
								onClick={() => {
									setPosition("top-right");
									toast("Test notification - you'll see toasts here", "info");
								}}
								className={`absolute top-2 right-2 w-3 h-3 rounded-full transition-all ${
									toastPosition === "top-right"
										? "bg-(--accent) scale-125 ring-2 ring-white/40"
										: "bg-(--accent) opacity-30 hover:opacity-70"
								}`}
								title="Top Right"
							/>
							{/* bottom-left */}
							<button
								type="button"
								onClick={() => {
									setPosition("bottom-left");
									toast("Test notification - you'll see toasts here", "info");
								}}
								className={`absolute bottom-2 left-2 w-3 h-3 rounded-full transition-all ${
									toastPosition === "bottom-left"
										? "bg-(--accent) scale-125 ring-2 ring-white/40"
										: "bg-(--accent) opacity-30 hover:opacity-70"
								}`}
								title="Bottom Left"
							/>
							{/* bottom-center */}
							<button
								type="button"
								onClick={() => {
									setPosition("bottom-center");
									toast("Test notification - you'll see toasts here", "info");
								}}
								className={`absolute bottom-2 left-1/2 -translate-x-1/2 w-3 h-3 rounded-full transition-all ${
									toastPosition === "bottom-center"
										? "bg-(--accent) scale-125 ring-2 ring-white/40"
										: "bg-(--accent) opacity-30 hover:opacity-70"
								}`}
								title="Bottom Center"
							/>
							{/* bottom-right */}
							<button
								type="button"
								onClick={() => {
									setPosition("bottom-right");
									toast("Test notification - you'll see toasts here", "info");
								}}
								className={`absolute bottom-2 right-2 w-3 h-3 rounded-full transition-all ${
									toastPosition === "bottom-right"
										? "bg-(--accent) scale-125 ring-2 ring-white/40"
										: "bg-(--accent) opacity-30 hover:opacity-70"
								}`}
								title="Bottom Right"
							/>
						</div>
					</div>

					<p className="text-center text-gray-500 text-xs mt-4 capitalize">
						{toastPosition.replace("-", " ")}
					</p>

					{/* Toast Timeout */}
					<div className="mt-6">
						<div className="flex items-center justify-between mb-3">
							<span className="text-sm text-gray-300 font-medium">
								Auto-dismiss
							</span>
							<span className="text-sm text-gray-400 tabular-nums">
								{(timeout / 1000).toFixed(1)}s
							</span>
						</div>
						<input
							type="range"
							min={1000}
							max={15000}
							step={500}
							value={timeout}
							onChange={(e) => {
								setToastTimeout(Number(e.target.value));
							}}
							style={
								{
									"--slider-fill": `${((timeout - 1000) / (15000 - 1000)) * 100}%`,
								} as React.CSSProperties
							}
							className="toast-timeout-slider"
						/>
						<div className="flex justify-between text-xs text-gray-600 mt-1.5">
							<span>1s</span>
							<span>15s</span>
						</div>
					</div>
				</SettingsSection>

				{/* Sidebar Quota Refresh */}
				<SettingsSection
					icon={Timer}
					title="Sidebar Quotas"
					collapsed={sidebarQuotaCollapsed}
					onToggle={toggleSidebarQuota}
				>
					<div className="space-y-5">
						<p className="text-gray-400 text-sm">
							Configure how often provider quota and balance data is refreshed
							in the sidebar panel.
						</p>
						<div className="flex items-center justify-between">
							<div>
								<p className="text-sm font-medium text-gray-300">
									Show Quotas Pill
								</p>
								<p className="text-gray-500 text-xs mt-0.5">
									Display the quota panel in the sidebar
								</p>
							</div>
							<Toggle
								checked={!quotaDisabled}
								onChange={(v) => {
									const newVal = !v;
									setQuotaDisabled(newVal);
									try {
										localStorage.setItem(
											"sidebarQuotaDisabled",
											String(newVal),
										);
									} catch {
										/* ignore */
									}
									toast(
										newVal
											? "Sidebar quotas disabled - pill hidden and auto-refresh paused"
											: "Sidebar quotas enabled - pill visible and auto-refresh resumed",
										newVal ? "info" : "success",
									);
									window.dispatchEvent(new CustomEvent("sidebarQuotaToggle"));
								}}
							/>
						</div>
						<div>
							<label
								htmlFor="quota-refresh-interval"
								className="block text-sm font-medium text-gray-300 mb-2"
							>
								Refresh Interval
							</label>
							<select
								id="quota-refresh-interval"
								disabled={quotaDisabled}
								value={(() => {
									try {
										return (
											localStorage.getItem("sidebarQuotaRefreshMin") || "5"
										);
									} catch {
										return "5";
									}
								})()}
								onChange={(e) => {
									const val = e.target.value;
									try {
										localStorage.setItem("sidebarQuotaRefreshMin", val);
									} catch {
										/* ignore */
									}
									window.dispatchEvent(
										new CustomEvent("sidebarQuotaRefreshChange"),
									);
									toast(
										val === "0"
											? "Sidebar quota auto-refresh disabled - use manual refresh"
											: `Quota refresh set to every ${val} minute${val === "1" ? "" : "s"}`,
										"success",
									);
								}}
								className="ui-input disabled:opacity-50 disabled:cursor-not-allowed"
							>
								<option value="1">1 minute</option>
								<option value="2">2 minutes</option>
								<option value="5">5 minutes (default)</option>
								<option value="10">10 minutes</option>
								<option value="15">15 minutes</option>
								<option value="30">30 minutes</option>
								<option value="0">Disabled (manual only)</option>
							</select>
							<p className="text-gray-500 text-xs mt-1">
								Minimum 1 minute. Changes take effect on next scheduled refresh.
							</p>
						</div>
					</div>
				</SettingsSection>

				{/* Dashboard Refresh */}
				<SettingsSection
					icon={LayoutDashboard}
					title="Dashboard Refresh"
					collapsed={dashboardCollapsed}
					onToggle={toggleDashboard}
				>
					<div className="space-y-5">
						<p className="text-gray-400 text-sm">
							Configure how often the dashboard stats and charts are refreshed
							automatically. Manual refresh button is hidden when set to 10
							seconds or faster.
						</p>
						<div>
							<label
								htmlFor="dashboard-refresh-interval"
								className="block text-sm font-medium text-gray-300 mb-2"
							>
								Refresh Interval
							</label>
							<select
								id="dashboard-refresh-interval"
								value={(() => {
									try {
										return localStorage.getItem("dashboardRefreshSec") || "30";
									} catch {
										return "30";
									}
								})()}
								onChange={(e) => {
									const val = e.target.value;
									try {
										localStorage.setItem("dashboardRefreshSec", val);
									} catch {
										/* ignore */
									}
									window.dispatchEvent(
										new CustomEvent("dashboardRefreshChange"),
									);
									toast(
										val === "0"
											? "Dashboard auto-refresh disabled - use manual refresh"
											: `Dashboard refresh set to every ${val} second${val === "1" ? "" : "s"}`,
										"success",
									);
								}}
								className="ui-input"
							>
								<option value="10">10 seconds (manual refresh hidden)</option>
								<option value="30">30 seconds (default)</option>
								<option value="60">1 minute</option>
								<option value="120">2 minutes</option>
								<option value="300">5 minutes</option>
								<option value="600">10 minutes</option>
								<option value="0">Disabled (manual only)</option>
							</select>
							<p className="text-gray-500 text-xs mt-1">
								At 10 seconds the manual refresh button is hidden. Changes take
								effect on next navigation to the dashboard.
							</p>
						</div>
					</div>
				</SettingsSection>

				{/* Logging */}
				<LoggingSettings
					collapsed={loggingCollapsed}
					onToggle={toggleLogging}
				/>

				{/* Data Storage */}
				<SettingsSection
					icon={Database}
					title="Data Storage"
					collapsed={dataStorageCollapsed}
					onToggle={toggleDataStorage}
				>
					<div className="space-y-4">
						<p className="text-gray-400 text-sm">
							Manage browser-local session data. Persisted data survives page
							reloads and browser restarts.
						</p>

						{/* Session Persistence */}
						<div className="space-y-3">
							<h3 className="text-xs font-semibold uppercase tracking-wider text-gray-500">
								Session Persistence
							</h3>
							<div className="flex items-center justify-between">
								<div>
									<p className="text-sm font-medium text-gray-300">
										Persist Chat
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										Remember messages, prompt, and persona across sessions
									</p>
								</div>
								<Toggle
									checked={persistChat}
									onChange={(v) => {
										const next = v;
										if (
											!next &&
											!confirm(
												"This will clear all saved chat messages. Continue?",
											)
										)
											return;
										setPersistChat(next);
										toast(
											next ? "Chat persistence enabled" : "Chat data cleared",
											next ? "success" : "info",
										);
									}}
								/>
							</div>

							<div className="flex items-center justify-between">
								<div>
									<p className="text-sm font-medium text-gray-300">
										Persist Arena
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										Remember bracket state and prompts across sessions
									</p>
								</div>
								<Toggle
									checked={persistArena}
									onChange={(v) => {
										const next = v;
										if (
											!next &&
											!confirm(
												"This will clear all saved arena data. Continue?",
											)
										)
											return;
										setPersistArena(next);
										toast(
											next ? "Arena persistence enabled" : "Arena data cleared",
											next ? "success" : "info",
										);
									}}
								/>
							</div>

							<div className="flex items-center justify-between">
								<div>
									<p className="text-sm font-medium text-gray-300">
										Persist AI Conversation
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										Remember conversation state and settings across sessions
									</p>
								</div>
								<Toggle
									checked={persistConversation}
									onChange={(v) => {
										const next = v;
										if (
											!next &&
											!confirm(
												"This will clear all saved conversation data. Continue?",
											)
										)
											return;
										setPersistConversation(next);
										toast(
											next
												? "Conversation persistence enabled"
												: "Conversation data cleared",
											next ? "success" : "info",
										);
									}}
								/>
							</div>
						</div>

						{/* Arena History */}
						<div className="space-y-3">
							<h3 className="text-xs font-semibold uppercase tracking-wider text-gray-500">
								Arena History
							</h3>
							<div className="flex items-center justify-between">
								<div>
									<p className="text-sm font-medium text-gray-300">
										Save Match History
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										Automatically save completed arena and compare sessions
									</p>
								</div>
								<Toggle
									checked={arenaHistoryEnabled}
									onChange={(v) => {
										const next = v;
										setArenaHistoryEnabled(next);
										toast(
											next
												? "Arena history enabled"
												: "Arena history disabled - existing entries preserved",
											next ? "success" : "info",
										);
									}}
								/>
							</div>

							<div>
								<label
									htmlFor="history-limit"
									className="block text-sm font-medium text-gray-300 mb-2"
								>
									Maximum Saved Matches
								</label>
								<select
									id="history-limit"
									value={arenaHistoryLimit}
									onChange={(e) => {
										const val = Number(e.target.value);
										setArenaHistoryLimit(val);
										toast(`History limit set to ${val} matches`, "success");
									}}
									className="ui-input disabled:opacity-50 disabled:cursor-not-allowed"
									disabled={!arenaHistoryEnabled}
								>
									<option value={10}>10 matches</option>
									<option value={25}>25 matches (default)</option>
									<option value={50}>50 matches</option>
									<option value={100}>100 matches</option>
								</select>
								<p className="text-gray-500 text-xs mt-1">
									Oldest matches are automatically removed when the limit is
									reached
								</p>
							</div>

							<div className="flex items-center justify-between">
								<div>
									<p className="text-sm font-medium text-gray-300">
										Clear History
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{getArenaHistoryCount()} entr
										{getArenaHistoryCount() === 1 ? "y" : "ies"} stored
									</p>
								</div>
								<button
									type="button"
									onClick={() => {
										if (
											confirm(
												"Delete all arena history? This cannot be undone.",
											)
										) {
											clearArenaHistory();
											toast("All arena history cleared", "info");
										}
									}}
									className="ui-btn ui-btn-danger text-xs px-3 py-1.5"
									disabled={getArenaHistoryCount() === 0}
								>
									Clear All
								</button>
							</div>
						</div>

						{/* Cache & Resets */}
						<div className="space-y-3">
							<h3 className="text-xs font-semibold uppercase tracking-wider text-gray-500">
								Cache &amp; Resets
							</h3>
							<div className="flex items-center justify-between">
								<div>
									<p className="text-sm font-medium text-gray-300">
										Provider Quota Cache
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{getProviderCacheCount()} cached entr
										{getProviderCacheCount() === 1 ? "y" : "ies"} (NanoGPT, Z.ai
										Coding Plan, DeepSeek)
									</p>
								</div>
								<button
									type="button"
									onClick={() => {
										if (
											confirm(
												"Clear all cached provider quota data? Fresh data will be fetched on next refresh.",
											)
										) {
											clearProviderCache();
											toast("Provider cache cleared", "info");
										}
									}}
									className="ui-btn ui-btn-danger text-xs px-3 py-1.5"
									disabled={getProviderCacheCount() === 0}
								>
									Clear Cache
								</button>
							</div>

							<div className="flex items-center justify-between">
								<div>
									<p className="text-sm font-medium text-gray-300">
										Dismissed Error Banners
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										Reset dismissed sidebar error pill states
									</p>
								</div>
								<button
									type="button"
									onClick={() => {
										localStorage.removeItem("dismissedAppErrorKey");
										localStorage.removeItem("dismissedReqErrorKey");
										window.dispatchEvent(
											new CustomEvent("dismissedErrorsReset"),
										);
										toast("Dismissed error banners reset", "info");
									}}
									className="ui-btn ui-btn-danger text-xs px-3 py-1.5"
								>
									Reset
								</button>
							</div>
						</div>
					</div>
				</SettingsSection>

				{/* Rate Limiting */}
				<RateLimitSettings
					collapsed={rateLimitCollapsed}
					onToggle={toggleRateLimit}
				/>

				{/* Proxy */}
				<ProxySettings collapsed={proxyCollapsed} onToggle={toggleProxy} />
			</div>

			{pickerOpen && (
				<ColorPickerModal
					color={pickerColor}
					onChange={setPickerColor}
					onClose={() => setPickerOpen(false)}
					onApply={applyPickerColor}
				/>
			)}
		</div>
	);
}

const RATE_LIMIT_RPS_OPTIONS = [
	{ value: "5", label: "5 req/s" },
	{ value: "10", label: "10 req/s" },
	{ value: "20", label: "20 req/s" },
	{ value: "50", label: "50 req/s" },
	{ value: "100", label: "100 req/s" },
	{ value: "0", label: "Unlimited" },
];

const RATE_LIMIT_BURST_OPTIONS = [
	{ value: "10", label: "10" },
	{ value: "20", label: "20" },
	{ value: "50", label: "50" },
	{ value: "100", label: "100" },
	{ value: "200", label: "200" },
];

function RateLimitSettings({
	collapsed,
	onToggle,
}: {
	collapsed: boolean;
	onToggle: () => void;
}) {
	const { toast } = useToast();
	const queryClient = useQueryClient();

	const { data: settings } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	const updateMutation = useMutation({
		mutationFn: (updates: Record<string, string>) =>
			api.settings.update(updates),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["settings"] });
			toast("Settings saved", "success");
		},
		onError: (err: Error) => {
			toast(`Failed to save: ${err.message}`, "error");
		},
	});

	const rateLimitEnabled = settings?.rate_limit_enabled !== "false";
	const rateLimitRPS = settings?.rate_limit_rps || "10";
	const rateLimitBurst = settings?.rate_limit_burst || "20";

	return (
		<SettingsSection
			icon={Gauge}
			title="Rate Limiting"
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					Control request throughput per virtual key to prevent abuse and ensure
					fair usage.
				</p>
				<div className="flex items-center justify-between">
					<div>
						<p className="text-sm font-medium text-gray-300">
							Enable Rate Limiting
						</p>
						<p className="text-gray-500 text-xs mt-0.5">
							Throttle proxy requests per virtual key
						</p>
					</div>
					<Toggle
						checked={rateLimitEnabled}
						onChange={(v) =>
							updateMutation.mutate({
								rate_limit_enabled: v ? "true" : "false",
							})
						}
					/>
				</div>

				{rateLimitEnabled && (
					<>
						<div>
							<label
								htmlFor="rate-limit-rps"
								className="block text-sm font-medium text-gray-300 mb-2"
							>
								Requests per Second
							</label>
							<select
								id="rate-limit-rps"
								value={rateLimitRPS}
								onChange={(e) =>
									updateMutation.mutate({
										rate_limit_rps: e.target.value,
									})
								}
								className="ui-input"
							>
								{RATE_LIMIT_RPS_OPTIONS.map((opt) => (
									<option key={opt.value} value={opt.value}>
										{opt.label}
									</option>
								))}
							</select>
							<p className="text-gray-500 text-xs mt-1">
								Sustained request rate allowed per virtual key (0 = unlimited)
							</p>
						</div>

						<div>
							<label
								htmlFor="rate-limit-burst"
								className="block text-sm font-medium text-gray-300 mb-2"
							>
								Burst Size
							</label>
							<select
								id="rate-limit-burst"
								value={rateLimitBurst}
								onChange={(e) =>
									updateMutation.mutate({
										rate_limit_burst: e.target.value,
									})
								}
								className="ui-input"
							>
								{RATE_LIMIT_BURST_OPTIONS.map((opt) => (
									<option key={opt.value} value={opt.value}>
										{opt.label}
									</option>
								))}
							</select>
							<p className="text-gray-500 text-xs mt-1">
								Maximum number of simultaneous requests before throttling kicks
								in
							</p>
						</div>
					</>
				)}
			</div>
		</SettingsSection>
	);
}

function ProxySettings({
	collapsed,
	onToggle,
}: {
	collapsed: boolean;
	onToggle: () => void;
}) {
	const { toast } = useToast();
	const queryClient = useQueryClient();

	const { data: settings } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	const updateMutation = useMutation({
		mutationFn: (updates: Record<string, string>) =>
			api.settings.update(updates),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["settings"] });
			toast("Settings saved", "success");
		},
		onError: (err: Error) => {
			toast(`Failed to save: ${err.message}`, "error");
		},
	});

	const requestTimeout = settings?.request_timeout || "1m0s";

	return (
		<SettingsSection
			icon={Timer}
			title="Proxy"
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					Configure proxy request behavior and timeouts.
				</p>
				<div>
					<label
						htmlFor="request-timeout"
						className="block text-sm font-medium text-gray-300 mb-2"
					>
						Request Timeout
					</label>
					<select
						id="request-timeout"
						value={requestTimeout}
						onChange={(e) =>
							updateMutation.mutate({
								request_timeout: e.target.value,
							})
						}
						className="ui-input"
					>
						{REQUEST_TIMEOUT_OPTIONS.map((opt) => (
							<option key={opt.value} value={opt.value}>
								{opt.label}
							</option>
						))}
					</select>
					<p className="text-gray-500 text-xs mt-1">
						Maximum time for non-streaming requests before timing out. Streaming
						requests automatically get 10× this duration to accommodate
						thinking/reasoning models.
					</p>
				</div>
			</div>
		</SettingsSection>
	);
}

const LOG_RETENTION_OPTIONS = [
	{ value: "0", label: "Disabled" },
	{ value: "24h", label: "1 day" },
	{ value: "168h", label: "1 week" },
	{ value: "720h", label: "1 month" },
];

const STALE_REQUEST_TIMEOUT_OPTIONS = [
	{ value: "5m0s", label: "5 minutes" },
	{ value: "10m0s", label: "10 minutes" },
	{ value: "15m0s", label: "15 minutes" },
	{ value: "30m0s", label: "30 minutes (default)" },
	{ value: "1h0m0s", label: "1 hour" },
	{ value: "2h0m0s", label: "2 hours" },
	{ value: "0s", label: "Disabled (never mark as stale)" },
];

const REQUEST_TIMEOUT_OPTIONS = [
	{ value: "30s", label: "30 seconds" },
	{ value: "1m0s", label: "1 minute (default)" },
	{ value: "2m0s", label: "2 minutes" },
	{ value: "5m0s", label: "5 minutes" },
	{ value: "10m0s", label: "10 minutes" },
];

function LoggingSettings({
	collapsed,
	onToggle,
}: {
	collapsed: boolean;
	onToggle: () => void;
}) {
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const [confirmDelete, setConfirmDelete] = useState(false);
	const [deleteSelection, setDeleteSelection] = useState("");
	const [confirmDeleteAppLogs, setConfirmDeleteAppLogs] = useState(false);

	const { data: settings } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	const updateMutation = useMutation({
		mutationFn: (updates: Record<string, string>) =>
			api.settings.update(updates),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["settings"] });
			toast("Settings saved", "success");
		},
		onError: (err: Error) => {
			toast(`Failed to save: ${err.message}`, "error");
		},
	});

	const purgeMutation = useMutation({
		mutationFn: (olderThan: string) => api.logs.purge(olderThan),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["logs"] });
			toast("Requests deleted", "success");
			setConfirmDelete(false);
			setDeleteSelection("");
		},
		onError: (err: Error) => {
			toast(`Failed to delete requests: ${err.message}`, "error");
			setConfirmDelete(false);
		},
	});

	const purgeAppLogsMutation = useMutation({
		mutationFn: () => api.appLogs.purge(),
		onSuccess: (data) => {
			queryClient.invalidateQueries({ queryKey: ["appLogs"] });
			toast(`Deleted ${data.deleted} log entries`, "success");
			setConfirmDeleteAppLogs(false);
		},
		onError: (err: Error) => {
			toast(`Failed to delete app logs: ${err.message}`, "error");
			setConfirmDeleteAppLogs(false);
		},
	});

	const logRetention = settings?.log_retention || "0";
	const staleRequestTimeout = settings?.stale_request_timeout || "30m0s";

	const getDeleteOlderThan = (selection: string): string => {
		switch (selection) {
			case "1d":
				return "24h";
			case "1w":
				return "168h";
			case "1m":
				return "720h";
			case "all":
				return "all";
			default:
				return "";
		}
	};

	return (
		<SettingsSection
			icon={ScrollText}
			title="Logging"
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<div>
					<label
						htmlFor="log-retention"
						className="block text-sm font-medium text-gray-300 mb-2"
					>
						Log Retention
					</label>
					<select
						id="log-retention"
						value={logRetention}
						onChange={(e) =>
							updateMutation.mutate({
								log_retention: e.target.value,
							})
						}
						className="ui-input"
					>
						{LOG_RETENTION_OPTIONS.map((opt) => (
							<option key={opt.value} value={opt.value}>
								{opt.label}
							</option>
						))}
					</select>
					{logRetention === "0" ? (
						<p className="text-amber-400 text-xs mt-1">
							Log retention is disabled. Logs will accumulate indefinitely until
							manually purged.
						</p>
					) : (
						<p className="text-gray-500 text-xs mt-1">
							Automatically delete logs older than this period
						</p>
					)}
				</div>

				<div>
					<label
						htmlFor="stale-request-timeout"
						className="block text-sm font-medium text-gray-300 mb-2"
					>
						Stale Request Timeout
					</label>
					<select
						id="stale-request-timeout"
						value={staleRequestTimeout}
						onChange={(e) =>
							updateMutation.mutate({
								stale_request_timeout: e.target.value,
							})
						}
						className="ui-input"
					>
						{STALE_REQUEST_TIMEOUT_OPTIONS.map((opt) => (
							<option key={opt.value} value={opt.value}>
								{opt.label}
							</option>
						))}
					</select>
					{staleRequestTimeout === "0s" ? (
						<p className="text-amber-400 text-xs mt-1">
							Stale request detection is disabled. Orphaned requests from server
							restarts will still be marked as failed, but age-based cleanup
							will not run.
						</p>
					) : (
						<p className="text-gray-500 text-xs mt-1">
							Mark pending/streaming requests as &ldquo;interrupted&rdquo; if
							they remain in-progress longer than this. Accounts for providers
							with long time-to-first-token.
						</p>
					)}
				</div>

				<div>
					<div className="flex items-center justify-between">
						<div>
							{!confirmDelete ? (
								<button
									type="button"
									onClick={() => setConfirmDelete(true)}
									className="ui-btn ui-btn-danger"
								>
									Delete Requests
								</button>
							) : (
								<div className="flex items-center gap-2">
									<select
										value={deleteSelection}
										onChange={(e) => setDeleteSelection(e.target.value)}
										className="ui-input px-3 py-1.5 text-xs"
									>
										<option value="">Select range...</option>
										<option value="1d">Older than 1 day</option>
										<option value="1w">Older than 1 week</option>
										<option value="1m">Older than 1 month</option>
										<option value="all">All logs</option>
									</select>
									<button
										type="button"
										disabled={!deleteSelection}
										onClick={() => {
											const olderThan = getDeleteOlderThan(deleteSelection);
											if (olderThan) purgeMutation.mutate(olderThan);
										}}
										className="ui-btn ui-btn-danger disabled:opacity-50 disabled:cursor-not-allowed"
									>
										Confirm Delete
									</button>
									<button
										type="button"
										onClick={() => {
											setConfirmDelete(false);
											setDeleteSelection("");
										}}
										className="ui-btn ui-btn-secondary"
									>
										Cancel
									</button>
								</div>
							)}
						</div>
						<div>
							{!confirmDeleteAppLogs ? (
								<button
									type="button"
									onClick={() => setConfirmDeleteAppLogs(true)}
									className="ui-btn ui-btn-danger"
								>
									Delete Logs
								</button>
							) : (
								<div className="flex items-center gap-2">
									<span className="text-xs text-red-400">
										Clear all application logs?
									</span>
									<button
										type="button"
										onClick={() => purgeAppLogsMutation.mutate()}
										disabled={purgeAppLogsMutation.isPending}
										className="ui-btn ui-btn-danger disabled:opacity-50 disabled:cursor-not-allowed"
									>
										{purgeAppLogsMutation.isPending ? "Deleting…" : "Confirm"}
									</button>
									<button
										type="button"
										onClick={() => setConfirmDeleteAppLogs(false)}
										className="ui-btn ui-btn-secondary"
									>
										Cancel
									</button>
								</div>
							)}
						</div>
					</div>
				</div>
			</div>
		</SettingsSection>
	);
}
