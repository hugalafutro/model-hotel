import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	Bell,
	ChevronDown,
	ChevronUp,
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
	Zap,
} from "lucide-react";
import type React from "react";
import { useCallback, useState } from "react";
import { HexColorPicker } from "react-colorful";
import { api } from "../api/client";
import { Modal } from "../components/Modal";
import { Spinner } from "../components/Spinner";
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

function formatRelativeTime(dateStr: string): string {
	const date = new Date(dateStr);
	const now = new Date();
	const diffMs = now.getTime() - date.getTime();
	const diffMin = Math.floor(diffMs / 60000);
	if (diffMin < 1) return "just now";
	if (diffMin < 60) return `${diffMin}m ago`;
	const diffHr = Math.floor(diffMin / 60);
	if (diffHr < 24) return `${diffHr}h ago`;
	const diffDay = Math.floor(diffHr / 24);
	return `${diffDay}d ago`;
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
	const [modelDiscoveryCollapsed, setModelDiscoveryCollapsed] = useState(() => {
		try {
			return (
				localStorage.getItem("settings_modelDiscoveryCollapsed") === "true"
			);
		} catch {
			return false;
		}
	});
	const [appearanceCollapsed, setAppearanceCollapsed] = useState(() => {
		try {
			return localStorage.getItem("settings_appearanceCollapsed") === "true";
		} catch {
			return false;
		}
	});
	const [toastCollapsed, setToastCollapsed] = useState(() => {
		try {
			return localStorage.getItem("settings_toastCollapsed") === "true";
		} catch {
			return false;
		}
	});
	const [sidebarQuotaCollapsed, setSidebarQuotaCollapsed] = useState(() => {
		try {
			return localStorage.getItem("settings_sidebarQuotaCollapsed") === "true";
		} catch {
			return false;
		}
	});
	const [dashboardCollapsed, setDashboardCollapsed] = useState(() => {
		try {
			return localStorage.getItem("settings_dashboardCollapsed") === "true";
		} catch {
			return false;
		}
	});
	const [dataStorageCollapsed, setDataStorageCollapsed] = useState(() => {
		try {
			return localStorage.getItem("settings_dataStorageCollapsed") === "true";
		} catch {
			return false;
		}
	});
	const [discoveryStatusCollapsed, setDiscoveryStatusCollapsed] = useState(
		() => {
			try {
				return (
					localStorage.getItem("settings_discoveryStatusCollapsed") === "true"
				);
			} catch {
				return false;
			}
		},
	);
	const [loggingCollapsed, setLoggingCollapsed] = useState(() => {
		try {
			return localStorage.getItem("settings_loggingCollapsed") === "true";
		} catch {
			return false;
		}
	});
	const [rateLimitCollapsed, setRateLimitCollapsed] = useState(() => {
		try {
			return localStorage.getItem("settings_rateLimitCollapsed") === "true";
		} catch {
			return false;
		}
	});

	const toggleModelDiscovery = useCallback(() => {
		setModelDiscoveryCollapsed((prev) => {
			const next = !prev;
			try {
				localStorage.setItem("settings_modelDiscoveryCollapsed", String(next));
			} catch {
				/* ignore */
			}
			return next;
		});
	}, []);
	const toggleAppearance = useCallback(() => {
		setAppearanceCollapsed((prev) => {
			const next = !prev;
			try {
				localStorage.setItem("settings_appearanceCollapsed", String(next));
			} catch {
				/* ignore */
			}
			return next;
		});
	}, []);
	const toggleToast = useCallback(() => {
		setToastCollapsed((prev) => {
			const next = !prev;
			try {
				localStorage.setItem("settings_toastCollapsed", String(next));
			} catch {
				/* ignore */
			}
			return next;
		});
	}, []);
	const toggleSidebarQuota = useCallback(() => {
		setSidebarQuotaCollapsed((prev) => {
			const next = !prev;
			try {
				localStorage.setItem("settings_sidebarQuotaCollapsed", String(next));
			} catch {
				/* ignore */
			}
			return next;
		});
	}, []);
	const toggleDashboard = useCallback(() => {
		setDashboardCollapsed((prev) => {
			const next = !prev;
			try {
				localStorage.setItem("settings_dashboardCollapsed", String(next));
			} catch {
				/* ignore */
			}
			return next;
		});
	}, []);
	const toggleDataStorage = useCallback(() => {
		setDataStorageCollapsed((prev) => {
			const next = !prev;
			try {
				localStorage.setItem("settings_dataStorageCollapsed", String(next));
			} catch {
				/* ignore */
			}
			return next;
		});
	}, []);
	const toggleDiscoveryStatus = useCallback(() => {
		setDiscoveryStatusCollapsed((prev) => {
			const next = !prev;
			try {
				localStorage.setItem("settings_discoveryStatusCollapsed", String(next));
			} catch {
				/* ignore */
			}
			return next;
		});
	}, []);
	const toggleLogging = useCallback(() => {
		setLoggingCollapsed((prev) => {
			const next = !prev;
			try {
				localStorage.setItem("settings_loggingCollapsed", String(next));
			} catch {
				/* ignore */
			}
			return next;
		});
	}, []);
	const toggleRateLimit = useCallback(() => {
		setRateLimitCollapsed((prev) => {
			const next = !prev;
			try {
				localStorage.setItem("settings_rateLimitCollapsed", String(next));
			} catch {
				/* ignore */
			}
			return next;
		});
	}, []);

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
		return (
			<div className="flex items-center justify-center h-64">
				<div
					className="animate-spin rounded-full h-12 w-12 border-b-2"
					style={{ borderColor: "var(--accent)" }}
				></div>
			</div>
		);
	}

	const discoveryInterval = settings?.discovery_interval || "6h";
	const discoveryOnStartup = settings?.discovery_on_startup !== "false";
	const discoveryOnCreate = settings?.discovery_on_provider_create !== "false";

	return (
		<div className="space-y-8 max-w-5xl">
			<div>
				<div className="flex items-center gap-3">
					<SettingsIcon size={28} strokeWidth={2} className="text-(--accent)" />
					<h1 className="text-2xl font-bold text-white">Settings</h1>
				</div>
				<p className="text-gray-400">Configure your Model Hotel instance</p>
			</div>

			<div className="space-y-6">
				{/* Model Discovery */}
				<div className="ui-card p-6">
					<div className="flex items-center justify-between mb-1">
						<div className="flex items-center gap-2">
							<Search size={18} className="text-(--accent)" />
							<h2 className="text-xl font-semibold text-white">
								Model Discovery
							</h2>
						</div>
						<button
							type="button"
							onClick={toggleModelDiscovery}
							className="p-1.5 rounded-md transition-all cursor-pointer text-gray-400 hover:text-(--accent)"
						>
							{modelDiscoveryCollapsed ? (
								<ChevronDown size={16} />
							) : (
								<ChevronUp size={16} />
							)}
						</button>
					</div>
					<div
						className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${modelDiscoveryCollapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"}`}
					>
						<div className="overflow-hidden">
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
											Periodic discovery is disabled. Models will only be
											discovered when you click "Discover Now" or "Discover
											All", or when a new provider is created.
										</p>
									) : (
										<p className="text-gray-500 text-xs mt-1">
											How often to automatically re-discover models from all
											enabled providers
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
									<button
										type="button"
										onClick={() =>
											updateMutation.mutate({
												discovery_on_startup: discoveryOnStartup
													? "false"
													: "true",
											})
										}
										className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
											discoveryOnStartup ? "bg-(--accent)" : "bg-gray-600"
										}`}
										disabled={isUpdating}
									>
										<span
											className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
												discoveryOnStartup ? "translate-x-6" : "translate-x-1"
											}`}
										/>
									</button>
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
									<button
										type="button"
										onClick={() =>
											updateMutation.mutate({
												discovery_on_provider_create: discoveryOnCreate
													? "false"
													: "true",
											})
										}
										className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
											discoveryOnCreate ? "bg-(--accent)" : "bg-gray-600"
										}`}
										disabled={isUpdating}
									>
										<span
											className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
												discoveryOnCreate ? "translate-x-6" : "translate-x-1"
											}`}
										/>
									</button>
								</div>
							</div>
						</div>
					</div>
				</div>

				{/* Discovery Status */}
				<div className="ui-card p-6">
					<ProviderDiscoveryList
						collapsed={discoveryStatusCollapsed}
						onToggle={toggleDiscoveryStatus}
					/>
				</div>

				{/* Appearance */}
				<div className="ui-card p-6">
					<div className="flex items-center justify-between mb-1">
						<div className="flex items-center gap-2">
							<Palette size={18} className="text-(--accent)" />
							<h2 className="text-xl font-semibold text-white">Appearance</h2>
						</div>
						<button
							type="button"
							onClick={toggleAppearance}
							className="p-1.5 rounded-md transition-all cursor-pointer text-gray-400 hover:text-(--accent)"
						>
							{appearanceCollapsed ? (
								<ChevronDown size={16} />
							) : (
								<ChevronUp size={16} />
							)}
						</button>
					</div>
					<div
						className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${appearanceCollapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"}`}
					>
						<div className="overflow-hidden">
							<div className="space-y-6">
								{/* UI Style */}
								<div>
									<p className="text-sm font-medium text-gray-300 mb-3">
										UI Style
									</p>
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
														className={
															active ? "text-(--accent)" : "text-gray-400"
														}
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
						</div>
					</div>
				</div>

				{/* Toast Notifications */}
				<div className="ui-card p-6">
					<div className="flex items-center justify-between mb-1">
						<div className="flex items-center gap-2">
							<Bell size={18} className="text-(--accent)" />
							<h2 className="text-xl font-semibold text-white">
								Toast Notifications
							</h2>
						</div>
						<button
							type="button"
							onClick={toggleToast}
							className="p-1.5 rounded-md transition-all cursor-pointer text-gray-400 hover:text-(--accent)"
						>
							{toastCollapsed ? (
								<ChevronDown size={16} />
							) : (
								<ChevronUp size={16} />
							)}
						</button>
					</div>
					<div
						className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${toastCollapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"}`}
					>
						<div className="overflow-hidden">
							<p className="text-gray-400 text-sm mb-4">
								Choose where notification toasts appear and how long they stay
								visible.
							</p>
							<div className="flex justify-center">
								<div className="relative w-40 h-26 rounded-lg border-2 border-gray-600 bg-gray-800/50">
									{/* top-left */}
									<button
										type="button"
										onClick={() => {
											setPosition("top-left");
											toast(
												"Test notification — you'll see toasts here",
												"info",
											);
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
											toast(
												"Test notification — you'll see toasts here",
												"info",
											);
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
											toast(
												"Test notification — you'll see toasts here",
												"info",
											);
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
											toast(
												"Test notification — you'll see toasts here",
												"info",
											);
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
											toast(
												"Test notification — you'll see toasts here",
												"info",
											);
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
											toast(
												"Test notification — you'll see toasts here",
												"info",
											);
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
						</div>
					</div>
				</div>

				{/* Sidebar Quota Refresh */}
				<div className="ui-card p-6">
					<div className="flex items-center justify-between mb-1">
						<div className="flex items-center gap-2">
							<Timer size={18} className="text-(--accent)" />
							<h2 className="text-xl font-semibold text-white">
								Sidebar Quotas
							</h2>
						</div>
						<button
							type="button"
							onClick={toggleSidebarQuota}
							className="p-1.5 rounded-md transition-all cursor-pointer text-gray-400 hover:text-(--accent)"
						>
							{sidebarQuotaCollapsed ? (
								<ChevronDown size={16} />
							) : (
								<ChevronUp size={16} />
							)}
						</button>
					</div>
					<div
						className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${sidebarQuotaCollapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"}`}
					>
						<div className="overflow-hidden">
							<div className="space-y-5">
								<p className="text-gray-400 text-sm">
									Configure how often provider quota and balance data is
									refreshed in the sidebar panel.
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
									<button
										type="button"
										onClick={() => {
											const newVal = !quotaDisabled;
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
													? "Sidebar quotas disabled — pill hidden and auto-refresh paused"
													: "Sidebar quotas enabled — pill visible and auto-refresh resumed",
												newVal ? "info" : "success",
											);
											window.dispatchEvent(
												new CustomEvent("sidebarQuotaToggle"),
											);
										}}
										className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
											quotaDisabled ? "bg-gray-600" : "bg-(--accent)"
										}`}
									>
										<span
											className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
												quotaDisabled ? "translate-x-1" : "translate-x-6"
											}`}
										/>
									</button>
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
													? "Sidebar quota auto-refresh disabled — use manual refresh"
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
										Minimum 1 minute. Changes take effect on next scheduled
										refresh.
									</p>
								</div>
							</div>
						</div>
					</div>
				</div>

				{/* Dashboard Refresh */}
				<div className="ui-card p-6">
					<div className="flex items-center justify-between mb-1">
						<div className="flex items-center gap-2">
							<LayoutDashboard size={18} className="text-(--accent)" />
							<h2 className="text-xl font-semibold text-white">
								Dashboard Refresh
							</h2>
						</div>
						<button
							type="button"
							onClick={toggleDashboard}
							className="p-1.5 rounded-md transition-all cursor-pointer text-gray-400 hover:text-(--accent)"
						>
							{dashboardCollapsed ? (
								<ChevronDown size={16} />
							) : (
								<ChevronUp size={16} />
							)}
						</button>
					</div>
					<div
						className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${dashboardCollapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"}`}
					>
						<div className="overflow-hidden">
							<div className="space-y-5">
								<p className="text-gray-400 text-sm">
									Configure how often the dashboard stats and charts are
									refreshed automatically. Manual refresh button is hidden when
									set to 10 seconds or faster.
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
												return (
													localStorage.getItem("dashboardRefreshSec") || "30"
												);
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
													? "Dashboard auto-refresh disabled — use manual refresh"
													: `Dashboard refresh set to every ${val} second${val === "1" ? "" : "s"}`,
												"success",
											);
										}}
										className="ui-input"
									>
										<option value="10">
											10 seconds (manual refresh hidden)
										</option>
										<option value="30">30 seconds (default)</option>
										<option value="60">1 minute</option>
										<option value="120">2 minutes</option>
										<option value="300">5 minutes</option>
										<option value="600">10 minutes</option>
										<option value="0">Disabled (manual only)</option>
									</select>
									<p className="text-gray-500 text-xs mt-1">
										At 10 seconds the manual refresh button is hidden. Changes
										take effect on next navigation to the dashboard.
									</p>
								</div>
							</div>
						</div>
					</div>
				</div>

				{/* Logging */}
				<LoggingSettings
					collapsed={loggingCollapsed}
					onToggle={toggleLogging}
				/>

				{/* Data Storage */}
				<div className="ui-card p-6">
					<div className="flex items-center justify-between mb-1">
						<div className="flex items-center gap-2">
							<Database size={18} className="text-(--accent)" />
							<h2 className="text-xl font-semibold text-white">Data Storage</h2>
						</div>
						<button
							type="button"
							onClick={toggleDataStorage}
							className="p-1.5 rounded-md transition-all cursor-pointer text-gray-400 hover:text-(--accent)"
						>
							{dataStorageCollapsed ? (
								<ChevronDown size={16} />
							) : (
								<ChevronUp size={16} />
							)}
						</button>
					</div>
					<div
						className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${dataStorageCollapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"}`}
					>
						<div className="overflow-hidden">
							<div className="space-y-4">
								<p className="text-gray-400 text-sm">
									Manage browser-local session data. Persisted data survives
									page reloads and browser restarts.
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
										<button
											type="button"
											onClick={() => {
												const next = !persistChat;
												if (
													!next &&
													!confirm(
														"This will clear all saved chat messages. Continue?",
													)
												)
													return;
												setPersistChat(next);
												toast(
													next
														? "Chat persistence enabled"
														: "Chat data cleared",
													next ? "success" : "info",
												);
											}}
											className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
												persistChat ? "bg-(--accent)" : "bg-gray-600"
											}`}
										>
											<span
												className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
													persistChat ? "translate-x-6" : "translate-x-1"
												}`}
											/>
										</button>
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
										<button
											type="button"
											onClick={() => {
												const next = !persistArena;
												if (
													!next &&
													!confirm(
														"This will clear all saved arena data. Continue?",
													)
												)
													return;
												setPersistArena(next);
												toast(
													next
														? "Arena persistence enabled"
														: "Arena data cleared",
													next ? "success" : "info",
												);
											}}
											className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
												persistArena ? "bg-(--accent)" : "bg-gray-600"
											}`}
										>
											<span
												className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
													persistArena ? "translate-x-6" : "translate-x-1"
												}`}
											/>
										</button>
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
										<button
											type="button"
											onClick={() => {
												const next = !persistConversation;
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
											className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
												persistConversation ? "bg-(--accent)" : "bg-gray-600"
											}`}
										>
											<span
												className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
													persistConversation
														? "translate-x-6"
														: "translate-x-1"
												}`}
											/>
										</button>
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
										<button
											type="button"
											onClick={() => {
												const next = !arenaHistoryEnabled;
												setArenaHistoryEnabled(next);
												toast(
													next
														? "Arena history enabled"
														: "Arena history disabled — existing entries preserved",
													next ? "success" : "info",
												);
											}}
											className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
												arenaHistoryEnabled ? "bg-(--accent)" : "bg-gray-600"
											}`}
										>
											<span
												className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
													arenaHistoryEnabled
														? "translate-x-6"
														: "translate-x-1"
												}`}
											/>
										</button>
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
												{getProviderCacheCount() === 1 ? "y" : "ies"} (NanoGPT,
												Z.ai Coding Plan, DeepSeek)
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
						</div>
					</div>
				</div>

				{/* Rate Limiting */}
				<RateLimitSettings
					collapsed={rateLimitCollapsed}
					onToggle={toggleRateLimit}
				/>
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
		<div className="ui-card p-6">
			<div className="flex items-center justify-between mb-1">
				<div className="flex items-center gap-2">
					<Gauge size={18} className="text-(--accent)" />
					<h2 className="text-xl font-semibold text-white">Rate Limiting</h2>
				</div>
				<button
					type="button"
					onClick={onToggle}
					className="p-1.5 rounded-md transition-all cursor-pointer text-gray-400 hover:text-(--accent)"
				>
					{collapsed ? <ChevronDown size={16} /> : <ChevronUp size={16} />}
				</button>
			</div>
			<div
				className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${collapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"}`}
			>
				<div className="overflow-hidden">
					<div className="space-y-5">
						<p className="text-gray-400 text-sm">
							Control request throughput per virtual key to prevent abuse and
							ensure fair usage.
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
							<button
								type="button"
								onClick={() =>
									updateMutation.mutate({
										rate_limit_enabled: rateLimitEnabled ? "false" : "true",
									})
								}
								className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
									rateLimitEnabled ? "bg-(--accent)" : "bg-gray-600"
								}`}
							>
								<span
									className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
										rateLimitEnabled ? "translate-x-6" : "translate-x-1"
									}`}
								/>
							</button>
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
										Sustained request rate allowed per virtual key (0 =
										unlimited)
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
										Maximum number of simultaneous requests before throttling
										kicks in
									</p>
								</div>
							</>
						)}
					</div>
				</div>
			</div>
		</div>
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
		<div className="ui-card p-6">
			<div className="flex items-center justify-between mb-1">
				<div className="flex items-center gap-2">
					<ScrollText size={18} className="text-(--accent)" />
					<h2 className="text-xl font-semibold text-white">Logging</h2>
				</div>
				<button
					type="button"
					onClick={onToggle}
					className="p-1.5 rounded-md transition-all cursor-pointer text-gray-400 hover:text-(--accent)"
				>
					{collapsed ? <ChevronDown size={16} /> : <ChevronUp size={16} />}
				</button>
			</div>
			<div
				className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${collapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"}`}
			>
				<div className="overflow-hidden">
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
									Log retention is disabled. Logs will accumulate indefinitely
									until manually purged.
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
									Stale request detection is disabled. Orphaned requests from
									server restarts will still be marked as failed, but age-based
									cleanup will not run.
								</p>
							) : (
								<p className="text-gray-500 text-xs mt-1">
									Mark pending/streaming requests as &ldquo;interrupted&rdquo;
									if they remain in-progress longer than this. Accounts for
									providers with long time-to-first-token.
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
											className="px-3 py-1.5 text-xs rounded-full border bg-red-900/40 text-red-300 border-red-700/50 cursor-pointer hover:brightness-125 transition-all"
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
												className="px-3 py-1.5 text-xs rounded-full border bg-red-900/50 text-red-400 border-red-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(239,68,68,0.2)] transition-all disabled:opacity-50 disabled:cursor-not-allowed"
											>
												Confirm Delete
											</button>
											<button
												type="button"
												onClick={() => {
													setConfirmDelete(false);
													setDeleteSelection("");
												}}
												className="px-3 py-1.5 text-xs rounded-full border bg-gray-900/40 text-gray-300 border-gray-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(156,163,175,0.15)] transition-all"
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
											className="px-3 py-1.5 text-xs rounded-full border bg-red-900/40 text-red-300 border-red-700/50 cursor-pointer hover:brightness-125 transition-all"
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
												className="px-3 py-1.5 text-xs rounded-full border bg-red-900/50 text-red-400 border-red-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(239,68,68,0.2)] transition-all disabled:opacity-50 disabled:cursor-not-allowed"
											>
												{purgeAppLogsMutation.isPending
													? "Deleting…"
													: "Confirm"}
											</button>
											<button
												type="button"
												onClick={() => setConfirmDeleteAppLogs(false)}
												className="px-3 py-1.5 text-xs rounded-full border border-gray-700 text-gray-400 hover:text-white hover:bg-gray-700 transition-colors cursor-pointer"
											>
												Cancel
											</button>
										</div>
									)}
								</div>
							</div>
						</div>
					</div>
				</div>
			</div>
		</div>
	);
}

