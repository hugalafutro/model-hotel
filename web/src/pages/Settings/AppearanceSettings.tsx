import { Palette } from "lucide-react";
import type React from "react";
import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSelect } from "../../components/SettingsSelect";
import { Toggle } from "../../components/Toggle";
import { useTheme } from "../../context/ThemeContext";
import { useToast } from "../../context/ToastContext";
import { ColorPickerModal } from "./ColorPickerModal";
import { UI_STYLES } from "./constants";

interface AppearanceSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function AppearanceSettings({
	collapsed,
	onToggle,
}: AppearanceSettingsProps) {
	const { t } = useTranslation();
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
		timeout: toastTimeout,
		setTimeout: setToastTimeout,
	} = useToast();

	const [pickerOpen, setPickerOpen] = useState(false);
	const [pickerColor, setPickerColor] = useState(accentColor);

	// Sidebar Quota state
	const [quotaDisabled, setQuotaDisabled] = useState(() => {
		try {
			return localStorage.getItem("sidebarQuotaDisabled") === "true";
		} catch {
			return false;
		}
	});
	const [refreshMin, setRefreshMin] = useState(() => {
		try {
			return localStorage.getItem("sidebarQuotaRefreshMin") || "5";
		} catch {
			return "5";
		}
	});

	// Dashboard Refresh state
	const [refreshSec, setRefreshSec] = useState(() => {
		try {
			return localStorage.getItem("dashboardRefreshSec") || "30";
		} catch {
			return "30";
		}
	});

	const openPicker = useCallback(() => {
		setPickerColor(accentColor);
		setPickerOpen(true);
	}, [accentColor]);

	const applyPickerColor = useCallback(() => {
		setAccentColor(pickerColor);
		setPickerOpen(false);
	}, [pickerColor, setAccentColor]);

	const handleQuotaRefreshChange = (val: string) => {
		setRefreshMin(val);
		try {
			localStorage.setItem("sidebarQuotaRefreshMin", val);
		} catch {
			/* ignore */
		}
		window.dispatchEvent(new CustomEvent("sidebarQuotaRefreshChange"));
		toast(
			val === "0"
				? t("settings.sidebarQuota.disabled")
				: t("settings.sidebarQuota.intervalSet", {
						minutes: val,
						count: Number(val),
					}),
			"success",
		);
	};

	const handleDashboardRefreshChange = (val: string) => {
		setRefreshSec(val);
		try {
			localStorage.setItem("dashboardRefreshSec", val);
		} catch {
			/* ignore */
		}
		window.dispatchEvent(new CustomEvent("dashboardRefreshChange"));
		toast(
			val === "0"
				? t("settings.dashboard.disabled")
				: t("settings.dashboard.intervalSet", {
						seconds: val,
						count: Number(val),
					}),
			"success",
		);
	};

	return (
		<SettingsSection
			icon={Palette}
			title={t("settings.appearance.title")}
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-6">
				{/* UI Style */}
				<div>
					<p className="text-sm font-medium text-gray-300 mb-3">
						{t("settings.appearance.uiStyle")}
					</p>
					<div className="grid grid-cols-3 gap-3">
						{UI_STYLES.map((style) => {
							const Icon = style.icon;
							const active = uiStyle === style.id;
							const labelKey =
								style.id === "clean-saas"
									? "cleanSaas"
									: style.id === "cyber-terminal"
										? "cyberTerminal"
										: "glassmorphism";
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
											{t(`settings.appearance.uiStyles.${labelKey}` as const)}
										</p>
										<p className="text-[10px] text-gray-500 mt-0.5">
											{t(
												`settings.appearance.uiStyles.${labelKey}Description` as const,
											)}
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
						<p className="text-sm font-medium text-gray-300">
							{t("settings.appearance.theme")}
						</p>
						<p className="text-gray-500 text-xs mt-0.5">
							{t("settings.appearance.themeDescription")}
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
							{t("settings.appearance.dark")}
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
							{t("settings.appearance.light")}
						</button>
					</div>
				</div>

				{/* Accent Color */}
				<div>
					<p className="text-sm font-medium text-gray-300 mb-2">
						{t("settings.appearance.accentColor")}
					</p>
					<div className="flex flex-wrap gap-2 py-1 px-1">
						{accentPresets.map((preset) => (
							<button
								key={preset.name}
								type="button"
								onClick={() => setAccentColor(preset.color)}
								className={`w-8 h-8 rounded-full border-2 border-transparent transition-transform hover:scale-110 ${
									accentColor === preset.color
										? "ring-2 ring-white scale-110"
										: ""
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
							title={t("settings.appearance.customColor")}
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
									<title>{t("settings.appearance.colorPicker.apply")}</title>
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

				{/* Sidebar Quotas */}
				<div className="pt-2 border-t border-gray-700/50">
					<h3 className="text-xs font-semibold uppercase tracking-wider text-gray-500 mb-3">
						{t("settings.sidebarQuota.title")}
					</h3>
					<div className="grid grid-cols-2 gap-x-8 gap-y-5 items-start">
						<div className="space-y-5">
							<div className="flex items-center justify-between">
								<div>
									<p className="text-sm font-medium text-gray-300">
										{t("settings.sidebarQuota.showQuotasPill")}
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{t("settings.sidebarQuota.showQuotasPillDescription")}
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
												? t("settings.sidebarQuota.disabledQuotas")
												: t("settings.sidebarQuota.enabledQuotas"),
											newVal ? "info" : "success",
										);
										window.dispatchEvent(new CustomEvent("sidebarQuotaToggle"));
									}}
								/>
							</div>
						</div>
						<div className="space-y-5">
							<SettingsSelect
								id="quota-refresh-interval"
								label={t("settings.sidebarQuota.refreshInterval")}
								value={refreshMin}
								options={[
									{ value: "1", label: t("settings.sidebarQuota.intervals.1") },
									{ value: "2", label: t("settings.sidebarQuota.intervals.2") },
									{ value: "5", label: t("settings.sidebarQuota.intervals.5") },
									{
										value: "10",
										label: t("settings.sidebarQuota.intervals.10"),
									},
									{
										value: "15",
										label: t("settings.sidebarQuota.intervals.15"),
									},
									{
										value: "30",
										label: t("settings.sidebarQuota.intervals.30"),
									},
									{
										value: "0",
										label: t("settings.sidebarQuota.intervals.disabled"),
									},
								]}
								onChange={handleQuotaRefreshChange}
								disabled={quotaDisabled}
								description={t(
									"settings.sidebarQuota.refreshInterval.description",
								)}
							/>
						</div>
					</div>
				</div>

				{/* Dashboard Refresh */}
				<div className="pt-2 border-t border-gray-700/50">
					<h3 className="text-xs font-semibold uppercase tracking-wider text-gray-500 mb-3">
						{t("settings.dashboard.title")}
					</h3>
					<SettingsSelect
						id="dashboard-refresh-interval"
						label={t("settings.dashboard.refreshInterval")}
						value={refreshSec}
						options={[
							{
								value: "10",
								label: t("settings.dashboard.intervals.10"),
							},
							{
								value: "30",
								label: t("settings.dashboard.intervals.30"),
							},
							{
								value: "60",
								label: t("settings.dashboard.intervals.60"),
							},
							{
								value: "120",
								label: t("settings.dashboard.intervals.120"),
							},
							{
								value: "300",
								label: t("settings.dashboard.intervals.300"),
							},
							{
								value: "600",
								label: t("settings.dashboard.intervals.600"),
							},
							{
								value: "0",
								label: t("settings.dashboard.intervals.disabled"),
							},
						]}
						onChange={handleDashboardRefreshChange}
						description={t("settings.dashboard.refreshInterval.description")}
					/>
				</div>

				{/* Toast Notifications */}
				<div className="pt-2 border-t border-gray-700/50">
					<h3 className="text-xs font-semibold uppercase tracking-wider text-gray-500 mb-3">
						{t("settings.toast.title")}
					</h3>
					<div className="flex justify-center">
						<div className="relative w-40 h-26 rounded-lg border-2 border-gray-600 bg-gray-800/50 overflow-visible">
							{/* top-left */}
							<button
								type="button"
								onClick={() => {
									setPosition("top-left");
									toast(t("settings.toast.testNotification"), "info");
								}}
								className={`absolute top-2 left-2 w-3 h-3 rounded-full transition-all ${
									toastPosition === "top-left"
										? "bg-(--accent) opacity-100"
										: "bg-(--accent) opacity-30 hover:opacity-70"
								}`}
								title={t("settings.toast.position.topLeft")}
							/>
							{/* top-center */}
							<button
								type="button"
								onClick={() => {
									setPosition("top-center");
									toast(t("settings.toast.testNotification"), "info");
								}}
								className={`absolute top-2 left-1/2 -translate-x-1/2 w-3 h-3 rounded-full transition-all ${
									toastPosition === "top-center"
										? "bg-(--accent) opacity-100"
										: "bg-(--accent) opacity-30 hover:opacity-70"
								}`}
								title={t("settings.toast.position.topCenter")}
							/>
							{/* top-right */}
							<button
								type="button"
								onClick={() => {
									setPosition("top-right");
									toast(t("settings.toast.testNotification"), "info");
								}}
								className={`absolute top-2 right-2 w-3 h-3 rounded-full transition-all ${
									toastPosition === "top-right"
										? "bg-(--accent) opacity-100"
										: "bg-(--accent) opacity-30 hover:opacity-70"
								}`}
								title={t("settings.toast.position.topRight")}
							/>
							{/* bottom-left */}
							<button
								type="button"
								onClick={() => {
									setPosition("bottom-left");
									toast(t("settings.toast.testNotification"), "info");
								}}
								className={`absolute bottom-2 left-2 w-3 h-3 rounded-full transition-all ${
									toastPosition === "bottom-left"
										? "bg-(--accent) opacity-100"
										: "bg-(--accent) opacity-30 hover:opacity-70"
								}`}
								title={t("settings.toast.position.bottomLeft")}
							/>
							{/* bottom-center */}
							<button
								type="button"
								onClick={() => {
									setPosition("bottom-center");
									toast(t("settings.toast.testNotification"), "info");
								}}
								className={`absolute bottom-2 left-1/2 -translate-x-1/2 w-3 h-3 rounded-full transition-all ${
									toastPosition === "bottom-center"
										? "bg-(--accent) opacity-100"
										: "bg-(--accent) opacity-30 hover:opacity-70"
								}`}
								title={t("settings.toast.position.bottomCenter")}
							/>
							{/* bottom-right */}
							<button
								type="button"
								onClick={() => {
									setPosition("bottom-right");
									toast(t("settings.toast.testNotification"), "info");
								}}
								className={`absolute bottom-2 right-2 w-3 h-3 rounded-full transition-all ${
									toastPosition === "bottom-right"
										? "bg-(--accent) opacity-100"
										: "bg-(--accent) opacity-30 hover:opacity-70"
								}`}
								title={t("settings.toast.position.bottomRight")}
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
								{t("settings.toast.autoDismiss")}
							</span>
							<span className="text-sm text-gray-400 tabular-nums">
								{t("settings.toast.seconds", {
									seconds: (toastTimeout / 1000).toFixed(1),
								})}
							</span>
						</div>
						<input
							type="range"
							min={1000}
							max={15000}
							step={500}
							value={toastTimeout}
							onChange={(e) => {
								setToastTimeout(Number(e.target.value));
							}}
							style={
								{
									"--slider-fill": `${((toastTimeout - 1000) / (15000 - 1000)) * 100}%`,
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

			{pickerOpen && (
				<ColorPickerModal
					color={pickerColor}
					onChange={setPickerColor}
					onClose={() => setPickerOpen(false)}
					onApply={applyPickerColor}
				/>
			)}
		</SettingsSection>
	);
}
