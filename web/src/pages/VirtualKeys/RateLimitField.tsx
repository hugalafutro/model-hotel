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
	// The unit in brackets ("(requests/sec)", "（最大同時）") goes on its own line,
	// so three of these side by side wrap evenly instead of at whatever word
	// the column width happens to cut. The space keeps the accessible name
	// the whole label.
	const label = t(labelKey);
	const unit = label.match(/^(.*?)\s*([(（].*)$/);
	return (
		<div>
			<label htmlFor={id} className="block ui-label mb-1">
				{unit ? (
					<>
						{unit[1]} <br />
						{unit[2]}
					</>
				) : (
					label
				)}
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
