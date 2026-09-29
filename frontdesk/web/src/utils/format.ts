// Number formatting for quota badges and modals. The shared
// formatters live once in web-shared/ and are re-exported here, so every
// existing "utils/format" import keeps working and both frontends render the
// same numbers, in the same locale. Date and relative-time formatting lives in
// ./time.ts; do not duplicate it here.

import { setFormatLanguage } from "@web-shared/format";
import i18next from "i18next";

// The shared formatters follow the app language, as the dates in ./time.ts do.
setFormatLanguage(() => i18next.language);

export {
	formatCompact,
	formatCount,
	formatDecimal,
	formatDollars,
	formatKwh,
	formatLocale,
	formatTokens,
} from "@web-shared/format";
