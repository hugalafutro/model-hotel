import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { ChevronRight } from "@/lib/icons";

/**
 * One collapsible section of cards: the custom groups, or one letter of the
 * auto groups. The header is a button so the whole line toggles; the body
 * collapses through grid-template-rows so it animates without measuring.
 */
export function GroupSection({
	id,
	title,
	count,
	collapsed,
	onToggle,
	children,
}: {
	id: string;
	title: ReactNode;
	count: number;
	collapsed: boolean;
	onToggle: () => void;
	children: ReactNode;
}) {
	const { t } = useTranslation();
	return (
		<section id={id}>
			<button
				type="button"
				onClick={onToggle}
				className="flex items-center gap-3 mb-3 w-full text-left group"
			>
				<ChevronRight
					size={16}
					className={`ui-icon-btn-in-group text-gray-500 transition-transform ${collapsed ? "" : "rotate-90"}`}
				/>
				<span className="ui-link-accent-in-group text-lg font-bold text-(--accent)">
					{title}
				</span>
				<div className="flex-1 h-px bg-gray-700/50" />
				<span className="text-xs text-gray-500">
					{t("failover.group_count", { count })}
				</span>
			</button>
			<div
				className="grid transition-[grid-template-rows] duration-200 ease-in-out"
				style={{ gridTemplateRows: collapsed ? "0fr" : "1fr" }}
			>
				<div className="overflow-hidden">
					<div className="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-3 gap-4">
						{children}
					</div>
				</div>
			</div>
		</section>
	);
}
