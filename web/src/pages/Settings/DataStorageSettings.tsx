import { Database } from "lucide-react";
import { SettingsSection } from "../../components/SettingsSection";
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
			title="Data Storage"
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-4">
				<p className="text-gray-400 text-sm">
					Manage browser-local session data. Persisted data survives page reload
					and browser restarts.
				</p>

				{/* Session Persistence */}
				<div className="space-y-3">
					<h3 className="text-xs font-semibold uppercase tracking-wider text-gray-500">
						Session Persistence
					</h3>
					<div className="flex items-center justify-between">
						<div>
							<p className="text-sm font-medium text-gray-300">Persist Chat</p>
							<p className="text-gray-500 text-xs mt-0.5">
								Remember messages, prompt, and persona across sessions
							</p>
						</div>
						<Toggle
							checked={persistChat}
							onChange={(v) => {
								const next = v;
								if (
									!next &&
									!confirm("This will clear all saved chat messages. Continue?")
								)
									return;
								setPersistChat(next);
								toast(
									next ? "Chat persistence enabled" : "Chat data cleared",
									next ? "success" : "info",
								);
							}}
						/>
					</div>

					<div className="flex items-center justify-between">
						<div>
							<p className="text-sm font-medium text-gray-300">Persist Arena</p>
							<p className="text-gray-500 text-xs mt-0.5">
								Remember bracket state and prompts across sessions
							</p>
						</div>
						<Toggle
							checked={persistArena}
							onChange={(v) => {
								const next = v;
								if (
									!next &&
									!confirm("This will clear all saved arena data. Continue?")
								)
									return;
								setPersistArena(next);
								toast(
									next ? "Arena persistence enabled" : "Arena data cleared",
									next ? "success" : "info",
								);
							}}
						/>
					</div>

					<div className="flex items-center justify-between">
						<div>
							<p className="text-sm font-medium text-gray-300">
								Persist AI Conversation
							</p>
							<p className="text-gray-500 text-xs mt-0.5">
								Remember conversation state and settings across sessions
							</p>
						</div>
						<Toggle
							checked={persistConversation}
							onChange={(v) => {
								const next = v;
								if (
									!next &&
									!confirm(
										"This will clear all saved conversation data. Continue?",
									)
								)
									return;
								setPersistConversation(next);
								toast(
									next
										? "Conversation persistence enabled"
										: "Conversation data cleared",
									next ? "success" : "info",
								);
							}}
						/>
					</div>
				</div>

				{/* Arena History */}
				<div className="space-y-3">
					<h3 className="text-xs font-semibold uppercase tracking-wider text-gray-500">
						Arena History
					</h3>
					<div className="flex items-center justify-between">
						<div>
							<p className="text-sm font-medium text-gray-300">
								Save Match History
							</p>
							<p className="text-gray-500 text-xs mt-0.5">
								Automatically save completed arena and compare sessions
							</p>
						</div>
						<Toggle
							checked={arenaHistoryEnabled}
							onChange={(v) => {
								const next = v;
								setArenaHistoryEnabled(next);
								toast(
									next
										? "Arena history enabled"
										: "Arena history disabled - existing entries preserved",
									next ? "success" : "info",
								);
							}}
						/>
					</div>

					<div>
						<label
							htmlFor="history-limit"
							className="block text-sm font-medium text-gray-300 mb-2"
						>
							Maximum Saved Matches
						</label>
						<select
							id="history-limit"
							value={arenaHistoryLimit}
							onChange={(e) => {
								const val = Number(e.target.value);
								setArenaHistoryLimit(val);
								toast(`History limit set to ${val} matches`, "success");
							}}
							className="ui-input disabled:opacity-50 disabled:cursor-not-allowed"
							disabled={!arenaHistoryEnabled}
						>
							<option value={10}>10 matches</option>
							<option value={25}>25 matches (default)</option>
							<option value={50}>50 matches</option>
							<option value={100}>100 matches</option>
						</select>
						<p className="text-gray-500 text-xs mt-1">
							Oldest matches are automatically removed when the limit is reached
						</p>
					</div>

					<div className="flex items-center justify-between">
						<div>
							<p className="text-sm font-medium text-gray-300">Clear History</p>
							<p className="text-gray-500 text-xs mt-0.5">
								{getArenaHistoryCount()} entr
								{getArenaHistoryCount() === 1 ? "y" : "ies"} stored
							</p>
						</div>
						<button
							type="button"
							onClick={() => {
								if (
									confirm("Delete all arena history? This cannot be undone.")
								) {
									clearArenaHistory();
									toast("All arena history cleared", "info");
								}
							}}
							className="ui-btn ui-btn-danger text-xs px-3 py-1.5"
							disabled={getArenaHistoryCount() === 0}
						>
							Clear All
						</button>
					</div>
				</div>

				{/* Cache & Resets */}
				<div className="space-y-3">
					<h3 className="text-xs font-semibold uppercase tracking-wider text-gray-500">
						Cache &amp; Resets
					</h3>
					<div className="flex items-center justify-between">
						<div>
							<p className="text-sm font-medium text-gray-300">
								Provider Quota Cache
							</p>
							<p className="text-gray-500 text-xs mt-0.5">
								{getProviderCacheCount()} cached entr
								{getProviderCacheCount() === 1 ? "y" : "ies"} (NanoGPT, Z.ai
								Coding Plan, DeepSeek)
							</p>
						</div>
						<button
							type="button"
							onClick={() => {
								if (
									confirm(
										"Clear all cached provider quota data? Fresh data will be fetched on next refresh.",
									)
								) {
									clearProviderCache();
									toast("Provider cache cleared", "info");
								}
							}}
							className="ui-btn ui-btn-danger text-xs px-3 py-1.5"
							disabled={getProviderCacheCount() === 0}
						>
							Clear Cache
						</button>
					</div>

					<div className="flex items-center justify-between">
						<div>
							<p className="text-sm font-medium text-gray-300">
								Dismissed Error Banners
							</p>
							<p className="text-gray-500 text-xs mt-0.5">
								Reset dismissed sidebar error pill states
							</p>
						</div>
						<button
							type="button"
							onClick={() => {
								localStorage.removeItem("dismissedAppErrorKey");
								localStorage.removeItem("dismissedReqErrorKey");
								window.dispatchEvent(new CustomEvent("dismissedErrorsReset"));
								toast("Dismissed error banners reset", "info");
							}}
							className="ui-btn ui-btn-danger text-xs px-3 py-1.5"
						>
							Reset
						</button>
					</div>
				</div>
			</div>
		</SettingsSection>
	);
}
