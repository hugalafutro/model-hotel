import type { QuotaModalKit } from "@web-shared/quota/modals";
import i18n from "../i18n";

/**
 * A kit for rendering the shared quota modal bodies on their own: this app's
 * real strings (so a missing sub-key shows up as the raw key), a bare dialog
 * for the shell, and a time format that echoes the instant it was given, so a
 * test can assert WHICH instant a label was built from, in any locale.
 */
export const testQuotaKit: QuotaModalKit = {
	t: (key, opts) => i18n.t(`components.providerModals.${key}`, opts),
	time: {
		absolute: (iso) => (iso ? `at ${iso}` : "-"),
		relative: (iso) => (iso ? `in ${iso}` : "-"),
	},
	shell: ({ title, subtitle, children }) => (
		<div role="dialog" aria-label={title}>
			{subtitle && <p data-testid="quota-subtitle">{subtitle}</p>}
			{children}
		</div>
	),
};

/** The props every body takes besides its payload and bar mode. */
export const quotaBodyChrome = {
	providerName: "acct",
	fetchedAt: "2026-07-26T10:00:00Z",
	kit: testQuotaKit,
};
