import {
	Bot,
	ChevronDown,
	ChevronRight,
	Clock,
	Columns3,
	Filter,
	History,
	Swords,
	Trash2,
	Trophy,
} from "lucide-react";
import { useMemo, useState } from "react";
import { Modal } from "../components/Modal";
import { ARENA_PROMPTS, CHAT_PERSONAS } from "../data/presets";
import {
	type ArenaHistoryEntry,
	clearArenaHistory,
	deleteArenaHistoryEntry,
	getArenaHistory,
	getArenaHistoryCount,
	type HistoryBracketRound,
	type HistoryMatchup,
	type HistoryMode,
	type HistoryResponse,
} from "../utils/arenaHistory";
import { PaginationBar } from "./DataTable";

interface ArenaHistoryModalProps {
	onClose: () => void;
	onRestore?: (entry: ArenaHistoryEntry) => void;
}

// FilterMode mirrors the HistoryMode values plus "all"
type FilterMode = "all" | HistoryMode;

function roundLabel(roundIdx: number, totalRounds: number): string {
	if (totalRounds === 1) return "Match";
	if (roundIdx === totalRounds - 1) return "Final";
	if (roundIdx === totalRounds - 2) return "Semifinals";
	if (roundIdx === totalRounds - 3) return "Quarterfinals";
	return `Round ${roundIdx + 1}`;
}

function shortModelName(modelId: string): string {
	return modelId.split("/").pop() ?? modelId;
}

function formatDate(timestamp: number): string {
	return new Date(timestamp).toLocaleDateString(undefined, {
		month: "short",
		day: "numeric",
		year: "numeric",
	});
}

function formatTime(timestamp: number): string {
	return new Date(timestamp).toLocaleTimeString(undefined, {
		hour: "2-digit",
		minute: "2-digit",
	});
}

function truncate(str: string, maxLen: number): string {
	if (str.length <= maxLen) return str;
	return `${str.slice(0, maxLen)}…`;
}

function findPromptPreset(id: string | null) {
	if (!id) return null;
	return ARENA_PROMPTS.find((p) => p.id === id) ?? null;
}

function findPersonaPreset(id: string | null) {
	if (!id) return null;
	return CHAT_PERSONAS.find((p) => p.id === id) ?? null;
}

function getWinnerFromMatchup(mu: HistoryMatchup): string | null {
	if (mu.vote === "A" && mu.slotA) return mu.slotA.modelId;
	if (mu.vote === "B" && mu.slotB) return mu.slotB.modelId;
	return null;
}

