import { Target } from "lucide-react";
import { Spinner } from "../../components/Spinner";
import { RangeToggle } from "./ToggleGroup";
import type { Range } from "./types";

export function TokenSplitBar({
	prompt,
	completion,
	total,
	range,
	onRangeChange,
	loading,
}: {
	prompt: number;
	completion: number;
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
			<div className="flex rounded-lg overflow-hidden h-6">
				<div
					className="flex items-center justify-center text-[10px] font-semibold text-white tracking-wider overflow-hidden whitespace-nowrap shrink-0"
					style={{
						width: `${promptPct}%`,
						backgroundColor: "#818cf8",
					}}
				>
					{promptPct > 12 ? `${promptPct.toFixed(0)}%` : ""}
				</div>
				<div
					className="flex items-center justify-center text-[10px] font-semibold text-white tracking-wider overflow-hidden whitespace-nowrap shrink-0"
					style={{
						width: `${completionPct}%`,
						backgroundColor: "#059669",
					}}
				>
					{completionPct > 6 ? `${completionPct.toFixed(0)}%` : ""}
				</div>
			</div>
			<div className="flex justify-between mt-3 text-sm">
				<div className="flex items-center gap-1.5">
					<span
						className="w-2 h-2 rounded-full"
						style={{ backgroundColor: "#818cf8" }}
					/>
					<span className="text-(--text-tertiary)">Prompt</span>
					<span className="font-medium text-(--text-primary) ml-1">
						{prompt.toLocaleString()}
					</span>
				</div>
				<div className="flex items-center gap-1.5">
					<span
						className="w-2 h-2 rounded-full"
						style={{ backgroundColor: "#059669" }}
					/>
					<span className="text-(--text-tertiary)">Completion</span>
					<span className="font-medium text-(--text-primary) ml-1">
						{completion.toLocaleString()}
					</span>
				</div>
			</div>
		</div>
	);
}
