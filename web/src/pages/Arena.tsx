import { useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { ArenaHistoryModal } from "../components/ArenaHistoryModal";
import { PageHeader } from "../components/PageHeader";
import { parseCapabilities, proxyModelID } from "../utils/model";
import { ArenaBracketBar } from "./Arena/ArenaBracketBar";
import { ArenaControls } from "./Arena/ArenaControls";
import { ArenaResponseGrid } from "./Arena/ArenaResponseGrid";
import { ParamEditorModal } from "./Arena/ParamEditorModal";
import { useArena } from "./Arena/useArena";
import { WinnerSummaryModal } from "./Arena/WinnerSummaryModal";

export function Arena() {
	const { t } = useTranslation();
	// Refs are split off so the ref-free `arena` rest object can be read freely
	// in the JSX without tripping the react-hooks/refs lint.
	const {
		refs: { abortMapRef },
		...arena
	} = useArena();

	const displayNameMap = useMemo(() => {
		const map = new Map<string, string>();
		for (const m of arena.enabledModels) {
			const key = proxyModelID(m.provider_name, m.model_id);
			const name = m.display_name || m.name || m.model_id;
			if (name && name !== m.model_id) map.set(key, name);
		}
		return map;
	}, [arena.enabledModels]);

	// Auto-scroll the page viewport during streaming so response cards stay visible.
	// Uses instant scroll because Firefox cancels in-progress smooth scrolls
	// when scrollTo is called again rapidly during streaming.
	const streamingContentLen = arena.rounds.reduce(
		(sum, round) =>
			sum +
			round.matchups.reduce((s, mu) => {
				if (mu.responseA) s += (mu.responseA.content || "").length;
				if (mu.responseB) s += (mu.responseB.content || "").length;
				return s;
			}, 0),
		0,
	);
	// biome-ignore lint/correctness/useExhaustiveDependencies: streamingContentLen triggers re-scroll on streaming updates
	useEffect(() => {
		if (!arena.isRunning) return;
		const nearBottom =
			document.documentElement.scrollHeight -
				window.scrollY -
				window.innerHeight <
			200;
		if (nearBottom) {
			window.scrollTo({
				top: document.documentElement.scrollHeight,
				behavior: "instant",
			});
		}
	}, [streamingContentLen, arena.isRunning]);

	return (
		<div className="flex flex-col gap-6 min-h-full">
			{/* Header */}
			<PageHeader
				icon={arena.arenaIcon}
				title={t(
					arena.arenaMode === "competition"
						? "arena.title.arena"
						: "arena.title.compare",
				)}
				description={
					arena.arenaMode === "competition"
						? t("arena.description.arena")
						: t("arena.description.compare")
				}
			/>

			<ArenaControls arena={arena} abortMapRef={abortMapRef} />

			<ArenaBracketBar arena={arena} displayNameMap={displayNameMap} />

			<ArenaResponseGrid arena={arena} abortMapRef={abortMapRef} />

			{/* Winner Modal */}
			{arena.winnerModal && (
				<WinnerSummaryModal
					winner={arena.winnerModal.winner}
					rounds={arena.winnerModal.rounds}
					onClose={() => arena.setWinnerModal(null)}
				/>
			)}

			{/* Inline Param Editor */}
			{arena.paramEditorModel && (
				<ParamEditorModal
					modelId={arena.paramEditorModel}
					params={arena.modelParams[arena.paramEditorModel] ?? {}}
					onChange={(params) => {
						const model = arena.paramEditorModel;
						if (model) {
							arena.setModelParams((prev) => ({
								...prev,
								[model]: params,
							}));
						}
					}}
					onClose={() => arena.setParamEditorModel(null)}
					knownProviders={arena.enabledModels.map((m) => m.provider_name)}
					reasoning={(() => {
						const model = arena.enabledModels.find(
							(m) =>
								`${m.provider_name}/${m.model_id}` === arena.paramEditorModel,
						);
						return model
							? (parseCapabilities(model.capabilities).reasoning ?? false)
							: false;
					})()}
				/>
			)}

			{/* Match History Modal */}
			{arena.showHistoryModal && (
				<ArenaHistoryModal onClose={() => arena.setShowHistoryModal(false)} />
			)}
		</div>
	);
}
