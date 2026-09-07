import { RefreshCw } from "@/lib/icons";

/** What a status pill can be showing. */
export type DotState = "checking" | "ok" | "warn" | "error";

const DOT_CLASS: Record<Exclude<DotState, "checking">, string> = {
	ok: "bg-green-500",
	warn: "bg-amber-500",
	error: "bg-red-500",
};

/**
 * A small coloured dot with a label, or a spinner while the check that decides
 * the colour is still running.
 */
export function StatusDot({
	state,
	label,
}: {
	state: DotState;
	label: string;
}) {
	if (state === "checking") {
		return (
			<span className="inline-flex items-center gap-1.5 text-gray-400">
				<RefreshCw size={12} className="animate-spin" />
				{label}
			</span>
		);
	}
	return (
		<span className="inline-flex items-center gap-1.5 text-gray-300">
			<span
				className={`inline-block w-2 h-2 rounded-full ${DOT_CLASS[state]}`}
				aria-hidden="true"
			/>
			{label}
		</span>
	);
}
