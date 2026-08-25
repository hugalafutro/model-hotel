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
	// Set: the compact labelled line the alerts wizard uses, where the name of
	// the field sits beside its monospace value and the button carries both in
	// its accessible name. Unset: the full-width monospace field-style box the
	// sync wizard uses for an endpoint URL, which has its own heading above it.
	label?: string;
	/** data-testid for the copy button; omitted when unset. */
	testId?: string;
}) {
	const { t } = useTranslation();
	const { copy, copied } = useCopyToClipboard();
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
			<button
				type="button"
				className="ui-btn ui-btn-sm"
				data-testid={testId}
				aria-label={labelled ? `${t("common.copy")}: ${label}` : undefined}
				onClick={() => {
					void copy(value);
				}}
			>
				{copied ? t("common.copied") : t("common.copy")}
			</button>
		</div>
	);
}
