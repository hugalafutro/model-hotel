import { useTranslation } from "react-i18next";
import { ResetButton } from "./ResetButton";
import { Toggle } from "./Toggle";

interface SettingToggleRowProps {
	label: string;
	description: string;
	checked: boolean;
	onChange: (checked: boolean) => void;
	/** Locks the switch: a parent setting is off, or a save is in flight. */
	disabled?: boolean;
	/** Restores the server default; omitted for a preference that has none. */
	onReset?: () => void;
	resetDisabled?: boolean;
	testId?: string;
	className?: string;
}

// SettingToggleRow is the one shape a boolean setting takes on the Settings
// page: the label with its reset beside it, the description under, the switch
// at the far end. The label doubles as the switch's accessible name.
export function SettingToggleRow({
	label,
	description,
	checked,
	onChange,
	disabled,
	onReset,
	resetDisabled,
	testId,
	className,
}: SettingToggleRowProps) {
	const { t } = useTranslation();
	return (
		<div
			className={`flex items-center justify-between gap-3 ${className ?? ""}`.trim()}
			data-testid={testId}
		>
			<div className="min-w-0">
				<div className="flex items-center gap-1">
					<p className="text-sm font-medium text-gray-300">{label}</p>
					{onReset && (
						<ResetButton
							tooltip={t("settings.common.resetSetting")}
							onClick={onReset}
							size={12}
							disabled={resetDisabled}
						/>
					)}
				</div>
				<p className="text-gray-500 text-xs mt-0.5">{description}</p>
			</div>
			<Toggle
				checked={checked}
				size="sm"
				disabled={disabled}
				onChange={onChange}
				ariaLabel={label}
			/>
		</div>
	);
}