export function ArenaHistoryModal({
	onClose,
	onRestore,
}: ArenaHistoryModalProps) {
	const [entries, setEntries] = useState<ArenaHistoryEntry[]>(() =>
		getArenaHistory(),
	);
	const [activeFilter, setActiveFilter] = useState<FilterMode>("all");
	const [expandedId, setExpandedId] = useState<string | null>(null);
	const [page, setPage] = useState(1);
	const [pageSize, setPageSize] = useState(10);
	const [confirmClear, setConfirmClear] = useState(false);

	const filteredEntries = useMemo(() => {
		if (activeFilter === "all") return entries;
		return entries.filter((e) => e.mode === activeFilter);
	}, [entries, activeFilter]);

	const totalPages = Math.max(1, Math.ceil(filteredEntries.length / pageSize));
	const safePage = Math.min(page, totalPages);

	const pagedEntries = useMemo(() => {
		const start = (safePage - 1) * pageSize;
		return filteredEntries.slice(start, start + pageSize);
	}, [filteredEntries, safePage, pageSize]);

	const handlePageChange = (p: number) => {
		setPage(p);
		setExpandedId(null);
	};

	const handlePageSizeChange = (s: number) => {
		setPageSize(s);
		setPage(1);
		setExpandedId(null);
	};

	const handleDelete = (id: string) => {
		deleteArenaHistoryEntry(id);
		setEntries(getArenaHistory());
		if (expandedId === id) setExpandedId(null);
	};

	const handleClearAll = () => {
		if (!confirmClear) {
			setConfirmClear(true);
			return;
		}
		clearArenaHistory();
		setEntries([]);
		setConfirmClear(false);
		setExpandedId(null);
	};

	const toggleExpand = (id: string) => {
		setExpandedId((prev) => (prev === id ? null : id));
	};

	const handleRestore = (entry: ArenaHistoryEntry) => {
		if (onRestore) {
			onRestore(entry);
			onClose();
		}
	};

	const filterButtons: {
		label: string;
		value: FilterMode;
		icon: typeof Swords;
	}[] = [
		{ label: "All", value: "all", icon: Filter },
		{ label: "Competition", value: "competition", icon: Swords },
		{ label: "Compare", value: "compare", icon: Columns3 },
	];

	const renderCompetitionDetail = (entry: ArenaHistoryEntry) => {
		if (!entry.rounds || entry.rounds.length === 0) return null;

		return (
			<div className="mt-3 space-y-3">
				{entry.rounds.map((round: HistoryBracketRound, rIdx: number) => (
					// biome-ignore lint/suspicious/noArrayIndexKey: tournament rounds have no stable unique id
					<div key={`${entry.id}-round-${rIdx}`}>
						<h5 className="text-xs font-semibold text-(--text-secondary) uppercase tracking-wider mb-1.5">
							{roundLabel(rIdx, entry.rounds?.length ?? 0)}
						</h5>
						<div className="space-y-1.5">
							{round.matchups.map((mu: HistoryMatchup) => {
								const winnerModelId = getWinnerFromMatchup(mu);
								const aName = mu.slotA ? shortModelName(mu.slotA.modelId) : "?";
								const bName = mu.slotB ? shortModelName(mu.slotB.modelId) : "?";
								const aIsWinner =
									winnerModelId &&
									mu.slotA &&
									winnerModelId === mu.slotA.modelId;
								const bIsWinner =
									winnerModelId &&
									mu.slotB &&
									winnerModelId === mu.slotB.modelId;

								return (
									<div
										// biome-ignore lint/suspicious/noArrayIndexKey: rIdx needed to namespace matchups across rounds
										key={`${rIdx}-${mu.slotA?.modelId ?? "na"}-vs-${mu.slotB?.modelId ?? "na"}`}
										className="flex items-center gap-2 text-xs px-2 py-1 rounded bg-(--surface) border border-(--border-subtle)"
									>
										<span
											className={`flex-1 truncate ${
												aIsWinner
													? "text-(--accent) font-semibold"
													: "text-(--text-secondary)"
											}`}
										>
											{aIsWinner && (
												<Trophy size={10} className="inline mr-1 -mt-0.5" />
											)}
											{aName}
										</span>
										<span className="text-(--text-tertiary)">vs</span>
										<span
											className={`flex-1 truncate text-right ${
												bIsWinner
													? "text-(--accent) font-semibold"
													: "text-(--text-secondary)"
											}`}
										>
											{bName}
											{bIsWinner && (
												<Trophy size={10} className="inline ml-1 -mt-0.5" />
											)}
										</span>
										<span className="text-(--text-tertiary) ml-1 min-w-5 text-center">
											{mu.vote ? `→ ${mu.vote}` : "-"}
										</span>
									</div>
								);
							})}
						</div>
					</div>
				))}

				{entry.winner && (
					<div className="flex items-center gap-2 mt-2 px-2 py-1.5 rounded bg-(--accent)/10 border border-(--accent)/30">
						<Trophy size={14} className="text-(--accent)" />
						<span className="text-xs font-semibold text-(--accent)">
							Winner: {shortModelName(entry.winner)}
						</span>
					</div>
				)}
			</div>
		);
	};

	const renderCompareDetail = (entry: ArenaHistoryEntry) => {
		const models = entry.compareModels ?? [];
		const responses = entry.compareResponses ?? [];

		if (models.length === 0 && responses.length === 0) return null;

		return (
			<div className="mt-3 space-y-2">
				<span className="text-xs text-(--text-tertiary)">
					{responses.length} response
					{responses.length !== 1 ? "s" : ""}
				</span>
				{responses.map((resp: HistoryResponse) => {
					const modelName = shortModelName(resp.modelId);
					return (
						<div
							key={resp.modelId}
							className="px-2 py-1.5 rounded bg-(--surface) border border-(--border-subtle)"
						>
							<div className="flex items-center gap-2 mb-1">
								<Bot size={12} className="text-(--text-tertiary)" />
								<span className="text-xs font-medium text-(--text-secondary)">
									{modelName}
								</span>
								{resp.metrics && (
									<span className="text-[10px] text-(--text-tertiary) ml-auto">
										{resp.metrics.durationMs > 0
											? `${(resp.metrics.durationMs / 1000).toFixed(1)}s`
											: ""}
										{resp.metrics.tokensPerSecond !== null &&
											resp.metrics.tokensPerSecond > 0 &&
											` · ${resp.metrics.tokensPerSecond.toFixed(0)} tok/s`}
									</span>
								)}
							</div>
							{resp.error ? (
								<p className="text-xs text-red-400 truncate">
									Error: {resp.error}
								</p>
							) : (
								<p className="text-xs text-(--text-tertiary) whitespace-pre-wrap wrap-break-word">
									{truncate(resp.content, 200)}
								</p>
							)}
						</div>
					);
				})}
			</div>
		);
	};

	const renderEntryDetail = (entry: ArenaHistoryEntry) => {
		const preset = findPromptPreset(entry.promptPresetId);
		const persona = findPersonaPreset(entry.comparePersonaId);

		return (
			<div>
				{/* Preset & Persona badges */}
				<div className="flex items-center gap-2 mt-3 flex-wrap">
					{preset ? (
						<span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-medium bg-(--accent)/10 text-(--accent) border border-(--accent)/20">
							<span>{preset.icon}</span>
							{preset.label}
						</span>
					) : (
						<span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-medium bg-(--surface) text-(--text-tertiary) border border-(--border-subtle)">
							Custom prompt
						</span>
					)}
					{persona && (
						<span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-medium bg-(--accent)/10 text-(--accent) border border-(--accent)/20">
							<span>{persona.icon}</span>
							{persona.label}
						</span>
					)}
				</div>

				{/* Mode-specific content */}
				{entry.mode === "competition"
					? renderCompetitionDetail(entry)
					: renderCompareDetail(entry)}

				{/* Action row */}
				<div className="flex items-center gap-2 mt-3 pt-2 border-t border-(--border-subtle)">
					{onRestore && (
						<button
							type="button"
							onClick={() => handleRestore(entry)}
							className="ui-btn ui-btn-secondary text-xs px-2 py-1"
						>
							Restore Setup
						</button>
					)}
					<button
						type="button"
						onClick={() => handleDelete(entry.id)}
						className="ui-btn ui-btn-danger text-xs px-2 py-1 ml-auto flex items-center gap-1"
					>
						<Trash2 size={12} />
						Delete
					</button>
				</div>
			</div>
		);
	};

	const header = (
		<div className="flex items-center gap-3 mb-4">
			<h2 className="text-xl font-bold text-white flex items-center gap-2">
				<History size={20} className="text-(--accent)" />
				Match History
			</h2>
			<div className="flex items-center gap-1 ml-2">
				{filterButtons.map(({ label, value, icon: Icon }) => (
					<button
						key={value}
						type="button"
						onClick={() => {
							setActiveFilter(value);
							setPage(1);
							setExpandedId(null);
						}}
						className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
							activeFilter === value
								? "bg-(--accent)/20 text-(--accent) border border-(--accent)/40 cursor-default"
								: "text-(--text-tertiary) hover:text-(--text-secondary) border border-transparent cursor-pointer"
						}`}
					>
						<Icon size={12} className="inline mr-1 -mt-0.5" />
						{label}
					</button>
				))}
			</div>
		</div>
	);

	return (
		<Modal
			header={header}
			onClose={onClose}
			maxWidth="max-w-4xl"
			scrollable={true}
		>
			{/* History list */}
			{filteredEntries.length === 0 ? (
				<div className="flex flex-col items-center justify-center py-12">
					<History size={48} className="text-(--text-tertiary) mx-auto mb-4" />
					<p className="text-(--text-tertiary)">No match history yet</p>
					<p className="text-(--text-tertiary) text-xs mt-1">
						Completed arena and compare sessions will appear here
					</p>
				</div>
			) : (
				<div className="space-y-2">
					{pagedEntries.map((entry) => {
						const isExpanded = expandedId === entry.id;
						const preset = findPromptPreset(entry.promptPresetId);

						return (
							<div key={entry.id} className="ui-card p-4">
								{/* Collapsed summary row */}
								<button
									type="button"
									onClick={() => toggleExpand(entry.id)}
									className="w-full flex items-center gap-3 text-left cursor-pointer"
								>
									{/* Left: mode badge + timestamp */}
									<div className="flex items-center gap-2 shrink-0">
										{entry.mode === "competition" ? (
											<Swords size={14} className="text-(--accent)" />
										) : (
											<Columns3 size={14} className="text-(--accent)" />
										)}
										<div className="flex flex-col">
											<span className="text-[10px] text-(--text-tertiary) leading-tight">
												{formatDate(entry.timestamp)}
											</span>
											<span className="text-[10px] text-(--text-tertiary) leading-tight flex items-center gap-0.5">
												<Clock size={8} />
												{formatTime(entry.timestamp)}
											</span>
										</div>
									</div>

									{/* Middle: model names + preset */}
									<div className="flex-1 min-w-0 flex flex-col gap-0.5">
										<span className="text-sm text-(--text-primary) truncate">
											{entry.mode === "competition"
												? entry.rounds &&
													entry.rounds.length > 0 &&
													entry.rounds[0].matchups.length > 0
													? entry.rounds[0].matchups
															.map((mu) => {
																const names: string[] = [];
																if (mu.slotA)
																	names.push(shortModelName(mu.slotA.modelId));
																if (mu.slotB)
																	names.push(shortModelName(mu.slotB.modelId));
																return names.join(" vs ");
															})
															.join(" · ")
													: "Bracket"
												: entry.compareModels
													? entry.compareModels.map(shortModelName).join(", ")
													: "Compare"}
										</span>
										<div className="flex items-center gap-1">
											{preset && (
												<span className="text-[10px] text-(--text-tertiary)">
													{preset.icon} {preset.label}
												</span>
											)}
										</div>
									</div>

									{/* Right: winner / response count + expand chevron */}
									<div className="flex items-center gap-2 shrink-0">
										{entry.mode === "competition" && entry.winner && (
											<span className="flex items-center gap-1 text-xs text-(--accent)">
												<Trophy size={12} />
												{shortModelName(entry.winner)}
											</span>
										)}
										{entry.mode === "compare" && entry.compareResponses && (
											<span className="text-xs text-(--text-tertiary)">
												{entry.compareResponses.length} resp.
											</span>
										)}
										{isExpanded ? (
											<ChevronDown
												size={14}
												className="text-(--text-tertiary)"
											/>
										) : (
											<ChevronRight
												size={14}
												className="text-(--text-tertiary)"
											/>
										)}
									</div>
								</button>

								{/* Expanded detail */}
								<div
									className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${isExpanded ? "grid-rows-[1fr]" : "grid-rows-[0fr]"}`}
								>
									<div className="overflow-hidden">
										{renderEntryDetail(entry)}
									</div>
								</div>
							</div>
						);
					})}
				</div>
			)}

			{/* Pagination */}
			{filteredEntries.length > 0 && (
				<div className="mt-4">
					<PaginationBar
						page={safePage}
						totalPages={totalPages}
						totalItems={filteredEntries.length}
						pageSize={pageSize}
						onPageChange={handlePageChange}
						onPageSizeChange={handlePageSizeChange}
						label="entries"
					/>
				</div>
			)}

			{/* Footer */}
			<div className="mt-4 pt-3 border-t border-(--border-subtle) flex items-center justify-between">
				<button
					type="button"
					onClick={handleClearAll}
					onBlur={() => setConfirmClear(false)}
					className={`ui-btn ui-btn-danger text-xs flex items-center gap-1 ${
						confirmClear ? "ring-2 ring-red-400/50" : ""
					}`}
				>
					<Trash2 size={12} />
					{confirmClear ? "Click again to confirm" : "Clear All History"}
				</button>
				<span className="text-xs text-(--text-tertiary)">
					{getArenaHistoryCount()} entr
					{entries.length === 1 ? "y" : "ies"}
				</span>
			</div>
		</Modal>
	);
}
