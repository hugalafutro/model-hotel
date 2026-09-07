import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	AppleLogo,
	LinuxLogo,
	Monitor,
	Palette,
	Plus,
	WindowsLogo,
} from "@/lib/icons";
import { ResetButton } from "../../components/ResetButton";
import { SettingsGroup } from "../../components/SettingsGroup";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSlider } from "../../components/SettingsSlider";
import { SettingToggleRow } from "../../components/SettingToggleRow";
import { THEME_DEFAULT_ACCENT, useTheme } from "../../context/ThemeContext";
import { useToast } from "../../context/ToastContext";
import { detectOS } from "../../utils/os";
import { ColorPickerModal } from "./ColorPickerModal";
import { UI_STYLES } from "./constants";

// Icon for the "Follow System" button reflects the detected OS, falling back
// to a generic monitor.
const OS_ICONS = {
	macos: AppleLogo,
	windows: WindowsLogo,
	linux: LinuxLogo,
	unknown: Monitor,
} as const;

// The six toast anchors: where the dot sits on the preview monitor and the
// settings.toast.position.* key naming it.
const TOAST_POSITIONS = [
	{ id: "top-left", cls: "top-2 left-2", key: "topLeft" },
	{
		id: "top-center",
		cls: "top-2 left-1/2 -translate-x-1/2",
		key: "topCenter",
	},
	{ id: "top-right", cls: "top-2 right-2", key: "topRight" },
	{ id: "bottom-left", cls: "bottom-2 left-2", key: "bottomLeft" },
	{
		id: "bottom-center",
		cls: "bottom-2 left-1/2 -translate-x-1/2",
		key: "bottomCenter",
	},
	{ id: "bottom-right", cls: "bottom-2 right-2", key: "bottomRight" },
] as const;

