import type { CSSProperties, ReactNode } from "react";

type NoticeVariant = "warn" | "info";

// Notice is the block-level callout used across Front Desk panels: a full-width
// ui-badge that wraps readably instead of staying on one line. The variant picks
// the colour; pass `style` only for margin overrides (padding, radius, and the
// wrapping behaviour are fixed so every notice looks the same).
export function Notice({
	variant = "warn",
	style,
	children,
}: {
	variant?: NoticeVariant;
	style?: CSSProperties;
	children: ReactNode;
}) {
	return (
		<div
			className={`ui-badge ui-badge-${variant}`}
			style={{
				display: "block",
				padding: "0.5rem 0.7rem",
				borderRadius: "var(--radius-sm)",
				whiteSpace: "normal",
				textWrap: "pretty",
				lineHeight: 1.45,
				...style,
			}}
		>
			{children}
		</div>
	);
}
