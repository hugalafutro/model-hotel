import { useTranslation } from "react-i18next";

/**
 * The status strip under a virtualised table: which rows of the total are on
 * screen on the left, what is still loading on the right.
 */
export function VirtualTableFooter({
	range,
	isLoadingBefore,
	isLoadingAfter,
	children,
}: {
	range: string;
	isLoadingBefore: boolean;
	isLoadingAfter: boolean;
	/** Extra status shown after the loading labels. */
	children?: React.ReactNode;
}) {
	const { t } = useTranslation();
	return (
		<div className="flex items-center justify-between px-3 py-2 text-xs text-gray-500 border-t border-gray-800">
			<span>{range}</span>
			<span className="flex items-center gap-2">
				{isLoadingBefore && (
					<span className="text-(--accent)">{t("common.loadingNewer")}</span>
				)}
				{isLoadingAfter && (
					<span className="text-(--accent)">{t("common.loadingOlder")}</span>
				)}
				{children}
			</span>
		</div>
	);
}
