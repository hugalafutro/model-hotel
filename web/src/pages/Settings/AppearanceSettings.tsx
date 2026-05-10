import { Palette } from "lucide-react";
import { useCallback, useState } from "react";
import { SettingsSection } from "../../components/SettingsSection";
import { useTheme } from "../../context/ThemeContext";
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
	const {
		theme,
		setTheme,
		uiStyle,
		setUIStyle,
		accentColor,
		setAccentColor,
		accentPresets,
	} = useTheme();

	const [pickerOpen, setPickerOpen] = useState(false);
	const [pickerColor, setPickerColor] = useState(accentColor);

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
			title="Appearance"
			collapsed={collapsed}
			onToggle={onToggle}
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
					<p className="text-sm font-medium text-gray-300 mb-2">Accent Color</p>
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
