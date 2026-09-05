import { useTranslation } from "react-i18next";
import { Bot } from "@/lib/icons";
import { ConversationConfig } from "../components/ConversationConfig";
import { ModelDetailPanel } from "../components/ModelDetailPanel";
import { PageHeader } from "../components/PageHeader";
import { ChatControls } from "./Chat/ChatControls";
import { ChatInputArea } from "./Chat/ChatInputArea";
import { ChatMessageList } from "./Chat/ChatMessageList";
import { useChat } from "./Chat/useChat";

export function Chat() {
	const { t } = useTranslation();
	// Refs are split off so the ref-free `chat` rest object can be read freely
	// in the JSX without tripping the react-hooks/refs lint.
	const {
		refs: { lastPromptRef, messagesContainerRef, imageInputRef, audioInputRef },
		...chat
	} = useChat();

	return (
		<div
			className={`flex flex-col gap-6 ${chat.chatSubMode === "conversation" ? "min-h-full" : "h-full overflow-hidden"}`}
		>
			{/* Header */}
			<PageHeader
				icon={chat.chatIcon}
				title={
					chat.chatSubMode === "chat"
						? t("chat.misc.titleChat")
						: t("chat.misc.titleConversation")
				}
				description={
					chat.chatSubMode === "chat"
						? t("chat.misc.descriptionChat")
						: t("chat.misc.descriptionConversation")
				}
			/>

			<ChatControls chat={chat} lastPromptRef={lastPromptRef} />

			{/* Conversation Config */}
			{chat.chatSubMode === "conversation" && (
				<ConversationConfig
					maxTurns={chat.maxTurns}
					onMaxTurnsChange={chat.setMaxTurns}
					turnDelayMs={chat.turnDelayMs}
					onTurnDelayMsChange={chat.setTurnDelayMs}
					conversationState={chat.conversationState}
					currentTurn={chat.currentTurn}
					turnCountdown={chat.turnCountdown}
					configCollapsed={chat.configCollapsed}
					onToggleCollapsed={() => chat.setConfigCollapsed((c) => !c)}
					input={chat.input}
					onInputChange={chat.setInput}
					onStart={() => {
						chat.setControlsCollapsed(true);
						chat.runConversation(false);
					}}
					onContinue={() => {
						chat.setControlsCollapsed(true);
						chat.runConversation(true);
					}}
					onRetry={() => {
						chat.setControlsCollapsed(true);
						chat.handleRetryConversation();
					}}
					onStop={() => {
						chat.setControlsCollapsed(false);
						chat.handleStopConversation();
					}}
					canStart={chat.canStartConversation}
					disabledReason={chat.conversationDisabledReason}
					selectedModel={chat.selectedModel}
					selectedModelB={chat.selectedModelB}
					failedModel={chat.failedConversationModel}
				/>
			)}

			{/* Chat Area: Model Details + Messages */}
			<div
				className={`flex gap-4 flex-1 ${chat.chatSubMode === "conversation" ? "overflow-visible" : "min-h-0 overflow-hidden"}`}
			>
				{/* Sidebar */}
				<div
					className={`shrink-0 flex flex-col ${
						chat.chatSubMode === "conversation"
							? "w-1/3 gap-3 overflow-visible"
							: "min-h-0 overflow-y-auto w-1/4"
					}`}
				>
					{chat.chatSubMode === "chat" ? (
						chat.selectedModelObj ? (
							<ModelDetailPanel
								model={chat.selectedModelObj}
								params={chat.messageParams}
								onParamsChange={chat.setMessageParams}
								pulseBorder={
									chat.isStreaming &&
									chat.chatSubMode === "chat" &&
									chat.messages.length > 0 &&
									chat.messages[chat.messages.length - 1].role ===
										"assistant" &&
									chat.messages[chat.messages.length - 1].model ===
										chat.chatSelectedModel
								}
							/>
						) : (
							<div className="ui-card p-4 flex flex-col items-center justify-center text-(--text-tertiary) text-xs">
								<Bot size={32} strokeWidth={1} className="mb-2 opacity-40" />
								<p>{t("chat.placeholder.selectModel")}</p>
							</div>
						)
					) : (
						<>
							{chat.selectedModelObj ? (
								<ModelDetailPanel
									model={chat.selectedModelObj}
									params={chat.messageParams}
									onParamsChange={chat.setMessageParams}
									collapsible
									tint="default"
									pulseBorder={
										chat.isStreaming &&
										chat.messages.length > 0 &&
										chat.messages[chat.messages.length - 1].role ===
											"assistant" &&
										chat.messages[chat.messages.length - 1].model ===
											chat.selectedModel
									}
								/>
							) : (
								<div className="ui-card p-3 flex items-center justify-center text-(--text-tertiary) text-xs">
									<Bot size={20} className="mr-2 opacity-40" />
									{t("chat.placeholder.selectModelA")}
								</div>
							)}
							{chat.selectedModelObjB ? (
								<ModelDetailPanel
									model={chat.selectedModelObjB}
									params={chat.messageParamsB}
									onParamsChange={chat.setMessageParamsB}
									collapsible
									tint="blue"
									pulseBorder={
										chat.isStreaming &&
										chat.messages.length > 0 &&
										chat.messages[chat.messages.length - 1].role ===
											"assistant" &&
										chat.messages[chat.messages.length - 1].model ===
											chat.selectedModelB
									}
								/>
							) : (
								<div className="ui-card p-3 flex items-center justify-center text-(--text-tertiary) text-xs">
									<Bot size={20} className="mr-2 opacity-40" />
									{t("chat.placeholder.selectModelB")}
								</div>
							)}
						</>
					)}
				</div>

				{/* Messages */}
				<div
					ref={messagesContainerRef}
					className={`flex-1 pr-1 space-y-4 ${
						chat.chatSubMode === "conversation"
							? "overflow-visible"
							: "min-h-0 overflow-y-auto pb-4"
					}`}
				>
					{chat.messages.length === 0 && (
						<div className="flex flex-col items-center justify-center py-20 text-(--text-tertiary)">
							{chat.chatSubMode === "chat" ? (
								<Bot size={48} strokeWidth={1} className="mb-4 opacity-40" />
							) : (
								<div className="relative mb-4 w-20 h-12 flex items-center justify-center">
									<Bot
										size={48}
										strokeWidth={1}
										className="opacity-40 absolute left-0"
									/>
									<Bot
										size={48}
										strokeWidth={1}
										className="opacity-40 absolute right-0 scale-x-[-1]"
									/>
								</div>
							)}
							<p>
								{chat.chatSubMode === "chat"
									? t("chat.message.emptyChat")
									: t("chat.message.emptyConversation")}
							</p>
						</div>
					)}

					<ChatMessageList
						messages={chat.messages}
						chatSubMode={chat.chatSubMode}
						isStreaming={chat.isStreaming}
						selectedModelB={chat.selectedModelB}
						enabledModels={chat.enabledModels}
						onStopConversation={chat.handleStopConversation}
						onStop={chat.handleStop}
						onRegenerate={chat.handleRegenerate}
						onDeleteMessage={chat.handleDeleteMessage}
						activePersonaIdB={chat.activePersonaIdB}
						conversationActivePersonaIdA={chat.conversationActivePersonaIdA}
						chatActivePersonaId={chat.chatActivePersonaId}
					/>
				</div>
			</div>

			<ChatInputArea
				chat={chat}
				lastPromptRef={lastPromptRef}
				imageInputRef={imageInputRef}
				audioInputRef={audioInputRef}
			/>
		</div>
	);
}
