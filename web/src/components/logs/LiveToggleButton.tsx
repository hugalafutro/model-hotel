import { useToast } from "../../context/ToastContext";

interface LiveToggleButtonProps {
	enabled: boolean;
	onToggle: (next: boolean) => void;
}

export function LiveToggleButton({ enabled, onToggle }: LiveToggleButtonProps) {
	const { toast } = useToast();

	return (
		<button
			type="button"
			onClick={() => {
				onToggle(!enabled);
				toast(enabled ? "Live updates paused" : "Live updates resumed", "info");
			}}
			className={`flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-semibold transition-colors ${
				enabled
					? "bg-green-500/20 text-green-400 hover:bg-green-500/30"
					: "bg-gray-700 text-gray-400 hover:bg-gray-600"
			}`}
		>
			<span
				className={`w-1.5 h-1.5 rounded-full transition-colors ${
					enabled ? "bg-green-400" : "bg-gray-500"
				}`}
			/>
			Live
		</button>
	);
}
