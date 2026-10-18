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
	breakUnit = false,
}: {
	id: string;
	labelKey: string;
	field: keyof typeof LIMITS;
	value: string;
	onChange: (value: string) => void;
	// Put the bracketed unit on its own line, for a row of narrow columns.
	breakUnit?: boolean;
}) {
	const { t } = useTranslation();
	// With breakUnit the unit in brackets ("(requests/sec)", "（最大同時）")
	// starts a line of its own, so three of these side by side wrap evenly
	// instead of at whatever word the column width happens to cut. The space
	// keeps the accessible name the whole label.
	const label = t(labelKey);
	const unit = breakUnit ? label.match(/^(.*?)\s*([(（].*)$/) : null;
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
