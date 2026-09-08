/* eslint-disable react-refresh/only-export-components -- useCollapsible lives beside the toggle it drives */

import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import {
	ChevronDown,
	ChevronsDownUp,
	ChevronsUpDown,
	ChevronUp,
} from "@/lib/icons";
import { storedBool, useLocalStorage } from "../hooks/useLocalStorage";

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

	const label = collapsed
		? (expandTitle ?? t("common.expand"))
		: (collapseTitle ?? t("common.collapse"));

	return (
		<button
			type="button"
			onClick={onToggle}
			className={className}
			title={label}
			aria-label={label}
		>
			{icons}
		</button>
	);
}

interface CollapseBodyProps {
	collapsed: boolean;
	children: React.ReactNode;
	/**
	 * While expanded, pads the clip box and pulls the padding back out (layout
	 * unchanged), so a child's hover glow or focus ring is not clipped. The
	 * collapsed box stays tight: padding is unsqueezable, so a bleed there
	 * would leave a visible band.
	 */
	bleed?: boolean;
	/** Makes the collapsed body inert, so it is not read out or focusable. */
	inert?: boolean;
	/** Transition length in ms. Default 300. */
	durationMs?: 200 | 300;
	/** Extra classes on the inner (clipping) element. */
	className?: string;
	/** Id for the animated region, for aria-controls. */
	id?: string;
}

/**
 * The body half of a collapse: a 0fr/1fr grid row that animates its own height
 * without the caller measuring anything.
 */
export function CollapseBody({
	collapsed,
	children,
	bleed = false,
	inert = false,
	durationMs = 300,
	className = "",
	id,
}: CollapseBodyProps) {
	return (
		<div
			id={id}
			className={`grid transition-[grid-template-rows] ${durationMs === 200 ? "duration-200" : "duration-300"} ease-in-out ${
				collapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"
			}`}
		>
			<div
				className={`overflow-hidden${bleed && !collapsed ? " p-4 -m-4" : ""}${className ? ` ${className}` : ""}`}
				inert={inert && collapsed}
			>
				{children}
			</div>
		</div>
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
		{ enabled: !!storageKey, deserialize: storedBool },
	);

	const toggle = useCallback(() => {
		setCollapsed((prev) => !prev);
	}, [setCollapsed]);

	return { collapsed, toggle };
}
