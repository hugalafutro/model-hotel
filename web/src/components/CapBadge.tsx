import { memo } from "react";
import type { ModelCapabilities } from "../api/types";
import { CAP_META, type CapKey, hasCap } from "./capMeta";

export const CapBadge = memo(function CapBadge({
	caps,
	capKey,
	variant = "active",
}: {
	caps: ModelCapabilities | null;
	capKey: CapKey;
	variant?: "active" | "muted" | "disabled";
}) {
	const meta = CAP_META.find((m) => m.key === capKey);
	if (!meta || !hasCap(caps, capKey)) return null;
	const style =
		variant === "muted"
			? meta.muted
			: variant === "disabled"
				? meta.disabled
				: meta.style;
	return (
		<span
			className={`ui-badge inline-flex items-center px-1.5 py-0.5 rounded text-[11px] font-medium border ${style}`}
		>
			{meta.label}
		</span>
	);
});
