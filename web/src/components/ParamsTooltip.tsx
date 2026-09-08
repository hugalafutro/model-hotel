import { Settings } from "@/lib/icons";
import type { GenerationParams } from "../api/types";

/**
 * The gear that names a reply's custom generation parameters, one per line in
 * its tooltip. Renders nothing when no parameter carries a value.
 */
export function ParamsTooltip({
	params,
	size = 10,
}: {
	params?: GenerationParams;
	size?: number;
}) {
	const entries = Object.entries(params ?? {}).filter(
		([, v]) => v !== undefined,
	);
	if (entries.length === 0) return null;
	const lines = entries
		.map(
			([k, v]) =>
				`${k.replace(/_/g, " ").replace(/^\w/, (c) => c.toUpperCase())}: ${v}`,
		)
		.join("\n");
	return (
		<span className="shrink-0 text-(--accent) cursor-help" title={lines}>
			<Settings size={size} />
		</span>
	);
}