const THEMES = [
	{ id: "dark", labelKey: "settings.appearance.dark" },
	{ id: "system", labelKey: "settings.appearance.followSystem" },
	{ id: "light", labelKey: "settings.appearance.light" },
] as const;

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
		themePreference,
		setTheme,
		uiStyle,
		setUIStyle,
		accentColor,
		accentIsExplicit,
		setAccentColor,
		accentPresets,
	} = useTheme();
	// The per-theme default accents are not presets, but they are not a
	// user's custom pick either — the dashed swatch must stay neutral.
	const isCustomAccent =
		!!accentColor &&
		!accentPresets.some((p) => p.color === accentColor) &&
		!Object.values(THEME_DEFAULT_ACCENT).includes(accentColor);
	const {
		toast,
		position: toastPosition,
		setPosition,
		timeout: toastTimeout,
		setTimeout: setToastTimeout,
		fuse: toastFuse,
		setFuse: setToastFuse,
	} = useToast();

	const [pickerOpen, setPickerOpen] = useState(false);
	const [pickerColor, setPickerColor] = useState(accentColor);

	// Reflect the browser-detected OS in the "Follow System" button icon.
	const SystemIcon = useMemo(() => OS_ICONS[detectOS()], []);

	const openPicker = useCallback(() => {
		setPickerColor(accentColor);
		setPickerOpen(true);
	}, [accentColor]);

	const applyPickerColor = useCallback(() => {
		setAccentColor(pickerColor);
		setPickerOpen(false);
	}, [pickerColor, setAccentColor]);

	return (
		<SettingsSection
			icon={Palette}
			title={t("settings.appearance.title")}
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-6">
				{/* UI Style + Toast Notifications (2 columns) */}
				<div className="grid grid-cols-2 gap-x-6 gap-y-5 [align-items:start]">
					{/* UI Style */}
					<SettingsGroup title={t("settings.appearance.uiStyle")}>
						<div className="grid grid-cols-1 gap-3">
							{UI_STYLES.map((style) => {
								const Icon = style.icon;
								const active = uiStyle === style.id;
								const labelKey = style.i18nKey;
								return (
									<button
										key={style.id}
										type="button"
										onClick={() => setUIStyle(style.id)}
										className={`ui-style-card flex flex-col items-center gap-2 p-3 rounded-xl border transition-all ${
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
					</SettingsGroup>

					{/* Toast Notifications */}
					<SettingsGroup title={t("settings.toast.title")}>
						<div className="flex justify-center">
							<div className="toast-monitor relative w-40 h-26 rounded-lg border-2 border-gray-600 bg-gray-800/50 overflow-visible">
								{TOAST_POSITIONS.map(({ id, cls, key }) => (
									<button
										key={id}
										type="button"
										onClick={() => {
											setPosition(id);
											toast(t("settings.toast.testNotification"), "info");
										}}
										className={`absolute ${cls} w-3 h-3 rounded-full transition-all bg-(--accent) ${
											toastPosition === id
												? "opacity-100"
												: "opacity-30 hover:opacity-70"
										}`}
										title={t(`settings.toast.position.${key}`)}
									/>
								))}
							</div>
						</div>

						<p className="text-center text-gray-500 text-xs mt-4">
							{t(
								`settings.toast.position.${TOAST_POSITIONS.find((p) => p.id === toastPosition)?.key ?? "bottomRight"}`,
							)}
						</p>

						<SettingsSlider
							id="toast-autodismiss"
							label={t("settings.toast.autoDismiss")}
							value={toastTimeout / 1000}
							min={1}
							max={30}
							step={1}
							clampStep={1}
							unit="s"
							onChange={(v) => setToastTimeout(v * 1000)}
						/>

						<SettingToggleRow
							className="mt-4"
							label={t("settings.toast.fuseEffect")}
							description={t("settings.toast.fuseEffectDescription")}
							checked={toastFuse}
							onChange={setToastFuse}
						/>
					</SettingsGroup>
				</div>

				{/* Accent Color + Theme */}
				<div className="flex items-center justify-between gap-6">
					<div className="flex items-center gap-3">
						<p className="text-sm font-medium text-gray-300">
							{t("settings.appearance.accentColor")}
						</p>
						<div className="flex flex-wrap gap-2 py-1 px-1">
							{accentPresets.map((preset) => (
								<button
									key={preset.name}
									type="button"
									onClick={() => setAccentColor(preset.color)}
									className={`color-swatch w-8 h-8 border-2 border-transparent transition-transform hover:scale-110 ${
										accentColor === preset.color
											? "ring-2 ring-white scale-110"
											: ""
									}`}
									style={{
										backgroundColor: preset.color,
									}}
									title={t(preset.name)}
								/>
							))}
							<button
								type="button"
								onClick={openPicker}
								className={`color-swatch w-8 h-8 border-2 border-dashed border-gray-500 flex items-center justify-center hover:border-gray-400 transition-colors ${
									isCustomAccent ? "bg-gray-800" : ""
								}`}
								title={t("settings.appearance.customColor")}
							>
								{isCustomAccent ? (
									<div
										className="w-5 h-5 rounded-full"
										style={{
											backgroundColor: accentColor,
										}}
									/>
								) : (
									<Plus size={16} className="text-gray-400" />
								)}
							</button>
							{accentIsExplicit && (
								<ResetButton
									tooltip={t("settings.appearance.resetAccent")}
									onClick={() => setAccentColor("")}
									className="self-center"
								/>
							)}
						</div>
					</div>

					<div className="flex items-center gap-3">
						<p className="text-sm font-medium text-gray-300">
							{t("settings.appearance.theme")}
						</p>
						<div className="theme-mode-toggle">
							{THEMES.map(({ id, labelKey }) => (
								<button
									key={id}
									type="button"
									onClick={() => setTheme(id)}
									aria-label={id === "system" ? t(labelKey) : undefined}
									title={id === "system" ? t(labelKey) : undefined}
									className={`ui-btn ${
										themePreference === id
											? "ui-btn-primary"
											: "ui-btn-secondary"
									}`}
								>
									{id === "system" ? <SystemIcon size={16} /> : t(labelKey)}
								</button>
							))}
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
