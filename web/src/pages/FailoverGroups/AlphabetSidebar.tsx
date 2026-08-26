import { useTranslation } from "react-i18next";

const scrollTo = (id: string) =>
	document
		.getElementById(id)
		?.scrollIntoView({ behavior: "smooth", block: "start" });

/**
 * The sticky letter rail beside the list. Only worth its column once there
 * are enough sections to jump between, so it stays hidden below four letters
 * unless a custom section exists to jump to.
 */
export function AlphabetSidebar({
	letters,
	hasCustom,
}: {
	letters: string[];
	hasCustom: boolean;
}) {
	const { t } = useTranslation();
	if (!(letters.length > 3 || hasCustom)) return null;
	return (
		<nav
			aria-label={t("failoverGroups.alphabetSidebar")}
			className="hidden xl:flex flex-col items-center gap-1 pt-2 sticky top-4 self-start"
		>
			{hasCustom && (
				<button
					type="button"
					onClick={() => scrollTo("failover-section-custom")}
					className="ui-link-accent text-xs font-medium text-(--accent) px-1.5 py-0.5 rounded"
					aria-label={t("failover.nav_custom")}
				>
					★
				</button>
			)}
			{letters.map((letter) => (
				<button
					key={letter}
					type="button"
					onClick={() => scrollTo(`failover-section-${letter}`)}
					className="ui-link-accent text-xs font-medium text-gray-500 px-1.5 py-0.5 rounded"
				>
					{letter}
				</button>
			))}
		</nav>
	);
}
