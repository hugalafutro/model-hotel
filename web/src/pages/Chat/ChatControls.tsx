import { useTranslation } from "react-i18next";
import {
	CircleStop,
	Eraser,
	MessageSquare,
	RotateCcw,
	Users,
} from "@/lib/icons";
import { ActionIconButton } from "../../components/ActionIconButton";
import { CollapsibleToggle } from "../../components/CollapsibleToggle";
import { ModelPicker } from "../../components/ModelPicker";
import { PersonaPicker } from "../../components/PersonaPicker";
import { SubModeToggle } from "../../components/SubModeToggle";
import { CHAT_PERSONAS } from "../../data/presets";
import type { ChatRefs, ChatView } from "./useChat";

/** The controls card: sub-mode, model and persona pickers, the conversation setup, and the clear and reset actions. */
export function ChatControls({
	chat,
	lastPromptRef,
}: {
	chat: ChatView;
	lastPromptRef: ChatRefs["lastPromptRef"];
}) {
	const { t } = useTranslation();
	return (
		<>
			{/* Controls */}
			<div className="ui-card p-4 shrink-0">
				<div className="flex items-center justify-between">
					<div className="flex items-center gap-3">
						<span className="text-sm font-semibold text-(--text-primary)">
							{t("chat.controls.title")}
						</span>
						<SubModeToggle
							options={[
								{
									value: "chat" as const,
									label: t("chat.chatWithAi"),
									icon: MessageSquare,
								},
								{
									value: "conversation" as const,
									label: t("chat.aiConversation"),
									icon: Users,
								},
							]}
							value={chat.chatSubMode}
							onChange={chat.setChatSubMode}
						/>
					</div>
					<div className="flex items-center gap-1">
						{(chat.messages.length > 0 ||
							(chat.chatSubMode === "conversation" &&
								(chat.conversationState === "completed" ||
									chat.conversationState === "paused" ||
									chat.conversationState === "error")) ||
							chat.selectedModel ||
							(chat.chatSubMode === "conversation" && chat.selectedModelB) ||
							!!chat.activePersonaId ||
							!!chat.systemPrompt.trim() ||
							(chat.chatSubMode === "conversation" &&
								(!!chat.activePersonaIdB || !!chat.systemPromptB.trim()))) && (
							<>
								{chat.isStreaming && chat.chatSubMode === "chat" && (
									<ActionIconButton
										icon={CircleStop}
										onClick={() => {
											chat.setControlsCollapsed(false);
											chat.handleStop();
										}}
										title={t("chat.controls.stop")}
										color="red"
									/>
								)}
								{/* Light reset: clear messages/results only, keep model/persona/params */}
								{chat.messages.length > 0 && (
									<ActionIconButton
										icon={Eraser}
										onClick={() => {
											if (chat.chatSubMode === "conversation") {
												chat.clearConversationAbort();
											}
											chat.setMessages([]);
											chat.setInput(lastPromptRef.current);
											chat.setConversationState("idle");
											chat.setCurrentTurn(0);
											chat.setTurnCountdown(0);
											chat.setIsStreaming(false);
											chat.toast(
												chat.chatSubMode === "chat"
													? t("chat.toast.chatCleared")
													: t("chat.toast.conversationCleared"),
												"info",
											);
										}}
										title={t("chat.controls.clearMessages")}
										color="amber"
										pulse={
											chat.chatSubMode === "conversation" &&
											(chat.conversationState === "completed" ||
												chat.conversationState === "paused" ||
												chat.conversationState === "error")
										}
									/>
								)}
								<ActionIconButton
									icon={RotateCcw}
									onClick={() => chat.setPendingFullReset(true)}
									title={t("chat.controls.resetAll")}
									color="red"
								/>
							</>
						)}
						<CollapsibleToggle
							collapsed={chat.controlsCollapsed}
							onToggle={() => chat.setControlsCollapsed((c) => !c)}
						/>
					</div>
				</div>
				<div
					className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
						chat.controlsCollapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"
					}`}
				>
					<div className="overflow-hidden">
						<div className="space-y-4 pt-4">
							{chat.chatSubMode === "chat" ? (
								<>
									<ModelPicker
										models={chat.enabledModels}
										selected={chat.selectedModel}
										onChange={chat.setSelectedModel}
										multi={false}
										onRandom={chat.handleRandomModel}
									/>
									<PersonaPicker
										personas={CHAT_PERSONAS}
										activePersonaId={chat.activePersonaId}
										systemPrompt={chat.systemPrompt}
										onActivePersonaChange={chat.setActivePersonaId}
										onSystemPromptChange={chat.setSystemPrompt}
										onRandom={chat.handleRandomPersona}
									/>
								</>
							) : (
								<div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
									<div>
										<label
											htmlFor="model-a-picker"
											className="text-sm font-semibold text-(--accent) mb-2 block"
										>
											{t("chat.controls.modelA")}
										</label>
										<ModelPicker
											id="model-a-picker"
											models={chat.enabledModels}
											selected={chat.selectedModel}
											onChange={chat.setSelectedModel}
											multi={false}
											onRandom={chat.handleRandomModel}
											disabled={chat.conversationState === "running"}
										/>
										<div className="mt-3">
											<PersonaPicker
												personas={CHAT_PERSONAS}
												activePersonaId={chat.activePersonaId}
												systemPrompt={chat.systemPrompt}
												onActivePersonaChange={chat.setActivePersonaId}
												onSystemPromptChange={chat.setSystemPrompt}
												onRandom={chat.handleRandomPersona}
												label={t("chat.controls.personaA")}
												disabled={chat.conversationState === "running"}
											/>
										</div>
									</div>
									<div>
										<label
											htmlFor="model-b-picker"
											className="text-sm font-semibold text-(--accent) mb-2 block"
										>
											{t("chat.controls.modelB")}
										</label>
										<ModelPicker
											id="model-b-picker"
											models={chat.enabledModels}
											selected={chat.selectedModelB}
											onChange={chat.setSelectedModelB}
											multi={false}
											onRandom={chat.handleRandomModelB}
											disabled={chat.conversationState === "running"}
										/>
										<div className="mt-3">
											<PersonaPicker
												personas={CHAT_PERSONAS}
												activePersonaId={chat.activePersonaIdB}
												systemPrompt={chat.systemPromptB}
												onActivePersonaChange={chat.setActivePersonaIdB}
												onSystemPromptChange={chat.setSystemPromptB}
												onRandom={chat.handleRandomPersonaB}
												label={t("chat.controls.personaB")}
												disabled={chat.conversationState === "running"}
											/>
										</div>
									</div>
								</div>
							)}
						</div>
					</div>
				</div>
			</div>
		</>
	);
}
