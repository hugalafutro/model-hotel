import { Modal } from "../../components/Modal";
import { formatMs } from "./utils";

/* =========================================================
   Overhead modal
   ===================================================== */
export interface OverheadBreakdown {
	proxy_overhead_ms: number;
	parse_ms: number;
	model_lookup_ms: number;
	provider_lookup_ms: number;
	key_decrypt_ms: number;
	safe_dial_ms: number;
	settings_read_ms: number;
}

export function OverheadModal({
	breakdown,
	onClose,
}: {
	breakdown: OverheadBreakdown;
	onClose: () => void;
}) {
	const total =
		breakdown.parse_ms +
		breakdown.model_lookup_ms +
		breakdown.provider_lookup_ms +
		breakdown.key_decrypt_ms +
		breakdown.safe_dial_ms +
		breakdown.settings_read_ms;
	return (
		<Modal
			title="Proxy Overhead Breakdown"
			onClose={onClose}
			maxWidth="max-w-sm"
		>
			<div className="space-y-2">
				<div className="flex justify-between text-sm">
					<span className="text-gray-400">Request parsing</span>
					<span className="text-gray-200 font-mono">
						{formatMs(breakdown.parse_ms)}
					</span>
				</div>
				<div className="flex justify-between text-sm">
					<span className="text-gray-400">Model/failover lookup</span>
					<span className="text-gray-200 font-mono">
						{formatMs(breakdown.model_lookup_ms)}
					</span>
				</div>
				<div className="flex justify-between text-sm">
					<span className="text-gray-400">Provider lookup</span>
					<span className="text-gray-200 font-mono">
						{formatMs(breakdown.provider_lookup_ms)}
					</span>
				</div>
				<div className="flex justify-between text-sm">
					<span className="text-gray-400">Key decryption</span>
					<span className="text-gray-200 font-mono">
						{formatMs(breakdown.key_decrypt_ms)}
					</span>
				</div>
				<div className="flex justify-between text-sm">
					<span className="text-gray-400">DNS safety check</span>
					<span className="text-gray-200 font-mono">
						{formatMs(breakdown.safe_dial_ms)}
					</span>
				</div>
				<div className="flex justify-between text-sm">
					<span className="text-gray-400">Settings reads</span>
					<span className="text-gray-200 font-mono">
						{formatMs(breakdown.settings_read_ms)}
					</span>
				</div>
				<div className="border-t border-gray-700 my-2" />
				<div className="flex justify-between text-sm font-semibold">
					<span className="text-gray-300">Total overhead</span>
					<span className="text-(--accent) font-mono">{formatMs(total)}</span>
				</div>
			</div>
		</Modal>
	);
}
