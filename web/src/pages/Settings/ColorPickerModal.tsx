import { HexColorPicker } from "react-colorful";
import { useTranslation } from "react-i18next";
import { Modal } from "../../components/Modal";

interface ColorPickerModalProps {
	color: string;
	onChange: (color: string) => void;
	onClose: () => void;
	onApply: () => void;
}

export function ColorPickerModal({
	color,
	onChange,
	onClose,
	onApply,
}: ColorPickerModalProps) {
	const { t } = useTranslation();
	return (
		<Modal
			title={t("settings.colorPicker.title")}
			onClose={onClose}
			maxWidth="max-w-sm"
		>
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
						data-testid="color-preview"
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
						{t("settings.colorPicker.cancel")}
					</button>
					<button
						type="button"
						onClick={onApply}
						className="flex-1 px-3 py-2 rounded-lg text-sm font-medium ui-btn ui-btn-primary"
					>
						{t("settings.colorPicker.apply")}
					</button>
				</div>
			</div>
		</Modal>
	);
}
