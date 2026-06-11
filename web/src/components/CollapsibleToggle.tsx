/* eslint-disable react-refresh/only-export-components */

import {
	ChevronDown,
	ChevronsDownUp,
	ChevronsUpDown,
	ChevronUp,
} from "lucide-react";
import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";

interface CollapsibleToggleProps {
	collapsed: boolean;
	onToggle: () => void;
	expandTitle?: string;
	collapseTitle?: string;
	/** Icon style: "single" uses ChevronUp/Down, "double" uses ChevronsUpDown/DownUp. Default "single" */
	iconStyle?: "single" | "double";
	/** Icon size in px. Default 14 */
	size?: number;
	/** Visual variant: "accent" glows on hover, "muted" is a subtle gray. Default "accent" */
	variant?: "accent" | "muted";
	/** Override the default className entirely */
	className?: string;
}

export function CollapsibleToggle({
	collapsed,
	onToggle,
	expandTitle,
	collapseTitle,
	iconStyle = "single",
	size = 14,
	variant = "accent",
	className: overrideClassName,
}: CollapsibleToggleProps) {
	const { t } = useTranslation();
	const className =
		overrideClassName ??
		(variant === "muted"
			? "p-1.5 rounded-md transition-all text-gray-400 hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)]"
			: "p-1.5 rounded-md transition-all text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)]");

	const icons =
		iconStyle === "double" ? (
			collapsed ? (
				<ChevronsUpDown size={size} />
			) : (
				<ChevronsDownUp size={size} />
			)
		) : collapsed ? (
			<ChevronDown size={size} />
		) : (
			<ChevronUp size={size} />
		);

	return (
		<button
			type="button"
			onClick={onToggle}
			className={className}
			title={
				collapsed
					? (expandTitle ?? t("common.expand"))
					: (collapseTitle ?? t("common.collapse"))
			}
			aria-label={
				collapsed
					? (expandTitle ?? t("common.expand"))
					: (collapseTitle ?? t("common.collapse"))
			}
		>
			{icons}
		</button>
	);
}

/**
 * Hook for collapsible state with optional localStorage persistence.
 * Eliminates the repeated useState + useCallback + localStorage boilerplate.
 */
export function useCollapsible(
	storageKey?: string,
	defaultValue = false,
): {
	collapsed: boolean;
	toggle: () => void;
} {
	const [collapsed, setCollapsed] = useState(() => {
		if (!storageKey) return defaultValue;
		try {
			return localStorage.getItem(storageKey) === "true";
		} catch {
			return defaultValue;
		}
	});

	const toggle = useCallback(() => {
		setCollapsed((prev) => {
			const next = !prev;
			if (storageKey) {
				try {
					localStorage.setItem(storageKey, String(next));
				} catch {
					/* ignore */
				}
			}
			return next;
		});
	}, [storageKey]);

	return { collapsed, toggle };
}