function ProviderDiscoveryList({
	collapsed,
	onToggle,
}: {
	collapsed: boolean;
	onToggle: () => void;
}) {
	const { toast } = useToast();
	const { data: providers, isLoading } = useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
	});

	const { data: models } = useQuery({
		queryKey: ["models"],
		queryFn: () => api.models.list(),
	});

	const queryClient = useQueryClient();
	const [discoveringId, setDiscoveringId] = useState<string | null>(null);

	const discoverAllMutation = useMutation({
		mutationFn: async () => {
			toast("Discovering models for all providers…", "info");
			return api.providers.discoverAll();
		},
		onSuccess: (data) => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			queryClient.invalidateQueries({ queryKey: ["models"] });
			for (const r of data.results) {
				if (r.error) {
					toast(`${r.provider_name}: ${r.error}`, "error");
				} else {
					toast(`${r.provider_name}: ${r.discovered} models`, "success");
				}
			}
			if (data.discovered > 0) {
				toast(
					`Discovered ${data.discovered} models across ${data.succeeded} providers`,
					"success",
				);
			} else if (data.failed > 0) {
				toast(`Discovery failed for all ${data.failed} providers`, "error");
			}
		},
		onError: (err: Error) => {
			toast(`Discover all failed: ${err.message}`, "error");
		},
	});

	const discoverMutation = useMutation({
		mutationFn: async (id: string) => {
			setDiscoveringId(id);
			toast("Discovering models…", "info");
			return api.providers.discover(id);
		},
		onSuccess: (data) => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			queryClient.invalidateQueries({ queryKey: ["models"] });
			toast(`Discovered ${data?.discovered ?? "new"} models`, "success");
		},
		onError: (err: Error) => {
			toast(`Discovery failed: ${err.message}`, "error");
		},
		onSettled: () => {
			setDiscoveringId(null);
		},
	});

	if (isLoading) return <p className="text-gray-500">Loading providers...</p>;

	const modelCounts: Record<string, number> = {};
	for (const m of models || []) {
		if (m.enabled) {
			modelCounts[m.provider_id] = (modelCounts[m.provider_id] || 0) + 1;
		}
	}

	return (
		<div className="flex flex-col min-h-0 flex-1">
			<div className="flex items-center justify-between mb-1 shrink-0">
				<div className="flex items-center gap-2">
					<Zap size={18} className="text-(--accent)" />
					<h2 className="text-xl font-semibold text-white">Discovery Status</h2>
				</div>
				<div className="flex items-center gap-2">
					{providers && providers.length > 0 && (
						<button
							type="button"
							onClick={() => discoverAllMutation.mutate()}
							disabled={discoverAllMutation.isPending || discoveringId !== null}
							className="ui-btn ui-btn-secondary"
						>
							{discoverAllMutation.isPending ? (
								<>
									<Spinner /> Discovering…
								</>
							) : (
								"Discover All"
							)}
						</button>
					)}
					<button
						type="button"
						onClick={onToggle}
						className="p-1.5 rounded-md transition-all cursor-pointer text-gray-400 hover:text-(--accent)"
					>
						{collapsed ? <ChevronDown size={16} /> : <ChevronUp size={16} />}
					</button>
				</div>
			</div>
			<div
				className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${collapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"}`}
			>
				<div className="overflow-hidden">
					{providers?.length === 0 && (
						<p className="text-gray-500 text-sm shrink-0">
							No providers configured yet.
						</p>
					)}
					<div className="overflow-y-auto min-h-0 flex-1 mt-2 space-y-0 pr-2">
						{[...(providers ?? [])]
							.sort((a, b) => {
								const aTime = a.last_discovered_at
									? new Date(a.last_discovered_at).getTime()
									: 0;
								const bTime = b.last_discovered_at
									? new Date(b.last_discovered_at).getTime()
									: 0;
								return bTime - aTime;
							})
							.map((p) => (
								<div
									key={p.id}
									className="flex items-center justify-between py-2"
								>
									<div className="flex items-center gap-3">
										<span
											className={`w-2 h-2 rounded-full ${p.enabled ? "bg-green-400" : "bg-gray-500"}`}
										/>
										<div>
											<p className="text-sm font-medium text-white">{p.name}</p>
											<p className="text-xs text-gray-500">
												{modelCounts[p.id] || 0} models
												{p.last_discovered_at &&
													` · Last discovered ${formatRelativeTime(p.last_discovered_at)}`}
											</p>
										</div>
									</div>
									<button
										type="button"
										onClick={() => discoverMutation.mutate(p.id)}
										disabled={
											discoveringId !== null || discoverAllMutation.isPending
										}
										className={`px-3 py-1.5 text-xs rounded-full border transition-all ${
											discoveringId === p.id
												? "bg-(--accent-lighter) text-(--accent) border-(--accent-light) cursor-not-allowed"
												: discoveringId !== null ||
														discoverAllMutation.isPending
													? "bg-gray-800/50 text-gray-600 border-gray-700/30 cursor-not-allowed"
													: "bg-(--accent-light) text-(--accent) border-(--accent-lighter) cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(129,140,248,0.2)]"
										}`}
									>
										{discoveringId === p.id ? (
											<>
												<Spinner /> Discovering…
											</>
										) : (
											"Discover Now"
										)}
									</button>
								</div>
							))}
					</div>
				</div>
			</div>
		</div>
	);
}
