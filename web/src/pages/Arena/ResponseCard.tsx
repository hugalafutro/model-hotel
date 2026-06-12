import {
	AlertCircle,
	ArrowLeftRight,
	CheckCircle2,
	CircleStop,
	RefreshCw,
	Trophy,
	X,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { Model } from "../../api/types";
import { CopyButton } from "../../components/CopyButton";
import { ModelDetailModal } from "../../components/ModelDetailPanel";
import { ModelReplyCard } from "../../components/ModelReplyCard";
import { useDisableModel } from "../../hooks/useDisableModel";
import { parseCapabilities, proxyModelID } from "../../utils/model";
import { VoteThumb } from "./shared";
import type { ResponseCardProps } from "./types";

export function ResponseCard({
	response,
	vote,
	slotKey,
	roundIdx,
	matchupIdx,
	onVote,
	onRetry,
	onSwapModel,
	onCancelSlot,
	showVote,
	enabledModels,
	params,
}: ResponseCardProps) {
	const { t } = useTranslation();
	const [detailModel, setDetailModel] = useState<Model | null>(null);
	const disableModelMutation = useDisableModel(enabledModels);
	const isWinner = vote === slotKey;
	const isLoser = vote !== null && vote !== slotKey;

	const modelObj = enabledModels.find(
		(m) => proxyModelID(m.provider_name, m.model_id) === response.model,
	);

	return (
		<>
			<ModelReplyCard
				model={response.model}
				content={response.content}
				thinkingContent={response.thinkingContent}
				error={response.error}
				metrics={response.metrics}
				isStreaming={!response.done}
				startTimeMs={response.startTimeMs}
				isWinner={isWinner}
				isLoser={isLoser}
				shortenModelName={true}
				showInfoIcon={true}
				params={params}
				isReasoningModel={
					!!modelObj && !!parseCapabilities(modelObj.capabilities).reasoning
				}
				onModelNameClick={modelObj ? () => setDetailModel(modelObj) : undefined}
				onDisableModel={
					response.error && response.model
						? () => disableModelMutation.mutate(response.model)
						: undefined
				}
				afterModel={
					response.error && response.done ? (
						<button
							type="button"
							onClick={() =>
								onSwapModel(roundIdx, matchupIdx, slotKey, response.model)
							}
							className="shrink-0 text-red-400 hover:text-red-300 transition-colors"
							title={t("arena.swapModel.title")}
							aria-label={t("arena.swapModel.title")}
						>
							<X size={14} />
						</button>
					) : null
				}
				headerEnd={
					<>
						{response.done && !response.error && (
							<>
								<span title={t("arena.completed.title")}>
									<CheckCircle2 size={14} className="text-green-400" />
								</span>
								<button
									type="button"
									onClick={() => onRetry(roundIdx, matchupIdx, slotKey)}
									className="ui-icon-btn"
									title={t("arena.reroll.title")}
									aria-label={t("arena.reroll.title")}
								>
									<RefreshCw size={14} />
								</button>
								<button
									type="button"
									onClick={() =>
										onSwapModel(roundIdx, matchupIdx, slotKey, response.model)
									}
									className="ui-icon-btn"
									title={t("arena.swapModel.title")}
									aria-label={t("arena.swapModel.title")}
								>
									<ArrowLeftRight size={14} />
								</button>
							</>
						)}
						{response.error && (
							<>
								<span title={t("arena.error.title")}>
									<AlertCircle size={14} className="text-red-400" />
								</span>
								<button
									type="button"
									onClick={() => onRetry(roundIdx, matchupIdx, slotKey)}
									className="text-(--text-tertiary) hover:text-(--text-primary) transition-colors"
									title={t("arena.retry.title")}
									aria-label={t("arena.retry.title")}
								>
									<RefreshCw size={14} />
								</button>
							</>
						)}
						<button
							type="button"
							onClick={() =>
								onCancelSlot(roundIdx, matchupIdx, slotKey, response.model)
							}
							className={`text-red-400/60 hover:text-red-400 transition-colors ${response.done ? "invisible pointer-events-none" : ""}`}
							title={t("arena.cancel.title")}
							aria-label={t("arena.cancel.title")}
							disabled={response.done}
							tabIndex={response.done ? -1 : undefined}
						>
							<CircleStop size={14} />
						</button>
						{isWinner && (
							<span title={t("arena.winner.title")}>
								<Trophy size={14} className="text-amber-400" />
							</span>
						)}
						{response.done && response.content && (
							<CopyButton text={response.content} size={12} />
						)}
						{showVote && (
							<button
								type="button"
								onClick={
									vote === null
										? () => onVote(roundIdx, matchupIdx, slotKey)
										: undefined
								}
								disabled={vote !== null}
								className={`flex items-center gap-1 transition-all ${
									vote === null ? "" : "cursor-default"
								} ${
									isWinner
										? "text-green-400 hover:text-green-300"
										: "text-(--text-tertiary) hover:text-(--text-secondary)"
								}`}
								title={vote === null ? t("arena.vote.title") : undefined}
								aria-label={
									vote === null ? t("arena.vote.title") : t("arena.vote.voted")
								}
							>
								<VoteThumb
									size={18}
									isWinner={isWinner}
									animating={vote === null}
								/>
							</button>
						)}
					</>
				}
				footerEnd={null}
				className="flex flex-col h-full"
				headerClassName="px-4 py-1.5 border-b border-(--border-subtle)"
				bodyClassName="px-4 pt-0 overflow-y-auto flex-1 min-h-0"
				footerClassName="px-4 py-0.5 border-t border-(--border-subtle)"
			/>
			{detailModel && (
				<ModelDetailModal
					model={detailModel}
					onClose={() => setDetailModel(null)}
				/>
			)}
		</>
	);
}
