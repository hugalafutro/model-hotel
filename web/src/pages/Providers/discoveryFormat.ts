// Value formatting shared by the two discovery modals, split out of
// discoveryPrimitives.tsx so that file exports components only: mixing
// component and non-component exports in one .tsx breaks Vite Fast Refresh
// (eslint react-refresh/only-export-components) for every consumer of it.
import { formatTokens } from "../../utils/format";
import { formatPrice } from "../../utils/model";

// Pricing fields render as "$<price>"; the rest are token counts. Internal:
// consumers go through formatFieldValue, which is the whole point of the set.
const PRICE_FIELDS = new Set([
	"input_price",
	"output_price",
	"input_price_cache",
]);

// formatFieldValue renders a metadata value for the Updated section, using the
// same formatters as the Models table; a null/undefined value reads as `unset`.
export function formatFieldValue(
	field: string,
	value: number | null | undefined,
	unset: string,
): string {
	if (value == null) return unset;
	return PRICE_FIELDS.has(field)
		? `$${formatPrice(value)}`
		: formatTokens(value);
}
