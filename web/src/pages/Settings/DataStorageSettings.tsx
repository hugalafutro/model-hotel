import { Database } from "lucide-react";
import { useTranslation } from "react-i18next";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSelect } from "../../components/SettingsSelect";
import { Toggle } from "../../components/Toggle";
import { useStorage } from "../../context/StorageContext";
import { useToast } from "../../context/ToastContext";
import {
	clearArenaHistory,
	getArenaHistoryCount,
} from "../../utils/arenaHistory";
import { clearProviderCache, getProviderCacheCount } from "./constants";

interface DataStorageSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function DataStorageSettings({
	collapsed,
	onToggle,
}: DataStorageSettingsProps) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const {
		persistChat,
		setPersistChat,
		persistArena,
		setPersistArena,
		persistConversation,
		setPersistConversation,
		arenaHistoryEnabled,
		setArenaHistoryEnabled,
		arenaHistoryLimit,
		setArenaHistoryLimit,
	} = useStorage();

	return (
		<SettingsSection
			icon={Database}
			title={t("settings.dataStorage.title")}
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					{t("settings.dataStorage.description")}
				</p>

				{/* Session Persistence */}
				<div>
					<h3 className="text-xs font-semibold uppercase tracking-wider text-gray-500 mb-3">
						{t("settings.dataStorage.sessionPersistence")}
					</h3>
					<div className="grid grid-cols-2 gap-x-8 gap-y-5">
						<div className="space-y-5">
							<div className="flex items-center justify-between">
								<div>
									<p className="text-sm font-medium text-gray-300">
										{t("settings.dataStorage.persistChat")}
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{t("settings.dataStorage.persistChatDescription")}
									</p>
								</div>
								<Toggle
									checked={persistChat}
									onChange={(v) => {
										const next = v;
										if (
											!next &&
											!confirm(t("settings.dataStorage.persistChatConfirm"))
										)
											return;
										setPersistChat(next);
										toast(
											next
												? t("settings.dataStorage.persistChatEnabled")
												: t("settings.dataStorage.persistChatDisabled"),
											next ? "success" : "info",
										);
									}}
								/>
							</div>

							<div className="flex items-center justify-between">
								<div>
									<p className="text-sm font-medium text-gray-300">
										{t("settings.dataStorage.persistArena")}
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{t("settings.dataStorage.persistArenaDescription")}
									</p>
								</div>
								<Toggle
									checked={persistArena}
									onChange={(v) => {
										const next = v;
										if (
											!next &&
											!confirm(t("settings.dataStorage.persistArenaConfirm"))
										)
											return;
										setPersistArena(next);
										toast(
											next
												? t("settings.dataStorage.persistArenaEnabled")
												: t("settings.dataStorage.persistArenaDisabled"),
											next ? "success" : "info",
										);
									}}
								/>
							</div>

							<div className="flex items-center justify-between">
								<div>
									<p className="text-sm font-medium text-gray-300">
										{t("settings.dataStorage.persistConversation")}
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{t("settings.dataStorage.persistConversationDescription")}
									</p>
								</div>
								<Toggle
									checked={persistConversation}
									onChange={(v) => {
										const next = v;
										if (
											!next &&
											!confirm(
												t("settings.dataStorage.persistConversationConfirm"),
											)
										)
											return;
										setPersistConversation(next);
										toast(
											next
												? t("settings.dataStorage.persistConversationEnabled")
												: t("settings.dataStorage.persistConversationDisabled"),
											next ? "success" : "info",
										);
									}}
								/>
							</div>
						</div>
						<div className="space-y-5" />
					</div>
				</div>

				{/* Arena History */}
				<div>
					<h3 className="text-xs font-semibold uppercase tracking-wider text-gray-500 mb-3">
						{t("settings.dataStorage.arenaHistory")}
					</h3>
					<div className="grid grid-cols-2 gap-x-8 gap-y-5">
						<div className="space-y-5">
							<div className="flex items-center justify-between">
								<div>
									<p className="text-sm font-medium text-gray-300">
										{t("settings.dataStorage.saveMatchHistory")}
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{t("settings.dataStorage.saveMatchHistoryDescription")}
									</p>
								</div>
								<Toggle
									checked={arenaHistoryEnabled}
									onChange={(v) => {
										const next = v;
										setArenaHistoryEnabled(next);
										toast(
											next
												? t("settings.dataStorage.saveMatchHistoryEnabled")
												: t("settings.dataStorage.saveMatchHistoryDisabled"),
											next ? "success" : "info",
										);
									}}
								/>
							</div>

							<div className="flex items-center justify-between">
								<div>
									<p className="text-sm font-medium text-gray-300">
										{t("settings.dataStorage.clearHistory")}
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{t("settings.dataStorage.clearHistoryDescription", {
											count: getArenaHistoryCount(),
										})}
									</p>
								</div>
								<button
									type="button"
									onClick={() => {
										if (
											confirm(t("settings.dataStorage.clearHistoryConfirm"))
										) {
											clearArenaHistory();
											toast(
												t("settings.dataStorage.clearHistoryAllCleared"),
												"info",
											);
										}
									}}
									className="ui-btn ui-btn-danger text-xs px-3 py-1.5"
									disabled={getArenaHistoryCount() === 0}
								>
									{t("settings.dataStorage.clearHistoryAll")}
								</button>
							</div>
						</div>

						<div className="space-y-5">
							<SettingsSelect
								id="history-limit"
								label={t("settings.dataStorage.maxSavedMatches")}
								value={String(arenaHistoryLimit)}
								options={[
									{
										value: "10",
										label: t("settings.dataStorage.matches.10"),
									},
									{
										value: "25",
										label: t("settings.dataStorage.matches.25"),
									},
									{
										value: "50",
										label: t("settings.dataStorage.matches.50"),
									},
									{
										value: "100",
										label: t("settings.dataStorage.matches.100"),
									},
								]}
								onChange={(v) => {
									const val = Number(v);
									setArenaHistoryLimit(val);
									toast(
										t("settings.dataStorage.historyLimitToast", { count: val }),
										"success",
									);
								}}
								disabled={!arenaHistoryEnabled}
								description={t(
									"settings.dataStorage.maxSavedMatches.description",
								)}
							/>
						</div>
					</div>
				</div>

				{/* Cache & Resets */}
				<div>
					<h3 className="text-xs font-semibold uppercase tracking-wider text-gray-500 mb-3">
						{t("settings.dataStorage.cacheAndResets")}
					</h3>
					<div className="grid grid-cols-2 gap-x-8 gap-y-5">
						<div className="space-y-5">
							<div className="flex items-center justify-between">
								<div>
									<p className="text-sm font-medium text-gray-300">
										{t("settings.dataStorage.providerQuotaCache")}
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{t("settings.dataStorage.providerQuotaCacheDescription", {
											count: getProviderCacheCount(),
										})}
									</p>
								</div>
								<button
									type="button"
									onClick={() => {
										if (confirm(t("settings.dataStorage.clearCacheConfirm"))) {
											clearProviderCache();
											toast(
												t("settings.dataStorage.clearCacheCleared"),
												"info",
											);
										}
									}}
									className="ui-btn ui-btn-danger text-xs px-3 py-1.5"
									disabled={getProviderCacheCount() === 0}
								>
									{t("settings.dataStorage.clearCache")}
								</button>
							</div>
						</div>

						<div className="space-y-5">
							<div className="flex items-center justify-between">
								<div>
									<p className="text-sm font-medium text-gray-300">
										{t("settings.dataStorage.dismissedErrorBanners")}
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{t("settings.dataStorage.dismissedErrorBannersDescription")}
									</p>
								</div>
								<button
									type="button"
									onClick={() => {
										localStorage.removeItem("dismissedAppErrorKey");
										localStorage.removeItem("dismissedReqErrorKey");
										window.dispatchEvent(
											new CustomEvent("dismissedErrorsReset"),
										);
										toast(
											t("settings.dataStorage.resetDismissedBanners"),
											"info",
										);
									}}
									className="ui-btn ui-btn-danger text-xs px-3 py-1.5"
								>
									{t("settings.dataStorage.reset")}
								</button>
							</div>
						</div>
					</div>
				</div>
			</div>
		</SettingsSection>
	);
}
