import i18next from "i18next";

/**
 * The heading a tournament round is shown under. Lives here rather than beside
 * the bracket builders because both the Arena pages and the history modal in
 * components/ label rounds, and a component may not reach into pages/.
 */
export function getRoundLabel(
	roundIdx: number,
	totalRounds: number,
	arenaMode: string,
): string {
	if (arenaMode === "compare") return i18next.t("arena.round.generation");
	if (totalRounds === 1) return i18next.t("arena.round.match");
	if (roundIdx === totalRounds - 1) return i18next.t("arena.round.final");
	if (roundIdx === totalRounds - 2) return i18next.t("arena.round.semifinals");
	if (roundIdx === totalRounds - 3)
		return i18next.t("arena.round.quarterfinals");
	return i18next.t("arena.round.numbered", { num: roundIdx + 1 });
}
