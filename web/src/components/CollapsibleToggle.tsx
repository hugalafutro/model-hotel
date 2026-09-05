/* eslint-disable react-refresh/only-export-components -- useCollapsible lives beside the toggle it drives */

import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import {
	ChevronDown,
	ChevronsDownUp,
	ChevronsUpDown,
	ChevronUp,
} from "@/lib/icons";
import { useLocalStorage } from "../hooks/useLocalStorage";

interface CollapsibleToggleProps {
	collapsed: boolean;
	onToggle: () => void;
	expandTitle?: string;
	collapseTitle?: string;
	/** Icon style: "single" uses ChevronUp/Down, "double" uses ChevronsUpDown/DownUp. Default "single" */
	iconStyle?: "single" | "double";
	/** Icon size in px. Default 14 */
	size?: number;
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
	className: overrideClassName,
}: CollapsibleToggleProps) {
	const { t } = useTranslation();
	const className = overrideClassName ?? "ui-icon-btn p-1.5 rounded-md";

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
	// Stored as "true"/"false"; anything else reads as expanded.
	const [collapsed, setCollapsed] = useLocalStorage<boolean>(
		storageKey ?? "",
		defaultValue,
		{ enabled: !!storageKey, deserialize: (stored) => stored === "true" },
	);

	const toggle = useCallback(() => {
		setCollapsed((prev) => !prev);
	}, [setCollapsed]);

	return { collapsed, toggle };
}
