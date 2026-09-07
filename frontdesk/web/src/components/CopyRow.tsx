import { useTranslation } from "react-i18next";
import { useCopyToClipboard } from "../hooks/useCopyToClipboard";

// CopyRow is one value the operator would otherwise have to retype somewhere
// else - a phone, another Front Desk, a client config - with a button that
// saves the retyping. A blocked clipboard is silent: the text stays selectable
// either way, and the button simply never says "Copied".
export function CopyRow({
	value,
	label,
	testId,
}: {
	/** The text shown and copied. An empty value renders nothing. */
	value: string;
	/**
	 * Set: the compact labelled line the alerts wizard uses, where the name of
	 * the field sits beside its monospace value and the button carries both in
	 * its accessible name. Unset: the full-width monospace field-style box the
	 * sync wizard uses for an endpoint URL, which has its own heading above it.
	 */
	label?: string;
	/** data-testid for the copy button; omitted when unset. */
	testId?: string;
}) {
	if (value === "") return null;
	const labelled = label !== undefined;
	return (
		<div
			className="fd-row"
			style={{ gap: labelled ? "0.4rem" : "0.5rem", alignItems: "center" }}
		>
			{labelled && (
				<span className="fd-faint" style={{ fontSize: "0.8rem" }}>
					{label}
				</span>
			)}
			<code
				className={labelled ? "fd-mono" : "ui-input fd-mono"}
				style={
					labelled
						? { fontSize: "0.8rem", userSelect: "all" }
						: { flex: "1 1 auto", padding: "0.3rem 0.5rem", userSelect: "all" }
				}
			>
				{value}
			</code>
			<CopyButton value={value} name={label} testId={testId} />
		</div>
	);
}

// CopyButton puts one value on the clipboard: the button half of CopyRow, also
// used on its own beside a value that already has its own row markup (a saved
// alert destination). A blocked clipboard is silent; the button simply never
// says "Copied" and the text beside it stays selectable.
export function CopyButton({
	value,
	name,
	testId,
}: {
	/** The text copied. */
	value: string;
	// What the button copies, for its accessible name ("Copy: Webhook URL").
	// Unset: the button carries no label of its own, for a value whose own
	// heading already names it.
	name?: string;
	/** data-testid for the button; omitted when unset. */
	testId?: string;
}) {
	const { t } = useTranslation();
	// The "Copied" label reverts on a timer the hook drops if the button goes
	// first: removing a destination unmounts it, and firing then would set state
	// on an unmounted button.
	const { copy, copied } = useCopyToClipboard();
	return (
		<button
			type="button"
			className="ui-btn ui-btn-sm"
			data-testid={testId}
			aria-label={
				name === undefined ? undefined : `${t("common.copy")}: ${name}`
			}
			onClick={() => {
				void copy(value);
			}}
		>
			{copied ? t("common.copied") : t("common.copy")}
		</button>
	);
}
