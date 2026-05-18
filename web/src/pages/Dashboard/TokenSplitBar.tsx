import { Target } from "lucide-react";
import { Spinner } from "../../components/Spinner";
import { RangeToggle } from "./ToggleGroup";
import { computeTileSegments } from "./tokenTileUtils";
import type { Range } from "./types";

const PROMPT_COLOR = "#818cf8";
const COMPLETION_COLOR = "#059669";

export function TokenSplitBar({
	prompt,
	completion,
	total,
	range,
	onRangeChange,
	loading,
}: {
	/** Prompt token count. */
	prompt: number;
	/** Completion token count. */
	completion: number;
	/** Total tokens displayed in the header. Should equal prompt + completion for consistent display. */
	total: number;
	range: Range;
	onRangeChange: (r: Range) => void;
	loading?: boolean;
}) {
	const totalPC = prompt + completion;
	if (totalPC === 0) {
		return (
			<div className="ui-card p-6">
				<div className="flex items-center justify-between mb-1">
					<h3 className="text-lg font-semibold text-(--text-primary) flex items-center gap-2">
						<Target size={18} className="text-(--accent)" />
						Token Mix
						{loading && <Spinner className="ml-1" />}
					</h3>
					<RangeToggle value={range} onChange={onRangeChange} />
				</div>
				<p className="text-sm text-(--text-muted) text-center py-12">
					No token data yet. Token mix will appear here once traffic flows.
				</p>
			</div>
		);
	}
	const promptPct = (prompt / totalPC) * 100;
	const completionPct = (completion / totalPC) * 100;
	const tiles = computeTileSegments(promptPct, completionPct);

	return (
		<div className="ui-card p-6">
			<div className="flex items-center justify-between mb-1">
				<h3 className="text-lg font-semibold text-(--text-primary) flex items-center gap-2">
					<Target size={18} className="text-(--accent)" />
					Token Mix
					{loading && <Spinner className="ml-1" />}
				</h3>
				<RangeToggle value={range} onChange={onRangeChange} />
			</div>
			<p
				className="text-2xl font-bold text-(--text-primary) mb-4"
				style={{ textTransform: "none" }}
			>
				{total.toLocaleString()}{" "}
				<span className="text-sm font-normal text-(--text-muted)">Tokens</span>
			</p>
			<div
				className="flex gap-0.5 h-6"
				role="img"
				aria-label={`Token mix: ${promptPct.toFixed(1)}% prompt, ${completionPct.toFixed(1)}% completion`}
			>
				{tiles.map((tile, i) => (
					<div
						// biome-ignore lint/suspicious/noArrayIndexKey: static tile grid, never reordered
						key={`${tile.type}-${i}`}
						className="flex-1 rounded-sm"
						data-tile-type={tile.type}
						style={{
							backgroundColor:
								tile.type === "prompt" ? PROMPT_COLOR : COMPLETION_COLOR,
							opacity: tile.opacity,
						}}
						title={
							tile.type === "prompt"
								? `Prompt: ${promptPct.toFixed(1)}% (${prompt.toLocaleString()} tokens)`
								: `Completion: ${completionPct.toFixed(1)}% (${completion.toLocaleString()} tokens)`
						}
					/>
				))}
			</div>
			<div className="flex justify-between mt-3 text-sm">
				<div className="flex items-center gap-1.5">
					<span
						className="w-2 h-2 rounded-full"
						style={{ backgroundColor: PROMPT_COLOR }}
					/>
					<span className="text-(--text-tertiary)">Prompt</span>
					<span className="font-medium text-(--text-primary) ml-1">
						{prompt.toLocaleString()}
					</span>
					<span className="text-(--text-muted) text-xs ml-1">
						{promptPct.toFixed(0)}%
					</span>
				</div>
				<div className="flex items-center gap-1.5">
					<span
						className="w-2 h-2 rounded-full"
						style={{ backgroundColor: COMPLETION_COLOR }}
					/>
					<span className="text-(--text-tertiary)">Completion</span>
					<span className="font-medium text-(--text-primary) ml-1">
						{completion.toLocaleString()}
					</span>
					<span className="text-(--text-muted) text-xs ml-1">
						{completionPct.toFixed(0)}%
					</span>
				</div>
			</div>
		</div>
	);
}
