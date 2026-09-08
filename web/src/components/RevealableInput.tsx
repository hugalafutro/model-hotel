import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Eye, EyeOff } from "@/lib/icons";

/**
 * A secret field the operator can unmask to check what they typed. The reveal
 * button is out of the tab order: it is a convenience for the eyes, and
 * tabbing from the field should reach the form's next control.
 */
export function RevealableInput({
	id,
	value,
	onChange,
	placeholder,
	required,
	maxLength,
}: {
	id: string;
	value: string;
	onChange: (value: string) => void;
	placeholder?: string;
	required?: boolean;
	maxLength?: number;
}) {
	const { t } = useTranslation();
	const [revealed, setRevealed] = useState(false);
	return (
		<div className="relative">
			<input
				id={id}
				type={revealed ? "text" : "password"}
				value={value}
				onChange={(e) => onChange(e.target.value)}
				className="ui-input pr-10! overflow-hidden"
				placeholder={placeholder}
				required={required}
				maxLength={maxLength}
			/>
			<button
				type="button"
				tabIndex={-1}
				onClick={() => setRevealed((v) => !v)}
				className="ui-icon-btn absolute right-3 top-1/2 -translate-y-1/2"
				aria-label={
					revealed
						? t("providers.form_api_key_hide")
						: t("providers.form_api_key_show")
				}
			>
				{revealed ? <EyeOff size={18} /> : <Eye size={18} />}
			</button>
		</div>
	);
}
