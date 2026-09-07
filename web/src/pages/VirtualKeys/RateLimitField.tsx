import { useTranslation } from "react-i18next";

/** The bounds each rate-limit input accepts, matching the API's. */
const LIMITS = {
	rps: { min: "0", max: "10000", step: "any" },
	burst: { min: "1", max: "10000" },
	tpm: { min: "1", max: "100000000" },
} as const;

/**
 * One rate-limit number input with its label. Empty means "inherit the global
 * setting", which is what the placeholder says.
 */
export function RateLimitField({
	id,
	labelKey,
	field,
	value,
	onChange,
}: {
	id: string;
	labelKey: string;
	field: keyof typeof LIMITS;
	value: string;
	onChange: (value: string) => void;
}) {
	const { t } = useTranslation();
	return (
		<div>
			<label
				htmlFor={id}
				className="block text-sm font-medium text-gray-300 mb-1"
			>
				{t(labelKey)}
			</label>
			<input
				id={id}
				type="number"
				{...LIMITS[field]}
				value={value}
				onChange={(e) => onChange(e.target.value)}
				className="ui-input"
				placeholder={t("virtualkeys.modal.form.placeholderGlobal")}
			/>
		</div>
	);
}
