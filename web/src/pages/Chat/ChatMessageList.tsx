import { useTranslation } from "react-i18next";
import { CircleStop, Mic, RefreshCw, Trash2 } from "@/lib/icons";
import type { ChatMessage, Model } from "../../api/types";
import { CopyButton } from "../../components/CopyButton";
import { MarkdownContent } from "../../components/MarkdownContent";
import { ModelReplyCard } from "../../components/ModelReplyCard";
import { CHAT_PERSONAS } from "../../data/presets";
import { useDisableModel } from "../../hooks/useDisableModel";
import { formatTime } from "../../utils/format";
import { isReasoningModel } from "../../utils/model";

export interface ChatMessageListProps {
	messages: ChatMessage[];
	chatSubMode: "chat" | "conversation";
	isStreaming: boolean;
	selectedModelB: string;
	enabledModels: Model[];
	onStopConversation: () => void;
	onStop: () => void;
	onRegenerate: () => void;
	onDeleteMessage: (index: number) => void;
	activePersonaIdB: string | null;
	conversationActivePersonaIdA: string | null;
	chatActivePersonaId: string | null;
}

export function ChatMessageList({
	messages,
	chatSubMode,
	isStreaming,
	selectedModelB,
	enabledModels,
	onStopConversation,
	onStop,
	onRegenerate,
	onDeleteMessage,
	activePersonaIdB,
	conversationActivePersonaIdA,
	chatActivePersonaId,
}: ChatMessageListProps) {
	const { t } = useTranslation();
	const disableModelMutation = useDisableModel(enabledModels);
	const lastAssistantIdx = messages.findLastIndex(
		(m) => m.role === "assistant",
	);

	// Conversation turns are numbered by position among the assistant replies:
	// counted once here rather than re-scanned for every message.
	const turnNumbers = new Map<number, number>();
	let assistantSeen = 0;
	messages.forEach((m, i) => {
		if (m.role === "assistant") turnNumbers.set(i, ++assistantSeen);
	});

	return (
		<>
			{messages.map((msg, i) => {
				if (msg.role === "system") return null;
				const isUser = msg.role === "user";
				const isStreamingThis = isStreaming && i === messages.length - 1;
				const isModelB =
					msg.role === "assistant" && msg.model === selectedModelB;
				const isLastAssistant = i === lastAssistantIdx;
				// In conversation mode, only show delete on last assistant (or currently streaming)
				const canDelete =
					chatSubMode === "chat" ||
					(isLastAssistant && !isStreaming) ||
					(isStreamingThis && isLastAssistant);

				const turnNumber =
					chatSubMode === "conversation" ? turnNumbers.get(i) : undefined;

				// Persona lookup for conversation mode
				const personaForModel = isModelB
					? CHAT_PERSONAS.find((p) => p.id === activePersonaIdB)
					: chatSubMode === "conversation"
						? CHAT_PERSONAS.find((p) => p.id === conversationActivePersonaIdA)
						: CHAT_PERSONAS.find((p) => p.id === chatActivePersonaId);
				const personaName =
					msg.role === "assistant" && personaForModel
						? `${personaForModel.icon} ${t(personaForModel.label)}`
						: undefined;
				const personaTooltip = personaForModel
					? t(personaForModel.systemPrompt)
					: undefined;

				/* ── User message ── */
				if (isUser) {
					// In conversation mode, user message is centered and gray
					const isConversationMode = chatSubMode === "conversation";
					return (
						<div
							key={`user-${msg.timestamp}`}
							className={`flex ${isConversationMode ? "justify-center" : "justify-end"}`}
						>
							<div
								className={`max-w-[80%] p-2.5 ${isConversationMode ? "bg-gray-500/20 text-(--text-primary) border border-gray-500/30" : "bg-(--accent) text-white"}`}
								style={{
									borderRadius: "var(--radius-card)",
								}}
							>
								{msg.imageUrl && (
									<img
										src={msg.imageUrl}
										alt={t("chat.aria.userAttachment")}
										className="max-h-48 rounded mb-1.5"
									/>
								)}
								{msg.audioAttachment && (
									<div
										className={`flex items-center gap-1.5 mb-1.5 text-xs ${isConversationMode ? "text-(--text-secondary)" : "text-white/80"}`}
									>
										<Mic size={12} />
										<span>
											{msg.audioAttachment.format.toUpperCase()}{" "}
											{t("chat.message.audio")}
										</span>
									</div>
								)}
								{(msg.content || (!msg.imageUrl && !msg.audioAttachment)) && (
									<MarkdownContent
										className={`${isConversationMode ? "" : "[&_strong]:text-white [&_em]:text-white/80"}`}
									>
										{msg.content}
									</MarkdownContent>
								)}
								<div
									className={`flex items-center gap-3 text-[11px] mt-0.5 ${isConversationMode ? "text-(--text-secondary)" : "text-white/60"}`}
								>
									<span>{formatTime(msg.timestamp)}</span>
									<CopyButton
										text={msg.content}
										size={10}
										className={`inline-flex items-center transition-all ${isConversationMode ? "text-(--text-secondary) hover:text-(--text-primary)" : "text-white hover:drop-shadow-[var(--glow-text-primary)]"}`}
									/>
								</div>
							</div>
						</div>
					);
				}

				/* ── Assistant message: Model B on the right, Model A / chat on the left ── */
				const isB = chatSubMode === "conversation" && isModelB;
				return (
					<div
						key={`assistant-${msg.timestamp}`}
						className={`flex ${isB ? "justify-end" : "justify-start"}`}
					>
						<div className="max-w-[80%]">
							<ModelReplyCard
								model={msg.model || ""}
								content={msg.content}
								thinkingContent={msg.thinkingContent}
								error={msg.error}
								metrics={msg.metrics}
								isStreaming={isStreamingThis}
								startTimeMs={isStreamingThis ? msg.timestamp : undefined}
								shortenModelName={false}
								isReasoningModel={isReasoningModel(
									enabledModels,
									msg.model || "",
								)}
								tint={isB ? "blue" : undefined}
								personaName={personaName}
								personaTooltip={personaTooltip}
								turnNumber={turnNumber}
								params={msg.params}
								onDisableModel={
									msg.error && msg.model
										? () => disableModelMutation.mutate(msg.model as string)
										: undefined
								}
								headerEnd={
									isStreamingThis ? (
										<button
											type="button"
											onClick={
												chatSubMode === "conversation"
													? onStopConversation
													: onStop
											}
											className="text-red-400/60 hover:text-red-400 transition-colors ml-1"
											title={t("chat.aria.cancel")}
											aria-label={t("chat.aria.cancel")}
										>
											<CircleStop size={14} />
										</button>
									) : (
										i === lastAssistantIdx &&
										chatSubMode === "chat" && (
											<button
												type="button"
												onClick={onRegenerate}
												className="ui-icon-btn ml-1"
												title={t("chat.aria.regenerate")}
												aria-label={t("chat.aria.regenerate")}
											>
												<RefreshCw size={14} />
											</button>
										)
									)
								}
								footerStart={<span>{formatTime(msg.timestamp)}</span>}
								footerEnd={
									<div className="flex items-center gap-2">
										<CopyButton text={msg.content} size={10} />
										{canDelete && (
											<button
												type="button"
												className="ui-icon-btn ui-icon-btn-danger inline-flex items-center"
												onClick={() => onDeleteMessage(i)}
												title={t("chat.aria.deleteMessage")}
												aria-label={t("chat.aria.deleteMessage")}
											>
												<Trash2 size={10} />
											</button>
										)}
									</div>
								}
								className={`rounded-xl p-4 ${isB ? "rounded-br-sm" : "rounded-bl-sm"}`}
								headerClassName="mb-2"
								footerClassName="mt-2"
							/>
						</div>
					</div>
				);
			})}
		</>
	);
}
