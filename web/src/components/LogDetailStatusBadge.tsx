import { Activity, AlertTriangle } from "lucide-react";

interface StatusBadgeProps {
	code: number;
	state: string;
	errorMessage?: string;
}

export function StatusBadge({ code, state, errorMessage }: StatusBadgeProps) {
	if (state === "pending" || state === "streaming") {
		return (
			<span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-blue-500/15 text-blue-400 border border-blue-500/30">
				<span className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-pulse" />
				{state === "streaming" ? "Streaming" : "Pending"}
			</span>
		);
	}

	if (code === 0) {
		return (
			<span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-red-500/15 text-red-400 border border-red-500/30">
				<AlertTriangle size={12} />
				Failed{errorMessage ? `: ${errorMessage}` : ""}
			</span>
		);
	}

	if (code >= 200 && code < 300) {
		return (
			<span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-green-500/15 text-green-400 border border-green-500/30">
				<Activity size={12} />
				{code} OK
			</span>
		);
	}

	if (code >= 400 && code < 500) {
		return (
			<span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-orange-500/15 text-orange-400 border border-orange-500/30">
				<AlertTriangle size={12} />
				{code} Client Error
			</span>
		);
	}

	if (code >= 500) {
		return (
			<span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-red-500/15 text-red-400 border border-red-500/30">
				<AlertTriangle size={12} />
				{code} Server Error
			</span>
		);
	}

	return <span className="text-xs text-(--text-secondary)">{code}</span>;
}
